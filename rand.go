package main

import (
	"crypto/rand"
	"math/big"
)

var (
	letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
)

// Random string chars, n=length
func RandText(n int) string {
	b := make([]rune, n)
	for i := range b {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		if err != nil {
			// Fallback should never happen with crypto/rand
			panic("crypto/rand failed: " + err.Error())
		}
		b[i] = letters[idx.Int64()]
	}
	return string(b)
}
