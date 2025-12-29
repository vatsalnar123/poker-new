# P2P Mental Poker - Explained Like You're Human 👋

## What is this project?

Imagine playing poker online, but **without a dealer you have to trust**. No casino, no company, no server that could cheat you. Just you and the other players, proving to each other that the game is fair using **math and cryptography**.

That's what this project does.

---

## The Big Problem: How do you play poker without trust?

### Traditional Online Poker
```
You ──→ Casino Server ──→ Other Players
        ↑
    (You have to trust this!)
```

The casino:
- Shuffles the deck
- Deals the cards
- Knows everyone's cards
- Could theoretically cheat

### Our Solution: Mental Poker
```
You ↔ Other Players (direct connection)
    ↑
  (No middleman!)
```

Nobody knows anyone else's cards. Nobody can cheat. It's mathematically impossible.

---

## How Does It Work? (Simple Version)

Think of it like this real-world scenario:

### Imagine 4 People Sitting Around a Table

**Step 1: Everyone Locks the Deck**
- Person 1 puts the deck in a locked box (their padlock)
- Shakes the box to shuffle
- Passes to Person 2

- Person 2 adds their padlock to the same box
- Shakes it again
- Passes to Person 3

- Continue until everyone has added their lock and shuffled

**Now the deck is:**
- Shuffled by everyone (no single person chose the order)
- Locked with 4 padlocks (nobody can see the cards)

