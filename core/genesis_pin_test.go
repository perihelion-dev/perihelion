package core_test

import (
	"encoding/hex"
	"testing"

	"perihelion/core"
)

// The mainnet genesis hash must be exactly what the live network agreed on.
// If this ever changes, the software forks itself off the real chain.
func TestMainnetGenesisUnchanged(t *testing.T) {
	core.SelectNetwork(core.Mainnet)
	h := core.GenesisBlock().Header.Hash()
	got := hex.EncodeToString(h[:])
	const want = "348495b860aa61e166f8f822e63f229885e1744f47121b9c1fafc2711a6f62e0"
	if got != want {
		t.Fatalf("MAINNET GENESIS CHANGED\n got  %s\n want %s\nThis would fork every node running this build off the live network.", got, want)
	}
	id := core.ChainID()
	if hex.EncodeToString(id[:])[:16] != "28b70fd19a738da0" {
		t.Fatalf("mainnet chain-ID changed: %x", id[:8])
	}
}
