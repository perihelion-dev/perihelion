package core_test

import (
	"context"
	"testing"

	"perihelion/core"
	"perihelion/wallet"
)

func TestBlockVersionRules(t *testing.T) {
	if !core.ValidBlockVersion(1) {
		t.Fatal("legacy version 1 must remain valid forever")
	}
	if !core.ValidBlockVersion(core.VersionSignalBase) {
		t.Fatal("signalling base version must be valid")
	}
	if !core.ValidBlockVersion(core.VersionSignalBase | 0x5) {
		t.Fatal("signalling version with bits must be valid")
	}
	if core.ValidBlockVersion(0) || core.ValidBlockVersion(2) || core.ValidBlockVersion(0x4000_0000) {
		t.Fatal("malformed versions must be rejected")
	}
	if !core.SignalsBit(core.VersionSignalBase|1<<3, 3) {
		t.Fatal("bit 3 should signal")
	}
	if core.SignalsBit(core.VersionSignalBase|1<<3, 4) {
		t.Fatal("bit 4 should not signal")
	}
	if core.SignalsBit(1, 0) {
		t.Fatal("legacy version 1 signals nothing")
	}
}

// TestSignalledBlocksAccepted mines with signal bits set and verifies the
// network accepts those blocks like any others — signalling is an opinion,
// not a rule violation.
func TestSignalledBlocksAccepted(t *testing.T) {
	shrinkPow(t)
	core.SetMinerSignalBits(1 << 7)
	defer core.SetMinerSignalBits(0)

	c := openTestChain(t)
	w, err := wallet.New()
	if err != nil {
		t.Fatal(err)
	}
	if err := core.Mine(context.Background(), c, w.Address(), 3, nil); err != nil {
		t.Fatal(err)
	}
	blk, err := c.BlockByHeight(3)
	if err != nil {
		t.Fatal(err)
	}
	if !core.SignalsBit(blk.Header.Version, 7) {
		t.Fatal("mined block should carry the configured signal bit")
	}
	h, _, _ := c.TipInfo()
	if h != 3 {
		t.Fatalf("height = %d, want 3", h)
	}
}

// TestDeploymentTally exercises the full lifecycle against a real chain:
// counting, threshold, lock-in and activation heights are all derived purely
// from blocks — the maintainer has no input into any of it.
func TestDeploymentTally(t *testing.T) {
	shrinkPow(t)
	d := core.Deployment{Name: "test-rule", Bit: 2, StartHeight: 1, TimeoutHeight: 1_000_000}

	c := openTestChain(t)
	w, err := wallet.New()
	if err != nil {
		t.Fatal(err)
	}

	// Not started: no blocks yet beyond genesis.
	st, err := c.DeploymentStatus(d)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != core.StateDefined {
		t.Fatalf("state = %v, want defined", st.State)
	}

	// Mine 4 signalling blocks: the window is still filling.
	core.SetMinerSignalBits(1 << d.Bit)
	defer core.SetMinerSignalBits(0)
	if err := core.Mine(context.Background(), c, w.Address(), 4, nil); err != nil {
		t.Fatal(err)
	}
	st, err = c.DeploymentStatus(d)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != core.StateStarted {
		t.Fatalf("state = %v, want started", st.State)
	}
	if st.WindowSignals != 4 || st.WindowBlocks != 4 {
		t.Fatalf("tally = %d/%d, want 4/4", st.WindowSignals, st.WindowBlocks)
	}
}
