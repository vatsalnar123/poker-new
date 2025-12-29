package crypto

import (
	"golang.org/x/crypto/sha3"
)

// Cipher is a simple commutative stream cipher using SHAKE128.
type Cipher struct {
	secret []byte
}

// NewCipher creates a new cipher instance from a secret passphrase.
func NewCipher(secret string) *Cipher {
	return &Cipher{secret: []byte(secret)}
}

// XOR applies the XOR operation to the data using a key stream derived from the secret and a salt.
// The salt (e.g., card index) ensures that each piece of data (each card) has a unique key stream.
func (c *Cipher) XOR(data []byte, salt []byte) []byte {
	// Create a SHAKE128 hash that can generate a key stream of any length.
	h := sha3.NewShake128()
	// Write the secret and the salt to the hash to seed the key stream generation.
	h.Write(c.secret)
	h.Write(salt)

	// Create a key stream of the same length as the data.
	keyStream := make([]byte, len(data))
	h.Read(keyStream)

	// XOR the data with the key stream.
	result := make([]byte, len(data))
	for i := range data {
		result[i] = data[i] ^ keyStream[i]
	}

	return result
}
