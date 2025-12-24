# Use a multi-stage build to create a small final image.

# --- Build Stage ---
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy go.mod and go.sum files and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the bootstrap node and the poker client
RUN go build -o /bootstrap ./cmd/bootstrap
RUN go build -o /poker ./cmd/poker


# --- Final Stage ---
FROM alpine:latest

# Copy the built binaries from the builder stage
COPY --from=builder /bootstrap /usr/local/bin/bootstrap
COPY --from=builder /poker /usr/local/bin/poker

# Expose the bootstrap node's default port
EXPOSE 4001

# The entrypoint will be set in docker-compose to select which binary to run.
ENTRYPOINT ["/bin/sh"] 