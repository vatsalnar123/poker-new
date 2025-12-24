package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	mrand "math/rand"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/multiformats/go-multiaddr"
	"github.com/multiformats/go-multihash"

	appcrypto "github.com/vatsalnarula123/poker-new/pkg/crypto"
	"github.com/vatsalnarula123/poker-new/pkg/game"
)

const (
	PokerProtocol = "/poker/1.0.0"
)

var roomName = flag.String("room", "poker-room-abc", "The name of the poker room to join")

var handStrengthToString = map[game.HandStrength]string{
	game.HighCard:      "High Card",
	game.OnePair:       "One Pair",
	game.TwoPair:       "Two Pair",
	game.ThreeOfAKind:  "Three of a Kind",
	game.Straight:      "Straight",
	game.Flush:         "Flush",
	game.FullHouse:     "Full House",
	game.FourOfAKind:   "Four of a Kind",
	game.StraightFlush: "Straight Flush",
	game.RoyalFlush:    "Royal Flush",
}

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
	Type          string          `json:"type"`
	PlayerID      string          `json:"player_id"`
	Data          interface{}     `json:"data"`
	GameState     *game.GameState `json:"game_state,omitempty"`
	Cards         []game.Card     `json:"cards,omitempty"`
	EncryptedDeck [][]byte        `json:"encrypted_deck,omitempty"`
	Action        *PlayerAction   `json:"action,omitempty"`
	Commitment    string          `json:"commitment,omitempty"`
}

// PlayerAction represents a specific game move made by a player.
type PlayerAction struct {
	Type         string      `json:"type"` // "bet", "check", "fold", "reveal"
	Amount       int         `json:"amount,omitempty"`
	RevealedHand []game.Card `json:"revealed_hand,omitempty"`
	RevealedSalt string      `json:"revealed_salt,omitempty"`
}

// BetAction represents a betting action
type BetAction struct {
	Action string `json:"action"` // "bet", "call", "fold", "raise", "check"
	Amount int    `json:"amount"`
}

// PokerClient represents a poker player client
type PokerClient struct {
	host           host.Host
	dht            *dht.IpfsDHT
	ctx            context.Context
	cancel         context.CancelFunc
	peers          map[peer.ID]network.Stream
	peerMutex      sync.Mutex
	playerName     string
	gameState      *game.GameState
	gameStateMutex sync.Mutex
	isHost         bool
	myHand         []game.Card
	myCipher       *appcrypto.Cipher
	shutdown       chan struct{}
	peerUpdate     chan struct{}
}

func NewPokerClient(h host.Host, dht *dht.IpfsDHT, isHost bool, name string, ctx context.Context, cancel context.CancelFunc) *PokerClient {
	p := &PokerClient{
		host:       h,
		dht:        dht,
		ctx:        ctx,
		cancel:     cancel,
		peers:      make(map[peer.ID]network.Stream),
		isHost:     isHost,
		playerName: name,
		gameState:  game.NewGameState(),
		shutdown:   make(chan struct{}),
		peerUpdate: make(chan struct{}),
	}
	p.myCipher = appcrypto.NewCipher(p.playerName)
	if isHost {
		p.gameState.HostID = h.ID()
		p.gameState.AddPlayer(&game.Player{
			ID:    h.ID(),
			Name:  name,
			Stack: 1000,
		})
	}
	h.SetStreamHandler(PokerProtocol, p.handleStream)
	return p
}

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
	if err := p.dht.Bootstrap(p.ctx); err != nil {
		return err
	}
	return nil
}

