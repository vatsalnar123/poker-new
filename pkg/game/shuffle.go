package game

import (
	"fmt"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/vatsalnarula123/poker-new/pkg/crypto"
)

// ShufflePhase represents the current phase of the shuffle protocol
type ShufflePhase string

const (
	PhaseEncrypt ShufflePhase = "encrypt" // First pass: encrypt + shuffle
	PhaseDecrypt ShufflePhase = "decrypt" // Second pass: decrypt only
	PhaseDone    ShufflePhase = "done"    // Shuffle complete
)

// ShuffleState tracks the progress of the distributed shuffle.
type ShuffleState struct {
	MyID         peer.ID
	Participants []peer.ID
	CurrentStep  int
	Phase        ShufflePhase
	Cipher       *crypto.Cipher
}

// NewShuffleState creates a new shuffle state.
func NewShuffleState(myID peer.ID, participants []peer.ID, secret string) *ShuffleState {
	return &ShuffleState{
		MyID:         myID,
		Participants: participants,
		CurrentStep:  0,
		Phase:        PhaseEncrypt,
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
		// Use the ORIGINAL card index as salt for consistency
		salt := []byte(fmt.Sprintf("%d", card.OriginalIdx))
		processedDeck[i] = EncryptedCard{
			Data:        ss.Cipher.XOR(card.Data, salt),
			OriginalIdx: card.OriginalIdx,
		}
	}

	ss.CurrentStep++

	// Update phase based on current step
	numPlayers := len(ss.Participants)
	if ss.CurrentStep <= numPlayers {
		ss.Phase = PhaseEncrypt
	} else if ss.CurrentStep <= numPlayers*2 {
		ss.Phase = PhaseDecrypt
	} else {
		ss.Phase = PhaseDone
	}

	return processedDeck, "", nil
}

// PerformShuffle shuffles the deck (only during encryption phase).
func (ss *ShuffleState) PerformShuffle(deck EncryptedDeck) {
	deck.Shuffle()
}

// ShouldShuffle returns true if we should shuffle in the current phase.
func (ss *ShuffleState) ShouldShuffle() bool {
	return ss.Phase == PhaseEncrypt
}

// IsComplete returns true if the shuffle protocol is complete.
func (ss *ShuffleState) IsComplete() bool {
	return ss.Phase == PhaseDone
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
