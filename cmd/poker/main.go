package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/multiformats/go-multihash"

	"github.com/vatsalnarula123/poker-new/pkg/crypto"
	"github.com/vatsalnarula123/poker-new/pkg/game"
)

const (
	PokerProtocol     = "/poker/1.0.0"
	RoomInfoProtocol  = "/poker-room-info/1.0.0"
	LobbyName         = "poker-lobby-v1"
	MaxPlayersPerRoom = 8
	MaxMessageSize    = 64 * 1024 // 64KB max message size
	MaxPeers          = 100       // Maximum peer connections

	// Mental Poker Message Types
	MsgJoin               = "join"
	MsgShuffleInit        = "shuffle_init"
	MsgShuffleRound       = "shuffle_round"
	MsgCardAssign         = "card_assign"
	MsgKeyRequest         = "key_request"
	MsgKeyResponse        = "key_response"
	MsgCommunityReveal    = "community_reveal"
	MsgHandCommitment     = "hand_commitment"
	MsgHandReveal         = "hand_reveal"
	MsgWinnerAnnouncement = "winner_announcement"
	MsgGameStateUpdate    = "game_state_update"
	MsgChat               = "chat"
)

// Helper function to create CID from string
func createCID(s string) (cid.Cid, error) {
	hash := sha256.Sum256([]byte(s))
	mh, err := multihash.EncodeName(hash[:], "sha2-256")
	if err != nil {
		return cid.Cid{}, err
	}
	return cid.NewCidV1(cid.Raw, mh), nil
}

// GameMessage represents a message sent between poker players
type GameMessage struct {
	Type     string      `json:"type"`
	PlayerID string      `json:"player_id"`
	Data     interface{} `json:"data"`
}

// BetAction represents a betting action
type BetAction struct {
	Action string `json:"action"` // "bet", "call", "fold", "raise", "check"
	Amount int    `json:"amount"`
}

// Room represents a public poker table
type Room struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	HostID     peer.ID `json:"host_id"`
	Players    int     `json:"players"`
	MaxPlayers int     `json:"max_players"`
}

// PokerClient represents a poker player client
type PokerClient struct {
	host            host.Host
	dht             *dht.IpfsDHT
	ctx             context.Context
	cancel          context.CancelFunc
	peers           map[peer.ID]network.Stream
	peersMu         sync.RWMutex
	playerName      string
	gameState       *game.GameState
	shuffleState    *game.ShuffleState
	cardLockManager *game.CardLockManager
	keyProvider     *game.KeyProvider
	myCipher        *crypto.Cipher
	currentRoom     *Room
	discoveredRooms []*Room
	roomMu          sync.RWMutex   // Protects currentRoom and discoveredRooms
	isAdvertising   int32          // Use atomic for thread-safe access
	wg              sync.WaitGroup // Track goroutines for clean shutdown

	// Synchronization for game readiness
	playerReadyMap  map[peer.ID]bool
	readyMu         sync.Mutex
	allPlayersReady bool
}

// NewPokerClient creates a new poker client
func NewPokerClient(bootstrapAddr string) (*PokerClient, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Create libp2p host
	h, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"),
		libp2p.EnableNATService(),
		libp2p.EnableRelay(),
	)
	if err != nil {
		cancel()
		return nil, err
	}

	// Create DHT
	dht, err := dht.New(ctx, h)
	if err != nil {
		h.Close()
		cancel()
		return nil, err
	}

	client := &PokerClient{
		host:            h,
		dht:             dht,
		ctx:             ctx,
		cancel:          cancel,
		peers:           make(map[peer.ID]network.Stream),
		playerName:      fmt.Sprintf("Player_%s", h.ID().String()[:8]),
		gameState:       game.NewGameState(),
		discoveredRooms: make([]*Room, 0),
	}

	// Set stream handler for incoming connections
	h.SetStreamHandler(PokerProtocol, client.handleStream)
	h.SetStreamHandler(RoomInfoProtocol, client.handleRoomInfoRequest)

	// Initialize mental poker components
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		h.Close()
		cancel()
		return nil, fmt.Errorf("failed to generate random secret: %v", err)
	}
	client.myCipher = crypto.NewCipher(string(secret))
	client.keyProvider = game.NewKeyProvider(client.myCipher)
	client.playerReadyMap = make(map[peer.ID]bool)

	// Connect to bootstrap node
	if bootstrapAddr != "" {
		if err := client.connectToBootstrap(bootstrapAddr); err != nil {
			log.Printf("Failed to connect to bootstrap: %v", err)
		}
	}

	return client, nil
}

// connectToBootstrap connects to the bootstrap node
func (p *PokerClient) connectToBootstrap(bootstrapAddr string) error {
	maddr, err := multiaddr.NewMultiaddr(bootstrapAddr)
	if err != nil {
		return err
	}

	info, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		return err
	}

	if err := p.host.Connect(p.ctx, *info); err != nil {
		return err
	}

	// Bootstrap the DHT
	if err := p.dht.Bootstrap(p.ctx); err != nil {
		return err
	}

	return nil
}

// handleStream handles incoming streams from other players
func (p *PokerClient) handleStream(s network.Stream) {
	remotePeer := s.Conn().RemotePeer()
	log.Printf("New connection from %s", remotePeer.String())

	// Check peer limit
	p.peersMu.RLock()
	peerCount := len(p.peers)
	p.peersMu.RUnlock()

	if peerCount >= MaxPeers {
		log.Printf("Max peers reached, rejecting connection from %s", remotePeer.String())
		s.Close()
		return
	}

	// Store the stream for this peer
	p.peersMu.Lock()
	p.peers[remotePeer] = s
	p.peersMu.Unlock()

	// Read messages from the stream with buffer limits
	scanner := bufio.NewScanner(s)
	scanner.Buffer(make([]byte, 4096), MaxMessageSize)

	for scanner.Scan() {
		var msg GameMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			log.Printf("Error unmarshaling message: %v", err)
			continue
		}

		p.handleGameMessage(msg, remotePeer)
	}

	// Check for scanner errors
	if err := scanner.Err(); err != nil {
		log.Printf("Stream error from %s: %v", remotePeer.String(), err)
	}

	// Clean up when connection closes
	p.peersMu.Lock()
	delete(p.peers, remotePeer)
	p.peersMu.Unlock()
	log.Printf("Connection closed from %s", remotePeer.String())
}

