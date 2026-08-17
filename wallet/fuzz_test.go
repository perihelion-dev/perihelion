package wallet

import "testing"

// FuzzMnemonic: the recovery-phrase parser accepts whatever a user types.
// It must never panic, must reject anything that is not exactly 24 known
// words with a valid checksum, and must round-trip every valid phrase.
func FuzzMnemonic(f *testing.F) {
	// A genuine phrase as a seed.
	entropy := make([]byte, 32)
	for i := range entropy {
		entropy[i] = byte(i * 7)
	}
	valid, _ := entropyToMnemonic(entropy)
	f.Add(valid)
	f.Add("")
	f.Add("bad bad bad")
	f.Add(valid + " extra")
	f.Fuzz(func(t *testing.T, s string) {
		e, err := mnemonicToEntropy(s)
		if err != nil {
			return
		}
		if len(e) != 32 {
			t.Fatalf("accepted phrase produced %d bytes of entropy, want 32", len(e))
		}
		back, err := entropyToMnemonic(e)
		if err != nil {
			t.Fatalf("re-encode failed: %v", err)
		}
		e2, err := mnemonicToEntropy(back)
		if err != nil {
			t.Fatalf("re-decode failed: %v", err)
		}
		for i := range e {
			if e[i] != e2[i] {
				t.Fatal("mnemonic round trip changed the entropy")
			}
		}
	})
}
