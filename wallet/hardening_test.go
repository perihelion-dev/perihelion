package wallet_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"perihelion/wallet"
)

// TestTamperedKDFParamsRejected ensures a modified wallet file cannot force an
// unbounded key-derivation allocation when the user types their password.
func TestTamperedKDFParamsRejected(t *testing.T) {
	w, _, err := wallet.Create("password123")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "wallet.json")
	if err := w.Save(path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m["argon_mem_kib"] = 64 * 1024 * 1024 // 64 GiB
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := wallet.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	err = loaded.Unlock("password123")
	if err == nil {
		t.Fatal("absurd KDF parameters were accepted")
	}
	if !strings.Contains(err.Error(), "key-derivation") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestCiphertextBoundToAddress ensures the stored public key is authenticated:
// swapping it invalidates the ciphertext rather than silently yielding a
// wallet whose displayed address does not match its key.
func TestCiphertextBoundToAddress(t *testing.T) {
	w1, _, err := wallet.Create("password123")
	if err != nil {
		t.Fatal(err)
	}
	w2, _, err := wallet.Create("password123")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	p1 := filepath.Join(dir, "w1.json")
	p2 := filepath.Join(dir, "w2.json")
	if err := w1.Save(p1); err != nil {
		t.Fatal(err)
	}
	if err := w2.Save(p2); err != nil {
		t.Fatal(err)
	}
	var f1, f2 map[string]any
	b1, _ := os.ReadFile(p1)
	b2, _ := os.ReadFile(p2)
	json.Unmarshal(b1, &f1)
	json.Unmarshal(b2, &f2)

	f1["pub"] = f2["pub"] // graft a foreign address onto our ciphertext
	out, _ := json.Marshal(f1)
	if err := os.WriteFile(p1, out, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := wallet.Load(p1)
	if err != nil {
		t.Fatal(err)
	}
	if err := loaded.Unlock("password123"); err == nil {
		t.Fatal("swapped public key was accepted — AAD binding is not effective")
	}
}