// handleGameMessage processes incoming game messages
func (p *PokerClient) handleGameMessage(msg GameMessage, from peer.ID) {
	switch msg.Type {
	case MsgJoin:
		fmt.Printf("🎮 %s joined the game!\n", msg.PlayerID)
		p.gameState.AddPlayer(&game.Player{
			ID:    from,
			Name:  msg.PlayerID,
			Stack: 1000,
			Bet:   0,
		})

	case MsgShuffleInit:
		p.handleShuffleInit(msg, from)

	case MsgShuffleRound:
		p.handleShuffleRound(msg, from)

	case MsgCardAssign:
		p.handleCardAssign(msg, from)

	case MsgKeyRequest:
		p.handleKeyRequest(msg, from)

	case MsgKeyResponse:
		p.handleKeyResponse(msg, from)

	case "final_deck":
		p.handleFinalDeck(msg, from)

	case MsgCommunityReveal:
		p.handleCommunityReveal(msg, from)

	case MsgHandCommitment:
		p.handleHandCommitment(msg, from)

	case MsgHandReveal:
		p.handleHandReveal(msg, from)

	case MsgWinnerAnnouncement:
		p.handleWinnerAnnouncement(msg, from)

	case MsgGameStateUpdate:
		// Handle full game state update (including blinds)
		p.handleStateUpdate(msg, from)

	case "bet":
		data, ok := msg.Data.(map[string]interface{})
		if !ok {
			log.Printf("Invalid bet data format from %s", msg.PlayerID)
			return
		}

		action, ok := data["action"].(string)
		if !ok {
			log.Printf("Invalid action type from %s", msg.PlayerID)
			return
		}

		// Validate action type
		validActions := map[string]bool{
			"bet": true, "call": true, "fold": true,
			"raise": true, "check": true,
		}
		if !validActions[action] {
			log.Printf("Invalid action from %s: %s", msg.PlayerID, action)
			return
		}

		amountFloat, ok := data["amount"].(float64)
		if !ok {
			log.Printf("Invalid amount type from %s", msg.PlayerID)
			return
		}

		// Validate amount range
		if amountFloat < 0 || amountFloat > float64(math.MaxInt32) {
			log.Printf("Amount out of safe range from %s: %f", msg.PlayerID, amountFloat)
			return
		}
		amount := int(amountFloat)

		// Validate amount bounds for game
		if amount < 0 || amount > 1000000 {
			log.Printf("Amount out of game bounds from %s: %d", msg.PlayerID, amount)
			return
		}

		fmt.Printf("💰 %s performed action: %s (amount: %d)\n", msg.PlayerID, action, amount)

		if err := p.gameState.ApplyAction(from, action, amount); err != nil {
			log.Printf("Error applying action: %v", err)
		} else {
			fmt.Printf("State updated. Pot: %d, Current Bet: %d\n", p.gameState.Pot, p.gameState.CurrentBet)
		}

	case "chat":
		if chatMsg, ok := msg.Data.(string); ok {
			fmt.Printf("💬 %s: %s\n", msg.PlayerID, chatMsg)
		}
	case "game_state":
		fmt.Printf("🎲 Game state update from %s\n", msg.PlayerID)
	default:
		fmt.Printf("📨 Unknown message type from %s: %s\n", msg.PlayerID, msg.Type)
	}
}

// handleShuffleInit initializes the shuffle state and shuffles the deck
func (p *PokerClient) handleShuffleInit(msg GameMessage, from peer.ID) {
	fmt.Printf("🎲 Initializing shuffle...\n")
	p.shuffleState = game.NewShuffleState(p.host.ID(), p.gameState.GetPlayerOrder(), "some_secret") // We'll improve the secret
	// Start encryption round
}

// handleShuffleRound processes a shuffle/encryption round
func (p *PokerClient) handleShuffleRound(msg GameMessage, from peer.ID) {
	fmt.Printf("🔄 Processing shuffle round from %s...\n", msg.PlayerID)
	// Unmarshal deck from msg.Data
	deckData, err := json.Marshal(msg.Data)
	if err != nil {
		log.Printf("Error marshaling deck data: %v", err)
		return
	}
	var encryptedDeck game.EncryptedDeck
	if err := json.Unmarshal(deckData, &encryptedDeck); err != nil {
		log.Printf("Error unmarshaling deck: %v", err)
		return
	}

	// Handle shuffle message
	newDeck, _, err := p.shuffleState.HandleShuffleMessage(encryptedDeck)
	if err != nil {
		log.Printf("Error handling shuffle message: %v", err)
		return
	}

	// Shuffle only during encryption phase (first pass)
	if p.shuffleState.ShouldShuffle() {
		p.shuffleState.PerformShuffle(newDeck)
		fmt.Printf("🔀 Shuffled deck (Phase: %s, Step: %d)\n", p.shuffleState.Phase, p.shuffleState.CurrentStep)
	} else {
		fmt.Printf("🔓 Decrypting deck (Phase: %s, Step: %d)\n", p.shuffleState.Phase, p.shuffleState.CurrentStep)
	}

	// If we are host and shuffle is complete
	if p.host.ID() == p.gameState.HostID && p.shuffleState.IsComplete() {
		fmt.Println("✅ Shuffle complete! Broadcasting final deck...")
		// Store encrypted deck in game state
		p.gameState.EncryptedDeck = newDeck
		// Broadcast final deck to all players before assigning
		p.sendMessageToOthers(GameMessage{
			Type:     "final_deck",
			PlayerID: p.playerName,
			Data:     newDeck,
		})
		// Wait a moment for broadcast to propagate
		time.Sleep(100 * time.Millisecond)
		p.assignCards(newDeck)
		return
	}

	// Send to next peer
	nextPeerID := p.shuffleState.GetNextPeer()
	p.sendMessageToPeer(nextPeerID, GameMessage{
		Type:     MsgShuffleRound,
		PlayerID: p.playerName,
		Data:     newDeck,
	})
}

// handleCardAssign processes card assignments and requests keys
func (p *PokerClient) handleCardAssign(msg GameMessage, from peer.ID) {
	fmt.Printf("🔒 Received card assignments from host\n")
	assignmentsData, err := json.Marshal(msg.Data)
	if err != nil {
		return
	}
	var assignments []game.CardAssignment
	if err := json.Unmarshal(assignmentsData, &assignments); err != nil {
		return
	}

	// Initialize LockManager if needed
	if p.cardLockManager == nil {
		p.cardLockManager = game.NewCardLockManager(p.host.ID(), p.gameState.GetPlayerOrder())
	}

	// Sync assignments and request keys for MY cards
	for _, assign := range assignments {
		p.cardLockManager.LockCard(assign.CardIndex, assign.OwnerID)

		// If it's my card, request keys from everyone else
		if assign.OwnerID == p.host.ID() {
			fmt.Printf("📬 Card %d assigned to ME. Requesting decryption keys...\n", assign.CardIndex)
			p.sendMessageToOthers(GameMessage{
				Type:     MsgKeyRequest,
				PlayerID: p.playerName,
				Data:     game.KeyRequest{CardIndex: assign.CardIndex, ForPlayer: p.host.ID()},
			})
		}
	}
}

