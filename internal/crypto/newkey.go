package crypto

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// CreateKey makes a new random key and writes it to path, replacing a key file
// that is there. The caller has to be sure nothing still needs the old key (see
// `app reset-key`, which clears what the old key held and keeps a copy first).
func CreateKey(path string) (*Box, error) {
	key := make([]byte, keySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate secret key: %w", err)
	}
	if err := writeKeyFile(path, []byte(base64.StdEncoding.EncodeToString(key))); err != nil {
		return nil, fmt.Errorf("write secret key: %w", err)
	}
	return &Box{key: key}, nil
}
