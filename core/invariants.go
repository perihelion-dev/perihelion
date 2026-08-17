package core

import "fmt"

// Money invariants, checked on every block as it connects.
//
// The rules that keep the supply honest are already enforced piecemeal during
// validation — no duplicate input, outputs never exceed inputs, the coinbase
// pays exactly subsidy plus pool payout, emission never exceeds the bound. A
// previously fixed critical bug — duplicate inputs minting coins from nothing
// — is exactly the class where a single missed check becomes consensus. So in
// addition to the piecemeal checks, the block as a whole is reconciled here,
// with arithmetic that cannot overflow, before it is allowed to connect:
//
//   value in  =  value out  +  fees              (per transaction and per block)
//   fees      =  burned + pool contribution      (exactly, no rounding loss)
//   coinbase  =  subsidy + pool payout           (checked in connectTip)
//   Σ UTXO after == Σ UTXO before − in + out + coinbase   (implied by the above)
//
// These are not new rules. They are the existing rules restated as a sum, so
// that a defect in any single check is caught by the total. If a block ever
// fails here it means validation let something through, and refusing the
// block is the correct response even though we cannot say which check erred.

// addChecked adds two amounts and fails on overflow. Amounts are bounded by
// MaxSupply per output and 1,000 outputs per transaction, so overflow is not
// reachable through valid data; the check exists so that if any bound is ever
// relaxed by mistake, arithmetic fails loudly rather than wrapping silently.
func addChecked(a, b uint64) (uint64, error) {
	if a > ^uint64(0)-b {
		return 0, fmt.Errorf("amount overflow")
	}
	return a + b, nil
}

// blockLedger accumulates the money flow of one block.
type blockLedger struct {
	in, out, fees, burned, pooled uint64
}

// addTx records a validated non-coinbase transaction. fee is what checkTx
// computed; in and out are re-summed here independently so that the ledger
// does not simply trust the value it is meant to check.
func (l *blockLedger) addTx(t *Tx, inSum uint64, fee uint64) error {
	var out uint64
	var err error
	for i := range t.Outputs {
		if out, err = addChecked(out, t.Outputs[i].Value); err != nil {
			return err
		}
	}
	// The one arithmetic identity every transaction must satisfy.
	expectFee := inSum - out // safe: checkTx already established out <= inSum
	if expectFee != fee {
		return fmt.Errorf("fee mismatch: inputs %d − outputs %d = %d, but validation reported %d",
			inSum, out, expectFee, fee)
	}
	if l.in, err = addChecked(l.in, inSum); err != nil {
		return err
	}
	if l.out, err = addChecked(l.out, out); err != nil {
		return err
	}
	if l.fees, err = addChecked(l.fees, fee); err != nil {
		return err
	}
	bn, pl := SplitFee(fee)
	if bn+pl != fee { // SplitFee must conserve value exactly
		return fmt.Errorf("fee split loses value: %d + %d != %d", bn, pl, fee)
	}
	if l.burned, err = addChecked(l.burned, bn); err != nil {
		return err
	}
	if l.pooled, err = addChecked(l.pooled, pl); err != nil {
		return err
	}
	return nil
}

// reconcile verifies the block-level identity once every transaction has been
// added: everything that came in either went out, was burned, or entered the
// pool. Nothing appears, nothing vanishes.
func (l *blockLedger) reconcile() error {
	rhs, err := addChecked(l.out, l.fees)
	if err != nil {
		return err
	}
	if l.in != rhs {
		return fmt.Errorf("block does not balance: in %d != out %d + fees %d", l.in, l.out, l.fees)
	}
	if l.burned+l.pooled != l.fees {
		return fmt.Errorf("fees %d != burned %d + pooled %d", l.fees, l.burned, l.pooled)
	}
	return nil
}
