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
	return Active().AddressPrefix + hex.EncodeToString(a[:]) + hex.EncodeToString(chk[:4])
}

// DecodeAddress parses an encoded address and verifies its checksum, so a
// mistyped address is rejected rather than sending coins into the void.
func DecodeAddress(s string) ([32]byte, error) {
	var a [32]byte
	s = strings.TrimSpace(s)
	prefix := Active().AddressPrefix
	if !strings.HasPrefix(s, prefix) || len(s) != len(prefix)+64+8 {
		// Give a precise error when the address belongs to another network:
		// that is the mistake this separation exists to catch.
		for _, n := range []*Network{Mainnet, Testnet, Regtest} {
			if n != Active() && strings.HasPrefix(s, n.AddressPrefix) && len(s) == len(n.AddressPrefix)+64+8 {
				return a, fmt.Errorf("this is a %s address; this node runs on %s", n.Name, Active().Name)
			}
		}
		return a, fmt.Errorf("invalid address format")
	}
	payload, err := hex.DecodeString(s[len(prefix) : len(prefix)+64])
	if err != nil {
		return a, fmt.Errorf("invalid address encoding")
	}
	copy(a[:], payload)
	chk := H([]byte("PER:chk"), a[:])
	if s[len(prefix)+64:] != hex.EncodeToString(chk[:4]) {
		return a, fmt.Errorf("address checksum mismatch — please re-check for typos")
	}
	return a, nil
}
