#!/bin/bash

# Test script to run 4 poker instances
# Usage: ./test_4players.sh

echo "🎰 Starting 4-Player P2P Poker Test"
echo "===================================="
echo ""

# Build the poker client
echo "📦 Building poker client..."
go build -o poker-client cmd/poker/main.go
if [ $? -ne 0 ]; then
    echo "❌ Build failed!"
    exit 1
fi
echo "✅ Build successful"
echo ""

# Bootstrap node address (you'll need to replace this with actual bootstrap)
BOOTSTRAP="/ip4/127.0.0.1/tcp/4001/p2p/12D3KooWBhxVN8eNqZXRJJbvSMVHYQZXmv8CFgJbp8rFLJSVJKKr"

echo "To test the poker game:"
echo "1. Open 4 separate terminal windows"
echo "2. In each terminal, run one of these commands:"
echo ""
echo "Terminal 1 (Player 1 - Host):"
echo "  ./poker-client $BOOTSTRAP"
echo "  Then type: create TestRoom"
echo ""
echo "Terminal 2 (Player 2):"
echo "  ./poker-client $BOOTSTRAP"
echo "  Then type: rooms"
echo "  Then type: join 1"
echo ""
echo "Terminal 3 (Player 3):"
echo "  ./poker-client $BOOTSTRAP"
echo "  Then type: rooms"
echo "  Then type: join 1"
echo ""
echo "Terminal 4 (Player 4):"
echo "  ./poker-client $BOOTSTRAP"
echo "  Then type: rooms"
echo "  Then type: join 1"
echo ""
echo "5. In Terminal 1 (host), type: start"
echo ""
echo "Available commands during game:"
echo "  - bet <amount>  : Place a bet"
echo "  - call          : Call current bet"
echo "  - fold          : Fold your hand"
echo "  - check         : Check (if no bet)"
echo "  - chat <msg>    : Send chat message"
echo "  - peers         : Show connected peers"
echo ""
