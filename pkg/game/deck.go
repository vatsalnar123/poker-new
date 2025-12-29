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
// It includes the original index to maintain salt consistency across shuffles.
type EncryptedCard struct {
	Data         []byte // The encrypted card data
	OriginalIdx  int    // Original position in deck (for salt consistency)
}

// EncryptedDeck represents a deck of encrypted cards.
type EncryptedDeck []EncryptedCard

// Encrypt encrypts the deck using the given cipher.
func (d Deck) Encrypt(c *crypto.Cipher) EncryptedDeck {
	encrypted := make(EncryptedDeck, len(d))
	for i, card := range d {
		// Convert card to bytes (e.g., "Ah" for Ace of Hearts)
		cardBytes := []byte(card.String())
		// Use card index as salt for unique per-card keys
		salt := []byte(fmt.Sprintf("%d", i))
		encrypted[i] = EncryptedCard{
			Data:        c.XOR(cardBytes, salt),
			OriginalIdx: i,
		}
	}
	return encrypted
}

// Decrypt decrypts the encrypted deck using the given cipher.
func (ed EncryptedDeck) Decrypt(c *crypto.Cipher) [][]byte {
	decrypted := make([][]byte, len(ed))
	for i, ec := range ed {
		// Use ORIGINAL index as salt for consistency
		salt := []byte(fmt.Sprintf("%d", ec.OriginalIdx))
		decrypted[i] = c.XOR(ec.Data, salt)
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

// DecryptCard decrypts a single card using keys from all players
// This is used for selective reveal in mental poker
// keys should contain decryption keys from all OTHER players (not the owner)
func (ed EncryptedDeck) DecryptCard(index int, keys [][]byte) ([]byte, error) {
	if index < 0 || index >= len(ed) {
		return nil, fmt.Errorf("card index %d out of range", index)
	}

	// Start with the encrypted card data
	result := ed[index].Data

	// Apply each key (XOR with each player's key)
	// Since XOR is self-inverse, this removes each encryption layer
	for _, key := range keys {
		// XOR the result with this key
		decrypted := make([]byte, len(result))
		for i := range result {
			if i < len(key) {
				decrypted[i] = result[i] ^ key[i]
			} else {
				decrypted[i] = result[i]
			}
		}
		result = decrypted
	}

	return result, nil
}

// ParseCard converts decrypted bytes back to a Card
func ParseCard(data []byte) (Card, error) {
	cardStr := string(data)
	if len(cardStr) < 2 {
		return Card{}, fmt.Errorf("invalid card string: %s", cardStr)
	}

	// Parse rank (first character(s))
	var rank Rank
	var suitChar byte

	if len(cardStr) == 2 {
		// Single character rank (2-9, T, J, Q, K, A)
		switch cardStr[0] {
		case '2':
			rank = Two
		case '3':
			rank = Three
		case '4':
			rank = Four
		case '5':
			rank = Five
		case '6':
			rank = Six
		case '7':
			rank = Seven
		case '8':
			rank = Eight
		case '9':
			rank = Nine
		case 'T':
			rank = Ten
		case 'J':
			rank = Jack
		case 'Q':
			rank = Queen
		case 'K':
			rank = King
		case 'A':
			rank = Ace
		default:
			return Card{}, fmt.Errorf("invalid rank: %c", cardStr[0])
		}
		suitChar = cardStr[1]
	} else if len(cardStr) == 3 && cardStr[0] == '1' && cardStr[1] == '0' {
		// "10" rank
		rank = Ten
		suitChar = cardStr[2]
	} else {
		return Card{}, fmt.Errorf("invalid card format: %s", cardStr)
	}

	// Parse suit
	var suit Suit
	switch {
	case suitChar == 's' || string(suitChar) == "♠":
		suit = Spade
	case suitChar == 'h' || string(suitChar) == "♥":
		suit = Heart
	case suitChar == 'd' || string(suitChar) == "♦":
		suit = Diamond
	case suitChar == 'c' || string(suitChar) == "♣":
		suit = Club
	default:
		return Card{}, fmt.Errorf("invalid suit: %c", suitChar)
	}

	return Card{Suit: suit, Rank: rank}, nil
}