func (p *PokerClient) handleStream(s network.Stream) {
	remotePeer := s.Conn().RemotePeer()
	log.Printf("New connection from %s", remotePeer.String())

	p.peerMutex.Lock()
	p.peers[remotePeer] = s
	p.peerMutex.Unlock()

	p.determineHost()

	p.gameStateMutex.Lock()
	p.gameState.AddPlayer(&game.Player{ID: remotePeer, Name: fmt.Sprintf("Player_%s", remotePeer.String()[:4]), Stack: 1000})
	p.gameStateMutex.Unlock()

	p.peerUpdate <- struct{}{}

	scanner := bufio.NewScanner(s)
	for scanner.Scan() {
		var msg GameMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			log.Printf("Error unmarshaling message from %s: %v", remotePeer.String(), err)
			continue
		}
		p.handleIncomingMessage(remotePeer, msg)
	}

	p.peerMutex.Lock()
	delete(p.peers, remotePeer)
	p.peerMutex.Unlock()

	p.gameStateMutex.Lock()
	delete(p.gameState.Players, remotePeer)
	p.gameStateMutex.Unlock()

	p.determineHost()
	p.peerUpdate <- struct{}{}
}

func (p *PokerClient) handleIncomingMessage(from peer.ID, msg GameMessage) {
	p.gameStateMutex.Lock()
	defer p.gameStateMutex.Unlock()

	switch msg.Type {
	case "join_game":
		p.handleJoinGame(from, msg.PlayerID)
	case "welcome":
		if msg.GameState != nil {
			p.gameState = msg.GameState
			fmt.Println("\n--- Joined Game ---")
			p.printGameState()
		}
	case "game_state_update":
		if msg.GameState != nil {
			p.gameState = msg.GameState
			fmt.Println("\n--- Game State Updated ---")
			p.printGameState()
			fmt.Print("> ")
		}
	case "deal_cards":
		if msg.Cards != nil {
			p.myHand = msg.Cards
			fmt.Printf("📬 You have been dealt your hand: %v\n", p.myHand)
			p.sendCommitment()
		}
	case "shuffle_deck":
		if p.isHost && len(p.gameState.EncryptedDeck) == 0 {
			p.gameState.EncryptedDeck = msg.EncryptedDeck
			p.handleShuffleDeck(msg.EncryptedDeck) // Host also shuffles
		} else {
			p.handleShuffleDeck(msg.EncryptedDeck)
		}
	case "player_action":
		if p.isHost {
			p.handlePlayerAction(from, *msg.Action)
		}
	case "commit":
		if p.isHost {
			var playerID peer.ID
			for id, player := range p.gameState.Players {
				if player.Name == msg.PlayerID {
					playerID = id
					break
				}
			}
			if player, ok := p.gameState.Players[playerID]; ok {
				player.Commitment = msg.Commitment
				fmt.Printf("🔒 Received commitment from %s\n", msg.PlayerID)
				allCommitted := true
				for _, p := range p.gameState.Players {
					if p.InHand && p.Commitment == "" {
						allCommitted = false
						break
					}
				}
				if allCommitted {
					fmt.Println("✅ All players have committed. Starting first betting round.")
					p.broadcastGameState()
				}
			}
		}
	case "decrypt_request":
		p.handleDecryptRequest(from, msg)
	case "card_delivery":
		// Final delivery of partially decrypted cards to the target player
		if msg.Cards != nil { // These are actually encrypted bytes cast to Card? No, let's use EncryptedDeck
			// We need to handle the final decryption
			var hand []game.Card
			for _, encCard := range msg.EncryptedDeck {
				// Decrypt with my key (I am the target)
				decryptedBytes := p.myCipher.XOR(encCard)
				var card game.Card
				if err := json.Unmarshal(decryptedBytes, &card); err != nil {
					log.Printf("Error unmarshaling card: %v", err)
					continue
				}
				hand = append(hand, card)
			}
			p.myHand = hand
			fmt.Printf("📬 You have been dealt your hand: %v\n", p.myHand)
			p.sendCommitment()
		}

	case "chat":
		fmt.Printf("💬 %s: %s\n", msg.PlayerID, msg.Data.(string))
	default:
		fmt.Printf("📨 Unknown message type from %s: %s\n", msg.PlayerID, msg.Type)
	}
}

func (p *PokerClient) sendMessage(msg GameMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Error marshaling message: %v", err)
		return
	}
	p.peerMutex.Lock()
	defer p.peerMutex.Unlock()
	for id, stream := range p.peers {
		if _, err := stream.Write(append(data, '\n')); err != nil {
			log.Printf("Error writing to stream for peer %s: %v", id, err)
			stream.Close()
			delete(p.peers, id)
		}
	}
}

