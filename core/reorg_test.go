package core_test

import (
	"context"
	"errors"
	"math/big"
	"path/filepath"
	"testing"

	"perihelion/core"
	"perihelion/wallet"
)

func shrinkPow(t *testing.T) {
	t.Helper()
	oldMem, oldDiff := core.PowMemoryKiB, core.MinDifficulty
	core.PowMemoryKiB = 8
	core.MinDifficulty = big.NewInt(1)
	t.Cleanup(func() { core.PowMemoryKiB = oldMem; core.MinDifficulty = oldDiff })
}

func openTestChain(t *testing.T) *core.Chain {
	t.Helper()
	c, err := core.Open(filepath.Join(t.TempDir(), "chain.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// TestReorg builds two competing chains and verifies that the node abandons
// the shorter one entirely: heaviest chain wins, balances and supply follow.
func TestReorg(t *testing.T) {
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
	ctx := context.Background()
	if err := core.Mine(ctx, cA, wA.Address(), 2, nil); err != nil {
		t.Fatal(err)
	}
	if err := core.Mine(ctx, cB, wB.Address(), 4, nil); err != nil {
		t.Fatal(err)
	}

	// Feed B's heavier chain into A, block by block.
	for h := uint64(1); h <= 4; h++ {
		blk, err := cB.BlockByHeight(h)
		if err != nil {
			t.Fatal(err)
		}
		if err := cA.AcceptBlock(blk); err != nil {
			t.Fatalf("accept height %d: %v", h, err)
		}
	}

	hA, tipA, _ := cA.TipInfo()
	hB, tipB, _ := cB.TipInfo()
	if hA != 4 || tipA != tipB {
		t.Fatalf("no reorg: A at height %d, tips equal: %v", hA, tipA == tipB)
	}
	_ = hB

	// A's own mining rewards lived on the abandoned branch: gone.
	sp, im, err := cA.Balance(wA.Address())
	if err != nil {
		t.Fatal(err)
	}
	if sp+im != 0 {
		t.Fatalf("stale-branch rewards survived the reorg: %d", sp+im)
	}
	// B's rewards are now visible on A, and supply accounting matches.
	want := core.BlockSubsidy(1) + core.BlockSubsidy(2) + core.BlockSubsidy(3) + core.BlockSubsidy(4)
	_, imB, err := cA.Balance(wB.Address())
	if err != nil {
		t.Fatal(err)
	}
	if imB != want {
		t.Fatalf("B balance on A = %d, want %d", imB, want)
	}
	st, err := cA.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Emitted != want {
		t.Fatalf("emitted = %d, want %d after reorg", st.Emitted, want)
	}
}

// TestOrphan verifies that a block with an unknown parent is reported as an
// orphan so the network layer can fetch its ancestors.
func TestOrphan(t *testing.T) {
	shrinkPow(t)
	cA := openTestChain(t)
	cB := openTestChain(t)
	wB, err := wallet.New()
	if err != nil {
		t.Fatal(err)
	}
	if err := core.Mine(context.Background(), cB, wB.Address(), 3, nil); err != nil {
		t.Fatal(err)
	}
	blk3, err := cB.BlockByHeight(3)
	if err != nil {
		t.Fatal(err)
	}
	if err := cA.AcceptBlock(blk3); !errors.Is(err, core.ErrOrphan) {
		t.Fatalf("expected ErrOrphan, got %v", err)
	}
	// Delivering the ancestors first resolves the gap.
	for h := uint64(1); h <= 3; h++ {
		blk, _ := cB.BlockByHeight(h)
		if err := cA.AcceptBlock(blk); err != nil {
			t.Fatalf("accept height %d: %v", h, err)
		}
	}
	hA, _, _ := cA.TipInfo()
	if hA != 3 {
		t.Fatalf("height = %d, want 3", hA)
	}
}
