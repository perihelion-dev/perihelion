package core

import (
	"fmt"

	"github.com/cloudflare/circl/sign/mldsa/mldsa65"
)

// SigScheme is the post-quantum signature scheme used for all transactions:
// ML-DSA-65 (FIPS 204), NIST security category 3.
var SigScheme = mldsa65.Scheme()

type OutPoint struct {
	TxID  [32]byte
	Index uint32
}

type TxInput struct {
	Prev      OutPoint
	PubKey    []byte // ML-DSA-65 public key; must hash to the spent output's Addr
	Signature []byte
}

type TxOutput struct {
	Value uint64   // peri
	Addr  [32]byte // SHA3-256 of the recipient's public key
}

// Tx is a UTXO-style transaction. A coinbase transaction has no inputs and
// carries the block height in Extra, which makes its ID unique per block.
type Tx struct {
	Inputs  []TxInput
	Outputs []TxOutput
	Extra   []byte
}

func (t *Tx) IsCoinbase() bool { return len(t.Inputs) == 0 }

// serializeCore encodes everything except public keys and signatures. It is
// the preimage for both the transaction ID and the signing digest, so
// signatures can never be malleated into a different ID.
func (t *Tx) serializeCore() []byte {
	w := &buf{}
	w.u32(uint32(len(t.Inputs)))
	for i := range t.Inputs {
		w.raw(t.Inputs[i].Prev.TxID[:])
		w.u32(t.Inputs[i].Prev.Index)
	}
	w.u32(uint32(len(t.Outputs)))
	for i := range t.Outputs {
		w.u64(t.Outputs[i].Value)
		w.raw(t.Outputs[i].Addr[:])
	}
	w.bytes(t.Extra)
	return w.b
}

func (t *Tx) ID() [32]byte        { return H([]byte("PER:txid"), t.serializeCore()) }
func (t *Tx) SigDigest() [32]byte { return H([]byte("PER:sig"), t.serializeCore()) }

func (t *Tx) Serialize() []byte {
	w := &buf{}
	w.u32(uint32(len(t.Inputs)))
	for i := range t.Inputs {
		w.raw(t.Inputs[i].Prev.TxID[:])
		w.u32(t.Inputs[i].Prev.Index)
		w.bytes(t.Inputs[i].PubKey)
		w.bytes(t.Inputs[i].Signature)
	}
	w.u32(uint32(len(t.Outputs)))
	for i := range t.Outputs {
		w.u64(t.Outputs[i].Value)
		w.raw(t.Outputs[i].Addr[:])
	}
	w.bytes(t.Extra)
	return w.b
}

func DeserializeTx(b []byte) (*Tx, error) {
	r := &rdr{b: b}
	t := &Tx{}
	nIn := int(r.u32())
	if nIn > 10_000 {
		return nil, errTruncated
	}
	for i := 0; i < nIn; i++ {
		var in TxInput
		in.Prev.TxID = r.arr32()
		in.Prev.Index = r.u32()
		in.PubKey = r.bytes(MaxTxBytes)
		in.Signature = r.bytes(MaxTxBytes)
		t.Inputs = append(t.Inputs, in)
	}
	nOut := int(r.u32())
	if nOut > 10_000 {
		return nil, errTruncated
	}
	for i := 0; i < nOut; i++ {
		var o TxOutput
		o.Value = r.u64()
		o.Addr = r.arr32()
		t.Outputs = append(t.Outputs, o)
	}
	t.Extra = r.bytes(MaxTxBytes)
	if err := r.done(); err != nil {
		return nil, err
	}
	return t, nil
}

// AddrOfPubKey derives the address commitment for a public key.
func AddrOfPubKey(pub []byte) [32]byte { return H([]byte("PER:addr"), pub) }

// VerifySignatures checks that every input's public key matches the address of
// the output it spends and carries a valid ML-DSA-65 signature over the tx digest.
func (t *Tx) VerifySignatures(addrOf func(OutPoint) ([32]byte, bool)) error {
	digest := t.SigDigest()
	for i := range t.Inputs {
		in := &t.Inputs[i]
		want, ok := addrOf(in.Prev)
		if !ok {
			return fmt.Errorf("input %d: unknown or spent output", i)
		}
		if AddrOfPubKey(in.PubKey) != want {
			return fmt.Errorf("input %d: public key does not match output address", i)
		}
		pub, err := SigScheme.UnmarshalBinaryPublicKey(in.PubKey)
		if err != nil {
			return fmt.Errorf("input %d: bad public key: %w", i, err)
		}
		if !SigScheme.Verify(pub, digest[:], in.Signature, nil) {
			return fmt.Errorf("input %d: invalid signature", i)
		}
	}
	return nil
}

// CoinbaseExtra is the required Extra payload of a coinbase at the given height.
func CoinbaseExtra(height uint64) []byte { return be64(height) }
