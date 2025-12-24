package game

import "fmt"

// Suit represents the suit of a playing card.
type Suit int

// Rank represents the rank of a playing card.
type Rank int

const (
	Spade Suit = iota
	Club
	Diamond
	Heart
)

const (
	Two Rank = iota + 2
	Three
	Four
	Five
	Six
	Seven
	Eight
	Nine
	Ten
	Jack
	Queen
	King
	Ace
)

// Card represents a single playing card with a suit and a rank.
type Card struct {
	Suit Suit `json:"suit"`
	Rank Rank `json:"rank"`
}

func (s Suit) String() string {
	return [...]string{"♠", "♣", "♦", "♥"}[s]
}

func (r Rank) String() string {
	if r >= Two && r <= Ten {
		return fmt.Sprintf("%d", r)
	}
	// For Jack, Queen, King, Ace
	return map[Rank]string{Jack: "J", Queen: "Q", King: "K", Ace: "A"}[r]
}

func (c Card) String() string {
	return fmt.Sprintf("%s%s", c.Rank.String(), c.Suit.String())
}
