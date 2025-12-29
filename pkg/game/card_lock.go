package game

import (
	"fmt"

	"github.com/libp2p/go-libp2p/core/peer"
)

// CardLock represents a card that has been assigned to a specific player
// The card remains encrypted until all required keys are collected
type CardLock struct {
	CardIndex    int                // Position in the shuffled deck
	OwnerID      peer.ID            // Player who owns this card
	ReceivedKeys map[peer.ID][]byte // Decryption keys from other players
}

// CardLockManager manages card ownership and key collection
// It tracks which cards belong to which players and collects decryption keys
type CardLockManager struct {
	locks        map[int]*CardLock // Map of card index to lock
	participants []peer.ID         // All players in the game
	myID         peer.ID           // This player's ID
}

// NewCardLockManager creates a new card lock manager
func NewCardLockManager(myID peer.ID, participants []peer.ID) *CardLockManager {
	return &CardLockManager{
		locks:        make(map[int]*CardLock),
		participants: participants,
		myID:         myID,
	}
}

// LockCard assigns a card at the given index to a specific player
// The card remains encrypted at this point
func (clm *CardLockManager) LockCard(cardIndex int, ownerID peer.ID) {
	clm.locks[cardIndex] = &CardLock{
		CardIndex:    cardIndex,
		OwnerID:      ownerID,
		ReceivedKeys: make(map[peer.ID][]byte),
	}
}

// AddKey stores a decryption key from another player for a specific card
// Returns error if the card is not locked
func (clm *CardLockManager) AddKey(cardIndex int, fromPlayer peer.ID, key []byte) error {
	lock, exists := clm.locks[cardIndex]
	if !exists {
		return fmt.Errorf("card %d is not locked", cardIndex)
	}

	// Store the key from this player
	lock.ReceivedKeys[fromPlayer] = key
	return nil
}

// CanReveal checks if we have all keys needed to reveal a card
// A player needs keys from all OTHER players (not themselves)
func (clm *CardLockManager) CanReveal(cardIndex int) bool {
	lock, exists := clm.locks[cardIndex]
	if !exists {
		return false
	}

	// We need keys from all players except ourselves
	neededKeys := len(clm.participants) - 1
	return len(lock.ReceivedKeys) >= neededKeys
}

// GetLock returns the lock for a specific card
func (clm *CardLockManager) GetLock(cardIndex int) (*CardLock, bool) {
	lock, exists := clm.locks[cardIndex]
	return lock, exists
}

// IsMyCard checks if a card belongs to this player
func (clm *CardLockManager) IsMyCard(cardIndex int) bool {
	lock, exists := clm.locks[cardIndex]
	if !exists {
		return false
	}
	return lock.OwnerID == clm.myID
}

// GetMyCards returns all card indices that belong to this player
func (clm *CardLockManager) GetMyCards() []int {
	myCards := make([]int, 0)
	for cardIndex, lock := range clm.locks {
		if lock.OwnerID == clm.myID {
			myCards = append(myCards, cardIndex)
		}
	}
	return myCards
}

// GetCardOwner returns the owner of a specific card
func (clm *CardLockManager) GetCardOwner(cardIndex int) (peer.ID, error) {
	lock, exists := clm.locks[cardIndex]
	if !exists {
		return "", fmt.Errorf("card %d is not locked", cardIndex)
	}
	return lock.OwnerID, nil
}

// GetKeysForCard returns all collected keys for a specific card
func (clm *CardLockManager) GetKeysForCard(cardIndex int) ([][]byte, error) {
	lock, exists := clm.locks[cardIndex]
	if !exists {
		return nil, fmt.Errorf("card %d is not locked", cardIndex)
	}

	keys := make([][]byte, 0, len(lock.ReceivedKeys))
	for _, key := range lock.ReceivedKeys {
		keys = append(keys, key)
	}
	return keys, nil
}
