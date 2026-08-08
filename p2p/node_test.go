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

// TestTwoNodeSync runs two real nodes over TCP on localhost: node 1 mines and
// node 2 follows, then the roles flip. Both must converge on identical tips.
func TestTwoNodeSync(t *testing.T) {
	oldMem, oldDiff := core.PowMemoryKiB, core.MinDifficulty
	core.PowMemoryKiB = 8
	core.MinDifficulty = big.NewInt(1)
	defer func() { core.PowMemoryKiB = oldMem; core.MinDifficulty = oldDiff }()

	c1, err := core.Open(filepath.Join(t.TempDir(), "chain.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer c1.Close()
	c2, err := core.Open(filepath.Join(t.TempDir(), "chain.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()

	w1, err := wallet.New()
	if err != nil {
		t.Fatal(err)
	}
	w2, err := wallet.New()
	if err != nil {
		t.Fatal(err)
	}

	n1 := p2p.New(c1, nil)
	if err := n1.Start("127.0.0.1:0", nil); err != nil {
		t.Fatal(err)
	}
	defer n1.Stop()
	n2 := p2p.New(c2, nil)
	if err := n2.Start("127.0.0.1:0", []string{n1.Addr()}); err != nil {
		t.Fatal(err)
	}
	defer n2.Stop()

	ctx := context.Background()
	if err := core.MineLoop(ctx, c1, w1.Address(), 3, core.MineOpts{OnBlock: n1.BroadcastBlock}); err != nil {
		t.Fatal(err)
	}
	waitConverged(t, c1, c2, 15*time.Second)

	if err := core.MineLoop(ctx, c2, w2.Address(), 2, core.MineOpts{OnBlock: n2.BroadcastBlock}); err != nil {
		t.Fatal(err)
	}
	waitConverged(t, c1, c2, 15*time.Second)

	h1, _, _ := c1.TipInfo()
	if h1 != 5 {
		t.Fatalf("converged height = %d, want 5", h1)
	}
}

func waitConverged(t *testing.T, a, b *core.Chain, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ha, ta, _ := a.TipInfo()
		hb, tb, _ := b.TipInfo()
		if ha == hb && ta == tb {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	ha, _, _ := a.TipInfo()
	hb, _, _ := b.TipInfo()
	t.Fatalf("nodes did not converge: heights %d vs %d", ha, hb)
}
