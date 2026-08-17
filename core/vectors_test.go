package core_test

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"perihelion/core"
)

// Test vectors pin the exact bytes of every consensus-critical encoding, so
// that a second, independently written implementation can prove it agrees
// with this one byte for byte before it is trusted with a single block. They
// are written to docs/test-vectors.json on every run and checked against the
// values recorded there, so a change to any serialisation — deliberate or
// accidental — fails the build and shows exactly which bytes moved.
//
// What is pinned: the mainnet genesis header and its hash, the chain-ID, a
// canonical transaction's serialisation, its id, its legacy signing digest and
// its chain-bound signing digest, an address encoding, and the first ten
// block subsidies. Anything an alternative implementation must reproduce.

type vectors struct {
	Note              string   `json:"_note"`
	Network           string   `json:"network"`
	GenesisHeaderHex  string   `json:"genesis_header_hex"`
	GenesisHash       string   `json:"genesis_hash"`
	GenesisMerkleRoot string   `json:"genesis_merkle_root"`
	ChainID           string   `json:"chain_id"`
	TxSerialisedHex   string   `json:"tx_serialised_hex"`
	TxID              string   `json:"tx_id"`
	TxSigDigestLegacy string   `json:"tx_sigdigest_legacy"`
	TxSigDigestV2     string   `json:"tx_sigdigest_chainbound"`
	AddressOfZeroKey  string   `json:"address_of_zero_pubkey"`
	Subsidies         []string `json:"block_subsidies_1_to_10_peri"`
	SighashSwitchover uint64   `json:"sighash_chainid_height"`
}

func computeVectors() vectors {
	core.SelectNetwork(core.Mainnet)
	g := core.GenesisBlock()
	gh := g.Header.Hash()
	id := core.ChainID()

	// A canonical transaction with fixed, non-random content.
	tx := &core.Tx{
		Inputs: []core.TxInput{{
			Prev: core.OutPoint{TxID: [32]byte{0x01, 0x02, 0x03}, Index: 7},
		}},
		Outputs: []core.TxOutput{
			{Value: 123456789, Addr: [32]byte{0xaa}},
			{Value: 1, Addr: [32]byte{0xbb}},
		},
		Extra: []byte("vector"),
	}
	txid := tx.ID()
	d1 := tx.SigDigest()
	d2 := tx.SigDigestV2()

	var zeroKey [32]byte
	addr := core.EncodeAddress(core.AddrOfPubKey(zeroKey[:]))

	subs := make([]string, 0, 10)
	for h := uint64(1); h <= 10; h++ {
		subs = append(subs, core.FormatAmount(core.BlockSubsidy(h)))
	}
	return vectors{
		Note:              "Consensus test vectors for the Perihelion mainnet. An independent implementation must reproduce every value here exactly.",
		Network:           "mainnet",
		GenesisHeaderHex:  hex.EncodeToString(g.Header.Serialize()),
		GenesisHash:       hex.EncodeToString(gh[:]),
		GenesisMerkleRoot: hex.EncodeToString(g.Header.MerkleRoot[:]),
		ChainID:           hex.EncodeToString(id[:]),
		TxSerialisedHex:   hex.EncodeToString(tx.Serialize()),
		TxID:              hex.EncodeToString(txid[:]),
		TxSigDigestLegacy: hex.EncodeToString(d1[:]),
		TxSigDigestV2:     hex.EncodeToString(d2[:]),
		AddressOfZeroKey:  addr,
		Subsidies:         subs,
		SighashSwitchover: core.SighashChainIDHeight,
	}
}

func TestVectorsStable(t *testing.T) {
	got := computeVectors()

	// Hard-coded anchors that must never move; the file is derived from them.
	if got.GenesisHash != "348495b860aa61e166f8f822e63f229885e1744f47121b9c1fafc2711a6f62e0" {
		t.Fatalf("mainnet genesis hash changed: %s", got.GenesisHash)
	}
	if got.ChainID[:16] != "28b70fd19a738da0" {
		t.Fatalf("mainnet chain-id changed: %s", got.ChainID)
	}
	if got.Subsidies[0] != "10" {
		t.Fatalf("subsidy(1) = %s, want 10", got.Subsidies[0])
	}
	// The legacy and chain-bound digests must differ, or chain-binding is a no-op.
	if got.TxSigDigestLegacy == got.TxSigDigestV2 {
		t.Fatal("legacy and chain-bound digests are identical")
	}

	// Compare with the committed file, if present; write it if absent.
	path := filepath.Join("..", "docs", "test-vectors.json")
	if data, err := os.ReadFile(path); err == nil {
		var prev vectors
		if err := json.Unmarshal(data, &prev); err != nil {
			t.Fatalf("docs/test-vectors.json is not valid JSON: %v", err)
		}
		// Note is documentation and may be edited; everything else is pinned.
		prev.Note, got.Note = "", ""
		pj, _ := json.Marshal(prev)
		gj, _ := json.Marshal(got)
		if string(pj) != string(gj) {
			t.Fatalf("test vectors changed — a consensus encoding moved.\nwas:  %s\nnow:  %s\nIf this is intentional, delete docs/test-vectors.json and re-run to regenerate.", pj, gj)
		}
		return
	}
	got = computeVectors()
	out, _ := json.MarshalIndent(got, "", "  ")
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		t.Fatalf("cannot write vectors: %v", err)
	}
	t.Logf("wrote %s", path)
}
