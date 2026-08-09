package core

import (
	"encoding/hex"
	"fmt"
	"strings"
)

// Addresses are a consensus-level concept — a commitment to a public key that
// appears in every output — so their encoding lives here rather than in the
// wallet. That lets programs which only ever read the chain, such as the block
// explorer, render addresses without importing any key-handling code at all.

// EncodeAddress renders an address as "per1" + 64 hex characters + an
// 8-character checksum.
func EncodeAddress(a [32]byte) string {
	chk := H([]byte("PER:chk"), a[:])
	return "per1" + hex.EncodeToString(a[:]) + hex.EncodeToString(chk[:4])
}

// DecodeAddress parses an encoded address and verifies its checksum, so a
// mistyped address is rejected rather than sending coins into the void.
func DecodeAddress(s string) ([32]byte, error) {
	var a [32]byte
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "per1") || len(s) != 4+64+8 {
		return a, fmt.Errorf("invalid address format")
	}
	payload, err := hex.DecodeString(s[4 : 4+64])
	if err != nil {
		return a, fmt.Errorf("invalid address encoding")
	}
	copy(a[:], payload)
	chk := H([]byte("PER:chk"), a[:])
	if s[4+64:] != hex.EncodeToString(chk[:4]) {
		return a, fmt.Errorf("address checksum mismatch — please re-check for typos")
	}
	return a, nil
}
