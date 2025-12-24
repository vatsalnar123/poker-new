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

// XOR applies the XOR operation to the data using a key stream derived from the secret.
// The key stream is generated using SHAKE128 to match the length of the data.
func (c *Cipher) XOR(data []byte) []byte {
	// Create a SHAKE128 hash that can generate a key stream of any length.
	h := sha3.NewShake128()
	// Write the secret to the hash to seed the key stream generation.
	h.Write(c.secret)

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
