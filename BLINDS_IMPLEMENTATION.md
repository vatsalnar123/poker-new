# Small and Big Blind Implementation

## Overview
Added automatic small blind (2 chips) and big blind (4 chips) posting to enforce standard poker rules and create forced betting action.

## What Changed

### 1. GameState Structure ([pkg/game/state.go](pkg/game/state.go#L55-L59))

Added blind tracking fields:
```go
// Blind positions and amounts
SmallBlind       peer.ID `json:"small_blind"`        // Player in small blind position
BigBlind         peer.ID `json:"big_blind"`          // Player in big blind position
SmallBlindAmount int     `json:"small_blind_amount"` // Small blind amount (default: 2)
BigBlindAmount   int     `json:"big_blind_amount"`   // Big blind amount (default: 4)
```

### 2. New Functions Added

#### `SetupBlinds()` - [state.go:140-169](pkg/game/state.go#L140-L169)
Assigns blind positions based on dealer button:
- Small Blind = player left of dealer
- Big Blind = player left of small blind

```go
// Example with 4 players:
// Dealer: Alice (position 0)
// Small Blind: Bob (position 1)
// Big Blind: Charlie (position 2)
// First to act: David (position 3)
```

#### `PostBlinds()` - [state.go:171-218](pkg/game/state.go#L171-L218)
Automatically deducts blind amounts and adds to pot:
- Posts 2 chips from small blind player
- Posts 4 chips from big blind player
- Sets CurrentBet to 4 (big blind amount)
- Sets first to act as player left of big blind
- Handles all-in scenarios if player has fewer chips than blind amount

### 3. Integration with Game Flow

Modified [cmd/poker/main.go:846-877](cmd/poker/main.go#L846-L877) in `checkAndStartPreFlop()`:

**Before:**
```go
if allCommitted {
    p.gameState.Phase = game.PreFlop
    fmt.Println("✅ All players committed! Starting PreFlop betting phase.")
}
```

**After:**
```go
if allCommitted {
    p.gameState.Phase = game.PreFlop

    // Setup and post blinds (only host does this)
    if p.isHost() {
        p.gameState.SetupBlinds()
        p.gameState.PostBlinds()

        // Display blind info
        fmt.Printf("💰 Small Blind (%d): %s\n", 2, sbPlayer.Name)
        fmt.Printf("💰 Big Blind (%d): %s\n", 4, bbPlayer.Name)
        fmt.Printf("💵 Pot: %d chips\n", p.gameState.Pot)

        // Broadcast updated state to all players
        p.sendMessageToOthers(GameMessage{
            Type: "state_update",
            Data: p.gameState,
        })
    }
}
```

### 4. State Synchronization

Added `handleStateUpdate()` in [main.go:916-964](cmd/poker/main.go#L916-L964):
- Non-host players receive updated game state
- Synchronizes pot, bets, blinds across all players
- Displays blind information to all players

## Game Flow with Blinds

### Before (No Blinds)
```
✅ All players committed! Starting PreFlop betting phase.
Current bet: 0
Player 1: check
Player 2: check
Player 3: bet 10
```
**Problem:** Everyone just checks until someone has premium cards. Boring!

### After (With Blinds)
```
✅ All players committed! Starting PreFlop betting phase.
💰 Small Blind (2): Player_Bob
💰 Big Blind (4): Player_Charlie
💵 Pot: 6 chips
Current bet: 4

🎯 First to act: Player_David
Player_David: call 4 (or raise 50)
```
**Benefit:** Pot always has 6 chips to start, creating incentive to play hands!

## Example Hand

**Setup:**
- Dealer: Alice
- Small Blind: Bob (posts 2 chips)
- Big Blind: Charlie (posts 4 chips)
- Player: David

**Pot after blinds:** 6 chips

**PreFlop Betting:**
1. David: `call 4` → Total bet: 4, Pot: 10
2. Alice: `raise 50` → Total bet: 50, Pot: 60
3. Bob (SB): `fold` → Loses 2 chips
4. Charlie (BB): `call 46` → Already has 4 in, needs 46 more, Pot: 106
5. David: `call 46` → Pot: 152

**Why it matters:**
- Bob lost 2 chips (small blind) by folding
- Charlie only needed to add 46 more (already had 4 in from big blind)
- Pot starts at 6 chips instead of 0, creating action

## Default Blind Amounts

- **Small Blind:** 2 chips
- **Big Blind:** 4 chips
- **Starting Stack:** 1000 chips (per player)

These are set in [state.go:68-75](pkg/game/state.go#L68-L75):
```go
func NewGameState() *GameState {
    return &GameState{
        Players:          make(map[peer.ID]*Player),
        CommunityCards:   make([]Card, 0),
        Phase:            Idle,
        SmallBlindAmount: 2,  // Default small blind
        BigBlindAmount:   4,  // Default big blind
    }
}
```

## Turn Order with Blinds

**4-Player Example:**

Position | Player | Action Order PreFlop | Action Order Post-Flop
---------|--------|---------------------|----------------------
Button (Dealer) | Alice | 3rd to act | 1st to act
Small Blind | Bob | 4th to act (last) | 2nd to act
Big Blind | Charlie | 1st option to raise | 3rd to act
UTG (Under the Gun) | David | 1st to act | 4th to act (last)

**PreFlop:** David → Alice → Bob → Charlie (BB has option to raise)
**Post-Flop:** Alice → Bob → Charlie → David (button acts last)

## Security Considerations

✅ **Validated:**
- Blind amounts are positive (2 and 4)
- Players have sufficient stack (handles all-in)
- Only host posts blinds (prevents double-posting)
- State synchronized to all players

✅ **Thread-safe:**
- `SetupBlinds()` uses `gs.mu.Lock()`
- `PostBlinds()` uses `gs.mu.Lock()`
- All player stack updates are atomic

## Future Enhancements

1. **Rotating Dealer Button**
   - Currently dealer is set once
   - Should rotate clockwise after each hand

2. **Blind Increases (Tournament Mode)**
   - Increase blinds every N hands
   - Example: Start 2/4, increase to 4/8, 8/16, etc.

3. **Configurable Blinds**
   - Allow custom blind amounts when creating room
   - Support ante (forced bet from all players)

## Files Modified

- ✅ [pkg/game/state.go](pkg/game/state.go) - Added blind fields and functions
- ✅ [cmd/poker/main.go](cmd/poker/main.go) - Integrated blind posting and state sync
- ✅ [EXPLAINED.md](EXPLAINED.md) - Updated game flow examples
- ✅ [TESTING.md](TESTING.md) - Updated testing instructions
- ✅ [.gitignore](.gitignore) - Added poker-client binary and backup files

## Testing

Build and run:
```bash
go build -o poker-client cmd/poker/main.go
./poker-client
```

Expected output when PreFlop starts:
```
✅ All players committed! Starting PreFlop betting phase.
💰 Small Blind (2): Player_Bob
💰 Big Blind (4): Player_Charlie
💵 Pot: 6 chips
🎯 It's your turn! Current bet to call: 4
```

## Summary

✅ **What works:**
- Automatic blind posting
- Pot starts at 6 chips
- First to act is left of big blind
- All players see blind information
- State synchronized across network

🚧 **Not yet implemented:**
- Dealer button rotation (same dealer every hand)
- Blind increases over time
- Configurable blind amounts

The blind system is fully functional and ready for testing!