// handleKeyRequest generates and sends a decryption key for a specific card
func (p *PokerClient) handleKeyRequest(msg GameMessage, from peer.ID) {
	reqData, err := json.Marshal(msg.Data)
	if err != nil {
		return
	}
	var req game.KeyRequest
	if err := json.Unmarshal(reqData, &req); err != nil {
		return
	}

	// SECURITY CHECK 1: Verify card index is valid
	if len(p.gameState.EncryptedDeck) <= req.CardIndex {
		fmt.Printf("❌ Rejected key request: invalid card index %d\n", req.CardIndex)
		return
	}

	// SECURITY CHECK 2: Verify CardLockManager exists
	if p.cardLockManager == nil {
		fmt.Printf("❌ Rejected key request: CardLockManager not initialized\n")
		return
	}

	// SECURITY CHECK 3: Verify the card has been assigned to someone
	cardOwner, err := p.cardLockManager.GetCardOwner(req.CardIndex)
	if err != nil {
		fmt.Printf("❌ Rejected key request: card %d is not assigned to any player\n", req.CardIndex)
		return
	}

	// SECURITY CHECK 4: Verify the requester is the actual owner of the card
	if cardOwner != req.ForPlayer {
		fmt.Printf("❌ CHEATING ATTEMPT DETECTED: %s requested key for card %d owned by %s\n",
			req.ForPlayer.String()[:8], req.CardIndex, cardOwner.String()[:8])
		return
	}

	// SECURITY CHECK 5: Verify we're not providing a key to ourselves
	if req.ForPlayer == p.host.ID() {
		fmt.Printf("❌ Rejected key request: cannot provide key to myself\n")
		return
	}

	fmt.Printf("🔑 Providing key for card %d to %s (owner verified)\n", req.CardIndex, msg.PlayerID)

	encCard := p.gameState.EncryptedDeck[req.CardIndex]

	// Generate key using our KeyProvider (uses OriginalIdx from encCard)
	key := p.keyProvider.GenerateKeyForCard(encCard)

	p.sendMessageToPeer(from, GameMessage{
		Type:     MsgKeyResponse,
		PlayerID: p.playerName,
		Data:     game.KeyResponse{CardIndex: req.CardIndex, Key: key, FromPlayer: p.host.ID()},
	})
}

// handleKeyResponse stores a received decryption key
func (p *PokerClient) handleKeyResponse(msg GameMessage, from peer.ID) {
	respData, err := json.Marshal(msg.Data)
	if err != nil {
		return
	}
	var resp game.KeyResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return
	}

	if p.cardLockManager == nil {
		return
	}

	fmt.Printf("📥 Received key for card %d from %s\n", resp.CardIndex, msg.PlayerID)
	p.cardLockManager.AddKey(resp.CardIndex, from, resp.Key)

	// Check if we can reveal the card
	if p.cardLockManager.CanReveal(resp.CardIndex) {
		p.revealCard(resp.CardIndex)
	}
}

// handleFinalDeck processes the final shuffled deck from host
func (p *PokerClient) handleFinalDeck(msg GameMessage, from peer.ID) {
	fmt.Println("📦 Received final shuffled deck from host")

	deckData, err := json.Marshal(msg.Data)
	if err != nil {
		log.Printf("Error marshaling final deck: %v", err)
		return
	}
	var encryptedDeck game.EncryptedDeck
	if err := json.Unmarshal(deckData, &encryptedDeck); err != nil {
		log.Printf("Error unmarshaling final deck: %v", err)
		return
	}

	// Store the encrypted deck
	p.gameState.EncryptedDeck = encryptedDeck
	fmt.Printf("✅ Deck stored (%d cards)\n", len(encryptedDeck))
}

// handleCommunityReveal processes revealed community cards
func (p *PokerClient) handleCommunityReveal(msg GameMessage, from peer.ID) {
	data, err := json.Marshal(msg.Data)
	if err != nil {
		return
	}

	var revealData struct {
		Cards []game.Card `json:"cards"`
		Phase string      `json:"phase"`
	}
	if err := json.Unmarshal(data, &revealData); err != nil {
		return
	}

	fmt.Printf("🎴 Community cards revealed for %s: %v\n", revealData.Phase, revealData.Cards)
	p.gameState.CommunityCards = append(p.gameState.CommunityCards, revealData.Cards...)
}

// revealCommunityCards requests keys for and reveals community cards
func (p *PokerClient) revealCommunityCards(indices []int, phaseName string) {
	if !p.isHost() {
		return // Only host can initiate community card reveals
	}

	fmt.Printf("🔓 Revealing %d community cards for %s...\n", len(indices), phaseName)

	// Request keys from all players for these cards
	for _, idx := range indices {
		p.requestKeysForCard(idx)
	}

	// Wait for all keys to be collected, then reveal
	// For simplicity, we'll use a goroutine with a timeout
	p.wg.Add(1) // Track this goroutine to prevent memory leak
	go func() {
		defer p.wg.Done() // Signal completion when goroutine exits
		timeout := time.After(5 * time.Second)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		var revealedCards []game.Card

		for {
			select {
			case <-timeout:
				fmt.Printf("⚠️  Timeout waiting for community card keys\n")
				return
			case <-ticker.C:
				allReady := true
				for _, idx := range indices {
					if !p.cardLockManager.CanReveal(idx) {
						allReady = false
						break
					}
				}

				if allReady {
					// Decrypt all community cards
					for _, idx := range indices {
						keys, err := p.cardLockManager.GetKeysForCard(idx)
						if err != nil {
							continue
						}

						decryptedBytes, err := p.gameState.EncryptedDeck.DecryptCard(idx, keys)
						if err != nil {
							log.Printf("Error decrypting community card %d: %v", idx, err)
							continue
						}

						card, err := game.ParseCard(decryptedBytes)
						if err != nil {
							log.Printf("Error parsing community card %d: %v", idx, err)
							continue
						}

						revealedCards = append(revealedCards, card)
						fmt.Printf("🎴 %s card: %s\n", phaseName, card.String())
					}

					// Add to game state
					p.gameState.CommunityCards = append(p.gameState.CommunityCards, revealedCards...)

					// Broadcast to all players
					p.sendMessageToOthers(GameMessage{
						Type:     MsgCommunityReveal,
						PlayerID: p.playerName,
						Data: map[string]interface{}{
							"cards": revealedCards,
							"phase": phaseName,
						},
					})
					return
				}
			}
		}
	}()
}

// isHost returns true if this client is the game host
func (p *PokerClient) isHost() bool {
	return p.gameState != nil && p.host.ID() == p.gameState.HostID
}

