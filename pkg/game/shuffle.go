package game

import (
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/vatsalnarula123/poker-new/pkg/crypto"
)

// ShuffleState tracks the progress of the distributed shuffle.
type ShuffleState struct {
	MyID         peer.ID
	Participants []peer.ID
	CurrentStep  int
	Cipher       *crypto.Cipher
}

// NewShuffleState creates a new shuffle state.
func NewShuffleState(myID peer.ID, participants []peer.ID, secret string) *ShuffleState {
	return &ShuffleState{
		MyID:         myID,
		Participants: participants,
		CurrentStep:  0,
		Cipher:       crypto.NewCipher(secret),
	}
}

// HandleShuffleMessage processes a shuffle message and returns the next action.
// Returns the modified deck (if any) and the next peer to send it to.
// If nextPeer is empty, the shuffle is complete.
func (ss *ShuffleState) HandleShuffleMessage(deck EncryptedDeck) (EncryptedDeck, peer.ID, error) {
	// numPlayers := len(ss.Participants)

	// Determine which step we are in based on the received deck?
	// Actually, the state should be synchronized or implied.
	// For simplicity, we assume the message flow is strictly sequential:
	// P1 -> P2 -> ... -> Pn -> P1 -> P2 -> ... -> Pn -> Done

	// We need to know "who sent this" to know if it's our turn?
	// Or we just assume if we received it, it's our turn.

	// Logic:
	// If CurrentStep < numPlayers: Encryption Phase
	// If CurrentStep >= numPlayers: Decryption Phase

	// But wait, CurrentStep needs to track the GLOBAL step.
	// If I am P2, and I receive from P1, GlobalStep is 1 (P1 finished).
	// So I perform step 2.

	// We need to track the global step count.
	// Let's assume the message includes the step number, or we infer it.
	// For now, let's just increment our local counter of "times I touched the deck"?
	// No, that's ambiguous.

	// Let's rely on the caller to update ss.CurrentStep based on network messages.
	// But here, let's just implement the transformation logic.

	// Re-encrypt (XOR) the deck
	// This applies both for encryption (adding a layer) and decryption (removing a layer)
	// because XOR is its own inverse!
	// Encrypt: D ^ K
	// Decrypt: (D ^ K) ^ K = D

	// So the operation is ALWAYS the same: XOR with our key.
	// The only difference is whether we shuffle.
	// We shuffle during the Encryption phase (first pass).
	// We do NOT shuffle during the Decryption phase (second pass), otherwise we break the order for others?
	// Wait, SRA with commutative encryption:
	// P1 Encrypts -> Shuffle -> P2 Encrypts -> Shuffle -> ...
	// Decryption:
	// P1 Decrypts -> P2 Decrypts -> ...
	// If P1 decrypts, the cards are still encrypted by P2..Pn.
	// If P1 shuffles during decryption, P2 won't know which card is which to decrypt?
	// Actually, with commutative encryption, order doesn't matter for decryption *value*,
	// but if we want to preserve the *shuffled order*, we must NOT shuffle during decryption.

	processedDeck := make(EncryptedDeck, len(deck))
	for i, card := range deck {
		processedDeck[i] = ss.Cipher.XOR(card)
	}

	// If we are in the first pass (Encryption), we shuffle.
	// How do we know if it's the first pass?
	// We can track how many times we've seen the deck.
	// But `ShuffleState` is local.
	// Let's add a `Pass` field.

	ss.CurrentStep++

	// Heuristic: If we are just starting or in the first loop.
	// Let's assume the caller manages the phase.
	// But for this function, let's add a `shuffle` param?
	// Or better, let the caller call `Shuffle()` on the returned deck if needed.

	return processedDeck, "", nil
}

// PerformShuffle shuffles the deck.
func (ss *ShuffleState) PerformShuffle(deck EncryptedDeck) {
	deck.Shuffle()
}

// GetNextPeer returns the peer ID of the next player in the ring.
func (ss *ShuffleState) GetNextPeer() peer.ID {
	myIndex := -1
	for i, p := range ss.Participants {
		if p == ss.MyID {
			myIndex = i
			break
		}
	}

	if myIndex == -1 {
		return ""
	}

	nextIndex := (myIndex + 1) % len(ss.Participants)
	return ss.Participants[nextIndex]
}
