package core_test

import (
	"context"
	"testing"

	"perihelion/core"
	"perihelion/wallet"
)

// TestTxIndexAndReference covers the two properties a payment-verifying
// service depends on: a confirmed transaction can be found by id in one
// lookup, and the reference the sender attached survives verbatim.
func TestTxIndexAndReference(t *testing.T) {
	shrinkPow(t)
	c := openTestChain(t)
	payer, err := wallet.New()
	if err != nil {
		t.Fatal(err)
	}
	payee, err := wallet.New()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := core.Mine(ctx, c, payer.Address(), 22, nil); err != nil {
		t.Fatal(err)
	}

	amount, _ := core.ParseAmount("1.25")
	fee, _ := core.ParseAmount("0.001")
	const ref = "invoice-7f3a91"
	tx, err := wallet.BuildSendWithRef(c, payer, payee.Address(), amount, fee, []byte(ref))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SubmitTx(tx); err != nil {
		t.Fatal(err)
	}
	if err := core.Mine(ctx, c, payer.Address(), 1, nil); err != nil {
		t.Fatal(err)
	}

	got, height, err := c.TxByID(tx.ID())
	if err != nil {
		t.Fatalf("confirmed transaction not found by id: %v", err)
	}
	if height != 23 {
		t.Fatalf("indexed at height %d, want 23", height)
	}
	if string(got.Extra) != ref {
		t.Fatalf("reference = %q, want %q", got.Extra, ref)
	}
	var paid uint64
	for i := range got.Outputs {
		if got.Outputs[i].Addr == payee.Address() {
			paid += got.Outputs[i].Value
		}
	}
	if paid != amount {
		t.Fatalf("payee received %d, want %d", paid, amount)
	}

	// An unknown id must not resolve to anything.
	var bogus [32]byte
	bogus[0] = 0xab
	if _, _, err := c.TxByID(bogus); err == nil {
		t.Fatal("unknown transaction id resolved")
	}
}

// TestReferenceLengthEnforced ensures the consensus cap is enforced before a
// transaction is ever built, rather than surfacing later as a rejected block.
func TestReferenceLengthEnforced(t *testing.T) {
	shrinkPow(t)
	c := openTestChain(t)
	w, err := wallet.New()
	if err != nil {
		t.Fatal(err)
	}
	if err := core.Mine(context.Background(), c, w.Address(), 22, nil); err != nil {
		t.Fatal(err)
	}
	amount, _ := core.ParseAmount("1")
	fee, _ := core.ParseAmount("0.001")
	oversized := make([]byte, core.MaxTxExtra+1)
	if _, err := wallet.BuildSendWithRef(c, w, w.Address(), amount, fee, oversized); err == nil {
		t.Fatal("oversized payment reference accepted")
	}
}
