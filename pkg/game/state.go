package game

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"

	"github.com/libp2p/go-libp2p/core/peer"
)

// GamePhase represents the current stage of a poker hand.
type GamePhase int

const (
	Idle GamePhase = iota
	Shuffling
	WaitingForPlayers
	PreFlop
	Flop
	Turn
	River
	Showdown
)

// Player represents a player in the game, including their stake and current hand state.
type Player struct {
	ID         peer.ID `json:"id"`
	Name       string  `json:"name"`
	Stack      int     `json:"stack"`
	Hand       []Card  `json:"-"`          // Hand is private and not sent over the network.
	InHand     bool    `json:"in_hand"`    // Is the player still in the current hand?
	Bet        int     `json:"bet"`        // The player's current bet in the round.
	HasActed   bool    `json:"has_acted"`  // Has the player acted in the current round?
	Commitment string  `json:"commitment"` // The public commitment (hash) of the player's hand.
	Salt       string  `json:"-"`          // The private salt for the hand commitment.
}

// GameState holds all the shared information about the current state of the poker game.
type GameState struct {
	mu             sync.RWMutex        `json:"-"` // Protects concurrent access to Players map and game state
	Players        map[peer.ID]*Player `json:"players"`
	HostID         peer.ID             `json:"host_id"`
	Deck           Deck                `json:"-"` // Deck is not sent over network.
	EncryptedDeck  EncryptedDeck       `json:"encrypted_deck"`
	Pot            int                 `json:"pot"`
	CommunityCards []Card              `json:"community_cards"`
	Phase          GamePhase           `json:"phase"`
	CurrentTurn    peer.ID             `json:"current_turn"`
	CurrentBet     int                 `json:"current_bet"` // The highest bet amount for the current round
	LastRaiser     peer.ID             `json:"last_raiser"` // The peer who made the last aggressive action (bet or raise)
	Dealer         peer.ID             `json:"dealer"`

	// Blind positions and amounts
	SmallBlind       peer.ID `json:"small_blind"`        // Player in small blind position
	BigBlind         peer.ID `json:"big_blind"`          // Player in big blind position
	SmallBlindAmount int     `json:"small_blind_amount"` // Small blind amount (default: 2)
	BigBlindAmount   int     `json:"big_blind_amount"`   // Big blind amount (default: 4)

	// Community card indices in EncryptedDeck
	FlopIndices  []int `json:"flop_indices"`  // Indices of flop cards (3 cards)
	TurnIndex    int   `json:"turn_index"`    // Index of turn card
	RiverIndex   int   `json:"river_index"`   // Index of river card
}

// NewGameState creates and initializes a new GameState object.
func NewGameState() *GameState {
	return &GameState{
		Players:          make(map[peer.ID]*Player),
		CommunityCards:   make([]Card, 0),
		Phase:            Idle,
		SmallBlindAmount: 2, // Default small blind
		BigBlindAmount:   4, // Default big blind
	}
}

// GetPlayerOrder returns a sorted slice of player IDs for deterministic turn order.
func (gs *GameState) GetPlayerOrder() []peer.ID {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	ids := make([]peer.ID, 0, len(gs.Players))
	for id := range gs.Players {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		return ids[i].String() < ids[j].String()
	})
	return ids
}

// NextPlayerInOrder finds the next player who is still in the hand.
func (gs *GameState) NextPlayerInOrder(playerIDs []peer.ID, currentIndex int) peer.ID {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	numPlayers := len(playerIDs)
	for i := 1; i <= numPlayers; i++ {
		nextIndex := (currentIndex + i) % numPlayers
		nextPlayerID := playerIDs[nextIndex]
		if gs.Players[nextPlayerID].InHand {
			return nextPlayerID
		}
	}
	return "" // Should not happen in a valid game
}

// AdvanceTurn moves the turn to the next active player.
func (gs *GameState) AdvanceTurn() {
	playerOrder := gs.GetPlayerOrder()
	currentIndex := -1
	for i, id := range playerOrder {
		if id == gs.CurrentTurn {
			currentIndex = i
			break
		}
	}

	if currentIndex != -1 {
		gs.CurrentTurn = gs.NextPlayerInOrder(playerOrder, currentIndex)
	}
}

