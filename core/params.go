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

	//

	// GenesisTime: 2026-08-09 00:00:00 UTC — the mainnet start. The genesis
	// block carries no reward and no transactions: fair launch, no premine,
	// no founder allocation. Every PER in existence was mined after this
	// instant, by whoever was running a node.
	MainnetGenesisTime int64 = 1_786_233_600

	// MaxFutureDrift bounds how far a block timestamp may run ahead of wall clock.
	MaxFutureDrift int64 = 120
)

// SighashChainIDHeight is the height from which transaction signatures must
// commit to the chain's identity (see Tx.SigDigestV2). Below it both the
// original and the chain-bound digest are accepted; from it, only the
// chain-bound form is valid.
//
// Originally scheduled for block 60,000. Brought forward to 15,000 on
// 2026-08-17 (roughly 36 hours' notice from height 12,825) because the
// original margin bought nothing: the chain has carried fourteen transactions
// in total, all long confirmed, so there is no pending old-format signature
// to invalidate — and every day the switch waits is a day a fork sharing this
// history could replay a payment. Wallets have signed chain-bound since the
// change was introduced. Nodes on the network at the time of this change
// already accept the chain-bound form, so no upgrade is needed to keep
// following the chain; only a node that refuses to update its consensus rules
// would diverge, and only if a legacy-signed transaction were mined after the
// switch, which no current wallet produces.
const SighashChainIDHeight uint64 = 15_000

// GenesisMessage is committed to by the genesis block's merkle root. It fixes
// the chain's identity: any chain built on a different message is a different
// currency and can never merge with this one.
const MainnetGenesisMessage = "Perihelion genesis 2026-08-09 — quantum-safe money, mined by everyone, owned by no one"

// Mempool limits. These are node policy rather than consensus: they bound what
// a node is willing to hold and relay, and an operator changing them never
// forks the chain. Without a bound, anyone able to pay the minimum fee could
// grow every node's database without limit — cheap on a young chain.
// Variables so that tests can shrink them.
var (
	MaxMempoolTxs   = 5_000
	MaxMempoolBytes = 32 << 20 // 32 MiB
)

// Argon2id proof-of-work parameters. Consensus-critical: changing them forks
// the chain. Declared as variables only so tests can shrink the memory cost.
var (
	PowMemoryKiB uint32 = 64 * 1024 // 64 MiB per attempt: CPU-friendly, ASIC-hostile
	PowTimeCost  uint32 = 1
)