// revealCard decrypts and displays a card we own
func (p *PokerClient) revealCard(index int) {
	keys, err := p.cardLockManager.GetKeysForCard(index)
	if err != nil {
		return
	}

	// Add our own "key" (since DecryptCard needs keys from everyone)
	// Actually, the protocol needs keys from ALL participants.
	// We have the keys from others, now we apply our own encryption layer removal.

	decryptedBytes, err := p.gameState.EncryptedDeck.DecryptCard(index, keys)
	if err != nil {
		log.Printf("Error decrypting card %d: %v", index, err)
		return
	}

	card, err := game.ParseCard(decryptedBytes)
	if err != nil {
		log.Printf("Error parsing revealed card %d: %v", index, err)
		return
	}

	fmt.Printf("✨ Card %d Revealed: %s\n", index, card.String())

	// If this is MY card, add it to my hand
	player := p.gameState.Players[p.host.ID()]
	if player != nil && p.cardLockManager.IsMyCard(index) {
		player.Hand = append(player.Hand, card)
		fmt.Printf("🎴 Added to my hand: %s (total: %d cards)\n", card.String(), len(player.Hand))

		// If we have both hole cards, send commitment BEFORE PreFlop starts
		if len(player.Hand) == 2 {
			p.sendHandCommitment()

			// Check if all players have committed
			p.checkAndStartPreFlop()
		}
	}
}

// assignCards (Host only) assigns cards to players and broadcasts assignments
func (p *PokerClient) assignCards(deck game.EncryptedDeck) {
	fmt.Println("Assigning cards to players...")
	p.gameState.EncryptedDeck = game.EncryptedDeck(deck)

	playerIDs := p.gameState.GetPlayerOrder()
	assignments := make([]game.CardAssignment, 0)

	// Deterministic assignment: 2 cards per player
	cardIndex := 0
	for _, id := range playerIDs {
		assignments = append(assignments, game.CardAssignment{CardIndex: cardIndex, OwnerID: id})
		assignments = append(assignments, game.CardAssignment{CardIndex: cardIndex + 1, OwnerID: id})
		cardIndex += 2
	}

	// Reserve community cards: Flop (3), Turn (1), River (1)
	// Community cards are "owned" by a special peer.ID (empty = public)
	// For now, we'll use the Host's ID as a marker for community cards
	communityOwner := p.gameState.HostID
	flopIndices := []int{cardIndex, cardIndex + 1, cardIndex + 2}
	turnIndex := cardIndex + 3
	riverIndex := cardIndex + 4

	for _, idx := range flopIndices {
		assignments = append(assignments, game.CardAssignment{CardIndex: idx, OwnerID: communityOwner})
	}
	assignments = append(assignments, game.CardAssignment{CardIndex: turnIndex, OwnerID: communityOwner})
	assignments = append(assignments, game.CardAssignment{CardIndex: riverIndex, OwnerID: communityOwner})

	// Store community card indices in gameState for later reveal
	p.gameState.FlopIndices = flopIndices
	p.gameState.TurnIndex = turnIndex
	p.gameState.RiverIndex = riverIndex
	fmt.Printf("📦 Assigned %d hole cards + 5 community cards (Flop:%v, Turn:%d, River:%d)\n",
		cardIndex, flopIndices, turnIndex, riverIndex)

	// Broadcast assignments
	p.sendMessage(GameMessage{
		Type:     MsgCardAssign,
		PlayerID: p.playerName,
		Data:     assignments,
	})

	// Initialize lock manager for self
	p.cardLockManager = game.NewCardLockManager(p.host.ID(), playerIDs)
	for _, assign := range assignments {
		p.cardLockManager.LockCard(assign.CardIndex, assign.OwnerID)
	}

	// Request keys for my own cards
	for i, id := range playerIDs {
		if id == p.host.ID() {
			p.requestKeysForCard(i * 2)
			p.requestKeysForCard(i*2 + 1)
		}
	}

	// Set phase to WaitingForPlayers - will transition to PreFlop once all commit
	p.gameState.Phase = game.WaitingForPlayers
	fmt.Println("🃏 Cards dealt! Waiting for all players to commit their hands...")
}

// advanceToNextPhase advances the game to the next betting round and reveals community cards
func (p *PokerClient) advanceToNextPhase() {
	// Collect bets into pot
	for _, player := range p.gameState.Players {
		p.gameState.Pot += player.Bet
		player.Bet = 0
		player.HasActed = false
	}

	switch p.gameState.Phase {
	case game.PreFlop:
		p.gameState.Phase = game.Flop
		fmt.Println("🎴 Advancing to Flop...")
		// Reveal 3 flop cards
		if p.isHost() {
			p.revealCommunityCards(p.gameState.FlopIndices, "Flop")
		}

	case game.Flop:
		p.gameState.Phase = game.Turn
		fmt.Println("🎴 Advancing to Turn...")
		// Reveal turn card
		if p.isHost() {
			p.revealCommunityCards([]int{p.gameState.TurnIndex}, "Turn")
		}

	case game.Turn:
		p.gameState.Phase = game.River
		fmt.Println("🎴 Advancing to River...")
		// Reveal river card
		if p.isHost() {
			p.revealCommunityCards([]int{p.gameState.RiverIndex}, "River")
		}

	case game.River:
		p.gameState.Phase = game.Showdown
		fmt.Println("🏆 Advancing to Showdown...")
		// Showdown logic will be added next
	}

	// Reset betting state for new round
	p.gameState.CurrentBet = 0
	playerOrder := p.gameState.GetPlayerOrder()
	p.gameState.CurrentTurn = p.gameState.NextPlayerInOrder(playerOrder, 0)
}

// handleHandCommitment processes a hand commitment from a player
func (p *PokerClient) handleHandCommitment(msg GameMessage, from peer.ID) {
	data, err := json.Marshal(msg.Data)
	if err != nil {
		return
	}

	var commitment struct {
		Commitment string `json:"commitment"`
	}
	if err := json.Unmarshal(data, &commitment); err != nil {
		return
	}

	player := p.gameState.Players[from]
	if player != nil {
		player.Commitment = commitment.Commitment
		fmt.Printf("🔐 Received hand commitment from %s\n", player.Name)

		// Check if all players have committed -> start PreFlop
		p.checkAndStartPreFlop()
	}
}

// checkAndStartPreFlop checks if all players have committed their hands
// If so, transitions to PreFlop phase (allows betting to begin)
func (p *PokerClient) checkAndStartPreFlop() {
	// Only proceed if we're still waiting (not yet in PreFlop)
	if p.gameState.Phase != game.WaitingForPlayers {
		return
	}

	// Check if ALL players have non-empty commitments
	allCommitted := true
	for _, player := range p.gameState.Players {
		if player.InHand && player.Commitment == "" {
			allCommitted = false
			break
		}
	}

	if allCommitted {
		// All players committed! Now betting can begin
		p.gameState.Phase = game.PreFlop

		// Setup and post blinds (only host does this)
		if p.isHost() {
			// Setup blind positions
			p.gameState.SetupBlinds()

			// Post blinds
			if err := p.gameState.PostBlinds(); err != nil {
				fmt.Printf("❌ Error posting blinds: %v\n", err)
				return
			}

			// Get player names for blind positions
			sbPlayer := p.gameState.Players[p.gameState.SmallBlind]
			bbPlayer := p.gameState.Players[p.gameState.BigBlind]

			fmt.Println("✅ All players committed! Starting PreFlop betting phase.")
			fmt.Printf("💰 Small Blind (%d): %s\n", p.gameState.SmallBlindAmount, sbPlayer.Name)
			fmt.Printf("💰 Big Blind (%d): %s\n", p.gameState.BigBlindAmount, bbPlayer.Name)
			fmt.Printf("💵 Pot: %d chips\n", p.gameState.Pot)

			// Broadcast updated game state with blinds
			p.sendMessageToOthers(GameMessage{
				Type:     "state_update",
				PlayerID: p.playerName,
				Data:     p.gameState,
			})
		}
	}
}

