package core

import "math/big"

var MaxTarget = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

// MinDifficulty is the minimum expected number of hash attempts per block.
// Consensus-critical: a variable only so tests can lower it.
var MinDifficulty = big.NewInt(16)

func InitialTarget() *big.Int { return new(big.Int).Div(MaxTarget, MinDifficulty) }

// DifficultyOf converts a block's target into the expected number of hash
// attempts required to find it.
func DifficultyOf(target [32]byte) *big.Int {
	t := new(big.Int).SetBytes(target[:])
	if t.Sign() == 0 {
		return new(big.Int)
	}
	return new(big.Int).Div(MaxTarget, t)
}

// NextTarget returns the required PoW target for the child of the last header
// in headers (a consecutive chain segment, oldest first). Difficulty is fixed
// while the LWMA window fills, then follows LWMA-1 (zawy12): a linearly
// weighted moving average that re-targets every block. Per-block retargeting
// matters for a chain whose solar-powered miners join and leave with the sun;
// Bitcoin's two-week adjustment would swing wildly under such hashrate tides.
func NextTarget(headers []*BlockHeader) [32]byte {
	// Regtest never retargets: every block is minable at the floor, so a
	// test suite or a local experiment never waits on proof-of-work. This is
	// a property of the regtest network only and cannot leak into mainnet,
	// whose Network definition does not set it.
	if Active().AllowMinDifficultyBlocks {
		return TargetToBytes(InitialTarget())
	}
	n := len(headers) - 1
	if n < DifficultyWindow {
		return TargetToBytes(InitialTarget())
	}
	hs := headers[len(headers)-(DifficultyWindow+1):]
	N := int64(DifficultyWindow)
	sumD := new(big.Int)
	var L int64
	for j := int64(1); j <= N; j++ {
		st := hs[j].Time - hs[j-1].Time
		if st < 1 {
			st = 1
		}
		if st > 6*TargetBlockTime {
			st = 6 * TargetBlockTime
		}
		L += j * st
		target := new(big.Int).SetBytes(hs[j].Target[:])
		sumD.Add(sumD, new(big.Int).Div(MaxTarget, target))
	}
	// nextD = sumD * T * (N+1) / (2L); at steady state (st == T) this is avg(D).
	nextD := new(big.Int).Mul(sumD, big.NewInt(TargetBlockTime*(N+1)))
	nextD.Div(nextD, big.NewInt(2*L))
	if nextD.Cmp(MinDifficulty) < 0 {
		nextD.Set(MinDifficulty)
	}
	return TargetToBytes(new(big.Int).Div(MaxTarget, nextD))
}