// GenerateCommitment creates a hash from a hand of cards and a salt.
func GenerateCommitment(hand []Card, salt string) string {
	handStr := fmt.Sprintf("%v", hand)
	data := []byte(handStr + salt)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// AddPlayer adds a new player to the game state.
func (gs *GameState) AddPlayer(player *Player) {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	gs.Players[player.ID] = player
}

// SetupBlinds assigns small blind and big blind positions based on dealer
func (gs *GameState) SetupBlinds() {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	playerOrder := gs.getPlayerOrderUnsafe()
	if len(playerOrder) < 2 {
		return // Need at least 2 players
	}

	// Find dealer index
	dealerIdx := -1
	for i, id := range playerOrder {
		if id == gs.Dealer {
			dealerIdx = i
			break
		}
	}

	// If no dealer set, use first player
	if dealerIdx == -1 {
		dealerIdx = 0
		gs.Dealer = playerOrder[0]
	}

	// Small blind is left of dealer
	gs.SmallBlind = playerOrder[(dealerIdx+1)%len(playerOrder)]
	// Big blind is left of small blind
	gs.BigBlind = playerOrder[(dealerIdx+2)%len(playerOrder)]
}

// PostBlinds deducts blind amounts from players and adds to pot
func (gs *GameState) PostBlinds() error {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	sbPlayer := gs.Players[gs.SmallBlind]
	bbPlayer := gs.Players[gs.BigBlind]

	if sbPlayer == nil || bbPlayer == nil {
		return fmt.Errorf("blind players not found")
	}

	// Post small blind
	sbAmount := gs.SmallBlindAmount
	if sbAmount > sbPlayer.Stack {
		sbAmount = sbPlayer.Stack // All-in if not enough chips
	}
	sbPlayer.Stack -= sbAmount
	sbPlayer.Bet = sbAmount
	gs.Pot += sbAmount

	// Post big blind
	bbAmount := gs.BigBlindAmount
	if bbAmount > bbPlayer.Stack {
		bbAmount = bbPlayer.Stack // All-in if not enough chips
	}
	bbPlayer.Stack -= bbAmount
	bbPlayer.Bet = bbAmount
	gs.Pot += bbAmount

	// Set current bet to big blind amount
	gs.CurrentBet = bbAmount

	// First to act is left of big blind
	playerOrder := gs.getPlayerOrderUnsafe()
	bbIdx := -1
	for i, id := range playerOrder {
		if id == gs.BigBlind {
			bbIdx = i
			break
		}
	}
	if bbIdx != -1 {
		gs.CurrentTurn = playerOrder[(bbIdx+1)%len(playerOrder)]
	}

	return nil
}

// getPlayerOrderUnsafe is an internal helper (no lock - must be called with lock held)
func (gs *GameState) getPlayerOrderUnsafe() []peer.ID {
	ids := make([]peer.ID, 0, len(gs.Players))
	for id := range gs.Players {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		return ids[i].String() < ids[j].String()
	})
	return ids
}

