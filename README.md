<div align="center">

# Perihelion

**A post-quantum, CPU-mineable, deflationary proof-of-work cryptocurrency.**

Mainnet genesis: 2026-08-09 00:00:00 UTC · No premine · MIT licensed

</div>

---

Perihelion is a from-scratch cryptocurrency built for the next thirty years
rather than the last fifteen. Its signatures are quantum-resistant from block
one, its proof-of-work is designed so ordinary computers stay competitive, and
its monetary policy is deflationary while still paying miners indefinitely.

The consensus implementation is roughly 3,000 lines of Go. That is deliberate:
a monetary system should be small enough that a competent reader can audit it
in an afternoon.

## Design

| | Bitcoin | Perihelion |
|---|---|---|
| Signatures | ECDSA (secp256k1) | **ML-DSA-65** — FIPS 204, NIST security category 3 |
| Hashing | SHA-256 | **SHA3-256** |
| Proof-of-work | SHA-256d (ASIC-dominated) | **Argon2id**, 64 MiB per attempt (memory-hard) |
| Block interval | 10 minutes | **60 seconds** |
| Difficulty adjustment | every 2,016 blocks | **every block** (LWMA) |
| Supply | 21,000,000 fixed | **< 30,000,000** plus continuous fee burn |
| Long-run miner revenue | transaction fees only | **fee burn + smoothed reward pool** |

### Post-quantum cryptography

Every signature in Perihelion is ML-DSA-65 (Dilithium), standardised by NIST
in FIPS 204 and believed secure against both classical and quantum
adversaries. There is no elliptic-curve fallback anywhere in the protocol, so
there is no migration to perform later and no legacy path for an attacker to
target. Addresses are SHA3-256 commitments to the public key.

The trade-off is size: an ML-DSA-65 signature is approximately 3.3 KB versus
71 bytes for ECDSA. Perihelion accepts larger transactions in exchange for
long-term security.

### Egalitarian mining

The proof-of-work function is Argon2id with a 64 MiB memory cost per attempt.
Memory bandwidth, not raw arithmetic throughput, is the bottleneck — the
property that makes purpose-built hardware far less advantageous than it is
for SHA-256. A laptop, a desktop, or a small solar-powered machine can mine
Perihelion on equal footing.

Difficulty retargets on **every block** using LWMA (linearly weighted moving
average). This matters for a network whose participants power up and down with
daily cycles: Bitcoin's two-week adjustment window responds poorly to abrupt
hashrate changes, while Perihelion tracks them continuously.

### Monetary policy

Perihelion combines three mechanisms that are individually proven but rarely
combined:

**1 — Smooth emission under a hard bound.** The block subsidy begins at 10 PER
and decays by a fixed factor of 2999999/3000000 per block, halving roughly
every 3.95 years. Unlike a step-function halving, there is no overnight
revenue shock to the mining industry. Total emission is provably bounded below
**30,000,000 PER**. The schedule is pure integer arithmetic (`core/emission.go`)
and is identical on every platform.

| Year | Block subsidy | Circulating supply | % of bound |
|---:|---:|---:|---:|
| 1 | 8.39 PER | 4,824,348 | 16.1% |
| 4 | 4.96 PER | 15,121,499 | 50.4% |
| 8 | 2.46 PER | 22,621,007 | 75.4% |
| 20 | 0.30 PER | 29,099,858 | 97.0% |
| 40 | 0.009 PER | 29,972,992 | 99.9% |

**2 — Fee burn.** Half of every transaction fee is destroyed permanently.
Circulating supply therefore contracts with real economic activity — a
stronger property than a fixed cap, which merely stops growing.

**3 — Smoothed reward pool.** The other half of each fee enters an on-chain
pool that pays out 1/14,400 of its balance to each block's miner, a roughly
ten-day smoothing window. Miner revenue never falls off a cliff when the
subsidy fades, it scales with network usage, and the smoothing removes the
fee-sniping incentive that destabilises fee-only chains.

## Installation

Requires Go 1.26 or later.

```bash
git clone https://github.com/perihelion-dev/perihelion.git
cd perihelion
go build ./cmd/perihelion
```

