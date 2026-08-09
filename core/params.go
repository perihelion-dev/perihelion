// Package core implements Perihelion consensus: blocks, transactions,
// proof-of-work, difficulty and emission.
package core

// Amounts are integers in "peri", the smallest unit: 1 PER = 100,000,000 peri.
const (
	PER uint64 = 100_000_000

	// TargetBlockTime is the desired spacing between blocks, in seconds.
	TargetBlockTime int64 = 60

	// DifficultyWindow is the LWMA window; difficulty stays fixed for the
	// first DifficultyWindow blocks while the window fills.
	DifficultyWindow = 90

	// InitialReward is the block subsidy at height 1: 10 PER.
	InitialReward uint64 = 10 * PER

	// DecayFactorQ64 = floor(2^64 * 2999999/3000000). The subsidy at height h
	// is floor(InitialReward * f^(h-1)) with f = DecayFactorQ64/2^64, so total
	// emission stays strictly below 30,000,000 PER and halves every ~3.95 years.
	DecayFactorQ64 uint64 = 18446737924794860379

	// MaxSupply is the provable upper bound on emission, in peri.
	MaxSupply uint64 = 30_000_000 * PER

	// Fees: burn = fee/2 is destroyed forever; the rest feeds the miner reward
	// pool, which pays out pool/PoolPayoutBlocks per block (a ~10-day smoothing
	// window at 60s blocks), so miner income never falls off a cliff.
	PoolPayoutBlocks uint64 = 14_400

	// CoinbaseMaturity: coinbase outputs unlock after this many confirmations.
	CoinbaseMaturity uint64 = 20

	MaxBlockBytes        = 2_000_000
	MaxTxBytes           = 100_000
	MaxTxExtra           = 80
	MinRelayFee   uint64 = 1_000 // peri

	// GenesisTime: 2026-08-09 00:00:00 UTC — the mainnet start. The genesis
	// block carries no reward and no transactions: fair launch, no premine,
	// no founder allocation. Every PER in existence was mined after this
	// instant, by whoever was running a node.
	GenesisTime int64 = 1_786_233_600

	// MaxFutureDrift bounds how far a block timestamp may run ahead of wall clock.
	MaxFutureDrift int64 = 120
)

// GenesisMessage is committed to by the genesis block's merkle root. It fixes
// the chain's identity: any chain built on a different message is a different
// currency and can never merge with this one.
const GenesisMessage = "Perihelion genesis 2026-08-09 — quantum-safe money, mined by everyone, owned by no one"

// Argon2id proof-of-work parameters. Consensus-critical: changing them forks
// the chain. Declared as variables only so tests can shrink the memory cost.
var (
	PowMemoryKiB uint32 = 64 * 1024 // 64 MiB per attempt: CPU-friendly, ASIC-hostile
	PowTimeCost  uint32 = 1
)
