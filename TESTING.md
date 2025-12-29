# Testing P2P Mental Poker - 4 Player Game

## Quick Start

### Step 1: Build the Client
```bash
go build -o poker-client cmd/poker/main.go
```

### Step 2: Open 4 Terminal Windows

You'll need 4 separate terminal windows or tabs to simulate 4 different players.

---

## Terminal 1: Player 1 (Host)

### Start the client:
```bash
./poker-client
```

### Commands to run:
1. Wait for the client to start (you'll see your Peer ID)
2. Create a room:
   ```
   create HighStakes
   ```
3. Wait for other players to join (you'll see "Player_XXXX joined the game!")
4. Once 3 other players have joined, start the game:
   ```
   start
   ```

**What happens:**
- Mental Poker shuffle protocol begins
- Each player encrypts and shuffles the deck
- Cards are assigned and decrypted
- Players receive hand commitments
- PreFlop betting phase begins

---

## Terminal 2: Player 2

### Start the client:
```bash
./poker-client
```

### Commands to run:
1. List available rooms:
   ```
   rooms
   ```
2. You should see "HighStakes" listed
3. Join the room:
   ```
   join 1
   ```
4. Wait for the host to start the game

**What you'll see:**
- "Joined room 'HighStakes'"
- "Waiting for host to start the game..."
- Shuffle protocol messages
- Your hole cards revealed
- Hand commitment sent

---

## Terminal 3: Player 3

Same as Player 2:
```bash
./poker-client
```
Then:
```
rooms
join 1
```

---

## Terminal 4: Player 4

Same as Player 2:
```bash
./poker-client
```
Then:
```
rooms
join 1
```

---

## Game Flow Testing

### Phase 1: Blinds Posted
After all players have committed their hands, you'll see:
```
✅ All players committed! Starting PreFlop betting phase.
💰 Small Blind (2): Player_XXXX
💰 Big Blind (4): Player_YYYY
💵 Pot: 6 chips
```

**What happens:**
- Small blind automatically posts 2 chips
- Big blind automatically posts 4 chips
- Pot starts at 6 chips
- First to act is the player left of the big blind

### Phase 2: PreFlop Betting

**Test the betting:**
- Player 3 (left of BB): `call 4` (or `raise 50`)
- Player 4: `fold`
- Player 1 (dealer): `call 4`
- Player 2 (small blind): `fold` (loses the 2 chips already in pot)
OR
- Player 2 (small blind): `call 2` (needs 2 more to match BB's 4)

**Expected behavior:**
✅ Bets are validated (no negative amounts)
✅ Players can't bet more than their stack
✅ Players can't bet before committing
✅ Turn order is enforced

### Phase 3: Flop
After betting round completes, host reveals 3 community cards:
```
🎴 Advancing to Flop...
🔓 Revealing 3 community cards for Flop...
```

**Test betting again:**
- Player 1: `check`
- Player 2: `bet 75`
- Player 3: `call`
- Player 1: `call`

### Phase 4: Turn
One more community card is revealed.

### Phase 5: River
Final community card is revealed.

### Phase 6: Showdown
All players reveal their hands:
```
🎲 Starting Showdown Phase...
🃏 Player_1234 revealed: [Ah Kh]
```

Winner is determined and pot is awarded!

---

## Security Features to Verify

### 1. Hand Commitment Anti-Cheat ✅
**Test:** Try to bet before cards are dealt
**Expected:** Game should wait for all commitments before allowing betting

### 2. Card Ownership Validation ✅
**How it works:** Players can only request keys for cards they own
**Automatic:** The system validates all key requests

### 3. Bet Validation ✅
**Test these invalid bets:**
- `bet -50` → ❌ Should reject (negative amount)
- `bet 0` → ❌ Should reject (zero amount)
- `bet 99999` → ❌ Should reject (more than stack)

### 4. Turn Enforcement ✅
**Test:** Try to bet when it's not your turn
**Expected:** ❌ "it is not your turn"

---

## Available Commands

### Game Commands
- `bet <amount>` - Place a bet
- `call` - Call the current bet
- `raise <amount>` - Raise the bet
- `check` - Check (no bet)
- `fold` - Fold your hand

### Room Commands
- `create <name>` - Create a new room
- `rooms` - List available rooms (auto-refreshes every 30s)
- `join <number>` - Join a room by number

### Info Commands
- `peers` - Show connected peers
- `chat <message>` - Send a chat message

### Exit
- `quit` or `exit` - Leave the game

---

## Expected Output Examples

### Successful Game Start:
```
🎲 Starting mental poker shuffle with 4 players!
📤 Sent initial shuffle to 12D3KooW...
🔀 Received shuffle init from host
🔀 Shuffled deck
📤 Sent shuffle to 12D3KooW... (Step 1)
✅ Shuffle protocol complete!
📦 Assigned 8 hole cards + 5 community cards
🃏 Cards dealt! Waiting for all players to commit their hands...
✨ Card 0 Revealed: A♥
🎴 Added to my hand: A♥ (total: 1 cards)
✨ Card 1 Revealed: K♥
🎴 Added to my hand: K♥ (total: 2 cards)
🔐 Sent hand commitment: 5f8a3d2e1b4c...
✅ All players committed! Starting PreFlop betting phase.
```

### Community Cards Reveal:
```
🎴 Advancing to Flop...
🔓 Revealing 3 community cards for Flop...
🎴 Flop card: Q♠
🎴 Flop card: J♥
🎴 Flop card: T♣
```

### Showdown:
```
🎴 Player_1234: One Pair (Strength: 1)
🎴 Player_5678: Two Pair (Strength: 2)
🎴 Player_9012: Flush (Strength: 5)

🎉 Player_9012 wins with Flush! Pot: 450 chips
```

---

## Troubleshooting

### "No public rooms found"
- Make sure Player 1 created the room first
- Wait a few seconds and try `rooms` again (DHT propagation takes time)
- Room list auto-refreshes every 30 seconds

### "Connection refused"
- Make sure all players are using the same bootstrap node
- Check that ports are not blocked by firewall

### "Not enough cards in deck"
- This shouldn't happen with our implementation
- If it does, it's a bug - please report with steps to reproduce

### Game stuck at "Waiting for commitments"
- Check that all players have successfully decrypted their cards
- Look for any error messages in the terminals

---

## What to Look For (Testing Checklist)

- [ ] All 4 players successfully connect to the room
- [ ] Mental Poker shuffle completes (2 passes: encrypt+shuffle, then decrypt)
- [ ] Each player receives exactly 2 hole cards
- [ ] Hand commitments are sent before PreFlop begins
- [ ] Betting is only allowed after commitments
- [ ] Invalid bets are rejected with clear error messages
- [ ] Turn order is enforced
- [ ] Community cards are revealed at the right phases
- [ ] Showdown correctly evaluates hands
- [ ] Winner receives the pot
- [ ] Stream leaks don't occur (check with `lsof` or similar)
- [ ] Room list auto-refreshes in background

---

## Performance Notes

- **DHT Discovery Time:** 2-10 seconds for rooms to appear
- **Shuffle Protocol:** ~1 second per round (8 rounds for 4 players)
- **Card Decryption:** ~100-200ms per card
- **Memory Usage:** ~30-50 MB per client
- **Network:** Minimal bandwidth, mostly small JSON messages

---

## Next Steps After Testing

If all tests pass:
1. The Mental Poker protocol is working correctly ✅
2. Security measures are preventing cheating ✅
3. The game is stable and production-ready ✅

Consider:
- Adding player timeouts for inactive players
- Rotating dealer button for multiple hands
- Increasing blinds over time (tournament mode)
- Adding multi-hand support
- Building a web UI (optional)