// handleHandReveal processes a hand reveal from a player at showdown
func (p *PokerClient) handleHandReveal(msg GameMessage, from peer.ID) {
	data, err := json.Marshal(msg.Data)
	if err != nil {
		return
	}

	var reveal struct {
		Hand []game.Card `json:"hand"`
		Salt string      `json:"salt"`
	}
	if err := json.Unmarshal(data, &reveal); err != nil {
		return
	}

	player := p.gameState.Players[from]
	if player == nil {
		return
	}

	// Verify commitment
	expectedCommitment := game.GenerateCommitment(reveal.Hand, reveal.Salt)
	if player.Commitment != expectedCommitment {
		fmt.Printf("⚠️  Player %s hand commitment verification FAILED!\n", player.Name)
		return
	}

	// Store verified hand
	player.Hand = reveal.Hand
	fmt.Printf("✅ Player %s revealed hand: %v (verified)\n", player.Name, reveal.Hand)

	// If we're the host, check if all players revealed and determine winner
	if p.isHost() {
		p.checkAndDetermineWinner()
	}
}

// handleStateUpdate processes a full game state update from the host
func (p *PokerClient) handleStateUpdate(msg GameMessage, from peer.ID) {
	data, err := json.Marshal(msg.Data)
	if err != nil {
		fmt.Printf("❌ Error marshaling state update: %v\n", err)
		return
	}

	var updatedState game.GameState
	if err := json.Unmarshal(data, &updatedState); err != nil {
		fmt.Printf("❌ Error unmarshaling state update: %v\n", err)
		return
	}

	// Update local game state with new values
	p.gameState.Phase = updatedState.Phase
	p.gameState.Pot = updatedState.Pot
	p.gameState.CurrentBet = updatedState.CurrentBet
	p.gameState.CurrentTurn = updatedState.CurrentTurn
	p.gameState.SmallBlind = updatedState.SmallBlind
	p.gameState.BigBlind = updatedState.BigBlind
	p.gameState.SmallBlindAmount = updatedState.SmallBlindAmount
	p.gameState.BigBlindAmount = updatedState.BigBlindAmount

	// Update player states (bets, stacks)
	for id, player := range updatedState.Players {
		if localPlayer, exists := p.gameState.Players[id]; exists {
			localPlayer.Bet = player.Bet
			localPlayer.Stack = player.Stack
			localPlayer.InHand = player.InHand
		}
	}

	// Display blind information if we just entered PreFlop
	if updatedState.Phase == game.PreFlop && p.gameState.Pot > 0 {
		sbPlayer := p.gameState.Players[p.gameState.SmallBlind]
		bbPlayer := p.gameState.Players[p.gameState.BigBlind]

		fmt.Println("✅ PreFlop betting phase started!")
		fmt.Printf("💰 Small Blind (%d): %s\n", p.gameState.SmallBlindAmount, sbPlayer.Name)
		fmt.Printf("💰 Big Blind (%d): %s\n", p.gameState.BigBlindAmount, bbPlayer.Name)
		fmt.Printf("💵 Pot: %d chips\n", p.gameState.Pot)

		// Show if it's your turn
		if p.gameState.CurrentTurn == p.host.ID() {
			fmt.Println("🎯 It's your turn! Current bet to call:", p.gameState.CurrentBet)
		}
	}
}

// handleWinnerAnnouncement processes a winner announcement from the host
func (p *PokerClient) handleWinnerAnnouncement(msg GameMessage, from peer.ID) {
	data, err := json.Marshal(msg.Data)
	if err != nil {
		return
	}

	var announcement struct {
		WinnerID   string `json:"winner_id"`
		WinnerName string `json:"winner_name"`
		WinAmount  int    `json:"win_amount"`
		HandType   string `json:"hand_type"`
	}
	if err := json.Unmarshal(data, &announcement); err != nil {
		return
	}

	fmt.Printf("\n🏆 ═══════════════════════════════════════ 🏆\n")
	fmt.Printf("   WINNER: %s\n", announcement.WinnerName)
	fmt.Printf("   Hand: %s\n", announcement.HandType)
	fmt.Printf("   Pot: %d chips\n", announcement.WinAmount)
	fmt.Printf("🏆 ═══════════════════════════════════════ 🏆\n\n")
}

// sendHandCommitment generates and sends a hand commitment
func (p *PokerClient) sendHandCommitment() {
	player := p.gameState.Players[p.host.ID()]
	if player == nil || len(player.Hand) == 0 {
		return
	}

	// Generate random salt
	saltBytes := make([]byte, 16)
	rand.Read(saltBytes)
	player.Salt = fmt.Sprintf("%x", saltBytes)

	// Generate commitment
	player.Commitment = game.GenerateCommitment(player.Hand, player.Salt)

	// Broadcast commitment
	p.sendMessageToOthers(GameMessage{
		Type:     MsgHandCommitment,
		PlayerID: p.playerName,
		Data: map[string]interface{}{
			"commitment": player.Commitment,
		},
	})

	fmt.Printf("🔐 Sent hand commitment: %s\n", player.Commitment[:16]+"...")
}

// revealHandAtShowdown reveals the player's hand with salt for verification
func (p *PokerClient) revealHandAtShowdown() {
	player := p.gameState.Players[p.host.ID()]
	if player == nil || len(player.Hand) == 0 {
		return
	}

	// Broadcast hand + salt
	p.sendMessageToOthers(GameMessage{
		Type:     MsgHandReveal,
		PlayerID: p.playerName,
		Data: map[string]interface{}{
			"hand": player.Hand,
			"salt": player.Salt,
		},
	})

	fmt.Printf("🃏 Revealed hand at showdown: %v\n", player.Hand)
}

