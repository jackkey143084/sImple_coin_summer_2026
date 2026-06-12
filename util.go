package main

import (
	"crypto/sha256"
	"encoding/hex"
	"crypto/rand"
)

func Hash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func randReader() *rand.Reader { r := rand.Reader; return &r }