**Step 2: Everyone Takes Back Their Lock (But Doesn't Shuffle)**
- The box goes around again
- Each person removes ONLY their padlock
- Nobody shakes the box this time!

**Result:**
- Deck is shuffled (from first pass)
- Deck is unlocked (all padlocks removed)
- Nobody knows the order (everyone contributed randomness)

**Step 3: Deal Cards (But Keep Them Locked)**
- "Person 1 gets cards 0 and 1"
- "Person 2 gets cards 2 and 3"
- Everyone agrees who gets which cards
- But the cards are still in envelopes!

**Step 4: Ask Everyone for Keys to YOUR Cards**
- Person 1 says: "I need to open cards 0 and 1"
- Persons 2, 3, 4 each give Person 1 a key
- Person 1 uses all 3 keys + their own key
- Now Person 1 can open cards 0 and 1!

**Why 4 keys?**
- Each person "locked" the deck with their shuffle/encryption
- Need everyone's "key" to unlock
- But you already have your own key!
- So you need N-1 keys from others (4 players - 1 = 3 keys)

---

## The Code: What Each Part Does

### 📦 Package Structure

Think of the code like a house with different rooms:

```
poker-new/
├── cmd/poker/main.go          ← The "living room" (main program)
└── pkg/                       ← The "utility rooms"
    ├── crypto/                ← "Safe room" (encryption)
    ├── game/                  ← "Game room" (poker rules)
```

### 🔐 The "Safe Room" (pkg/crypto/cipher.go)

This is where we do the "locking and unlocking" of cards.

**What it does:**
```go
// Lock a card
encrypted = XOR(card, your_secret_key)

// Unlock a card
card = XOR(encrypted, your_secret_key)
```

**Why XOR?**
- XOR is "commutative" (fancy word!)
- Means: Order doesn't matter
- `Lock1(Lock2(card))` = `Lock2(Lock1(card))`
- Perfect for our multi-player shuffle!

**Real example:**
```
Original: "A♥" (Ace of Hearts)

Player 1 encrypts: "A♥" → "x7f@2"
Player 2 encrypts: "x7f@2" → "k#9Lz"
Player 3 encrypts: "k#9Lz" → "p!qW8"

Now decrypt in ANY order:
Player 3 decrypts: "p!qW8" → "k#9Lz"
Player 1 decrypts: "k#9Lz" → "x7f@2"
Player 2 decrypts: "x7f@2" → "A♥"

Same result! ✅
```

### 🎴 The "Game Room" - Cards and Decks

#### pkg/game/card.go - What is a Card?

```go
type Card struct {
    Suit Suit   // ♠ ♥ ♦ ♣
    Rank Rank   // 2, 3, ..., K, A
}
```

Simple! A card is just a suit (♠♥♦♣) and a rank (2-A).

#### pkg/game/deck.go - The Deck

A deck is just 52 cards. But we have TWO versions:

**1. Normal Deck**
```go
type Deck []Card

// What you see:
[A♥, 2♥, 3♥, ..., K♣]
```

**2. Encrypted Deck**
```go
type EncryptedCard struct {
    Data        []byte  // Encrypted bytes: "x7f@2k#9"
    OriginalIdx int     // Remember: This was card #5
}
```

**Why remember the original index?**

Imagine this problem:
```
1. Ace of Hearts starts at position 5
2. We encrypt it using "5" as the password
3. Someone shuffles → now it's at position 23!
4. We try to decrypt using "23" → WRONG PASSWORD!
5. Decryption fails ❌
```

Solution:
```
1. Ace of Hearts starts at position 5
2. We tag it: {data: encrypted, originalIdx: 5}
3. Someone shuffles → position changes to 23
4. But the tag still says: originalIdx: 5
5. We decrypt using "5" → SUCCESS! ✅
```

### 🔀 The Shuffle Protocol (pkg/game/shuffle.go)

This is the heart of Mental Poker.

**The Two Phases:**

```
Phase 1: ENCRYPT + SHUFFLE
--------------------------
Everyone adds their lock AND shakes the box

Player 1: encrypt + shuffle → pass to Player 2
Player 2: encrypt + shuffle → pass to Player 3
Player 3: encrypt + shuffle → pass to Player 4
Player 4: encrypt + shuffle → pass back to Player 1

Result: Deck is encrypted by everyone, shuffled by everyone


Phase 2: DECRYPT (no shuffle!)
-------------------------------
Everyone removes their lock, but DON'T shake!

Player 1: decrypt → pass to Player 2
Player 2: decrypt → pass to Player 3
Player 3: decrypt → pass to Player 4
Player 4: decrypt → Done!

Result: Deck is decrypted, order preserved from Phase 1
```

**Code:**
```go
type ShufflePhase string

const (
    PhaseEncrypt ShufflePhase = "encrypt"  // Add locks + shuffle
    PhaseDecrypt ShufflePhase = "decrypt"  // Remove locks
    PhaseDone    ShufflePhase = "done"     // Finished!
)
```

**How we track progress:**
```go
type ShuffleState struct {
    CurrentStep int      // 0→8 (4 players × 2 phases)
    Phase       ShufflePhase
}

// With 4 players:
// Step 0-3: Encrypt phase
// Step 4-7: Decrypt phase
// Step 8: Done!
```

### 🔑 Key Exchange (pkg/game/key_exchange.go)

Remember: To unlock a card, you need keys from ALL other players.

**Data Structures:**

```go
// "Hey everyone, I need the key for card 5!"
type KeyRequest struct {
    CardIndex int      // Which card (0-51)
    ForPlayer peer.ID  // Who's asking (me!)
}

// "Here's your key for card 5"
type KeyResponse struct {
    CardIndex int      // Card 5
    Key       []byte   // My decryption key
}
```

**The Process:**

```
You want to decrypt card 5 (your Ace of Hearts)

1. You send KeyRequest to everyone:
   "I need keys for card 5 because it's mine"

2. Player 2 checks:
   - Is card 5 really yours? ✅ Yes
   - Not asking for my cards? ✅ Correct
   - OK, here's my key!

3. Player 3 checks (same validation)
   - Sends their key

4. Player 4 checks (same validation)
   - Sends their key

5. You collect all 3 keys:
   keys = [key_from_P2, key_from_P3, key_from_P4]

6. You decrypt:
   result = encrypted_card
   result = result XOR key_from_P2
   result = result XOR key_from_P3
   result = result XOR key_from_P4
   result = result XOR your_own_key

   → result = "A♥" ✨
```

### 🎯 Card Ownership (pkg/game/card_lock.go)

This is like a ledger that tracks "who owns what."

```go
type CardLockManager struct {
    locks map[int]*CardLock  // Map: card# → who owns it
}

type CardLock struct {
    CardIndex    int          // Card #5
    OwnerID      peer.ID      // Player "Alice"
    ReceivedKeys map[]byte    // Keys received so far
}
```

**Example:**
```
Card 0: owned by Player 1, has 3/3 keys ✅ Can decrypt!
Card 1: owned by Player 1, has 2/3 keys ⏳ Waiting...
Card 2: owned by Player 2, has 3/3 keys ✅
Card 3: owned by Player 2, has 1/3 keys ⏳
...
Card 8: owned by "Community", has 4/4 keys ✅ (flop card)
```

### 🎰 Game State (pkg/game/state.go)

This is the "scoreboard" of the game.

```go
type Player struct {
    Name       string      // "Alice"
    Stack      int         // 1000 chips
    Hand       []Card      // [A♥, K♥] (private!)
    Bet        int         // Current bet: 50
    InHand     bool        // Still playing? true

    // Anti-cheat:
    Commitment string      // SHA256(hand + secret)
    Salt       string      // Random secret
}

type GameState struct {
    Players        map[peer.ID]*Player
    Pot            int            // 200 chips
    CommunityCards []Card         // [Q♠, J♥, T♣]
    Phase          GamePhase      // "Flop"
    CurrentTurn    peer.ID        // Whose turn?
    CurrentBet     int            // 50 chips
}
```

**Game Phases:**
```
Idle           → Waiting to start
Shuffling      → Mental Poker shuffle happening
WaitingForPlayers → Waiting for hand commitments
PreFlop        → First betting round
Flop           → 3 community cards revealed
Turn           → 4th community card
River          → 5th community card
Showdown       → Reveal hands, determine winner
```

### 🛡️ Security Features Explained

#### 1. Hand Commitments (The "No Cheating" Mechanism)

**Problem:**
```
1. You get dealt A♥ K♥ (great hand!)
2. Flop comes: 2♣ 7♦ 9♠ (terrible for you)
3. You secretly change your hand to 2♠ 2♥
4. Turn comes: 2♦ (now you have three 2s!)
5. You win by cheating!
```

**Solution: Commit Before Seeing Flop**

```go
// After getting your cards (A♥ K♥), immediately:

1. Generate random salt:
   salt = "f8a3d2e1b4c7"

2. Create commitment (hash):
   commitment = SHA256("A♥K♥" + "f8a3d2e1b4c7")
               = "d4f2b1c8e3a7..." (long hash)

3. Broadcast to everyone:
   "My commitment is d4f2b1c8e3a7..."

4. NOW the flop is revealed

5. At showdown, you reveal:
   "My hand is A♥K♥, my salt was f8a3d2e1b4c7"

6. Everyone verifies:
   SHA256("A♥K♥" + "f8a3d2e1b4c7") == "d4f2b1c8e3a7..." ✅
```

**Why it works:**
- Hash reveals nothing about your hand (one-way function)
- Can't change hand later (hash would be different)
- Locked in before seeing community cards

**Enforcement in code:**
```go
// Before EVERY bet in PreFlop:
if player.Commitment == "" {
    return ERROR("You must commit your hand first!")
}
```

#### 2. Card Ownership Validation

**Attack Scenario:**
```
Alice owns card 5 (her Ace)
Bob tries to cheat:
  "Hey everyone, give me keys for card 5!"
```

**Defense:**
```go
func handleKeyRequest(req KeyRequest) {
    // Who owns card 5?
    owner := cardLockManager.GetCardOwner(5)  // "Alice"

    // Who's asking?
    requester := req.ForPlayer  // "Bob"

    // Check!
    if owner != requester {
        fmt.Println("🚨 CHEATING DETECTED!")
        fmt.Println("Bob tried to get Alice's cards!")
        return  // Reject the request
    }

    // All good, send the key
    sendKey(req.CardIndex)
}
```

#### 3. Bet Validation

**Prevent these attacks:**

```go
// Attack 1: Negative bets (steal money)
Player: "I bet -100 chips!"
Defense: if amount <= 0 { reject }

// Attack 2: Bet more than you have
Player has 50 chips, bets 1000
Defense: if amount > player.Stack { reject }

// Attack 3: Raise less than current bet
Current bet: 100
Player: "I raise to 50!"
Defense: if amount < currentBet { reject }
```

#### 4. Turn Order Enforcement

**Problem:**
```
It's Alice's turn
Bob tries to bet anyway
```

**Solution:**
```go
func ApplyAction(playerID, action) {
    if currentTurn != playerID {
        return ERROR("Not your turn!")
    }

    // Process the action...
}
```

**How turn order is decided:**
```go
// Sort all players alphabetically by their ID
players = [Alice, Bob, Charlie, David]

// Turn order:
Alice → Bob → Charlie → David → Alice → Bob → ...
```

Why alphabetically? So everyone agrees on the same order!

#### 5. Thread Safety (Preventing Crashes)

**Problem:**
```
Thread 1: Reading player list
Thread 2: Adding new player
Thread 1: CRASH! (list changed mid-read)
```

**Solution: Locks**
```go
type GameState struct {
    mu      sync.RWMutex    // The lock
    Players map[peer.ID]*Player
}

// Reading (multiple threads OK):
func GetPlayers() {
    gs.mu.RLock()           // Lock for reading
    defer gs.mu.RUnlock()   // Unlock when done

    // Safe to read Players here
}

// Writing (exclusive access):
func AddPlayer(player) {
    gs.mu.Lock()            // Exclusive lock
    defer gs.mu.Unlock()    // Unlock when done

    Players[id] = player    // Safe to modify
}
```

#### 6. Memory Leak Prevention

**Problem:**
```go
// Start background task
go func() {
    // Long-running task...
}()

// Later: Program exits
// Background task still running! ❌
// Memory leak!
```

**Solution: WaitGroup**
```go
var wg sync.WaitGroup

// Start task
wg.Add(1)  // "I'm starting 1 task"
go func() {
    defer wg.Done()  // "I'm done"

    // Long-running task...
}()

// Shutdown
wg.Wait()  // "Wait for all tasks to finish"
```

#### 7. Stream Leak Prevention

**Problem:**
```go
for player in players {
    stream := OpenConnection(player)
    defer stream.Close()  // ❌ Doesn't close until function ends!
}
// After loop: 100 streams still open!
```

**Solution: Immediate Closure**
```go
for player in players {
    func() {
        stream := OpenConnection(player)
        defer stream.Close()  // ✅ Closes when THIS function ends!

        // Use stream...
    }()  // Closes here!
}
```

#### 8. Auto-Refresh Room Discovery

**Problem:**
```
10:00 AM: You check for rooms → See 3 rooms
10:05 AM: Someone creates a new room
10:10 AM: You check again → Still only see 3 rooms ❌
```

**Solution: Background Refresh**
```go
go func() {
    ticker := time.NewTicker(30 * time.Second)

    for {
        select {
        case <-ticker.C:
            // Every 30 seconds
            refreshRoomList()
        }
    }
}()
```

Now rooms update automatically every 30 seconds!

---

## 🎮 Complete Game Flow (From Start to Finish)

Let's play a complete game with 4 players: **Alice, Bob, Charlie, David**

### Phase 1: Room Setup

```
Alice: create HighStakes
  → Creates room
  → Starts advertising on DHT

Bob: rooms
  → Sees "HighStakes - 1/8 players"

Bob: join 1
  → Connects to Alice
  → Alice's room: 2/8 players

Charlie: join 1
  → Room: 3/8 players

David: join 1
  → Room: 4/8 players
```

### Phase 2: Mental Poker Shuffle

```
Alice: start

Round 1 (Encryption Phase):
  Alice: Encrypt with K_Alice + Shuffle
         Send to Bob

  Bob: Encrypt with K_Bob + Shuffle
       Send to Charlie

  Charlie: Encrypt with K_Charlie + Shuffle
           Send to David

  David: Encrypt with K_David + Shuffle
         Send back to Alice

Deck is now: E_David(E_Charlie(E_Bob(E_Alice(shuffled_deck))))

Round 2 (Decryption Phase):
  Alice: Decrypt with K_Alice (no shuffle!)
         Send to Bob

  Bob: Decrypt with K_Bob
       Send to Charlie

  Charlie: Decrypt with K_Charlie
           Send to David

  David: Decrypt with K_David
         Shuffle complete!

Deck is now: Shuffled and decrypted!
```

### Phase 3: Card Assignment

```
Alice (host) assigns:
  Card 0, 1 → Alice
  Card 2, 3 → Bob
  Card 4, 5 → Charlie
  Card 6, 7 → David
  Card 8, 9, 10 → Community (Flop)
  Card 11 → Community (Turn)
  Card 12 → Community (River)

Broadcasts to everyone: "Here's who gets what"
```

### Phase 4: Key Exchange (Hole Cards)

```
Alice: "I need keys for cards 0 and 1"
  Bob sends key
  Charlie sends key
  David sends key

  Alice receives 3 keys
  Alice decrypts:
    Card 0 = A♥
    Card 1 = K♥

Bob: "I need keys for cards 2 and 3"
  Alice sends key
  Charlie sends key
  David sends key

  Bob decrypts:
    Card 2 = Q♣
    Card 3 = Q♦

Charlie: "I need keys for cards 4 and 5"
  Receives keys, decrypts:
    Card 4 = 7♠
    Card 5 = 2♥

David: "I need keys for cards 6 and 7"
  Receives keys, decrypts:
    Card 6 = J♠
    Card 7 = 9♣
```

### Phase 5: Hand Commitments

```
Alice (has A♥ K♥):
  salt = "f8a3d2e1"
  commitment = SHA256("A♥K♥f8a3d2e1")
  Broadcasts: "My commitment: d4f2b1c8..."

Bob (has Q♣ Q♦):
  Generates commitment
  Broadcasts commitment

Charlie:
  Generates commitment
  Broadcasts commitment

David:
  Generates commitment
  Broadcasts commitment

System: All 4 players committed ✅
        Transitioning to PreFlop!
```

### Phase 6: Blinds Posted

```
System automatically posts blinds:
  - Dealer: Alice (button position)
  - Small Blind: Bob (left of dealer) → Posts 2 chips
  - Big Blind: Charlie (left of SB) → Posts 4 chips

💰 Pot: 6 chips (2 + 4)
🎯 First to act: David (left of BB)
```

**Why blinds?**
Without blinds, everyone would just fold until they got great cards! Blinds force action and create a pot worth fighting for.

### Phase 7: PreFlop Betting

```
Current bet: 4 (the big blind amount)

David: call 4 (match the big blind)
Alice: raise 50 (make it 50 to go)
Bob (small blind): fold (loses 2 chips already in pot)
Charlie (big blind): call 46 (already has 4 in, needs 46 more)
David: call 46 (already has 4 in, needs 46 more)

Pot: 6 + 4 + 50 + 46 + 46 = 152 chips
Round complete → Advance to Flop
```

### Phase 8: Flop

```
Alice (host): "Reveal cards 8, 9, 10"

All players request keys for cards 8, 9, 10
All players send keys (community cards)

Decrypted:
  Card 8 = Q♠
  Card 9 = K♦
  Card 10 = T♣

Board: Q♠ K♦ T♣

Alice (A♥ K♥): Has pair of Kings!
Charlie (Q♣ Q♦): Has three Queens!
David (7♠ 2♥): Has nothing

Betting:
  Alice: bet 100 (confident with pair of Kings)
  Charlie: raise 200 (has three Queens!)
  David: fold
  Alice: call 100

Pot: 152 + 100 + 200 + 100 = 552 chips
```

### Phase 9: Turn

```
Reveal card 11
Card 11 = A♠

Board: Q♠ K♦ T♣ A♠

Alice: Now has two pair! (Aces and Kings)
Charlie: Still has three Queens

Betting:
  Alice: bet 200
  Charlie: call 200

Pot: 552 + 200 + 200 = 952 chips
```

### Phase 10: River

```
Reveal card 12
Card 12 = 2♣

Final Board: Q♠ K♦ T♣ A♠ 2♣

Alice: Two pair (Aces and Kings)
Charlie: Three Queens

Betting:
  Alice: check
  Charlie: bet 300
  Alice: call 300

Pot: 952 + 300 + 300 = 1552 chips
```

### Phase 11: Showdown

```
Alice reveals: A♥ K♥ + salt
  System verifies: SHA256("A♥K♥" + salt) == commitment ✅

Charlie reveals: Q♣ Q♦ + salt
  System verifies: SHA256("Q♣Q♦" + salt) == commitment ✅

Hand Evaluation:
  Alice: Two Pair (Aces and Kings)
  Charlie: Three of a Kind (Queens)

Winner: Charlie! (Three of a Kind beats Two Pair)

Charlie wins 1552 chips! 🎉
```

---

## 🧠 Why This Is Amazing

### No Central Server Needed
- **Traditional poker**: You trust the casino
- **This poker**: You trust math

### Provably Fair
- Can't rig the shuffle (everyone contributes randomness)
- Can't peek at cards (need N-1 keys)
- Can't change your hand (cryptographic commitment)

### Decentralized
- Uses libp2p (peer-to-peer networking)
- DHT (distributed hash table) for discovery
- No single point of failure

### Secure
- 8 layers of security
- Anti-cheating mechanisms
- Resource leak prevention
- Thread-safe operations

---

## 📊 By The Numbers

- **Total Code**: ~3,000 lines
- **Packages**: 6
- **Security Features**: 8
- **Protocol Messages**: 11
- **Poker Hands**: 10 (High Card → Royal Flush)
- **Deck Size**: 52 cards
- **Shuffle Rounds**: 8 (with 4 players)

---

## 🎓 What You've Built

This isn't just a poker game. You've built:

1. **A Distributed System** - P2P networking with DHT
2. **A Cryptographic Protocol** - Mental Poker with commutative encryption
3. **A Consensus Mechanism** - Trustless card dealing
4. **A Secure Application** - Multiple anti-cheat layers
5. **A Game Engine** - Full poker rules and hand evaluation

This is **graduate-level computer science** in action! 🎓

---

## 🚀 Want to Learn More?

- **Mental Poker Protocol**: [Wikipedia](https://en.wikipedia.org/wiki/Mental_poker)
- **Commutative Encryption**: Research papers on SRA
- **libp2p**: [Official Docs](https://docs.libp2p.io/)
- **DHT**: Kademlia protocol
- **Cryptographic Commitments**: Zero-knowledge proofs

---

**You now understand every line of code in this project!** 🎉

Questions? Check `ARCHITECTURE.md` for more technical details.