func (p *PokerClient) sendMessageToPeer(peerID peer.ID, msg GameMessage) {
	p.peerMutex.Lock()
	stream, ok := p.peers[peerID]
	p.peerMutex.Unlock()
	if !ok {
		log.Printf("Cannot send message to peer %s: no stream available", peerID)
		return
	}
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Error marshaling message for peer %s: %v", peerID, err)
		return
	}
	if _, err := stream.Write(append(data, '\n')); err != nil {
		log.Printf("Error sending message to %s: %v", peerID, err)
	}
}

func (p *PokerClient) determineHost() {
	p.peerMutex.Lock()
	defer p.peerMutex.Unlock()
	isHost := true
	myID := p.host.ID().String()
	for peerID := range p.peers {
		if peerID.String() < myID {
			isHost = false
			break
		}
	}
	if isHost != p.isHost {
		p.isHost = isHost
		if p.isHost {
			fmt.Println("\n👑 You are now the game host!")
			p.gameState.HostID = p.host.ID()
		} else {
			fmt.Println("\n🙂 You are a client. Waiting for host to start the game.")
		}
	}
}

func (p *PokerClient) printGameState() {
	fmt.Println("\n--- 🃏 POKER GAME STATE ---")
	fmt.Printf("Pot: %d\n", p.gameState.Pot)
	fmt.Printf("Community Cards: %v\n", p.gameState.CommunityCards)
	fmt.Println("Players:")
	for _, player := range p.gameState.Players {
		turnIndicator := ""
		if player.ID == p.gameState.CurrentTurn {
			turnIndicator = " <= YOUR TURN"
		}
		fmt.Printf("  - %s (Stack: %d, Bet: %d)%s\n", player.Name, player.Stack, player.Bet, turnIndicator)
	}
	if p.myHand != nil {
		fmt.Printf("Your Hand: %v\n", p.myHand)
	}
	fmt.Println("--------------------------")
}

func (p *PokerClient) startGame() {
	if !p.isHost {
		fmt.Println("Only the host can start the game.")
		return
	}
	fmt.Println("Starting a new hand...")
	p.resetGameState()
	p.gameState.Phase = game.Shuffling

	deck := game.NewDeck()
	deck.Shuffle()
	var encryptedDeck [][]byte
	for _, card := range deck {
		cardBytes, _ := json.Marshal(card)
		encryptedCard := p.myCipher.XOR(cardBytes)
		encryptedDeck = append(encryptedDeck, encryptedCard)
	}
	p.handleShuffleDeck(encryptedDeck)
}

func (p *PokerClient) handleShuffleDeck(deck [][]byte) {
	fmt.Println("Received deck to shuffle and encrypt.")
	mrand.Seed(time.Now().UnixNano())
	mrand.Shuffle(len(deck), func(i, j int) {
		deck[i], deck[j] = deck[j], deck[i]
	})
	var reEncryptedDeck [][]byte
	for _, cardBytes := range deck {
		encryptedCard := p.myCipher.XOR(cardBytes)
		reEncryptedDeck = append(reEncryptedDeck, encryptedCard)
	}
	playerOrder := p.gameState.GetPlayerOrder()
	myIndex := -1
	for i, id := range playerOrder {
		if id == p.host.ID() {
			myIndex = i
			break
		}
	}
	if myIndex == -1 {
		log.Println("Error: could not find myself in player order.")
		return
	}
	nextIndex := (myIndex + 1) % len(playerOrder)
	nextPlayerID := playerOrder[nextIndex]
	if nextPlayerID == p.gameState.HostID {
		fmt.Println("Deck has been shuffled by all players. Sending back to host.")
		p.gameState.EncryptedDeck = reEncryptedDeck
		p.dealCards()
	} else {
		fmt.Printf("Passing deck to %s to shuffle and encrypt...\n", p.gameState.Players[nextPlayerID].Name)
		p.sendMessageToPeer(nextPlayerID, GameMessage{
			Type:          "shuffle_deck",
			EncryptedDeck: reEncryptedDeck,
		})
	}
}

