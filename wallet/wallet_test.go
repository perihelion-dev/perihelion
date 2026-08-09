package wallet_test

import (
	"path/filepath"
	"testing"

	"perihelion/wallet"
)

func TestCreateUnlockRoundtrip(t *testing.T) {
	w, mnemonic, err := wallet.Create("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if w.Locked() {
		t.Fatal("freshly created wallet should be unlocked")
	}
	addr := w.Address()

	// Persist, reload (locked), unlock with the right password.
	path := filepath.Join(t.TempDir(), "wallet.json")
	if err := w.Save(path); err != nil {
		t.Fatal(err)
	}
	enc, err := wallet.IsEncrypted(path)
	if err != nil || !enc {
		t.Fatalf("saved wallet should be encrypted (enc=%v err=%v)", enc, err)
	}
	w2, err := wallet.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !w2.Locked() {
		t.Fatal("loaded v2 wallet must start locked")
	}
	if w2.Address() != addr {
		t.Fatal("address must be visible without unlocking")
	}
	if err := w2.Unlock("wrong password"); err == nil {
		t.Fatal("wrong password must fail")
	}
	if err := w2.Unlock("correct horse battery staple"); err != nil {
		t.Fatalf("correct password failed: %v", err)
	}
	if w2.Locked() || w2.Address() != addr {
		t.Fatal("unlock did not restore the key")
	}

	// The recovery phrase alone reconstructs the exact same address.
	w3, err := wallet.Restore(mnemonic, "a different password")
	if err != nil {
		t.Fatal(err)
	}
	if w3.Address() != addr {
		t.Fatal("restore from phrase produced a different address")
	}
}

func TestMnemonicChecksum(t *testing.T) {
	_, mnemonic, err := wallet.Create("password123")
	if err != nil {
		t.Fatal(err)
	}
	// Corrupting a word must be detected by the checksum / wordlist check.
	if _, err := wallet.Restore(mnemonic+" extra", "password123"); err == nil {
		t.Fatal("wrong word count accepted")
	}
	if _, err := wallet.Restore("notaword "+mnemonic, "password123"); err == nil {
		t.Fatal("invalid words accepted")
	}
}

func TestSignRequiresUnlock(t *testing.T) {
	w, _, err := wallet.Create("password123")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "wallet.json")
	if err := w.Save(path); err != nil {
		t.Fatal(err)
	}
	locked, err := wallet.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("signing a locked wallet should panic")
		}
	}()
	locked.Sign([]byte("digest"))
}
