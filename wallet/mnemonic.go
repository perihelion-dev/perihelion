package wallet

import (
	"crypto/sha3"
	"fmt"
	"strings"
)

// Perihelion recovery phrases encode a 256-bit secret as 24 words. The scheme
// mirrors BIP39's structure (256 bits entropy + 8-bit checksum → 24×11 bits)
// but uses Perihelion's own 2048-word list, generated deterministically below
// so the exact list is defined by the code and can never drift. The words are
// short, pronounceable and unambiguous to write down.
var (
	wordlist  []string
	wordIndex map[string]int
)

func init() {
	onsets := []string{
		"b", "d", "f", "g", "h", "j", "k", "l", "m", "n", "p", "r", "s", "t", "v", "z",
		"br", "dr", "fr", "gr", "pr", "tr", "kl", "pl", "sl", "sn", "sp", "st", "sk", "tw", "kr", "fl",
	}
	nuclei := []string{"a", "e", "i", "o", "u", "aa", "ee", "oo"}
	codas := []string{"b", "d", "k", "l", "m", "n", "r", "t"}
	if len(onsets)*len(nuclei)*len(codas) != 2048 {
		panic("perihelion wordlist: syllable counts must multiply to 2048")
	}
	wordlist = make([]string, 0, 2048)
	wordIndex = make(map[string]int, 2048)
	for _, o := range onsets {
		for _, nu := range nuclei {
			for _, c := range codas {
				w := o + nu + c
				if _, dup := wordIndex[w]; dup {
					panic("perihelion wordlist: collision on " + w)
				}
				wordIndex[w] = len(wordlist)
				wordlist = append(wordlist, w)
			}
		}
	}
	if len(wordlist) != 2048 {
		panic("perihelion wordlist: expected 2048 unique words")
	}
}

// entropyToMnemonic encodes exactly 32 bytes of entropy as 24 words.
func entropyToMnemonic(entropy []byte) (string, error) {
	if len(entropy) != 32 {
		return "", fmt.Errorf("entropy must be 32 bytes")
	}
	sum := sha3.Sum256(entropy)
	bits := make([]byte, 0, 33*8)
	appendByteBits := func(b byte) {
		for i := 7; i >= 0; i-- {
			bits = append(bits, (b>>uint(i))&1)
		}
	}
	for _, b := range entropy {
		appendByteBits(b)
	}
	appendByteBits(sum[0]) // 8-bit checksum → 264 bits total = 24×11
	words := make([]string, 24)
	for i := 0; i < 24; i++ {
		idx := 0
		for j := 0; j < 11; j++ {
			idx = idx<<1 | int(bits[i*11+j])
		}
		words[i] = wordlist[idx]
	}
	return strings.Join(words, " "), nil
}

// mnemonicToEntropy decodes a 24-word phrase back to its 32-byte entropy,
// verifying the checksum.
func mnemonicToEntropy(mnemonic string) ([]byte, error) {
	words := strings.Fields(strings.ToLower(strings.TrimSpace(mnemonic)))
	if len(words) != 24 {
		return nil, fmt.Errorf("recovery phrase must be exactly 24 words (got %d)", len(words))
	}
	bits := make([]byte, 0, 264)
	for _, w := range words {
		idx, ok := wordIndex[w]
		if !ok {
			return nil, fmt.Errorf("unknown word in recovery phrase: %q", w)
		}
		for j := 10; j >= 0; j-- {
			bits = append(bits, byte((idx>>uint(j))&1))
		}
	}
	entropy := make([]byte, 32)
	for i := 0; i < 32; i++ {
		var b byte
		for j := 0; j < 8; j++ {
			b = b<<1 | bits[i*8+j]
		}
		entropy[i] = b
	}
	sum := sha3.Sum256(entropy)
	var chk byte
	for j := 0; j < 8; j++ {
		chk = chk<<1 | bits[256+j]
	}
	if chk != sum[0] {
		return nil, fmt.Errorf("recovery phrase checksum does not match — please re-check the words")
	}
	return entropy, nil
}
