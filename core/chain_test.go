package core_test

import (
	"context"
	"math/big"
	"path/filepath"
	"testing"

	"perihelion/core"
	"perihelion/wallet"
)

// TestEndToEnd exercises the full consensus loop: mining, maturity, sending,
// fee burn, pool accounting and double-spend/signature rejection. PoW cost is
// shrunk so the test runs in seconds; consensus logic is unchanged.
func TestEndToEnd(t *testing.T) {
	oldMem, oldDiff := core.PowMemoryKiB, core.MinDifficulty
	core.PowMemoryKiB = 8
	core.MinDifficulty = big.NewInt(1)
	defer func() { core.PowMemoryKiB = oldMem; core.MinDifficulty = oldDiff }()

	c, err := core.Open(filepath.Join(t.TempDir(), "chain.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	wA, err := wallet.New()
	if err != nil {
		t.Fatal(err)
	}
	wB, err := wallet.New()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	logf := func(string, ...any) {}

	if err := core.Mine(ctx, c, wA.Address(), 22, logf); err != nil {
		t.Fatal(err)
	}

	spendable, immature, err := c.Balance(wA.Address())
	if err != nil {
		t.Fatal(err)
	}
	wantMature := core.BlockSubsidy(1) + core.BlockSubsidy(2) + core.BlockSubsidy(3)
	if spendable != wantMature {
		t.Fatalf("spendable = %d, want %d (blocks 1-3 mature at height 22)", spendable, wantMature)
	}
	if immature == 0 {
		t.Fatal("expected immature coinbase balance")
	}

	amount, _ := core.ParseAmount("5")
	fee, _ := core.ParseAmount("0.001")
	tx, err := wallet.BuildSend(c, wA, wB.Address(), amount, fee)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SubmitTx(tx); err != nil {
		t.Fatal(err)
	}
	if err := c.SubmitTx(tx); err == nil {
		t.Fatal("expected duplicate submission to be rejected")
	}
	if err := core.Mine(ctx, c, wA.Address(), 1, logf); err != nil {
		t.Fatal(err)
	}

	gotB, _, err := c.Balance(wB.Address())
	if err != nil {
		t.Fatal(err)
	}
	if gotB != amount {
		t.Fatalf("B balance = %d, want %d", gotB, amount)
	}

	st, err := c.Stats()
	if err != nil {
		t.Fatal(err)
	}
	burn, poolIn := core.SplitFee(fee)
	if st.Burned != burn {
		t.Fatalf("burned = %d, want %d", st.Burned, burn)
	}
	wantPool := poolIn - poolIn/core.PoolPayoutBlocks
	if st.Pool != wantPool {
		t.Fatalf("pool = %d, want %d", st.Pool, wantPool)
	}
	blk, err := c.BlockByHeight(23)
	if err != nil {
		t.Fatal(err)
	}
	if len(blk.Txs) != 2 {
		t.Fatalf("block 23 has %d txs, want 2", len(blk.Txs))
	}
	wantCoinbase := core.BlockSubsidy(23) + poolIn/core.PoolPayoutBlocks
	if blk.Txs[0].Outputs[0].Value != wantCoinbase {
		t.Fatalf("coinbase = %d, want %d (subsidy + pool payout)", blk.Txs[0].Outputs[0].Value, wantCoinbase)
	}

	// A tampered signature must be rejected.
	tx2, err := wallet.BuildSend(c, wA, wB.Address(), amount, fee)
	if err != nil {
		t.Fatal(err)
	}
	tx2.Inputs[0].Signature[0] ^= 1
	if err := c.SubmitTx(tx2); err == nil {
		t.Fatal("expected tampered signature to be rejected")
	}
}

func TestAddressRoundtrip(t *testing.T) {
	w, err := wallet.New()
	if err != nil {
		t.Fatal(err)
	}
	enc := wallet.EncodeAddress(w.Address())
	dec, err := wallet.DecodeAddress(enc)
	if err != nil {
		t.Fatal(err)
	}
	if dec != w.Address() {
		t.Fatal("address roundtrip mismatch")
	}
	// Corrupt one checksum character.
	bad := enc[:len(enc)-1]
	if enc[len(enc)-1] == '0' {
		bad += "1"
	} else {
		bad += "0"
	}
	if _, err := wallet.DecodeAddress(bad); err == nil {
		t.Fatal("expected checksum error")
	}
}

func TestSignatureScheme(t *testing.T) {
	w, err := wallet.New()
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte("perihelion test message")
	sig := w.Sign(msg)
	pub, err := core.SigScheme.UnmarshalBinaryPublicKey(w.PubBytes())
	if err != nil {
		t.Fatal(err)
	}
	if !core.SigScheme.Verify(pub, msg, sig, nil) {
		t.Fatal("valid signature rejected")
	}
	sig[0] ^= 1
	if core.SigScheme.Verify(pub, msg, sig, nil) {
		t.Fatal("tampered signature accepted")
	}
}
