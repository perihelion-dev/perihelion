package core_test

import (
	"testing"

	"perihelion/core"
)

// Fuzz targets for every parser that accepts bytes from the network. The
// property under test is the only one that matters for a parser facing
// hostile input: it must never panic, hang or allocate without bound, whatever
// the bytes. A wrong answer is fine — that is what validation is for — but a
// crash is a remote kill switch for every node running this code.
//
// Round-trip properties are checked too: anything that parses must re-encode
// to something that parses to the same value, or the wire format is ambiguous
// and two nodes could disagree about what they saw.
//
// Run with:  go test ./core/ -fuzz=FuzzDeserializeTx -fuzztime=60s

func FuzzDeserializeTx(f *testing.F) {
	// Seed with a valid transaction so the fuzzer starts from real structure.
	valid := (&core.Tx{
		Inputs:  []core.TxInput{{Prev: core.OutPoint{TxID: [32]byte{1}, Index: 0}, PubKey: []byte{1, 2, 3}, Signature: []byte{4, 5}}},
		Outputs: []core.TxOutput{{Value: 42, Addr: [32]byte{7}}},
		Extra:   []byte("seed"),
	}).Serialize()
	f.Add(valid)
	f.Add([]byte{})
	f.Add([]byte{0xff, 0xff, 0xff, 0xff}) // absurd count
	f.Fuzz(func(t *testing.T, data []byte) {
		tx, err := core.DeserializeTx(data)
		if err != nil {
			return
		}
		// Round trip: serialise, re-parse, must be identical.
		re := tx.Serialize()
		tx2, err := core.DeserializeTx(re)
		if err != nil {
			t.Fatalf("re-parse of own serialisation failed: %v", err)
		}
		if tx.ID() != tx2.ID() {
			t.Fatal("round trip changed the transaction id")
		}
		// These must never panic on any parsed input.
		_ = tx.SigDigest()
		_ = tx.SigDigestV2()
		_ = tx.IsCoinbase()
	})
}

func FuzzDeserializeBlock(f *testing.F) {
	g := core.GenesisBlock()
	f.Add(g.Serialize())
	f.Add([]byte{})
	f.Add(make([]byte, core.HeaderSize+4))
	f.Fuzz(func(t *testing.T, data []byte) {
		b, err := core.DeserializeBlock(data)
		if err != nil {
			return
		}
		re := b.Serialize()
		b2, err := core.DeserializeBlock(re)
		if err != nil {
			t.Fatalf("re-parse failed: %v", err)
		}
		if b.Header.Hash() != b2.Header.Hash() {
			t.Fatal("round trip changed the block hash")
		}
		// MerkleRoot over parsed transactions must not panic even if the
		// header's claimed root is nonsense.
		_ = core.MerkleRoot(b.Txs)
	})
}

func FuzzDeserializeHeader(f *testing.F) {
	f.Add(core.GenesisBlock().Header.Serialize())
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		h, err := core.DeserializeHeader(data)
		if err != nil {
			return
		}
		re := h.Serialize()
		if len(re) != core.HeaderSize {
			t.Fatalf("header re-serialised to %d bytes, want %d", len(re), core.HeaderSize)
		}
		h2, err := core.DeserializeHeader(re)
		if err != nil || h.Hash() != h2.Hash() {
			t.Fatal("header round trip failed")
		}
		// Difficulty math on arbitrary targets must not panic (division by
		// a zero target is the obvious trap).
		_ = core.DifficultyOf(h.Target)
	})
}

// FuzzAddress: DecodeAddress must never panic on any string, and every
// address EncodeAddress produces must decode back to the same bytes.
func FuzzAddress(f *testing.F) {
	f.Add("per1")
	f.Add("")
	f.Add(core.EncodeAddress([32]byte{1, 2, 3}))
	f.Fuzz(func(t *testing.T, s string) {
		a, err := core.DecodeAddress(s)
		if err != nil {
			return
		}
		if core.EncodeAddress(a) != s {
			t.Fatalf("decode/encode not idempotent for %q", s)
		}
	})
}

// FuzzAmount: ParseAmount must never panic and, where it succeeds, must
// round-trip through FormatAmount to the same value.
func FuzzAmount(f *testing.F) {
	f.Add("1.5")
	f.Add("0.00000001")
	f.Add("30000000")
	f.Add("")
	f.Add("1e9")
	f.Fuzz(func(t *testing.T, s string) {
		v, err := core.ParseAmount(s)
		if err != nil {
			return
		}
		back, err := core.ParseAmount(core.FormatAmount(v))
		if err != nil || back != v {
			t.Fatalf("amount round trip failed for %q: %d -> %s -> %d", s, v, core.FormatAmount(v), back)
		}
	})
}
