// Package wallet implements Perihelion key management: post-quantum
// ML-DSA-65 keypairs derived from a 24-word recovery phrase, encrypted at
// rest with the owner's password (Argon2id + AES-256-GCM), plus addresses and
// transaction building.
package wallet

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cloudflare/circl/sign"
	"golang.org/x/crypto/argon2"

	"perihelion/core"
)

// Wallet holds a public key and, once unlocked, the private key. The public
// key (and thus the address) is always available; signing requires an unlocked
// wallet.
type Wallet struct {
	pub  sign.PublicKey
	priv sign.PrivateKey // nil when locked
	enc  *encFile        // present for v2 (encrypted) wallets
}

// Argon2id work factors. Deliberately strong: unlocking a wallet is rare, so
// a brute-force guesser pays ~64 MiB and real CPU time per attempt.
const (
	argonTime    = 3
	argonMemKiB  = 64 * 1024
	argonThreads = 4

	// Upper bounds accepted when reading a wallet file. The parameters are
	// stored per wallet so that future versions can strengthen them, but a
	// tampered file must not be able to demand an unbounded allocation.
	maxArgonTime    = 16
	maxArgonMemKiB  = 1024 * 1024 // 1 GiB
	maxArgonThreads = 16
)

// zero overwrites a secret buffer once it is no longer needed. Go offers no
// guarantee against copies made by the garbage collector, so this narrows the
// window rather than closing it.
func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// New generates a fresh in-memory keypair with no seed phrase and no
// encryption. Used for tests and ephemeral keys; real wallets use Create.
func New() (*Wallet, error) {
	pub, priv, err := core.SigScheme.GenerateKey()
	if err != nil {
		return nil, err
	}
	return &Wallet{pub: pub, priv: priv}, nil
}

// Create makes a new wallet: 32 bytes of entropy become both the 24-word
// recovery phrase and the seed the ML-DSA-65 keypair is derived from. The
// returned mnemonic is shown to the user ONCE; it is never written to disk in
// clear. The wallet is returned unlocked and its encrypted form ready to Save.
func Create(password string) (w *Wallet, mnemonic string, err error) {
	if password == "" {
		return nil, "", fmt.Errorf("a password is required to protect the wallet")
	}
	entropy := make([]byte, 32)
	if _, err := rand.Read(entropy); err != nil {
		return nil, "", err
	}
	mnemonic, err = entropyToMnemonic(entropy)
	if err != nil {
		return nil, "", err
	}
	w, err = fromEntropy(entropy, password)
	zero(entropy)
	if err != nil {
		return nil, "", err
	}
	return w, mnemonic, nil
}

// Restore rebuilds a wallet from its 24-word recovery phrase and re-encrypts
// it under the given password. This is how a wallet moves to a new machine or
// recovers from a lost file — the phrase alone is sufficient.
func Restore(mnemonic, password string) (*Wallet, error) {
	if password == "" {
		return nil, fmt.Errorf("a password is required to protect the wallet")
	}
	entropy, err := mnemonicToEntropy(mnemonic)
	if err != nil {
		return nil, err
	}
	return fromEntropy(entropy, password)
}

func fromEntropy(entropy []byte, password string) (*Wallet, error) {
	if core.SigScheme.SeedSize() != 32 {
		return nil, fmt.Errorf("unexpected seed size %d", core.SigScheme.SeedSize())
	}
	pub, priv := core.SigScheme.DeriveKey(entropy)
	pb, err := pub.MarshalBinary()
	if err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	nonce := make([]byte, 12)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemKiB, argonThreads, 32)
	gcm, err := newGCM(key)
	zero(key)
	if err != nil {
		return nil, err
	}
	ct := gcm.Seal(nil, nonce, entropy, pb) // pubkey as AAD binds ciphertext to address
	return &Wallet{
		pub:  pub,
		priv: priv,
		enc: &encFile{
			Version: 2, Scheme: core.SigScheme.Name(),
			Pub: b64(pb), KDF: "argon2id",
			Time: argonTime, MemKiB: argonMemKiB, Threads: argonThreads,
			Salt: b64(salt), Nonce: b64(nonce), Ciphertext: b64(ct),
		},
	}, nil
}

