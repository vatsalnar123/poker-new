package game

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/vatsalnarula123/poker-new/pkg/crypto"
)

// Deck represents a collection of cards.
type Deck []Card

// NewDeck creates a standard 52-card deck.
func NewDeck() Deck {
	deck := make(Deck, 52)
	i := 0
	for suit := Spade; suit <= Heart; suit++ {
		for rank := Two; rank <= Ace; rank++ {
			deck[i] = Card{Suit: suit, Rank: rank}
			i++
		}
	}
	return deck
}

// Shuffle randomizes the order of the cards in the deck.
func (d Deck) Shuffle() {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	r.Shuffle(len(d), func(i, j int) {
		d[i], d[j] = d[j], d[i]
	})
}

// Deal removes n cards from the top of the deck and returns them.
func (d *Deck) Deal(n int) ([]Card, error) {
	if len(*d) < n {
		return nil, fmt.Errorf("not enough cards in deck to deal %d", n)
	}
	hand := (*d)[:n]
	*d = (*d)[n:]
	return hand, nil
}

// EncryptedCard represents a card that has been encrypted.
type EncryptedCard []byte

// EncryptedDeck represents a deck of encrypted cards.
type EncryptedDeck []EncryptedCard

// Encrypt encrypts the deck using the given cipher.
func (d Deck) Encrypt(c *crypto.Cipher) EncryptedDeck {
	encrypted := make(EncryptedDeck, len(d))
	for i, card := range d {
		// Convert card to bytes (e.g., "Ah" for Ace of Hearts)
		cardBytes := []byte(card.String())
		encrypted[i] = c.XOR(cardBytes)
	}
	return encrypted
}

// Decrypt decrypts the encrypted deck using the given cipher.
// Note: This may return a partially decrypted deck (still EncryptedDeck)
// or a fully decrypted Deck depending on the protocol state.
// Here we return bytes, and the caller decides if it's a valid Card.
func (ed EncryptedDeck) Decrypt(c *crypto.Cipher) [][]byte {
	decrypted := make([][]byte, len(ed))
	for i, ec := range ed {
		decrypted[i] = c.XOR(ec)
	}
	return decrypted
}

// Shuffle randomizes the order of the encrypted cards.
func (ed EncryptedDeck) Shuffle() {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	r.Shuffle(len(ed), func(i, j int) {
		ed[i], ed[j] = ed[j], ed[i]
	})
}
