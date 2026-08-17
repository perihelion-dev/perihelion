package core_test

import (
	"context"
	"math/big"
	"testing"

	"perihelion/core"
	"perihelion/wallet"
)

// TestCheckpointRefusesConflictingBranch: a block at a checkpointed height
// with a different hash must be rejected — that is the whole point of a
// checkpoint. The honest chain must pass through unharmed.
func TestCheckpointRefusesConflictingBranch(t *testing.T) {
	shrinkPow(t)
	saved := core.Checkpoints
	core.Checkpoints = map[uint64][32]byte{}
	defer func() { core.Checkpoints = saved }()

	// Build both chains BEFORE any checkpoint exists — a miner cannot build
	// past a checkpoint its own blocks contradict, which is correct behaviour
	// but would make constructing the rival impossible.
	honest := openTestChain(t)
	rival := openTestChain(t)
	w1, _ := wallet.New()
	w2, _ := wallet.New()
	ctx := context.Background()
	if err := core.Mine(ctx, honest, w1.Address(), 8, nil); err != nil {
		t.Fatal(err)
	}
	if err := core.Mine(ctx, rival, w2.Address(), 8, nil); err != nil {
		t.Fatal(err)
	}
	b5, err := honest.BlockByHeight(5)
	if err != nil {
		t.Fatal(err)
	}
	r5, _ := rival.BlockByHeight(5)
	if b5.Header.Hash() == r5.Header.Hash() {
		t.Fatal("test setup: rival chain should differ from honest chain")
	}
	core.Checkpoints = map[uint64][32]byte{5: b5.Header.Hash()}

	// A fresh node syncing the honest chain accepts everything.
	fresh := openTestChain(t)
	for h := uint64(1); h <= 8; h++ {
		blk, _ := honest.BlockByHeight(h)
		if err := fresh.AcceptBlock(blk); err != nil {
			t.Fatalf("honest block %d rejected: %v", h, err)
		}
	}

	// A fresh node fed the rival chain must refuse it exactly at the
	// checkpoint height, however much work it carries.
	victim := openTestChain(t)
	var refusedAt uint64
	for h := uint64(1); h <= 8; h++ {
		blk, _ := rival.BlockByHeight(h)
		if err := victim.AcceptBlock(blk); err != nil {
			refusedAt = h
			break
		}
	}
	if refusedAt != 5 {
		t.Fatalf("rival branch should be refused exactly at the checkpoint (height 5), was refused at %d", refusedAt)
	}
	// And the victim's tip must be at most 4 — nothing past the checkpoint
	// height was accepted from the wrong branch.
	if h, _, _ := victim.TipInfo(); h != 4 {
		t.Fatalf("victim tip = %d, want 4", h)
	}
}

// TestCheckpointDoesNotWeakenValidation: below a checkpoint only the
// memory-hard hash is assumed; every value rule still applies. A block with
// an inflated coinbase must still be rejected there.
func TestCheckpointDoesNotWeakenValidation(t *testing.T) {
	shrinkPow(t)
	saved := core.Checkpoints
	core.Checkpoints = map[uint64][32]byte{}
	defer func() { core.Checkpoints = saved }()

	c := openTestChain(t)
	w, _ := wallet.New()
	if err := core.Mine(context.Background(), c, w.Address(), 4, nil); err != nil {
		t.Fatal(err)
	}
	b4, _ := c.BlockByHeight(4)
	core.Checkpoints = map[uint64][32]byte{4: b4.Header.Hash()}

	fresh := openTestChain(t)
	for h := uint64(1); h <= 2; h++ {
		blk, _ := c.BlockByHeight(h)
		if err := fresh.AcceptBlock(blk); err != nil {
			t.Fatal(err)
		}
	}
	blk3, _ := c.BlockByHeight(3)
	blk3.Txs[0].Outputs[0].Value++
	blk3.Header.MerkleRoot = core.MerkleRoot(blk3.Txs)
	if err := fresh.AcceptBlock(blk3); err == nil {
		t.Fatal("inflated coinbase accepted below a checkpoint — checkpoints must not weaken value rules")
	}
}

// TestSyncSkipsPowBelowCheckpoint proves the point of the feature: below a
// checkpoint the memory-hard proof-of-work is not re-run. It feeds a fresh
// node blocks whose PoW has been deliberately broken (nonce altered) but whose
// hashes are re-linked so the chain is otherwise valid — they must be accepted
// below the checkpoint. The chain then hits the checkpoint with the wrong hash
// and is refused there, which is exactly the guarantee: skipping PoW below a
// checkpoint is safe because the checkpoint itself catches a wrong branch.
func TestSyncSkipsPowBelowCheckpoint(t *testing.T) {
	// Use a real (small) difficulty so that a wrong nonce actually fails PoW;
	// at MinDifficulty=1 every hash satisfies the target and the test could
	// not distinguish "PoW skipped" from "PoW trivially passed".
	oldMem, oldDiff := core.PowMemoryKiB, core.MinDifficulty
	core.PowMemoryKiB = 8
	core.MinDifficulty = big.NewInt(4096)
	defer func() { core.PowMemoryKiB = oldMem; core.MinDifficulty = oldDiff }()
	saved := core.Checkpoints
	core.Checkpoints = map[uint64][32]byte{}
	defer func() { core.Checkpoints = saved }()

	c := openTestChain(t)
	w, _ := wallet.New()
	if err := core.Mine(context.Background(), c, w.Address(), 6, nil); err != nil {
		t.Fatal(err)
	}
	b6, _ := c.BlockByHeight(6)
	core.Checkpoints = map[uint64][32]byte{6: b6.Header.Hash()}

	// Re-derive blocks 1..5 with a broken nonce, re-linking prev-hashes so
	// only the PoW is invalid. Force PoW to be impossible to satisfy by
	// choosing a nonce and checking it genuinely fails CheckPow.
	fresh := openTestChain(t)
	var prev [32]byte
	g := core.GenesisBlock()
	prev = g.Header.Hash()
	acceptedBroken := 0
	for h := uint64(1); h <= 5; h++ {
		blk, _ := c.BlockByHeight(h)
		blk.Header.PrevHash = prev
		blk.Header.Nonce = ^blk.Header.Nonce // almost certainly breaks PoW
		if core.CheckPow(&blk.Header) {
			t.Skip("altered nonce accidentally satisfies PoW; cannot demonstrate")
		}
		if err := fresh.AcceptBlock(blk); err != nil {
			t.Fatalf("block %d with broken PoW below checkpoint should be accepted (PoW assumed): %v", h, err)
		}
		acceptedBroken++
		prev = blk.Header.Hash()
	}
	if acceptedBroken != 5 {
		t.Fatalf("accepted %d broken-PoW blocks, want 5", acceptedBroken)
	}
	// Block 6 from this altered branch has a different hash than the
	// checkpoint and must be refused — the checkpoint does its job.
	blk6, _ := c.BlockByHeight(6)
	blk6.Header.PrevHash = prev
	if err := fresh.AcceptBlock(blk6); err == nil {
		t.Fatal("altered branch reached the checkpoint height with the wrong hash and was NOT refused")
	}
}