// Unlock derives the private key from an encrypted wallet using the password.
// A wrong password fails the AEAD tag check and returns an error; it can never
// yield a wrong-but-usable key.
func (w *Wallet) Unlock(password string) error {
	if w.priv != nil {
		return nil
	}
	if w.enc == nil {
		return fmt.Errorf("wallet has no encrypted key material")
	}
	e := w.enc
	salt, err := unb64(e.Salt)
	if err != nil {
		return err
	}
	nonce, err := unb64(e.Nonce)
	if err != nil {
		return err
	}
	ct, err := unb64(e.Ciphertext)
	if err != nil {
		return err
	}
	pb, err := unb64(e.Pub)
	if err != nil {
		return err
	}
	if err := e.checkKDFParams(); err != nil {
		return err
	}
	key := argon2.IDKey([]byte(password), salt, e.Time, e.MemKiB, e.Threads, 32)
	gcm, err := newGCM(key)
	zero(key)
	if err != nil {
		return err
	}
	entropy, err := gcm.Open(nil, nonce, ct, pb)
	if err != nil {
		return fmt.Errorf("wrong password")
	}
	pub, priv := core.SigScheme.DeriveKey(entropy)
	zero(entropy)
	w.pub, w.priv = pub, priv
	return nil
}

// checkKDFParams rejects key-derivation parameters outside the supported
// range, so a tampered wallet file cannot force a pathological allocation.
func (e *encFile) checkKDFParams() error {
	if e.KDF != "argon2id" {
		return fmt.Errorf("unsupported key derivation %q", e.KDF)
	}
	if e.Time == 0 || e.Time > maxArgonTime ||
		e.MemKiB == 0 || e.MemKiB > maxArgonMemKiB ||
		e.Threads == 0 || e.Threads > maxArgonThreads {
		return fmt.Errorf("wallet file declares unsupported key-derivation parameters")
	}
	return nil
}

// Locked reports whether the wallet still needs a password to sign.
func (w *Wallet) Locked() bool { return w.priv == nil }

