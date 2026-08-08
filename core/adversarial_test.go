package core_test

import (
	"context"
	"strings"
	"testing"

	"perihelion/core"
	"perihelion/wallet"
)

// TestManipulationRejected forges blocks and transactions the way an attacker
// would and verifies that consensus rejects every one of them.
func TestManipulationRejected(t *testing.T) {
	shrinkPow(t)
	c := openTestChain(t)
	w, err := wallet.New()
	if err != nil {
		t.Fatal(err)
	}
	if err := core.Mine(context.Background(), c, w.Address(), 2, nil); err != nil {
		t.Fatal(err)
	}

	// (a) Inflate the coinbase without fixing the merkle commitment.
	tmpl, err := c.NextBlockTemplate(w.Address())
	if err != nil {
		t.Fatal(err)
	}
	tmpl.Txs[0].Outputs[0].Value++
	err = c.AcceptBlock(tmpl)
	if err == nil || !strings.Contains(err.Error(), "merkle") {
		t.Fatalf("tampered tx not caught by merkle check: %v", err)
	}

	// (b) Inflate the coinbase WITH a fixed merkle root: the emission rule
	// itself must reject it.
	tmpl2, err := c.NextBlockTemplate(w.Address())
	if err != nil {
		t.Fatal(err)
	}
	tmpl2.Txs[0].Outputs[0].Value++
	tmpl2.Header.MerkleRoot = core.MerkleRoot(tmpl2.Txs)
	err = c.AcceptBlock(tmpl2)
	if err == nil || !strings.Contains(err.Error(), "coinbase pays") {
		t.Fatalf("inflated coinbase not rejected: %v", err)
	}

	// (c) Lie about the height.
	tmpl3, err := c.NextBlockTemplate(w.Address())
	if err != nil {
		t.Fatal(err)
	}
	tmpl3.Header.Height += 5
	if err := c.AcceptBlock(tmpl3); err == nil {
		t.Fatal("wrong height accepted")
	}

	// (d) Spend an output that does not exist.
	ghost := &core.Tx{
		Inputs:  []core.TxInput{{Prev: core.OutPoint{TxID: [32]byte{0xde, 0xad}, Index: 0}, PubKey: w.PubBytes()}},
		Outputs: []core.TxOutput{{Value: 1, Addr: w.Address()}},
	}
	digest := ghost.SigDigest()
	ghost.Inputs[0].Signature = w.Sign(digest[:])
	if err := c.SubmitTx(ghost); err == nil {
		t.Fatal("spend of nonexistent output accepted")
	}

	// The chain must be completely unaffected by all of the above.
	h, _, _ := c.TipInfo()
	if h != 2 {
		t.Fatalf("chain height changed to %d during attack attempts", h)
	}
}
