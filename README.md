# ♠️ P2P Poker – Decentralized Trustless Poker Platform

A decentralized, serverless multiplayer poker game built with **Go** and **libp2p**. It features a **trustless shuffle protocol (SRA)** and a synchronized **game state machine**, ensuring fair play without a central server.

## 🎯 Project Overview

**P2P Poker** allows players to join a private room and play Texas Hold'em directly over the internet.
-   **No Central Server**: Peer discovery via Kademlia DHT.
-   **Trustless**: Cryptographic shuffle ensures no one knows the deck order.
-   **Resilient**: Game state is synchronized across all peers.

---

## 🧱 Core Features

-   **Trustless Shuffle**: Implements the **SRA (Shamir-Rivest-Adleman) Mental Poker Protocol** using a commutative XOR cipher.
    -   Sequential encryption and shuffling by all players.
    -   Secure multi-layer decryption for dealing cards.
-   **Game State Machine**: Robust logic for handling turns, betting rounds (PreFlop, Flop, Turn, River), and Showdown.
-   **P2P Networking**:
    -   **Discovery**: Kademlia DHT for finding peers in a room.
    -   **Communication**: Encrypted libp2p streams for game messages.
-   **Deployment**: Dockerized setup for easy testing and deployment.

---

## 🛠️ Tech Stack

-   **Language**: Go 1.22+
-   **Networking**: `go-libp2p` (DHT, Streams, NAT Traversal)
-   **Cryptography**: Custom Commutative XOR Cipher (based on SHAKE128)
-   **Infrastructure**: Docker, Docker Compose

---

## 🚀 Getting Started

### Prerequisites
-   Go 1.22+
-   Docker (optional)

### Build
```bash
go build -o poker ./cmd/poker
go build -o bootstrap ./cmd/bootstrap
```

### Run Locally

1.  **Start Bootstrap Node**
    ```bash
    ./bootstrap
    # Copy the multiaddr, e.g., /ip4/127.0.0.1/tcp/4001/p2p/Qm...
    ```

2.  **Start Host (Player 1)**
    ```bash
    ./poker -host -name Alice -bootstrap /ip4/127.0.0.1/tcp/4001/p2p/Qm...
    ```

3.  **Start Client (Player 2)**
    ```bash
    ./poker -name Bob -bootstrap /ip4/127.0.0.1/tcp/4001/p2p/Qm...
    ```

4.  **Play**
    -   **Host**: Type `start` to begin the hand.
    -   **Commands**: `bet <amount>`, `call`, `check`, `fold`, `reveal`, `chat <msg>`.

---

## 📂 Project Structure

```
├── cmd/
│   ├── poker/          # Main game client
│   └── bootstrap/      # Bootstrap node
├── pkg/
│   ├── game/           # Game logic (State, Deck, Shuffle)
│   └── crypto/         # Cryptographic primitives (XOR Cipher)
├── p2p_poker.go        # Networking & Message Handling
├── Dockerfile          # Multi-stage build
└── docker-compose.yml  # Local cluster setup
```
