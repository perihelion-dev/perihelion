package p2p_test

import (
	"context"
	"math/big"
	"path/filepath"
	"testing"
	"time"

	"perihelion/core"
	"perihelion/p2p"
	"perihelion/wallet"
)

// TestMinerWaitsForSync verifies that a fresh node joining an existing network
// synchronises before it mines, instead of building a competing branch from
// genesis that the network would discard.
func TestMinerWaitsForSync(t *testing.T) {
	oldMem, oldDiff := core.PowMemoryKiB, core.MinDifficulty
	core.PowMemoryKiB = 8
	core.MinDifficulty = big.NewInt(1)
	defer func() { core.PowMemoryKiB = oldMem; core.MinDifficulty = oldDiff }()

	established, err := core.Open(filepath.Join(t.TempDir(), "chain.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer established.Close()
	fresh, err := core.Open(filepath.Join(t.TempDir(), "chain.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()

	w, err := wallet.New()
	if err != nil {
		t.Fatal(err)
	}
	// Build a chain the newcomer must catch up with.
	if err := core.Mine(context.Background(), established, w.Address(), 6, nil); err != nil {
		t.Fatal(err)
	}

	server := p2p.New(established, nil)
	if err := server.Start("127.0.0.1:0", nil); err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	newcomer := p2p.New(fresh, nil)
	if err := newcomer.Start("", []string{server.Addr()}); err != nil {
		t.Fatal(err)
	}
	defer newcomer.Stop()

	// Before any peer is known the newcomer must refuse to mine.
	if newcomer.Synced() {
		t.Fatal("a node with no peers and no chain must not consider itself synced")
	}

	w2, err := wallet.New()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go core.MineLoop(ctx, fresh, w2.Address(), 0, core.MineOpts{
		OnBlock: newcomer.BroadcastBlock,
		Ready:   newcomer.Synced,
	})

	// The newcomer must reach the established tip by syncing, and every block
	// it holds must be the established chain's block at that height.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if h, _, _ := fresh.TipInfo(); h >= 6 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	h, tip, _ := fresh.TipInfo()
	if h < 6 {
		t.Fatalf("newcomer height = %d, expected to sync to at least 6", h)
	}
	want, err := established.BlockByHeight(6)
	if err != nil {
		t.Fatal(err)
	}
	got, err := fresh.BlockByHeight(6)
	if err != nil {
		t.Fatal(err)
	}
	if got.Header.Hash() != want.Header.Hash() {
		t.Fatal("newcomer mined its own branch instead of adopting the network's chain")
	}
	_ = tip
}