// checkAndDetermineWinner checks if all active players revealed and determines winner
func (p *PokerClient) checkAndDetermineWinner() {
	allRevealed := true
	for _, player := range p.gameState.Players {
		if player.InHand && len(player.Hand) == 0 {
			allRevealed = false
			break
		}
	}

	if !allRevealed {
		return // Wait for all reveals
	}

	// Evaluate all hands and find winner
	var bestPlayer *game.Player
	var bestStrength game.HandStrength

	for _, player := range p.gameState.Players {
		if !player.InHand {
			continue
		}

		// Combine hole cards with community cards (7 total)
		allCards := append(player.Hand, p.gameState.CommunityCards...)
		if len(allCards) < 7 {
			continue
		}

		result := game.Evaluate(allCards)
		fmt.Printf("📊 %s: %s\n", player.Name, result.Strength)

		if bestPlayer == nil || result.Strength > bestStrength {
			bestPlayer = player
			bestStrength = result.Strength
		}
	}

	if bestPlayer == nil {
		return
	}

	// Award pot to winner
	bestPlayer.Stack += p.gameState.Pot
	winAmount := p.gameState.Pot
	p.gameState.Pot = 0

	fmt.Printf("\n🎉 %s wins %d chips with %s!\n", bestPlayer.Name, winAmount, bestStrength)

	// Broadcast winner announcement
	p.sendMessageToOthers(GameMessage{
		Type:     MsgWinnerAnnouncement,
		PlayerID: p.playerName,
		Data: map[string]interface{}{
			"winner_id":   bestPlayer.ID.String(),
			"winner_name": bestPlayer.Name,
			"win_amount":  winAmount,
			"hand_type":   bestStrength.String(),
		},
	})
}

// requestKeysForCard sends key requests to all other players for a specific card
func (p *PokerClient) requestKeysForCard(cardIndex int) {
	fmt.Printf("📬 Requesting keys for card %d...\n", cardIndex)
	p.sendMessageToOthers(GameMessage{
		Type:     MsgKeyRequest,
		PlayerID: p.playerName,
		Data:     game.KeyRequest{CardIndex: cardIndex, ForPlayer: p.host.ID()},
	})
}

// sendMessageToOthers sends a message to all peers EXCEPT ourselves
func (p *PokerClient) sendMessageToOthers(msg GameMessage) {
	p.sendMessage(msg) // Existing sendMessage already sends to all connected peers
}

// sendMessageToPeer sends a message to a specific peer
func (p *PokerClient) sendMessageToPeer(peerID peer.ID, msg GameMessage) {
	p.peersMu.RLock()
	stream, ok := p.peers[peerID]
	p.peersMu.RUnlock()

	if !ok {
		log.Printf("No stream found for peer %s", peerID)
		return
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Error marshaling message for peer %s: %v", peerID, err)
		return
	}

	if _, err := stream.Write(append(data, '\n')); err != nil {
		log.Printf("Error writing to stream for peer %s: %v", peerID, err)
		p.removePeer(peerID)
	}
}

// sendMessage sends a message to all connected peers
func (p *PokerClient) sendMessage(msg GameMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Error marshaling message: %v", err)
		return
	}

	// Check message size before sending
	if len(data) > MaxMessageSize {
		log.Printf("Message too large to send: %d bytes", len(data))
		return
	}

	// First pass: send with read lock, collect broken peers
	p.peersMu.RLock()
	brokenPeers := make([]peer.ID, 0)

	for peerID, stream := range p.peers {
		if _, err := stream.Write(append(data, '\n')); err != nil {
			log.Printf("Error sending message to %s: %v", peerID, err)
			brokenPeers = append(brokenPeers, peerID)
		}
	}
	p.peersMu.RUnlock()

	// Second pass: cleanup broken peers with write lock
	for _, peerID := range brokenPeers {
		p.removePeer(peerID)
	}
}

// removePeer safely removes a peer and closes the stream
func (p *PokerClient) removePeer(peerID peer.ID) {
	p.peersMu.Lock()
	defer p.peersMu.Unlock()

	if stream, exists := p.peers[peerID]; exists {
		stream.Close()
		delete(p.peers, peerID)
		log.Printf("Removed peer: %s", peerID)
	}
}

// handleRoomInfoRequest handles incoming room info requests
func (p *PokerClient) handleRoomInfoRequest(s network.Stream) {
	defer s.Close()

	p.roomMu.RLock()
	if p.currentRoom == nil {
		p.roomMu.RUnlock()
		return
	}

	// Create a copy with current player count
	roomInfo := &Room{
		ID:         p.currentRoom.ID,
		Name:       p.currentRoom.Name,
		HostID:     p.currentRoom.HostID,
		Players:    0, // Will update below
		MaxPlayers: p.currentRoom.MaxPlayers,
	}
	p.roomMu.RUnlock()

	// Get current player count
	p.peersMu.RLock()
	roomInfo.Players = len(p.peers) + 1
	p.peersMu.RUnlock()

	data, err := json.Marshal(roomInfo)
	if err != nil {
		log.Printf("Error marshaling room info: %v", err)
		return
	}
	s.Write(append(data, '\n'))
}

// createRoom creates a new public poker room
func (p *PokerClient) createRoom(name string) {
	// Generate room ID from name + host ID
	roomID := fmt.Sprintf("%s-%s", name, p.host.ID().String()[:8])

	newRoom := &Room{
		ID:         roomID,
		Name:       name,
		HostID:     p.host.ID(),
		Players:    1, // Just the host
		MaxPlayers: MaxPlayersPerRoom,
	}

	p.roomMu.Lock()
	p.currentRoom = newRoom
	p.roomMu.Unlock()

	atomic.StoreInt32(&p.isAdvertising, 1)

	// Start advertising in DHT
	p.wg.Add(1)
	go p.advertiseRoom()

	fmt.Printf("🎲 Created room: %s\n", name)
	fmt.Printf("Room ID: %s\n", roomID)
	fmt.Printf("Waiting for players... (max %d)\n", MaxPlayersPerRoom)
}

// advertiseRoom periodically advertises the room in the DHT lobby
func (p *PokerClient) advertiseRoom() {
	defer p.wg.Done()

	lobbyCID, err := createCID(LobbyName)
	if err != nil {
		log.Printf("Error creating lobby CID: %v", err)
		return
	}

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	// Advertise immediately on start
	if err := p.dht.Provide(p.ctx, lobbyCID, true); err != nil {
		if p.ctx.Err() != nil {
			return
		}
		log.Printf("Error advertising room: %v", err)
	}

	for {
		select {
		case <-p.ctx.Done():
			log.Println("Stopping room advertisement (context cancelled)")
			return

		case <-ticker.C:
			// Stop advertising if room is full or we're no longer hosting
			p.roomMu.RLock()
			roomNil := p.currentRoom == nil
			p.roomMu.RUnlock()

			if atomic.LoadInt32(&p.isAdvertising) == 0 || roomNil {
				log.Println("Stopping room advertisement")
				return
			}

			p.peersMu.RLock()
			playerCount := len(p.peers) + 1
			p.peersMu.RUnlock()

			if playerCount >= MaxPlayersPerRoom {
				atomic.StoreInt32(&p.isAdvertising, 0)
				fmt.Printf("🔒 Room is full (%d players). No longer advertising.\n", playerCount)
				return
			}

			if err := p.dht.Provide(p.ctx, lobbyCID, true); err != nil {
				if p.ctx.Err() != nil {
					return
				}
				log.Printf("Error advertising room: %v", err)
			}
		}
	}
}

