package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
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
)

const (
	PokerProtocol = "/poker/1.0.0"
	RoomName      = "poker-room-abc"
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

// PokerClient represents a poker player client
type PokerClient struct {
	host       host.Host
	dht        *dht.IpfsDHT
	ctx        context.Context
	cancel     context.CancelFunc
	peers      map[peer.ID]network.Stream
	playerName string
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
		host:       h,
		dht:        dht,
		ctx:        ctx,
		cancel:     cancel,
		peers:      make(map[peer.ID]network.Stream),
		playerName: fmt.Sprintf("Player_%s", h.ID().String()[:8]),
	}

	// Set stream handler for incoming connections
	h.SetStreamHandler(PokerProtocol, client.handleStream)

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
	defer s.Close()

	remotePeer := s.Conn().RemotePeer()
	log.Printf("New connection from %s", remotePeer.String())

	// Store the stream for this peer
	p.peers[remotePeer] = s

	// Read messages from the stream
	scanner := bufio.NewScanner(s)
	for scanner.Scan() {
		var msg GameMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			log.Printf("Error unmarshaling message: %v", err)
			continue
		}

		p.handleGameMessage(msg, remotePeer)
	}

	// Clean up when connection closes
	delete(p.peers, remotePeer)
}

// handleGameMessage processes incoming game messages
func (p *PokerClient) handleGameMessage(msg GameMessage, from peer.ID) {
	switch msg.Type {
	case "join":
		fmt.Printf("🎮 %s joined the game!\n", msg.PlayerID)
	case "bet":
		if data, ok := msg.Data.(map[string]interface{}); ok {
			action := data["action"].(string)
			amount := int(data["amount"].(float64))
			fmt.Printf("💰 %s performed action: %s (amount: %d)\n", msg.PlayerID, action, amount)
		}
	case "chat":
		fmt.Printf("💬 %s: %s\n", msg.PlayerID, msg.Data.(string))
	case "game_state":
		fmt.Printf("🎲 Game state update from %s\n", msg.PlayerID)
	default:
		fmt.Printf("📨 Unknown message type from %s: %s\n", msg.PlayerID, msg.Type)
	}
}

// sendMessage sends a message to all connected peers
func (p *PokerClient) sendMessage(msg GameMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Error marshaling message: %v", err)
		return
	}

	for peerID, stream := range p.peers {
		if _, err := stream.Write(append(data, '\n')); err != nil {
			log.Printf("Error sending message to %s: %v", peerID, err)
			// Remove broken connection
			stream.Close()
			delete(p.peers, peerID)
		}
	}
}

// discoverPeers discovers other players in the poker room
func (p *PokerClient) discoverPeers() {
	fmt.Printf("🔍 Discovering peers in room: %s\n", RoomName)

	// Create CID for the room
	roomCID, err := createCID(RoomName)
	if err != nil {
		log.Printf("Error creating CID for room: %v", err)
		return
	}

	// Advertise this node in the DHT
	go func() {
		for {
			if err := p.dht.Provide(p.ctx, roomCID, true); err != nil {
				log.Printf("Error providing DHT key: %v", err)
			}
			time.Sleep(10 * time.Second)
		}
	}()

	// Look for other peers
	go func() {
		for {
			providers, err := p.dht.FindProviders(p.ctx, roomCID)
			if err != nil {
				log.Printf("Error finding providers: %v", err)
				time.Sleep(5 * time.Second)
				continue
			}

			for _, provider := range providers {
				if provider.ID != p.host.ID() && p.peers[provider.ID] == nil {
					p.connectToPeer(provider)
				}
			}
			time.Sleep(5 * time.Second)
		}
	}()
}

// connectToPeer connects to a discovered peer
func (p *PokerClient) connectToPeer(peerInfo peer.AddrInfo) {
	if err := p.host.Connect(p.ctx, peerInfo); err != nil {
		log.Printf("Failed to connect to peer %s: %v", peerInfo.ID, err)
		return
	}

	stream, err := p.host.NewStream(p.ctx, peerInfo.ID, PokerProtocol)
	if err != nil {
		log.Printf("Failed to create stream to %s: %v", peerInfo.ID, err)
		return
	}

	p.peers[peerInfo.ID] = stream
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

	// Start peer discovery
	p.discoverPeers()

	// Start command interface
	go p.startCommandInterface()

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
	fmt.Println("- chat <message>: Send a chat message")
	fmt.Println("- bet <amount>: Place a bet")
	fmt.Println("- call: Call the current bet")
	fmt.Println("- fold: Fold your hand")
	fmt.Println("- check: Check (no bet)")
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
			if len(parts) > 1 {
				amount := parts[1]
				betAction := BetAction{Action: "bet", Amount: 0}
				if amount != "" {
					// Parse amount (simplified for demo)
					betAction.Amount = 100 // Default amount
				}
				p.sendMessage(GameMessage{
					Type:     "bet",
					PlayerID: p.playerName,
					Data:     betAction,
				})
			}
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
			fmt.Printf("Connected peers: %d\n", len(p.peers))
			for peerID := range p.peers {
				fmt.Printf("- %s\n", peerID.String())
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
	p.cancel()
	for _, stream := range p.peers {
		stream.Close()
	}
	p.dht.Close()
	p.host.Close()
}

func main() {
	// Default bootstrap address (replace with your bootstrap node)
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