## Usage

Create a wallet. You will be asked for a password and shown a 24-word recovery
phrase — write it down offline.

```bash
./perihelion wallet new
```

Join the network and mine:

```bash
./perihelion node --mine
```

Check your balance and send coins:

```bash
./perihelion balance
./perihelion send --to per1... --amount 1.5
```

Inspect the chain:

```bash
./perihelion info
./perihelion block 1000
```

All commands accept `--datadir` (default `~/.perihelion`). Run
`./perihelion help` for the full command list.

### Desktop wallet

`cmd/perihelion-app` is a graphical wallet for macOS, Windows and Linux with an
embedded node and miner — wallet creation and recovery, live balance and block
view, and sending, without a terminal.

```bash
go build ./cmd/perihelion-app
```

## Running a node

A node with no configuration bootstraps from the built-in seed. To choose your
own peers, list them in `~/.perihelion/seeds.txt` (one `host:port` per line) or
pass `--connect host:port`. `--connect none` disables outbound connections
entirely.

To operate a public seed node, accept inbound connections on the default port:

```bash
./perihelion node --listen :16180
```

The network requires no permission to grow. Anyone may run a seed, and nodes
depend on seeds only until they have peers of their own.

### Local RPC

When a node is running, it serves an authenticated JSON API on
`127.0.0.1:16181`. The token lives in `~/.perihelion/rpc-token`.

```bash
curl -H "X-Auth: $(cat ~/.perihelion/rpc-token)" http://127.0.0.1:16181/status
curl -H "X-Auth: $(cat ~/.perihelion/rpc-token)" http://127.0.0.1:16181/balance
curl -H "X-Auth: $(cat ~/.perihelion/rpc-token)" -X POST \
     -d '{"to":"per1...","amount":"1.5"}' http://127.0.0.1:16181/send
```

This is the integration surface for scripts, services and autonomous agents.

## Security

**Key custody.** Wallet files are encrypted at rest with AES-256-GCM under a
key derived by Argon2id (64 MiB, t=3). The public key is stored in clear so
that addresses and balances remain readable while locked; signing requires the
password. A wallet is fully recoverable from its 24-word phrase alone.

**Network.** Peers are treated as hostile. Every frame is length-bounded, and
every block and transaction is independently revalidated by the consensus layer
before it can affect state. A node connects only to peers its operator
configured; there is no discovery protocol, no telemetry, and no phone-home.
The `core` and `wallet` packages contain no networking code whatsoever.

**Implementation.** Written in Go — memory-safe, no `unsafe`. Dependencies are
pinned by hash and limited to Cloudflare CIRCL (ML-DSA), `golang.org/x/crypto`
(Argon2id) and bbolt (storage). `govulncheck` reports no known vulnerabilities.

**What is not yet true.** The code has not received an independent security
audit. The network is young, and a chain with modest hashrate is inexpensive to
attack until enough independent miners participate. Treat Perihelion as
experimental software, not as a store of value.

## Genesis

The genesis block contains no transactions and pays no reward. Its merkle root
commits to the following message, which fixes the chain's identity — a chain
built on any other message is a different currency and can never merge with
this one:

> Perihelion genesis 2026-08-09 — quantum-safe money, mined by everyone, owned by no one

There was no premine, no founder allocation, no presale and no token sale.
Every unit of PER in existence was mined after 2026-08-09 00:00:00 UTC under
rules that applied equally to everyone.

## Roadmap

- [x] Consensus core, UTXO model, post-quantum signatures
- [x] P2P gossip, initial sync, fork choice with atomic reorganisation
- [x] Encrypted wallets with 24-word recovery phrases
- [x] Desktop wallet with embedded node and miner
- [x] Mainnet genesis
- [ ] Additional independent seed nodes
- [ ] Block explorer
- [ ] Reproducible builds and signed releases
- [ ] Independent security audit

## Contributing

Run a node. Mine. Read `core/` and report anything that looks wrong — issues
and pull requests are welcome. There is no company behind Perihelion, no
treasury, and nothing to buy. The code is the entire proposition.

## License

MIT. See [LICENSE](LICENSE).
