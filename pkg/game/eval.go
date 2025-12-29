package game

import "sort"

// HandStrength represents the ranking of a poker hand.
type HandStrength int

const (
	HighCard HandStrength = iota
	OnePair
	TwoPair
	ThreeOfAKind
	Straight
	Flush
	FullHouse
	FourOfAKind
	StraightFlush
	RoyalFlush
)

// String returns the string representation of a HandStrength
func (hs HandStrength) String() string {
	switch hs {
	case HighCard:
		return "High Card"
	case OnePair:
		return "One Pair"
	case TwoPair:
		return "Two Pair"
	case ThreeOfAKind:
		return "Three of a Kind"
	case Straight:
		return "Straight"
	case Flush:
		return "Flush"
	case FullHouse:
		return "Full House"
	case FourOfAKind:
		return "Four of a Kind"
	case StraightFlush:
		return "Straight Flush"
	case RoyalFlush:
		return "Royal Flush"
	default:
		return "Unknown"
	}
}

// BestHand stores the result of a hand evaluation.
type BestHand struct {
	Strength HandStrength
	Cards    []Card // The 5 cards that make up the best hand, sorted high to low.
}

// Evaluate takes a slice of 7 cards (2 private + 5 community) and returns the best 5-card hand.
func Evaluate(allCards []Card) BestHand {
	if len(allCards) != 7 {
		return BestHand{Strength: HighCard} // Should not happen
	}

	var bestHand BestHand

	// Generate all 21 possible 5-card combinations from the 7 cards.
	combinations := generateCombinations(allCards, 5)

	for _, combo := range combinations {
		hand := evaluate5CardHand(combo)
		if hand.Strength > bestHand.Strength {
			bestHand = hand
		} else if hand.Strength == bestHand.Strength {
			// If strengths are equal, we need to compare kickers.
			// The higher-ranked hand is the one with the higher individual cards.
			for i := 0; i < 5; i++ {
				if hand.Cards[i].Rank > bestHand.Cards[i].Rank {
					bestHand = hand
					break
				}
				if hand.Cards[i].Rank < bestHand.Cards[i].Rank {
					break
				}
			}
		}
	}

	return bestHand
}

// evaluate5CardHand determines the strength of a single 5-card hand.
func evaluate5CardHand(hand []Card) BestHand {
	// Sort cards by rank, high to low, which is crucial for tie-breaking.
	sort.Slice(hand, func(i, j int) bool {
		return hand[i].Rank > hand[j].Rank
	})

	isFlush := checkFlush(hand)
	isStraight, highCard := checkStraight(hand)

	if isStraight && isFlush {
		if highCard == Ace {
			return BestHand{Strength: RoyalFlush, Cards: hand}
		}
		return BestHand{Strength: StraightFlush, Cards: hand}
	}

	rankCounts := getRankCounts(hand)
	var majorGroup, minorGroup Rank

	for r, count := range rankCounts {
		if count == 4 {
			return BestHand{Strength: FourOfAKind, Cards: hand}
		}
		if count == 3 {
			majorGroup = r
		}
		if count == 2 {
			minorGroup = r
		}
	}

	if majorGroup != 0 && minorGroup != 0 {
		return BestHand{Strength: FullHouse, Cards: hand}
	}

	if isFlush {
		return BestHand{Strength: Flush, Cards: hand}
	}

	if isStraight {
		return BestHand{Strength: Straight, Cards: hand}
	}

	if majorGroup != 0 {
		return BestHand{Strength: ThreeOfAKind, Cards: hand}
	}

	if len(rankCounts) == 3 { // Two pairs + one kicker = 3 distinct ranks
		return BestHand{Strength: TwoPair, Cards: hand}
	}

	if len(rankCounts) == 4 { // One pair + three kickers = 4 distinct ranks
		return BestHand{Strength: OnePair, Cards: hand}
	}

	return BestHand{Strength: HighCard, Cards: hand}
}

// --- Helper Functions ---

func checkFlush(hand []Card) bool {
	suit := hand[0].Suit
	for i := 1; i < 5; i++ {
		if hand[i].Suit != suit {
			return false
		}
	}
	return true
}

func checkStraight(hand []Card) (bool, Rank) {
	// Check for Ace-low straight (A, 2, 3, 4, 5)
	isAceLow := hand[0].Rank == Ace && hand[1].Rank == Five && hand[2].Rank == Four && hand[3].Rank == Three && hand[4].Rank == Two
	if isAceLow {
		// Reorder cards for Ace-low evaluation (5,4,3,2,A)
		reordered := append(hand[1:], hand[0])
		copy(hand, reordered)
		return true, Five
	}

	for i := 0; i < 4; i++ {
		if hand[i].Rank != hand[i+1].Rank+1 {
			return false, 0
		}
	}
	return true, hand[0].Rank
}

func getRankCounts(hand []Card) map[Rank]int {
	counts := make(map[Rank]int)
	for _, card := range hand {
		counts[card.Rank]++
	}
	return counts
}

// generateCombinations is a helper to generate all n-card combos from a set of cards.
func generateCombinations(cards []Card, n int) [][]Card {
	var result [][]Card
	var f func(int, []Card)
	f = func(start int, combo []Card) {
		if len(combo) == n {
			newCombo := make([]Card, n)
			copy(newCombo, combo)
			result = append(result, newCombo)
			return
		}
		if start >= len(cards) {
			return
		}
		// Include cards[start]
		f(start+1, append(combo, cards[start]))
		// Exclude cards[start]
		f(start+1, combo)
	}
	f(0, []Card{})
	// We generate more than needed, filter to get exact size N combinations
	finalResult := [][]Card{}
	for _, r := range result {
		if len(r) == n {
			finalResult = append(finalResult, r)
		}
	}
	return finalResult
}
