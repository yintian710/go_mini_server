package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
)

func hashPassword(raw string) string {
	if raw == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func verifyPassword(hashed, raw string) bool {
	if hashed == "" {
		return raw == ""
	}
	return hashed == hashPassword(raw)
}

func generateRoomNo() (string, error) {
	lengthInt, err := rand.Int(rand.Reader, big.NewInt(3))
	if err != nil {
		return "", fmt.Errorf("rand length: %w", err)
	}

	length := 4 + int(lengthInt.Int64())
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}

	v := make([]byte, length)
	for i := range b {
		if i == 0 {
			v[i] = '1' + (b[i] % 9)
			continue
		}
		v[i] = '0' + (b[i] % 10)
	}

	return string(v), nil
}

func generateActivationCode() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	v := strings.ToUpper(base64.RawStdEncoding.EncodeToString(b))
	if len(v) > 16 {
		v = v[:16]
	}
	return v, nil
}
