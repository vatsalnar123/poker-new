# ♠️ P2P Poker – A Decentralized Poker Game Built with go-libp2p

## 🎯 Project Overview

**P2P Poker** is a decentralized, serverless implementation of a multiplayer poker game built on top of [go-libp2p](https://github.com/libp2p/go-libp2p). Each player runs a lightweight node that connects directly to other players over the internet using a distributed peer-to-peer (P2P) protocol.

There is **no central server**. Peer discovery, communication, and game state synchronization are handled entirely using libp2p primitives like the Kademlia DHT, peer streams, and optional pubsub.

---

## 🧱 Core Features

- 🔗 **Global Peer Discovery** using libp2p’s DHT
- 💬 **Encrypted Direct Messaging** via libp2p streams
- 🎲 **Real-Time Game Communication** (moves, rounds, bets)
- 📡 **Bootstrap Nodes** for scalable internet connectivity
- 🐳 **Dockerized Deployment** for easy testing and infrastructure setup
- 🔐 **Future**: Cryptographic deck verification (commit-reveal or mental poker)

---

## 🧠 High-Level Architecture

- Each player is a **libp2p host**.
- A shared poker room name (e.g., `poker-room-abc`) is used to advertise presence in the DHT.
- Peers connect to a public **bootstrap node** to join the global network.
- After discovery, nodes establish **libp2p streams** for game communication.
- Game logic is synchronized via protocol messages exchanged over those streams.

[Player A] ---┐
│ → Bootstrap Node (public VPS) ←→ DHT network
[Player B] ---┘
↓
Direct encrypted libp2p stream
↓
Exchange actions, cards, moves

yaml
Copy
Edit

---

## 📦 Project Structure

/bootstrap_node.go → Static bootstrap node for public discovery
/p2p_poker.go → Main poker client (peer logic + game comm)
/Dockerfile → Multi-stage build for both components
/docker-compose.yml → Optional for local testing with multiple peers

yaml
Copy
Edit

---

## 🚀 How It Works

1. **Bootstrap Node**
   - Deployed on a VPS with a public IP.
   - Acts as the fixed entry point into the libp2p DHT.
   - Always online, does not play or hold game state.

2. **Poker Clients**
   - Start a libp2p host.
   - Connect to the bootstrap node.
   - Advertise a shared room name on the DHT.
   - Discover other players in the same room.
   - Open libp2p streams and exchange JSON messages like:
     ```json
     { "action": "bet", "amount": 100 }
     ```

---

## ⚙️ Tech Stack

- **Go 1.22**
- **libp2p** (go-libp2p)
- **Docker** for build/test/deployment
- Optional: `systemd` for long-running bootstrap node on VPS

---

## 📄 Future Roadmap

- 🃏 Full poker game engine with betting rounds and hand ranking
- 🧠 Mental Poker cryptographic shuffling / commit-reveal
- 🔄 Multiplayer state consensus & timeout handling
- 🌐 NAT traversal via relay v2 + hole punching
- 📈 Frontend client (web-based or terminal UI)

---

## 💻 Local Development

```bash
# build image
docker build -t p2p-poker .

# run poker client
docker run --rm --net=host p2p-poker
Or run multiple clients locally with:

bash
Copy
Edit
docker-compose up --build
🌍 Deploying Your Own Bootstrap Node
Deploy bootstrap_node.go on a VPS (e.g., DigitalOcean)

Expose TCP port 4001

Replace the multiaddr in your client config:

bash
Copy
Edit
/ip4/YOUR.IP.ADDR/tcp/4001/p2p/YOUR_PEER_ID
