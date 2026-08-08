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
./perihelion mine                     # solo mining
./perihelion balance
./perihelion send --to per1... --amount 1.5
./perihelion info
```

Run a networked node (P2P gossip + sync + local RPC):

```
./perihelion node --connect seed.example.org:16180 --mine
```

One binary, no configuration. Mining uses your CPU cores with Argon2id
(64 MiB per attempt) — the same hardware budget for everyone.

## Local RPC (the machine interface)

The node serves an authenticated JSON API on `127.0.0.1:16181` — loopback
only, protected by a token in `~/.perihelion/rpc-token`. This is how scripts
and AI agents hold PER and pay with it:

```
curl -H "X-Auth: $(cat ~/.perihelion/rpc-token)" http://127.0.0.1:16181/status
curl -H "X-Auth: $(cat ~/.perihelion/rpc-token)" http://127.0.0.1:16181/balance
curl -H "X-Auth: $(cat ~/.perihelion/rpc-token)" -X POST \
     -d '{"to":"per1...","amount":"1.5"}' http://127.0.0.1:16181/send
```

## Security posture

- **No telemetry, no phone-home.** The node only ever talks to peers you
  explicitly configure. `core` and `wallet` contain zero networking code.
- **Peers are untrusted.** Every message is size-capped, every received block
  and transaction is fully re-validated by consensus before it touches state.
- **Keys never leave your machine.** The wallet is a local file (mode 0600);
  the RPC binds to loopback and requires the local auth token.
- **Memory-safe implementation** (Go), no `unsafe`, dependencies pinned by
  hash in `go.sum`, `govulncheck` clean.

## Status

Experimental. Milestones 1–2 are complete: a fully validating chain with
post-quantum wallets, mining, fork choice with atomic reorgs (heaviest chain
wins), P2P block/tx gossip with initial sync, and a local RPC. Covered by
end-to-end, adversarial and two-node network tests. Not yet audited; do not
store value you cannot afford to lose.

Roadmap:
- [x] P2P networking: gossip, sync, fork choice (heaviest chain), reorgs
- [x] Local JSON-RPC API (wallet + status)
- [ ] Public testnet with seed nodes
- [ ] Wallet file encryption (passphrase)
- [ ] Block explorer
- [ ] Independent security review
- [ ] Mainnet genesis

## License

MIT. No premine, no token sale, no company. The code is the whole story.
