package game

import (
	"fmt"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/vatsalnarula123/poker-new/pkg/crypto"
)

// KeyProvider generates decryption keys for specific cards
// Each player has their own KeyProvider with their secret cipher
type KeyProvider struct {
	cipher *crypto.Cipher
}

// NewKeyProvider creates a new key provider with the given cipher
func NewKeyProvider(cipher *crypto.Cipher) *KeyProvider {
	return &KeyProvider{cipher: cipher}
}

// GenerateKeyForCard creates a decryption key for a specific encrypted card
// This key can be sent to another player to help them decrypt the card
func (kp *KeyProvider) GenerateKeyForCard(encryptedCard EncryptedCard) []byte {
	// Use the ORIGINAL card index as salt for consistency
	salt := []byte(fmt.Sprintf("%d", encryptedCard.OriginalIdx))
	return kp.cipher.XOR(encryptedCard.Data, salt)
}

// KeyRequest represents a request for a decryption key for a specific card
type KeyRequest struct {
	CardIndex  int     // Which card in the deck
	ForPlayer  peer.ID // Who is requesting the key
	FromPlayer peer.ID // Who should provide the key
}

// KeyResponse contains a decryption key for a specific card
type KeyResponse struct {
	CardIndex  int     // Which card this key is for
	ForPlayer  peer.ID // Who this key is for
	FromPlayer peer.ID // Who sent this key
	Key        []byte  // The decryption key
}

// CardAssignment represents assigning a card to a specific player
// This is broadcast to all players so everyone knows who owns which card
type CardAssignment struct {
	CardIndex int     // Position in the shuffled deck
	OwnerID   peer.ID // Player who owns this card
}
