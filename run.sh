#!/bin/sh

# Exit on error
set -e

# 1. Build the Docker image
echo "Building the p2p-poker image..."
docker-compose build

# 2. Start the bootstrap node in detached mode
echo "Starting bootstrap node..."
docker-compose up -d bootstrap

# 3. Get the bootstrap container's Peer ID
echo "Fetching bootstrap node's Peer ID..."
# Wait a moment for the node to start and print its ID
sleep 3
BOOTSTRAP_PEER_ID=$(docker-compose logs bootstrap | grep "Peer ID" | awk -F': ' '{print $2}' | tail -n 1)

if [ -z "$BOOTSTRAP_PEER_ID" ]; then
    echo "Error: Could not retrieve Peer ID from bootstrap node."
    docker-compose logs bootstrap
    docker-compose down
    exit 1
fi

echo "Bootstrap Peer ID: ${BOOTSTRAP_PEER_ID}"

# 4. Set the Peer ID as an environment variable for other services
export BOOTSTRAP_PEER_ID

# 5. Start the poker clients and attach to them
echo "Starting poker clients..."
docker-compose up --scale poker=2

# 6. Clean up on exit
echo "Shutting down all services..."
docker-compose down 