// listPublicRooms discovers and lists all public rooms
func (p *PokerClient) listPublicRooms() {
	fmt.Println("🔍 Searching for public rooms...")

	lobbyCID, err := createCID(LobbyName)
	if err != nil {
		log.Printf("Error creating lobby CID: %v", err)
		return
	}

	// Find all providers (room hosts)
	ctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
	defer cancel()

	providers, err := p.dht.FindProviders(ctx, lobbyCID)
	if err != nil {
		log.Printf("Error finding rooms: %v", err)
		return
	}

	// Build list of discovered rooms
	discovered := make([]*Room, 0)

	for _, provider := range providers {
		if provider.ID == p.host.ID() {
			continue // Skip self
		}

		// Connect to provider
		if err := p.host.Connect(ctx, provider); err != nil {
			continue
		}

		// Request room info - use closure to ensure stream closes immediately
		func() {
			stream, err := p.host.NewStream(ctx, provider.ID, RoomInfoProtocol)
			if err != nil {
				return
			}
			defer stream.Close() // CRITICAL: Close stream after this iteration

			scanner := bufio.NewScanner(stream)
			if scanner.Scan() {
				var room Room
				if err := json.Unmarshal(scanner.Bytes(), &room); err == nil {
					discovered = append(discovered, &room)
				}
			}
		}()
	}

	// Store discovered rooms with lock
	p.roomMu.Lock()
	p.discoveredRooms = discovered
	p.roomMu.Unlock()

	if len(discovered) == 0 {
		fmt.Println("No public rooms found. Create one with: create <name>")
		return
	}

	fmt.Println("\n🎰 Available Rooms:")
	fmt.Println("-------------------")
	for i, room := range discovered {
		fmt.Printf("%d. \"%s\" - %d/%d players - Host: %s\n",
			i+1, room.Name, room.Players, room.MaxPlayers, room.HostID.String()[:12])
	}
	fmt.Println("\nJoin with: join <number>")
}

// autoRefreshRoomList periodically refreshes the room list in the background
func (p *PokerClient) autoRefreshRoomList() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Only refresh if not currently in a room
			p.roomMu.RLock()
			inRoom := p.currentRoom != nil
			p.roomMu.RUnlock()

			if !inRoom {
				// Silently refresh room list (no output)
				p.refreshRoomListSilently()
			}
		case <-p.ctx.Done():
			return
		}
	}
}

// refreshRoomListSilently refreshes the room list without printing output
func (p *PokerClient) refreshRoomListSilently() {
	lobbyCID, err := createCID(LobbyName)
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
	defer cancel()

	providers, err := p.dht.FindProviders(ctx, lobbyCID)
	if err != nil {
		return
	}

	discovered := make([]*Room, 0)

	for _, provider := range providers {
		if provider.ID == p.host.ID() {
			continue
		}

		if err := p.host.Connect(ctx, provider); err != nil {
			continue
		}

		func() {
			stream, err := p.host.NewStream(ctx, provider.ID, RoomInfoProtocol)
			if err != nil {
				return
			}
			defer stream.Close()

			scanner := bufio.NewScanner(stream)
			if scanner.Scan() {
				var room Room
				if err := json.Unmarshal(scanner.Bytes(), &room); err == nil {
					discovered = append(discovered, &room)
				}
			}
		}()
	}

	// Update discovered rooms
	p.roomMu.Lock()
	p.discoveredRooms = discovered
	p.roomMu.Unlock()
}

// joinRoom joins an existing room by index from discovered rooms
func (p *PokerClient) joinRoom(index int) {
	p.roomMu.RLock()
	roomCount := len(p.discoveredRooms)
	if index < 1 || index > roomCount {
		p.roomMu.RUnlock()
		fmt.Printf("Invalid room number. Run 'rooms' to see available rooms.\n")
		return
	}

	room := p.discoveredRooms[index-1]
	p.roomMu.RUnlock()

	fmt.Printf("🔗 Joining room: %s\n", room.Name)

	// Find the host's address info
	peerInfo := peer.AddrInfo{ID: room.HostID}

	// Connect to host
	if err := p.host.Connect(p.ctx, peerInfo); err != nil {
		log.Printf("Failed to connect to room host: %v", err)
		return
	}

	// Create game stream
	stream, err := p.host.NewStream(p.ctx, room.HostID, PokerProtocol)
	if err != nil {
		log.Printf("Failed to join room: %v", err)
		return
	}

	p.peersMu.Lock()
	p.peers[room.HostID] = stream
	p.peersMu.Unlock()

	p.roomMu.Lock()
	p.currentRoom = room
	p.roomMu.Unlock()

	// Send join message
	joinMsg := GameMessage{
		Type:     "join",
		PlayerID: p.playerName,
		Data:     "Hello from " + p.playerName,
	}
	p.sendMessage(joinMsg)

	fmt.Printf("✅ Joined room: %s\n", room.Name)

	// Start listening for messages from host (tracked in WaitGroup)
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.listenToStream(stream, room.HostID)
	}()
}

// listenToStream listens for messages from a peer stream
func (p *PokerClient) listenToStream(s network.Stream, remotePeer peer.ID) {
	// Set up scanner with buffer limits
	scanner := bufio.NewScanner(s)
	scanner.Buffer(make([]byte, 4096), MaxMessageSize)

	for scanner.Scan() {
		var msg GameMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			log.Printf("Error unmarshaling message: %v", err)
			continue
		}
		p.handleGameMessage(msg, remotePeer)
	}

	// Check for scanner errors
	if err := scanner.Err(); err != nil {
		log.Printf("Stream error from %s: %v", remotePeer.String(), err)
	}

	// Clean up when connection closes
	p.peersMu.Lock()
	delete(p.peers, remotePeer)
	p.peersMu.Unlock()
	log.Printf("Connection closed from %s", remotePeer.String())
}

// connectToPeer connects to a discovered peer
func (p *PokerClient) connectToPeer(peerInfo peer.AddrInfo) {
	// Use timeout for connection
	ctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
	defer cancel()

	if err := p.host.Connect(ctx, peerInfo); err != nil {
		log.Printf("Failed to connect to peer %s: %v", peerInfo.ID, err)
		return
	}

	stream, err := p.host.NewStream(ctx, peerInfo.ID, PokerProtocol)
	if err != nil {
		log.Printf("Failed to create stream to %s: %v", peerInfo.ID, err)
		return
	}

	p.peersMu.Lock()
	p.peers[peerInfo.ID] = stream
	p.peersMu.Unlock()

	fmt.Printf("✅ Connected to peer: %s\n", peerInfo.ID.String())

	// Send join message
	joinMsg := GameMessage{
		Type:     "join",
		PlayerID: p.playerName,
		Data:     "Hello from " + p.playerName,
	}
	p.sendMessage(joinMsg)
}