func (p *PokerClient) dealCards() {
	if !p.isHost {
		return
	}
	fmt.Println("Dealing cards securely...")

	// We need to deal 2 cards to each player.
	// We take cards from the top of EncryptedDeck.

	gs := p.gameState
	deckIdx := 0

	for _, playerID := range gs.GetPlayerOrder() {
		player := gs.Players[playerID]
		if !player.InHand {
			continue
		}

		// Take 2 cards
		if deckIdx+2 > len(gs.EncryptedDeck) {
			log.Println("Not enough cards!")
			return
		}
		cardsToDeal := gs.EncryptedDeck[deckIdx : deckIdx+2]
		deckIdx += 2

		// Initiate decryption chain for this player
		// We send to the first peer in the ring (who is not the target)
		// Actually, we can just send to the next peer, and they check if they are target.
		// If they are target, they skip? No, target must NOT decrypt yet.
		// Target decrypts LAST.

		// Let's send to NextPlayer.
		// Message contains: EncryptedCards, TargetID, DecryptedBy list.

		msg := GameMessage{
			Type:          "decrypt_request",
			EncryptedDeck: cardsToDeal,
			Data: map[string]interface{}{
				"target": playerID.String(),
				"count":  0.0,
			},
		}

		// If I am NOT the target, I decrypt first.
		if p.host.ID() != playerID {
			// Decrypt
			var decrypted [][]byte
			for _, c := range cardsToDeal {
				decrypted = append(decrypted, p.myCipher.XOR(c))
			}
			msg.EncryptedDeck = decrypted
			msg.Data.(map[string]interface{})["count"] = 1.0
		}

		// Send to next player
		nextPeer := p.getNextPeer()
		if nextPeer != "" {
			p.sendMessageToPeer(nextPeer, msg)
		}
	}

	// Also deal community cards (burn and turn) - simplified for now
	// We should also decrypt community cards (everyone decrypts).
	// For now, let's just focus on hands.

	gs.Phase = game.PreFlop
	gs.CurrentTurn = gs.NextPlayerInOrder(gs.GetPlayerOrder(), 0)
	p.broadcastGameState()
}

func (p *PokerClient) handleDecryptRequest(from peer.ID, msg GameMessage) {
	data, ok := msg.Data.(map[string]interface{})
	if !ok {
		return
	}
	targetIDStr := data["target"].(string)
	count := int(data["count"].(float64))

	// Am I the target?
	if p.host.ID().String() == targetIDStr {
		// Check if everyone else has decrypted (numPlayers - 1)
		if count >= len(p.gameState.Players)-1 {
			// Ready for me!
			var hand []game.Card
			for _, encCard := range msg.EncryptedDeck {
				// Decrypt with my key (I am the target)
				decryptedBytes := p.myCipher.XOR(encCard)
				var card game.Card
				if err := json.Unmarshal(decryptedBytes, &card); err != nil {
					log.Printf("Error unmarshaling card: %v", err)
					continue
				}
				hand = append(hand, card)
			}
			p.myHand = hand
			fmt.Printf("📬 You have been dealt your hand: %v\n", p.myHand)
			p.sendCommitment()
			return
		}
		// Not ready yet, pass it on
		p.passDecryptRequest(msg, targetIDStr)
		return
	}

	// I am not the target. Decrypt my layer.
	var decrypted [][]byte
	for _, c := range msg.EncryptedDeck {
		decrypted = append(decrypted, p.myCipher.XOR(c))
	}
	msg.EncryptedDeck = decrypted

	// Increment count
	data["count"] = float64(count + 1)
	msg.Data = data

	// Pass to next
	p.passDecryptRequest(msg, targetIDStr)
}