// RevealMnemonic reconstructs the recovery phrase from an unlocked wallet, so
// the app can show it again to a user who wants to back it up later. Requires
// the wallet to be unlocked.
func (w *Wallet) RevealMnemonic(password string) (string, error) {
	if w.enc == nil {
		return "", fmt.Errorf("this wallet has no recovery phrase")
	}
	e := w.enc
	if err := e.checkKDFParams(); err != nil {
		return "", err
	}
	salt, _ := unb64(e.Salt)
	nonce, _ := unb64(e.Nonce)
	ct, _ := unb64(e.Ciphertext)
	pb, _ := unb64(e.Pub)
	key := argon2.IDKey([]byte(password), salt, e.Time, e.MemKiB, e.Threads, 32)
	gcm, err := newGCM(key)
	zero(key)
	if err != nil {
		return "", err
	}
	entropy, err := gcm.Open(nil, nonce, ct, pb)
	if err != nil {
		return "", fmt.Errorf("wrong password")
	}
	m, err := entropyToMnemonic(entropy)
	zero(entropy)
	return m, err
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (w *Wallet) PubBytes() []byte {
	b, err := w.pub.MarshalBinary()
	if err != nil {
		panic(err)
	}
	return b
}

func (w *Wallet) Address() [32]byte { return core.AddrOfPubKey(w.PubBytes()) }

// Sign signs a digest. Panics if the wallet is locked — callers must Unlock
// first; this guards against silently producing an unsigned transaction.
func (w *Wallet) Sign(digest []byte) []byte {
	if w.priv == nil {
		panic("wallet is locked")
	}
	return core.SigScheme.Sign(w.priv, digest, nil)
}

// --- persistence ---

type encFile struct {
	Version    int    `json:"version"`
	Scheme     string `json:"scheme"`
	Pub        string `json:"pub"`
	KDF        string `json:"kdf"`
	Time       uint32 `json:"argon_time"`
	MemKiB     uint32 `json:"argon_mem_kib"`
	Threads    uint8  `json:"argon_threads"`
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

// legacyFile is the original v1 plaintext format (no password). Still loadable
// so early wallets keep working, but Create only ever writes v2.
type legacyFile struct {
	Scheme string `json:"scheme"`
	Pub    string `json:"pub"`
	Priv   string `json:"priv"`
}

func (w *Wallet) Save(path string) error {
	if w.enc == nil {
		return fmt.Errorf("refusing to save an unencrypted wallet; use Create/Restore")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(w.enc, "", "  ")
	if err != nil {
		return err
	}
	return writeFilePrivate(path, data)
}

// writeFilePrivate writes atomically with 0600 perms (owner-only).
func writeFilePrivate(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Load reads a wallet file. A v2 file returns a LOCKED wallet (call Unlock);
// a v1 plaintext file returns an unlocked wallet for backward compatibility.
func Load(path string) (*Wallet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var probe struct {
		Version int    `json:"version"`
		Priv    string `json:"priv"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, err
	}
	if probe.Version >= 2 || probe.Priv == "" {
		var e encFile
		if err := json.Unmarshal(data, &e); err != nil {
			return nil, err
		}
		if e.Scheme != core.SigScheme.Name() {
			return nil, fmt.Errorf("wallet uses scheme %q, node expects %q", e.Scheme, core.SigScheme.Name())
		}
		pb, err := unb64(e.Pub)
		if err != nil {
			return nil, err
		}
		pub, err := core.SigScheme.UnmarshalBinaryPublicKey(pb)
		if err != nil {
			return nil, err
		}
		return &Wallet{pub: pub, enc: &e}, nil
	}
	// v1 plaintext
	var lf legacyFile
	if err := json.Unmarshal(data, &lf); err != nil {
		return nil, err
	}
	if lf.Scheme != core.SigScheme.Name() {
		return nil, fmt.Errorf("wallet uses scheme %q, node expects %q", lf.Scheme, core.SigScheme.Name())
	}
	pb, err := unb64(lf.Pub)
	if err != nil {
		return nil, err
	}
	sk, err := unb64(lf.Priv)
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

// SaveLegacy writes an unencrypted v1 wallet. Only for tests and the ephemeral
// keys created by New; real wallets are always encrypted.
func (w *Wallet) SaveLegacy(path string) error {
	if w.priv == nil {
		return fmt.Errorf("cannot save a locked wallet in legacy format")
	}
	pb, err := w.pub.MarshalBinary()
	if err != nil {
		return err
	}
	sk, err := w.priv.MarshalBinary()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(legacyFile{
		Scheme: core.SigScheme.Name(), Pub: b64(pb), Priv: b64(sk),
	}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeFilePrivate(path, data)
}

// IsEncrypted reports whether the file at path is a v2 (password-protected)
// wallet.
func IsEncrypted(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	var probe struct {
		Version int    `json:"version"`
		Priv    string `json:"priv"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return false, err
	}
	return probe.Version >= 2 || probe.Priv == "", nil
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
func unb64(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

// --- addresses & sending ---

// EncodeAddress and DecodeAddress are defined in core, where the address
// format belongs; they are re-exported here so existing callers keep working.
func EncodeAddress(a [32]byte) string         { return core.EncodeAddress(a) }
func DecodeAddress(s string) ([32]byte, error) { return core.DecodeAddress(s) }

// BuildSend creates and signs a transaction sending amount (in peri) to `to`,
// paying fee, with change returned to the wallet's own address.
func BuildSend(c *core.Chain, w *Wallet, to [32]byte, amount, fee uint64) (*core.Tx, error) {
	return BuildSendWithRef(c, w, to, amount, fee, nil)
}

// BuildSendWithRef is BuildSend with a payment reference recorded in the
// transaction. The reference is public and permanent — it is committed to by
// the transaction id and therefore by the block — so it is meant for invoice
// or order identifiers that let a recipient match a payment automatically,
// not for anything private. Keep it short; consensus caps it at
// core.MaxTxExtra bytes.
func BuildSendWithRef(c *core.Chain, w *Wallet, to [32]byte, amount, fee uint64, ref []byte) (*core.Tx, error) {
	if len(ref) > core.MaxTxExtra {
		return nil, fmt.Errorf("payment reference must be at most %d bytes", core.MaxTxExtra)
	}
	return buildSend(c, w, to, amount, fee, ref)
}

func buildSend(c *core.Chain, w *Wallet, to [32]byte, amount, fee uint64, ref []byte) (*core.Tx, error) {
	if w.Locked() {
		return nil, fmt.Errorf("wallet is locked — unlock it with your password first")
	}
	if amount == 0 {
		return nil, fmt.Errorf("amount must be positive")
	}
	need := amount + fee
	utxos, err := c.ListSpendable(w.Address(), need)
	if err != nil {
		return nil, err
	}
	var sum uint64
	tx := &core.Tx{Extra: ref}
	for _, u := range utxos {
		sum += u.Value
		tx.Inputs = append(tx.Inputs, core.TxInput{Prev: u.Out})
	}
	tx.Outputs = append(tx.Outputs, core.TxOutput{Value: amount, Addr: to})
	if change := sum - need; change > 0 {
		tx.Outputs = append(tx.Outputs, core.TxOutput{Value: change, Addr: w.Address()})
	}
	// Always sign chain-bound. Below the switchover height both forms verify,
	// so this loses nothing; at and above it, only this form is valid. Signing
	// the legacy form for any reason would create a transaction that a fork
	// sharing this history could replay.
	digest := tx.SigDigestV2()
	pb := w.PubBytes()
	sig := w.Sign(digest[:])
	for i := range tx.Inputs {
		tx.Inputs[i].PubKey = pb
		tx.Inputs[i].Signature = sig
	}
	return tx, nil
}