// Start starts the poker client
func (p *PokerClient) Start() {
	fmt.Printf("🎰 Starting P2P Poker Client\n")
	fmt.Printf("Player: %s\n", p.playerName)
	fmt.Printf("Peer ID: %s\n", p.host.ID().String())
	fmt.Println("\nTo get started:")
	fmt.Println("  - 'create <name>' to create a new room")
	fmt.Println("  - 'rooms' to see available rooms")
	fmt.Println("  - 'join <number>' to join a room")

	// Start command interface (tracked in WaitGroup)
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.startCommandInterface()
	}()

	// Start automatic room list refresh (every 30 seconds)
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.autoRefreshRoomList()
	}()

	// Wait for shutdown signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	fmt.Println("Shutting down poker client...")
	p.Shutdown()
}

// startCommandInterface starts the command line interface
func (p *PokerClient) startCommandInterface() {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Println("\n🎮 Poker Game Commands:")
	fmt.Println("- create <name>: Create a new room")
	fmt.Println("- rooms: List available rooms")
	fmt.Println("- join <number>: Join a room")
	fmt.Println("- start: Start the game (host only)")
	fmt.Println("- chat <message>: Send a chat message")
	fmt.Println("- bet <amount>: Place a bet")
	fmt.Println("- call/fold/check: Betting actions")
	fmt.Println("- peers: Show connected peers")
	fmt.Println("- quit: Exit the game")
	fmt.Print("> ")

	for scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			fmt.Print("> ")
			continue
		}

		parts := strings.Split(input, " ")
		command := parts[0]

		switch command {
		case "chat":
			if len(parts) > 1 {
				message := strings.Join(parts[1:], " ")
				p.sendMessage(GameMessage{
					Type:     "chat",
					PlayerID: p.playerName,
					Data:     message,
				})
			}
		case "bet":
			if len(parts) < 2 {
				fmt.Println("Usage: bet <amount>")
				fmt.Print("> ")
				continue
			}

			amount, err := strconv.Atoi(parts[1])
			if err != nil {
				fmt.Printf("Invalid amount: %v\n", err)
				fmt.Print("> ")
				continue
			}

			if amount <= 0 {
				fmt.Println("Amount must be positive")
				fmt.Print("> ")
				continue
			}

			p.sendMessage(GameMessage{
				Type:     "bet",
				PlayerID: p.playerName,
				Data:     BetAction{Action: "bet", Amount: amount},
			})
		case "call":
			p.sendMessage(GameMessage{
				Type:     "bet",
				PlayerID: p.playerName,
				Data:     BetAction{Action: "call", Amount: 0},
			})
		case "fold":
			p.sendMessage(GameMessage{
				Type:     "bet",
				PlayerID: p.playerName,
				Data:     BetAction{Action: "fold", Amount: 0},
			})
		case "check":
			p.sendMessage(GameMessage{
				Type:     "bet",
				PlayerID: p.playerName,
				Data:     BetAction{Action: "check", Amount: 0},
			})
		case "peers":
			p.peersMu.RLock()
			fmt.Printf("Connected peers: %d\n", len(p.peers))
			for peerID := range p.peers {
				fmt.Printf("- %s\n", peerID.String())
			}
			p.peersMu.RUnlock()
		case "create":
			if len(parts) > 1 {
				roomName := strings.Join(parts[1:], " ")
				p.createRoom(roomName)
			} else {
				fmt.Println("Usage: create <room name>")
			}
		case "rooms":
			p.listPublicRooms()
		case "join":
			if len(parts) > 1 {
				index := 0
				fmt.Sscanf(parts[1], "%d", &index)
				p.joinRoom(index)
			} else {
				fmt.Println("Usage: join <room number>")
			}
		case "start":
			if p.currentRoom == nil {
				fmt.Println("You must be in a room to start the game.")
			} else if p.currentRoom.HostID != p.host.ID() {
				fmt.Println("Only the room host can start the game.")
			} else {
				p.peersMu.RLock()
				playerCount := len(p.peers) + 1
				p.peersMu.RUnlock()
				if playerCount < 2 {
					fmt.Println("Need at least 2 players to start.")
				} else {
					fmt.Printf("🎲 Starting mental poker shuffle with %d players!\n", playerCount)
					// 1. Initialize host's deck
					deck := game.NewDeck()

					// 2. Initialize shuffle state
					playerIDs := p.gameState.GetPlayerOrder()
					p.shuffleState = game.NewShuffleState(p.host.ID(), playerIDs, "host_secret") // Improve secret later

					// 3. Start first shuffle round (host shuffles first)
					encryptedDeck := deck.Encrypt(p.myCipher)
					newDeck, _, _ := p.shuffleState.HandleShuffleMessage(encryptedDeck)
					p.shuffleState.PerformShuffle(newDeck)

					// 4. Send to next player
					nextPeerID := p.shuffleState.GetNextPeer()
					p.sendMessageToPeer(nextPeerID, GameMessage{
						Type:     MsgShuffleRound,
						PlayerID: p.playerName,
						Data:     newDeck,
					})

					p.gameState.Phase = game.Shuffling
					fmt.Println("🔄 Started shuffle round 1. Passing to next player...")
				}
			}
		case "quit":
			fmt.Println("Goodbye!")
			p.Shutdown()
			os.Exit(0)
		default:
			fmt.Printf("Unknown command: %s\n", command)
		}
		fmt.Print("> ")
	}
}

// Shutdown gracefully shuts down the client
func (p *PokerClient) Shutdown() {
	log.Println("Initiating shutdown...")

	// 1. Cancel context to stop all goroutines
	p.cancel()

	// 2. Wait for tracked goroutines to exit (with timeout)
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("All goroutines exited cleanly")
	case <-time.After(2 * time.Second):
		log.Println("Warning: Timeout waiting for goroutines to exit")
	}

	// 3. Close all peer connections
	p.peersMu.Lock()
	for peerID, stream := range p.peers {
		log.Printf("Closing connection to %s", peerID)
		stream.Close()
	}
	p.peers = make(map[peer.ID]network.Stream)
	p.peersMu.Unlock()

	// 4. Close DHT and host
	if err := p.dht.Close(); err != nil {
		log.Printf("Error closing DHT: %v", err)
	}

	if err := p.host.Close(); err != nil {
		log.Printf("Error closing host: %v", err)
	}

	log.Println("Shutdown complete")
}

func main() {
	// need to replace ths with the bootstrap node address
	bootstrapAddr := "/ip4/127.0.0.1/tcp/4001/p2p/12D3KooWBhxVN8eNqZXRJJbvSMVHYQZXmv8CFgJbp8rFLJSVJKKr"

	if len(os.Args) > 1 {
		bootstrapAddr = os.Args[1]
	}

	client, err := NewPokerClient(bootstrapAddr)
	if err != nil {
		log.Fatal(err)
	}

	client.Start()
}