func (p *PokerClient) passDecryptRequest(msg GameMessage, targetIDStr string) {
	nextPeer := p.getNextPeer()

	// If next is Host, we are done with the ring?
	// Not necessarily. We need to ensure everyone touched it.
	// But if we follow the ring P1->P2->...->Pn->P1, everyone touched it.
	// If I am Pn, next is P1 (Host).

	if nextPeer == p.gameState.HostID {
		// Cycle complete.
		// Host receives it.
		// But wait, `handleDecryptRequest` will be called on Host.
		// Host needs to know it's "Done".
		// Let's add a "sender" field or check if we are Host.
		// If I am Host, and I receive it from Pn, I know it's done?
		// Yes, if I initiated it.

		// Actually, if I am Host, I am the one calling this function?
		// No, this is called by Pn.
		// So Pn sends to Host.
		p.sendMessageToPeer(nextPeer, msg)
		return
	}

	p.sendMessageToPeer(nextPeer, msg)
}

func (p *PokerClient) getNextPeer() peer.ID {
	playerOrder := p.gameState.GetPlayerOrder()
	myIndex := -1
	for i, id := range playerOrder {
		if id == p.host.ID() {
			myIndex = i
			break
		}
	}
	if myIndex == -1 {
		return ""
	}
	nextIndex := (myIndex + 1) % len(playerOrder)
	return playerOrder[nextIndex]
}

func (p *PokerClient) broadcastGameState() {
	p.gameStateMutex.Lock()
	defer p.gameStateMutex.Unlock()
	msg := GameMessage{
		Type:      "game_state_update",
		GameState: p.gameState,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Error marshaling game state: %v", err)
		return
	}
	p.peerMutex.Lock()
	defer p.peerMutex.Unlock()
	for id, stream := range p.peers {
		if _, err := stream.Write(append(data, '\n')); err != nil {
			log.Printf("Error writing to stream for peer %s: %v", id, err)
		}
	}
	p.printGameState()
}

func (p *PokerClient) discoverPeers(roomName string) {
	routingDiscovery := routing.NewRoutingDiscovery(p.dht)
	routingDiscovery.Advertise(p.ctx, roomName)

	peerChan, err := routingDiscovery.FindPeers(p.ctx, roomName)
	if err != nil {
		log.Printf("Failed to find peers: %v", err)
		return
	}
	for peer := range peerChan {
		if peer.ID == p.host.ID() {
			continue
		}
		p.peerMutex.Lock()
		if _, ok := p.peers[peer.ID]; !ok {
			p.peerMutex.Unlock()
			if err := p.host.Connect(p.ctx, peer); err != nil {
				log.Printf("Failed to connect to peer %s: %v", peer.ID, err)
			} else {
				stream, err := p.host.NewStream(p.ctx, peer.ID, PokerProtocol)
				if err != nil {
					log.Printf("Failed to open stream to %s: %v", peer.ID, err)
				} else {
					p.peerMutex.Lock()
					p.peers[peer.ID] = stream
					p.peerMutex.Unlock()
					go p.handleStream(stream)
				}
			}
		} else {
			p.peerMutex.Unlock()
		}
	}
}

func (p *PokerClient) handlePeerUpdates() {
	for {
		select {
		case <-p.peerUpdate:
			p.broadcastGameState()
		case <-p.shutdown:
			return
		}
	}
}

func (p *PokerClient) startCLI() {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Println("\n🎮 Poker Game Commands:")
	fmt.Println("- chat <message>: Send a chat message")
	fmt.Println("- bet <amount>: Place a bet")
	fmt.Println("- call: Call the current bet")
	fmt.Println("- fold: Fold your hand")
	fmt.Println("- check: Check (no bet)")
	fmt.Println("- reveal: Reveal your hand at showdown")
	fmt.Println("- peers: Show connected peers")
	fmt.Println("- start: (Host only) Start a new hand")
	fmt.Println("- quit: Exit the game")
	fmt.Print("> ")
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) == 0 {
			fmt.Print("> ")
			continue
		}
		command := parts[0]

		switch command {
		case "start":
			p.startGame()
		case "bet", "call", "check", "fold", "reveal":
			p.sendPlayerAction(command, 0, nil, "")
		case "chat":
			if len(parts) > 1 {
				message := strings.Join(parts[1:], " ")
				p.sendMessage(GameMessage{Type: "chat", PlayerID: p.playerName, Data: message})
			} else {
				fmt.Println("Usage: chat <message>")
			}
		case "peers":
			p.peerMutex.Lock()
			fmt.Printf("Connected peers: %d\n", len(p.peers))
			for peerID := range p.peers {
				fmt.Printf("- %s\n", peerID.String())
			}
			p.peerMutex.Unlock()
		case "quit":
			fmt.Println("Goodbye!")
			p.Shutdown()
			return
		default:
			fmt.Println("Unknown command.")
		}
		fmt.Print("> ")
	}
}

