package util

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// GenerateID generates a unique ID
func GenerateID() string {
	return GenerateIDWithPrefix("")
}

// GenerateIDWithPrefix generates a unique ID with a prefix
func GenerateIDWithPrefix(prefix string) string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	id := hex.EncodeToString(bytes)
	if prefix != "" {
		return prefix + "_" + id
	}
	return id
}

// GenerateShortID generates a shorter ID (8 chars)
func GenerateShortID() string {
	bytes := make([]byte, 4)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// GenerateClientID generates a client order ID
func GenerateClientID() string {
	return "cid_" + time.Now().UTC().Format("20060102150405") + "_" + GenerateShortID()
}

// GenerateRequestID generates a request ID
func GenerateRequestID() string {
	return "req_" + GenerateShortID()
}