package core_test

import (
	"context"
	"math/big"
	"testing"

	"perihelion/core"
	"perihelion/wallet"
)

// TestChainIDBindsSignatures verifies the replay protection actually binds: a
// signature made for this chain must not verify against a different chain's
// digest. Without this, a fork sharing this history could replay payments.
func TestChainIDBindsSignatures(t *testing.T) {
	tx := &core.Tx{
		Inputs:  []core.TxInput{{Prev: core.OutPoint{TxID: [32]byte{1}, Index: 0}}},
		Outputs: []core.TxOutput{{Value: 100, Addr: [32]byte{2}}},
	}
	legacy := tx.SigDigest()
	bound := tx.SigDigestV2()
	if legacy == bound {
		t.Fatal("the chain-bound digest is identical to the unbound one — it binds nothing")
	}
	id := core.ChainID()
	if id == ([32]byte{}) {
		t.Fatal("chain id is empty")
	}
	// The identity must not depend on anything mutable. Difficulty parameters
	// are variables; if the chain id moved with them, a stray change would
	// silently invalidate every signature ever made.
	oldDiff := core.MinDifficulty
	core.MinDifficulty = big.NewInt(12345)
	defer func() { core.MinDifficulty = oldDiff }()
	if core.ChainID() != id {
		t.Fatal("chain id changed when a difficulty parameter changed — it is bound to mutable state")
	}
}

// TestSighashTransition checks the upgrade path: below the switchover height
// both digests are accepted so that nodes and wallets can be updated
// gradually, and from the switchover only the chain-bound one is.
func TestSighashTransition(t *testing.T) {
	if core.SighashChainIDHeight == 0 {
		t.Skip("no transition configured")
	}
	w, err := wallet.New()
	if err != nil {
		t.Fatal(err)
	}
	tx := &core.Tx{
		Inputs:  []core.TxInput{{Prev: core.OutPoint{TxID: [32]byte{7}, Index: 0}, PubKey: w.PubBytes()}},
		Outputs: []core.TxOutput{{Value: 1, Addr: w.Address()}},
	}
	addrOf := func(core.OutPoint) ([32]byte, bool) { return core.AddrOfPubKey(w.PubBytes()), true }

	// A signature in the old form: valid before the switchover, invalid after.
	legacy := tx.SigDigest()
	tx.Inputs[0].Signature = w.Sign(legacy[:])
	if err := tx.VerifySignatures(addrOf, core.SighashChainIDHeight-1); err != nil {
		t.Fatalf("legacy signature rejected before the switchover: %v", err)
	}
	if err := tx.VerifySignatures(addrOf, core.SighashChainIDHeight); err == nil {
		t.Fatal("legacy signature still accepted at the switchover height")
	}

	// A chain-bound signature: valid on both sides of the switchover.
	bound := tx.SigDigestV2()
	tx.Inputs[0].Signature = w.Sign(bound[:])
	for _, h := range []uint64{core.SighashChainIDHeight - 1, core.SighashChainIDHeight, core.SighashChainIDHeight + 1000} {
		if err := tx.VerifySignatures(addrOf, h); err != nil {
			t.Fatalf("chain-bound signature rejected at height %d: %v", h, err)
		}
	}
}

// TestReorgIsReported ensures history being rewritten never passes silently:
// on a chain with little hashrate a deep reorganisation is what a double-spend
// looks like, so the node must surface it.
func TestReorgIsReported(t *testing.T) {
	shrinkPow(t)
	cA := openTestChain(t)
	cB := openTestChain(t)
	wA, err := wallet.New()
	if err != nil {
		t.Fatal(err)
	}
	wB, err := wallet.New()
	if err != nil {
		t.Fatal(err)
	}
	var gotDepth, gotHeight uint64
	cA.OnReorg(func(depth, newHeight uint64) { gotDepth, gotHeight = depth, newHeight })

	ctx := context.Background()
	if err := core.Mine(ctx, cA, wA.Address(), 3, nil); err != nil {
		t.Fatal(err)
	}
	if err := core.Mine(ctx, cB, wB.Address(), 6, nil); err != nil {
		t.Fatal(err)
	}
	for h := uint64(1); h <= 6; h++ {
		blk, err := cB.BlockByHeight(h)
		if err != nil {
			t.Fatal(err)
		}
		if err := cA.AcceptBlock(blk); err != nil {
			t.Fatalf("height %d: %v", h, err)
		}
	}
	if gotDepth == 0 {
		t.Fatal("a reorganisation discarded confirmed blocks without reporting it")
	}
	// The switch happens as soon as the rival branch outweighs the active one,
	// which is at its fourth block: three confirmed blocks are discarded and
	// the tip becomes height 4. Blocks 5 and 6 then extend it normally.
	if gotDepth != 3 {
		t.Fatalf("reported depth %d, expected the 3 discarded blocks", gotDepth)
	}
	if gotHeight != 4 {
		t.Fatalf("reported new height %d, want 4 (the height at which the switch occurred)", gotHeight)
	}
	count, last := cA.ReorgStats()
	if count != 1 || last != 3 {
		t.Fatalf("reorg stats not recorded: count=%d last=%d", count, last)
	}
	// The node must have ended up on the rival chain in full.
	h, tip, _ := cA.TipInfo()
	bTip, bHash, _ := cB.TipInfo()
	if h != bTip || tip != bHash {
		t.Fatalf("did not converge on the heavier chain: at %d, expected %d", h, bTip)
	}
}