func (p *PokerClient) Shutdown() {
	close(p.shutdown)
	p.cancel()
	p.peerMutex.Lock()
	for _, stream := range p.peers {
		stream.Close()
	}
	p.peerMutex.Unlock()
	p.dht.Close()
	p.host.Close()
}

func (p *PokerClient) sendPlayerAction(actionType string, amount int, hand []game.Card, salt string) {
	action := &PlayerAction{
		Type:         actionType,
		Amount:       amount,
		RevealedHand: hand,
		RevealedSalt: salt,
	}
	p.sendMessage(GameMessage{
		Type:     "player_action",
		PlayerID: p.playerName,
		Action:   action,
	})
}

func (p *PokerClient) handlePlayerAction(playerID peer.ID, action PlayerAction) {
	player, ok := p.gameState.Players[playerID]
	if !ok {
		return
	}

	// Special handling for Reveal (Showdown)
	if action.Type == "reveal" {
		if p.gameState.Phase != game.Showdown {
			p.sendMessageToPeer(playerID, GameMessage{Type: "error", Data: "Not in showdown phase."})
			return
		}
		expected := p.gameState.Players[playerID].Commitment
		actual := game.GenerateCommitment(action.RevealedHand, action.RevealedSalt)
		if actual != expected {
			fmt.Printf("❌ Player %s revealed invalid hand!\n", player.Name)
			player.InHand = false
		} else {
			fmt.Printf("🃏 Player %s revealed hand: %v\n", player.Name, action.RevealedHand)
			player.Hand = action.RevealedHand
		}

		// Check if all active players have revealed
		allRevealed := true
		for _, p := range p.gameState.Players {
			if p.InHand && p.Hand == nil {
				allRevealed = false
				break
			}
		}
		if allRevealed {
			p.determineWinner()
		}
		return
	}

	// Use GameState.ApplyAction for standard moves
	err := p.gameState.ApplyAction(playerID, action.Type, action.Amount)
	if err != nil {
		log.Printf("Invalid action from %s: %v", player.Name, err)
		p.sendMessageToPeer(playerID, GameMessage{Type: "error", Data: err.Error()})
		return
	}

	p.broadcastGameState()
}

func (p *PokerClient) advancePhase() {
	for _, player := range p.gameState.Players {
		p.gameState.Pot += player.Bet
		player.Bet = 0
	}
	gs := p.gameState
	switch gs.Phase {
	case game.PreFlop:
		gs.Phase = game.Flop
		cards, _ := (&gs.Deck).Deal(3)
		gs.CommunityCards = append(gs.CommunityCards, cards...)
	case game.Flop:
		gs.Phase = game.Turn
		card, _ := (&gs.Deck).Deal(1)
		gs.CommunityCards = append(gs.CommunityCards, card...)
	case game.Turn:
		gs.Phase = game.River
		card, _ := (&gs.Deck).Deal(1)
		gs.CommunityCards = append(gs.CommunityCards, card...)
	case game.River:
		gs.Phase = game.Showdown
		p.determineWinner()
		return
	}
	gs.CurrentBet = 0
	playerOrder := gs.GetPlayerOrder()
	gs.CurrentTurn = gs.NextPlayerInOrder(playerOrder, 0)
	gs.LastRaiser = gs.CurrentTurn
	p.broadcastGameState()
}

