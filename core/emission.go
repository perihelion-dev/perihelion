package core

import "math/bits"

// mulQ64 multiplies two Q64 fixed-point fractions in [0,1), flooring.
func mulQ64(x, y uint64) uint64 {
	hi, _ := bits.Mul64(x, y)
	return hi
}

// decayPowQ64 returns f^n in Q64 for f = DecayFactorQ64/2^64 and n >= 1,
// using square-and-multiply with flooring at every step. This exact integer
// definition IS the consensus rule; every node computes identical values on
// every platform — no floating point anywhere near consensus.
func decayPowQ64(n uint64) uint64 {
	base := DecayFactorQ64
	var result uint64
	have := false
	for n > 0 {
		if n&1 == 1 {
			if !have {
				result = base
				have = true
			} else {
				result = mulQ64(result, base)
			}
		}
		n >>= 1
		if n > 0 {
			base = mulQ64(base, base)
		}
	}
	return result
}

// BlockSubsidy returns the base emission of a block, in peri. Height 0 (the
// genesis block) carries no subsidy: fair launch, no premine.
func BlockSubsidy(height uint64) uint64 {
	if height == 0 {
		return 0
	}
	e := height - 1
	if e == 0 {
		return InitialReward
	}
	f := decayPowQ64(e)
	hi, _ := bits.Mul64(InitialReward, f)
	return hi
}

// SplitFee divides a transaction fee into its burned part and the part added
// to the miner reward pool (50/50, odd remainder to the pool).
func SplitFee(fee uint64) (burn, pool uint64) {
	burn = fee / 2
	return burn, fee - burn
}

// PoolPayout is the amount the reward pool releases to the next block's miner.
func PoolPayout(poolBalance uint64) uint64 { return poolBalance / PoolPayoutBlocks }
