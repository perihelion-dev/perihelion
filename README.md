# Perihelion (PER)

**Post-quantum. CPU-mineable. Deflationary. Fair launch.**

Perihelion is a proof-of-work cryptocurrency designed for the decades ahead,
not the decade behind:

| | Bitcoin | Perihelion |
|---|---|---|
| Signatures | ECDSA (breakable by a large quantum computer) | **ML-DSA-65** (FIPS 204, lattice-based, NIST category 3) |
| Hashing | SHA-256 | **SHA3-256** |
| Mining | ASIC farms | **Argon2id, memory-hard — every PC can mine** |
| Block time | 10 minutes | **60 seconds** |
| Difficulty | every 2016 blocks | **every block (LWMA)** — stable even when solar-powered miners come and go with the sun |
| Supply | fixed 21M, miner income decays to fees | **< 30M hard bound + fee burn = shrinking supply**, miners paid forever from a smoothed reward pool |
| Launch | fair | **fair — no premine, no dev fund, genesis pays nobody** |

## Monetary design: circular deflationary emission

1. **Smooth emission, hard bound.** The block subsidy starts at 10 PER and
   decays by the fixed factor 2999999/3000000 per block — a halving every
   ~3.95 years with no halving shocks. Total emission is provably below
   30,000,000 PER. The exact integer formula lives in `core/emission.go`;
   anyone can recompute the entire supply curve.
2. **Fee burn.** Half of every transaction fee is destroyed forever. Real
   usage makes PER scarcer — genuinely deflationary, unlike a fixed cap.
3. **Miner reward pool.** The other half flows into an on-chain pool that pays
   out 1/14,400 of its balance to each block's miner (a ~10-day smoothing
   window). Miner income never falls off a cliff and grows with network usage.

## Quick start

```
go build ./cmd/perihelion
./perihelion wallet new
./perihelion mine
./perihelion balance
./perihelion send --to per1... --amount 1.5
./perihelion info
```

One binary, no configuration. Mining uses your CPU cores with Argon2id
(64 MiB per attempt) — the same hardware budget for everyone.

## Status

Experimental. This is milestone 1: a fully validating single-node chain with
post-quantum wallets, mining, transactions and supply accounting, covered by
an end-to-end test suite. Not yet audited; do not store value you cannot
afford to lose.

Roadmap:
- [ ] P2P networking: gossip, sync, fork choice (heaviest chain), reorgs
- [ ] JSON-RPC API for explorers, exchanges and AI agents
- [ ] Public testnet with seed nodes
- [ ] Independent security review
- [ ] Mainnet genesis

## License

MIT. No premine, no token sale, no company. The code is the whole story.