func (p *PokerClient) handleJoinGame(from peer.ID, playerName string) {
	if !p.isHost {
		return
	}
	fmt.Printf("Player %s wants to join the game.\n", playerName)
	p.gameState.AddPlayer(&game.Player{
		ID:    from,
		Name:  playerName,
		Stack: 1000,
	})
	p.sendMessageToPeer(from, GameMessage{
		Type:      "welcome",
		PlayerID:  p.playerName,
		GameState: p.gameState,
	})
	p.broadcastGameState()
}

func (p *PokerClient) determineWinner() {
	gs := p.gameState
	var bestHandOfAll game.BestHand
	var winner *game.Player
	allPlayersInHand := []*game.Player{}
	for _, player := range gs.Players {
		if player.InHand && player.Hand != nil {
			allPlayersInHand = append(allPlayersInHand, player)
		}
	}
	if len(allPlayersInHand) == 0 {
		fmt.Println("No players revealed their hands. No winner.")
		p.startGame()
		return
	}
	if len(allPlayersInHand) == 1 {
		winner = allPlayersInHand[0]
		fmt.Printf("🏆 Winner is %s (by default)!\n", winner.Name)
	} else {
		for _, player := range allPlayersInHand {
			playerCards := player.Hand
			all7Cards := append(gs.CommunityCards, playerCards...)
			currentBest := game.Evaluate(all7Cards)
			if bestHandOfAll.Strength == 0 || currentBest.Strength > bestHandOfAll.Strength {
				bestHandOfAll = currentBest
				winner = player
			}
		}
		fmt.Printf("🏆 Winner is %s with a %s!\n", winner.Name, handStrengthToString[bestHandOfAll.Strength])
	}
	if winner != nil {
		winner.Stack += gs.Pot
		gs.Players[winner.ID] = winner
	}
	p.startGame()
}

func (p *PokerClient) sendCommitment() {
	salt := fmt.Sprintf("%d", mrand.Intn(1_000_000_000))
	player := p.gameState.Players[p.host.ID()]
	player.Salt = salt
	commitment := game.GenerateCommitment(p.myHand, salt)
	p.sendMessage(GameMessage{
		Type:       "commit",
		PlayerID:   p.playerName,
		Commitment: commitment,
	})
	fmt.Println("🔒 Your hand has been committed to.")
}

func (p *PokerClient) resetGameState() {
	gs := p.gameState
	gs.Deck = game.NewDeck()
	gs.Deck.Shuffle()
	gs.EncryptedDeck = nil
	gs.CommunityCards = []game.Card{}
	gs.Pot = 0
	gs.CurrentBet = 0
	gs.Phase = game.Idle
	gs.LastRaiser = ""
	gs.CurrentTurn = ""
	for _, player := range gs.Players {
		player.Bet = 0
		player.HasActed = false
		player.InHand = true
		player.Commitment = ""
		player.Hand = nil
	}
}

func main() {
	bootstrapAddr := flag.String("bootstrap", "", "Address of the bootstrap node")
	isHost := flag.Bool("host", false, "Set to true to run as the host")
	playerName := flag.String("name", "", "Your player name")
	flag.Parse()

	if *playerName == "" {
		*playerName = fmt.Sprintf("Player_%d", mrand.Intn(1000))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	priv, _, err := crypto.GenerateKeyPairWithReader(crypto.Ed25519, 2048, rand.Reader)
	if err != nil {
		log.Fatal(err)
	}

	h, err := libp2p.New(
		libp2p.Identity(priv),
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"),
		libp2p.EnableNATService(),
		libp2p.EnableRelay(),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer h.Close()

	dht, err := dht.New(ctx, h)
	if err != nil {
		log.Fatal(err)
	}
	defer dht.Close()

	client := NewPokerClient(h, dht, *isHost, *playerName, ctx, cancel)

	if *bootstrapAddr != "" {
		if err := client.connectToBootstrap(*bootstrapAddr); err != nil {
			log.Printf("Failed to connect to bootstrap node: %v", err)
		}
	}

	go client.discoverPeers(*roomName)
	go client.handlePeerUpdates()

	fmt.Println("Poker client started. Your ID is:", h.ID().String())
	fmt.Println("Type 'help' for a list of commands.")

	client.startCLI()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	fmt.Println("\nShutting down...")
	client.Shutdown()
}
