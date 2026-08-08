package core

import (
	"math/big"

	"golang.org/x/crypto/argon2"
)

// PowHash is the Argon2id proof-of-work hash of a serialized header. The
// previous block hash is the salt, binding every attempt to the chain tip.
// The memory-hard parameters keep ordinary CPUs competitive with custom
// hardware — anyone with a PC (or a solar-powered mini computer) can mine.
func PowHash(header []byte, prevHash [32]byte) [32]byte {
	out := argon2.IDKey(header, prevHash[:], PowTimeCost, PowMemoryKiB, 1, 32)
	var h [32]byte
	copy(h[:], out)
	return h
}

// CheckPow verifies that a header's PoW hash meets its target.
func CheckPow(h *BlockHeader) bool {
	ph := PowHash(h.Serialize(), h.PrevHash)
	return new(big.Int).SetBytes(ph[:]).Cmp(new(big.Int).SetBytes(h.Target[:])) <= 0
}

func TargetToBytes(t *big.Int) [32]byte {
	var out [32]byte
	t.FillBytes(out[:])
	return out
}
