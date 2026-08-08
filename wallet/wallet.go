// Package wallet implements Perihelion key management: post-quantum
// ML-DSA-65 keypairs, addresses and transaction building.
package wallet

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudflare/circl/sign"

	"perihelion/core"
)

type Wallet struct {
	pub  sign.PublicKey
	priv sign.PrivateKey
}

// New generates a fresh ML-DSA-65 keypair. Every wallet is quantum-safe from
// day one — there is no elliptic-curve fallback anywhere in the protocol.
func New() (*Wallet, error) {
	pub, priv, err := core.SigScheme.GenerateKey()
	if err != nil {
		return nil, err
	}
	return &Wallet{pub: pub, priv: priv}, nil
}

func (w *Wallet) PubBytes() []byte {
	b, err := w.pub.MarshalBinary()
	if err != nil {
		panic(err)
	}
	return b
}

func (w *Wallet) Address() [32]byte { return core.AddrOfPubKey(w.PubBytes()) }

func (w *Wallet) Sign(digest []byte) []byte {
	return core.SigScheme.Sign(w.priv, digest, nil)
}

type walletFile struct {
	Scheme string `json:"scheme"`
	Pub    string `json:"pub"`
	Priv   string `json:"priv"`
}

func (w *Wallet) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	pb, err := w.pub.MarshalBinary()
	if err != nil {
		return err
	}
	sk, err := w.priv.MarshalBinary()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(walletFile{
		Scheme: core.SigScheme.Name(),
		Pub:    base64.StdEncoding.EncodeToString(pb),
		Priv:   base64.StdEncoding.EncodeToString(sk),
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func Load(path string) (*Wallet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var wf walletFile
	if err := json.Unmarshal(data, &wf); err != nil {
		return nil, err
	}
	if wf.Scheme != core.SigScheme.Name() {
		return nil, fmt.Errorf("wallet uses scheme %q, node expects %q", wf.Scheme, core.SigScheme.Name())
	}
	pb, err := base64.StdEncoding.DecodeString(wf.Pub)
	if err != nil {
		return nil, err
	}
	sk, err := base64.StdEncoding.DecodeString(wf.Priv)
	if err != nil {
		return nil, err
	}
	pub, err := core.SigScheme.UnmarshalBinaryPublicKey(pb)
	if err != nil {
		return nil, err
	}
	priv, err := core.SigScheme.UnmarshalBinaryPrivateKey(sk)
	if err != nil {
		return nil, err
	}
	return &Wallet{pub: pub, priv: priv}, nil
}

// EncodeAddress renders an address as "per1" + 64 hex chars + 8 checksum chars.
func EncodeAddress(a [32]byte) string {
	chk := core.H([]byte("PER:chk"), a[:])
	return "per1" + hex.EncodeToString(a[:]) + hex.EncodeToString(chk[:4])
}

func DecodeAddress(s string) ([32]byte, error) {
	var a [32]byte
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "per1") || len(s) != 4+64+8 {
		return a, fmt.Errorf("invalid address format")
	}
	payload, err := hex.DecodeString(s[4 : 4+64])
	if err != nil {
		return a, fmt.Errorf("invalid address encoding")
	}
	copy(a[:], payload)
	chk := core.H([]byte("PER:chk"), a[:])
	if s[4+64:] != hex.EncodeToString(chk[:4]) {
		return a, fmt.Errorf("address checksum mismatch — please re-check for typos")
	}
	return a, nil
}

// BuildSend creates and signs a transaction sending amount (in peri) to `to`,
// paying fee, with change returned to the wallet's own address.
func BuildSend(c *core.Chain, w *Wallet, to [32]byte, amount, fee uint64) (*core.Tx, error) {
	if amount == 0 {
		return nil, fmt.Errorf("amount must be positive")
	}
	need := amount + fee
	utxos, err := c.ListSpendable(w.Address(), need)
	if err != nil {
		return nil, err
	}
	var sum uint64
	tx := &core.Tx{}
	for _, u := range utxos {
		sum += u.Value
		tx.Inputs = append(tx.Inputs, core.TxInput{Prev: u.Out})
	}
	tx.Outputs = append(tx.Outputs, core.TxOutput{Value: amount, Addr: to})
	if change := sum - need; change > 0 {
		tx.Outputs = append(tx.Outputs, core.TxOutput{Value: change, Addr: w.Address()})
	}
	digest := tx.SigDigest()
	pb := w.PubBytes()
	sig := w.Sign(digest[:])
	for i := range tx.Inputs {
		tx.Inputs[i].PubKey = pb
		tx.Inputs[i].Signature = sig
	}
	return tx, nil
}
