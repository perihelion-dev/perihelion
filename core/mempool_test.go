package core_test

import (
	"context"
	"strings"
	"testing"

	"perihelion/core"
	"perihelion/wallet"
)

// TestMempoolIsBounded mounts the flooding attack the pool must survive: a
// party who can pay the minimum fee submits far more transactions than the
// pool may hold. Without a bound this grows every node's database without
// limit, which on a young chain costs the attacker almost nothing.
func TestMempoolIsBounded(t *testing.T) {
	shrinkPow(t)
	oldMax := core.MaxMempoolTxs
	core.MaxMempoolTxs = 8 // keep the test quick; the property is the same
	defer func() { core.MaxMempoolTxs = oldMax }()

	c := openTestChain(t)
	w, err := wallet.New()
	if err != nil {
		t.Fatal(err)
	}
	victim, err := wallet.New()
	if err != nil {
		t.Fatal(err)
	}
	// Mine enough blocks that many separate mature outputs exist to spend.
	if err := core.Mine(context.Background(), c, w.Address(), 40, nil); err != nil {
		t.Fatal(err)
	}

	amount, _ := core.ParseAmount("0.5")
	minFee := core.MinRelayFee
	accepted, rejected := 0, 0
	for i := 0; i < 30; i++ {
		tx, err := wallet.BuildSend(c, w, victim.Address(), amount, minFee)
		if err != nil {
			break // ran out of spendable outputs
		}
		if err := c.SubmitTx(tx); err != nil {
			rejected++
			continue
		}
		accepted++
	}

	st, err := c.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Mempool > core.MaxMempoolTxs {
		t.Fatalf("mempool holds %d transactions, above the cap of %d", st.Mempool, core.MaxMempoolTxs)
	}
	if rejected == 0 {
		t.Fatalf("flooding was never refused (accepted %d, pool %d)", accepted, st.Mempool)
	}
}

// TestMempoolRejectsConflicts ensures two transactions spending the same
// output cannot both wait in the pool — otherwise a miner could be handed a
// template that no node would accept.
func TestMempoolRejectsConflicts(t *testing.T) {
	shrinkPow(t)
	c := openTestChain(t)
	w, err := wallet.New()
	if err != nil {
		t.Fatal(err)
	}
	a, err := wallet.New()
	if err != nil {
		t.Fatal(err)
	}
	b, err := wallet.New()
	if err != nil {
		t.Fatal(err)
	}
	if err := core.Mine(context.Background(), c, w.Address(), 22, nil); err != nil {
		t.Fatal(err)
	}
	amount, _ := core.ParseAmount("1")
	fee, _ := core.ParseAmount("0.001")

	tx1, err := wallet.BuildSend(c, w, a.Address(), amount, fee)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SubmitTx(tx1); err != nil {
		t.Fatal(err)
	}
	// A second transaction built from the same UTXO set necessarily reuses at
	// least one input while the first is still pending.
	tx2, err := wallet.BuildSend(c, w, b.Address(), amount, fee)
	if err != nil {
		t.Fatal(err)
	}
	err = c.SubmitTx(tx2)
	if err == nil {
		t.Fatal("a conflicting transaction was accepted into the pool")
	}
	if !strings.Contains(err.Error(), "conflicts") && !strings.Contains(err.Error(), "double spend") {
		t.Fatalf("unexpected rejection reason: %v", err)
	}
}