// ApplyAction applies a player's action to the game state.
func (gs *GameState) ApplyAction(playerID peer.ID, action string, amount int) error {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// 0. CRITICAL SECURITY: Prevent betting before hand commitments
	if gs.Phase == PreFlop && action != "fold" {
		// Verify player has committed their hand
		player := gs.Players[playerID]
		if player == nil {
			return fmt.Errorf("player not found")
		}
		if player.Commitment == "" {
			return fmt.Errorf("❌ ANTI-CHEAT: must commit hand before betting")
		}
	}

	// 1. Validate it's the player's turn
	if gs.CurrentTurn != playerID {
		return fmt.Errorf("it is not %s's turn", playerID)
	}

	player := gs.Players[playerID]
	if player == nil {
		return fmt.Errorf("player not found")
	}

	// 2. BET VALIDATION: Prevent invalid bets
	if action == "bet" || action == "raise" {
		// Check for negative or zero bets
		if amount <= 0 {
			return fmt.Errorf("bet amount must be positive, got %d", amount)
		}
		// Check if player has enough chips
		diff := amount - player.Bet
		if diff > player.Stack {
			return fmt.Errorf("insufficient funds: need %d but only have %d", diff, player.Stack)
		}
		// Check minimum raise size
		if amount < gs.CurrentBet {
			return fmt.Errorf("bet amount %d is less than current bet %d", amount, gs.CurrentBet)
		}
	}

	// 3. Process Action
	switch action {
	case "fold":
		player.InHand = false
		fmt.Printf("Player %s folded\n", player.Name)

	case "check":
		if gs.CurrentBet > player.Bet {
			return fmt.Errorf("cannot check, must call %d", gs.CurrentBet-player.Bet)
		}
		// Check is valid if current bet is matched

	case "call":
		toCall := gs.CurrentBet - player.Bet
		if toCall > player.Stack {
			toCall = player.Stack // All-in
		}
		player.Stack -= toCall
		player.Bet += toCall
		gs.Pot += toCall

	case "bet", "raise":
		// Validation already done above, just process the bet
		diff := amount - player.Bet
		player.Stack -= diff
		player.Bet += diff // Total bet in this round
		gs.Pot += diff
		gs.CurrentBet = amount
		gs.LastRaiser = playerID

	case "reveal":
		if gs.Phase != Showdown {
			return fmt.Errorf("cannot reveal cards outside of showdown")
		}
		// Logic for reveal is handled by the caller (validating commitment)
		// or we can pass the hand/salt here?
		// For ApplyAction signature (string, int), we can't pass the hand.
		// So we might need a separate method for Reveal, or just handle the state update here?
		// If the caller validated the hand, they update player.Hand directly.
		// Let's leave Reveal out of ApplyAction for now, or just mark the player as "revealed"?
		// The main.go logic does validation.
		return nil // No state change in ApplyAction for reveal, handled separately

	default:
		return fmt.Errorf("unknown action: %s", action)
	}

	// 3. Mark player as having acted
	player.HasActed = true

	// 4. Advance Turn
	gs.AdvanceTurn()

	// 5. Check if round is complete
	if gs.isRoundComplete() {
		gs.nextPhase()
	}

	return nil
}

// isRoundComplete checks if the betting round is over.
func (gs *GameState) isRoundComplete() bool {
	// Round is complete if:
	// 1. All active players have acted (we need to track this, simplified here)
	// 2. All active players have matched the current bet (or are all-in)

	// Simplified check:
	// If the next player to act is the LastRaiser (or everyone checked to BB), we are done.
	// But AdvanceTurn moves CurrentTurn.

	// Better logic:
	// If CurrentTurn == LastRaiser (and LastRaiser is not empty), round is done?
	// No, because LastRaiser acts, then everyone calls. When it gets BACK to LastRaiser, they don't act again.
	// So if CurrentTurn would be LastRaiser, we stop?

	// Let's use a simpler heuristic for this prototype:
	// If everyone has acted at least once (needs tracking) and bets are equal.

	// For now, let's just check if bets are equal for all active players AND everyone has acted.
	for _, p := range gs.Players {
		if p.InHand && p.Stack > 0 {
			if !p.HasActed {
				return false
			}
			if p.Bet != gs.CurrentBet {
				return false
			}
		}
	}

	return true
}

// nextPhase transitions the game to the next phase.
func (gs *GameState) nextPhase() {
	// Reset bets for new round
	for _, p := range gs.Players {
		p.Bet = 0
		p.HasActed = false
	}
	gs.CurrentBet = 0
	gs.LastRaiser = "" // Reset raiser

	switch gs.Phase {
	case PreFlop:
		gs.Phase = Flop
		// Deal 3 community cards (logic handled by host/dealer in real game)
		// Here we just change phase.
	case Flop:
		gs.Phase = Turn
	case Turn:
		gs.Phase = River
	case River:
		gs.Phase = Showdown
	}

	// Reset turn order (usually starts left of dealer)
	// gs.CurrentTurn = ...
}
