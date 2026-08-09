package core_test

import (
	"context"
	"testing"

	"perihelion/core"
	"perihelion/wallet"
)

// TestDuplicateInputInflation is the money-printing attack a transaction must
// never permit: listing the same unspent output twice so that its value is
// counted twice, letting the sender create coins out of nothing without any
// hashpower at all. Every input must be counted once and only once.
func TestDuplicateInputInflation(t *testing.T) {
	shrinkPow(t)
	c := openTestChain(t)
	attacker, err := wallet.New()
	if err != nil {
		t.Fatal(err)
	}
	if err := core.Mine(context.Background(), c, attacker.Address(), 22, nil); err != nil {
		t.Fatal(err)
	}

	utxos, err := c.ListSpendable(attacker.Address(), 0)
	if err != nil || len(utxos) == 0 {
		t.Fatalf("no spendable outputs: %v", err)
	}
	target := utxos[0]

	// One output, spent four times over. Honest accounting values the inputs
	// at target.Value; the attack claims four times that.
	fee, _ := core.ParseAmount("0.001")
	claimed := target.Value*4 - fee
	tx := &core.Tx{
		Outputs: []core.TxOutput{{Value: claimed, Addr: attacker.Address()}},
	}
	for i := 0; i < 4; i++ {
		tx.Inputs = append(tx.Inputs, core.TxInput{Prev: target.Out})
	}
	digest := tx.SigDigest()
	pub := attacker.PubBytes()
	sig := attacker.Sign(digest[:])
	for i := range tx.Inputs {
		tx.Inputs[i].PubKey = pub
		tx.Inputs[i].Signature = sig
	}

	if err := c.SubmitTx(tx); err == nil {
		t.Fatal("a transaction spending the same output four times was accepted — coins can be created from nothing")
	}

	// It must also be impossible to sneak past the mempool straight into a
	// block, since a miner running modified software could try exactly that.
	before, err := c.Stats()
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := c.NextBlockTemplate(attacker.Address())
	if err != nil {
		t.Fatal(err)
	}
	tmpl.Txs = append(tmpl.Txs, tx)
	tmpl.Header.MerkleRoot = core.MerkleRoot(tmpl.Txs)
	if err := c.AcceptBlock(tmpl); err == nil {
		t.Fatal("a block containing a duplicate-input transaction was accepted")
	}
	after, err := c.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if after.Emitted != before.Emitted || after.Height != before.Height {
		t.Fatal("the attack changed chain state despite being rejected")
	}
}

// TestSupplyMatchesUTXOSet is the audit that would reveal an inflation bug
// having been exploited: every coin recorded as emitted, minus what was
// burned and what still waits in the reward pool, must exist in the unspent
// output set. If these ever disagree, coins were created outside the rules.
func TestSupplyMatchesUTXOSet(t *testing.T) {
	shrinkPow(t)
	c := openTestChain(t)
	a, err := wallet.New()
	if err != nil {
		t.Fatal(err)
	}
	b, err := wallet.New()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := core.Mine(ctx, c, a.Address(), 25, nil); err != nil {
		t.Fatal(err)
	}
	amount, _ := core.ParseAmount("2")
	fee, _ := core.ParseAmount("0.01")
	tx, err := wallet.BuildSend(c, a, b.Address(), amount, fee)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SubmitTx(tx); err != nil {
		t.Fatal(err)
	}
	if err := core.Mine(ctx, c, a.Address(), 2, nil); err != nil {
		t.Fatal(err)
	}

	audit, err := c.AuditSupply()
	if err != nil {
		t.Fatal(err)
	}
	if !audit.Consistent {
		t.Fatalf("supply audit failed: UTXO total %d, expected %d (emitted %d, burned %d, pool %d)",
			audit.UTXOTotal, audit.Expected, audit.Emitted, audit.Burned, audit.Pool)
	}
}
