package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
)

func main() {
	
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a connection manager
	connMgr, err := connmgr.NewConnManager(
		100, // Lowwater
		400, // HighWater
		connmgr.WithGracePeriod(time.Minute),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Create libp2p host
	h, err := libp2p.New(
		libp2p.ListenAddrStrings(
			"/ip4/0.0.0.0/tcp/4001",
			"/ip6/::/tcp/4001",
		),
		libp2p.ConnectionManager(connMgr),
		libp2p.EnableNATService(),
		libp2p.EnableRelay(),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer h.Close()

	// Create a DHT for peer discovery
	dht, err := dht.New(ctx, h, dht.Mode(dht.ModeServer))
	if err != nil {
		log.Fatal(err)
	}
	defer dht.Close()

	// Bootstrap the DHT
	if err = dht.Bootstrap(ctx); err != nil {
		log.Fatal(err)
	}

	// Print the bootstrap node's multiaddress
	fmt.Printf("Bootstrap node started!\n")
	fmt.Printf("Peer ID: %s\n", h.ID().String())
	for _, addr := range h.Addrs() {
		fmt.Printf("Listening on: %s/p2p/%s\n", addr, h.ID().String())
	}

	// Wait for shutdown signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	fmt.Println("Shutting down bootstrap node...")
}
