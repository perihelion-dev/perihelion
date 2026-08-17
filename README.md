<div align="center">

# Perihelion

**A post-quantum, CPU-mineable, deflationary proof-of-work cryptocurrency.**

Mainnet genesis: 2026-08-09 00:00:00 UTC · No premine · MIT licensed

</div>

---

> **This is experimental software.** It has not had an independent security
> audit. The network is days old and its hashrate is small, so recent history
> is cheap to rewrite. Its coins have no market value and may never have one.
> Do not put in anything you cannot afford to lose entirely. What the project
> does instead of asking for trust: every consensus rule is enforced by every
> node, the supply can be audited by anyone (`perihelion audit`), the early
> mining distribution is published in `docs/ROADMAP.md`, and every push runs
> the full test suite under the race detector, static analysis and fuzzing.


Perihelion is a from-scratch cryptocurrency built for the next thirty years
rather than the last fifteen. Its signatures use NIST-standardised
post-quantum cryptography from block one, its proof-of-work is designed so
ordinary computers stay competitive, and its monetary policy is deflationary
while still paying miners indefinitely.

The consensus implementation is roughly 3,000 lines of Go. That is deliberate:
a monetary system should be small enough that a competent reader can audit it
in an afternoon.

The design, its rationale and its limitations are set out in full in the
[whitepaper](WHITEPAPER.md).

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
in FIPS 204 and selected specifically for resistance to attack by quantum
computers. There is no elliptic-curve fallback anywhere in the protocol, so
there is no migration to perform later and no legacy path for an attacker to
target. Addresses are SHA3-256 commitments to the public key.

To be precise about what this claims: no cryptography is provably unbreakable.
"Post-quantum" means that after years of open international cryptanalysis, no
efficient attack — classical or quantum — is known against the underlying
lattice problem, which is why NIST standardised it as the replacement for
ECDSA and RSA. Bitcoin and Ethereum signatures, by contrast, are broken by a
sufficiently large quantum computer running Shor's algorithm. Should ML-DSA
ever be weakened, a successor scheme can be deployed through the network
governance process like any other consensus change.

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

Perihelion is distributed as **source only**. There are no prebuilt binaries
and no installers — you compile it yourself, from code you can read. That is
deliberate: a wallet binary handed to you by someone else is a binary you have
to trust, and this project would rather be verifiable than convenient.

Requires Go 1.26 or later.

```bash
git clone https://github.com/perihelion-dev/perihelion.git
cd perihelion
go build ./cmd/perihelion
```

Anyone offering a precompiled Perihelion wallet is not doing so on behalf of
this project. Build it yourself, or read the code of whoever built it for you.

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

Seeds are only an entry point. Once connected, nodes exchange the endpoints of
other nodes that accept inbound connections, dial up to eight peers spread
across distinct network prefixes, and remember what they learned in
`~/.perihelion/peers.txt` — so after the first run a node rejoins the network
without consulting any seed at all. A node that does not listen is never
advertised to anyone.

To operate a public node that others can find and connect to:

```bash
./perihelion node --listen :16180 --advertise your.public.ip:16180
```

The network requires no permission to grow. Anyone may run a public node, and
the more that do, the less any single operator matters.

### Local RPC

When a node is running, it serves an authenticated JSON API on
`127.0.0.1:16181`. The token lives in `~/.perihelion/rpc-token`.

```bash
curl -H "X-Auth: $(cat ~/.perihelion/rpc-token)" http://127.0.0.1:16181/status
curl -H "X-Auth: $(cat ~/.perihelion/rpc-token)" http://127.0.0.1:16181/balance
curl -H "X-Auth: $(cat ~/.perihelion/rpc-token)" -X POST \
     -d '{"to":"per1...","amount":"1.5"}' http://127.0.0.1:16181/send
```

Transactions may carry a public payment reference of up to 80 bytes — an
invoice or order id — so a recipient can match a payment to an obligation
without a side channel:

```bash
./perihelion send --to per1... --amount 0.25 --memo "invoice-7f3a91"
```

A service that only needs to *verify* payments should hold no keys at all. The
block explorer serves a read-only JSON API for that purpose (`/api/status`,
`/api/tx/{id}`, `/api/address/{addr}`), backed by a fully validating node.
[AGENTS.md](AGENTS.md) describes the machine-payment case in full, including
its limits.

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

**Recovery phrases.** The 24-word phrase is the sole backup that matters, and
it is shown once, on screen. The desktop wallet can copy it to the clipboard,
but only behind an explicit warning and it overwrites the clipboard afterwards
— the system clipboard is readable by every other program on the machine and
may sync to other devices. Write the words down on paper.

**What is not yet true.** The code has not received an independent security
audit. Secrets are overwritten in memory where practical, but Go offers no
guarantee against copies left by the garbage collector, so a compromised
operating system defeats any software wallet including this one. The network
is young, and a chain with modest hashrate is inexpensive to attack until
enough independent miners participate. Treat Perihelion as experimental
software, not as a store of value.

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
- [x] Network-activated governance (miner signalling)
- [ ] Additional independent seed nodes
- [ ] Block explorer
- [ ] Reproducible builds, so independently compiled binaries can be compared
- [ ] Independent security audit

## Networks

Perihelion runs three networks, separated on every layer that could cause
confusion — genesis, address prefix, chain-ID, wire magic, port and data
directory — so nothing on one can ever be mistaken for the other:

| | Mainnet | Testnet | Regtest |
|---|---|---|---|
| Purpose | the real thing | public experiments | private, local |
| Address prefix | `per1` | `tper1` | `rper1` |
| Port | 16180 | 26180 | 36180 |
| Data directory | `~/.perihelion` | `~/.perihelion-testnet` | `~/.perihelion-regtest` |
| Coins worth | whatever the world decides | **nothing, by design** | nothing |
| Difficulty floor | 16 | 4 | 1, never retargets |
| Seeds | two published | one published | none |

```
perihelion --testnet wallet new       # a tper1… address
perihelion --testnet node --mine      # join the public testnet
perihelion --regtest mine --blocks 100   # instant private chain for testing
```

A mainnet wallet rejects a testnet address with an error that names the
network. A mainnet node cannot complete a handshake with a testnet node. A
data directory refuses to open under a different network than it was created
for. Mining on mainnet with no peers reachable logs a loud warning that the
blocks are private and the coins are not real.

## Scheduled change: signatures bind to the chain from height 60,000

From block 60,000 a transaction signature must commit to the chain's identity,
so that a signature made here cannot be replayed on any chain that forks from
this history. Until then both the original and the chain-bound form are
accepted, so nodes and wallets can be updated at their own pace rather than in
lockstep.

**Operators must update before height 60,000.** A node still running older
software will reject transactions after that point and follow a different
chain. Mining is unaffected either way — coinbases carry no signature.

The change is safe to make only because it is being made early: every output
on this chain so far is a coinbase, so there is no existing signature to
invalidate.

## Governance

Perihelion has no owner. Consensus rules change only through on-chain miner
signalling — 90% of blocks across a ~one-week window, with activation one
window later — and monetary policy is permanently outside that process. The
repository maintainers hold no protocol authority: nodes adopt changes
voluntarily or not at all. The full process, including what can never change,
is specified in [GOVERNANCE.md](GOVERNANCE.md) and implemented in
`core/deployment.go`.

## Contributing

Run a node. Mine. Read `core/` and report anything that looks wrong — issues
and pull requests are welcome. Consensus-affecting proposals follow the PIP
process in [GOVERNANCE.md](GOVERNANCE.md). There is no company behind
Perihelion, no treasury, and nothing to buy. The code is the entire
proposition.

## License

MIT. See [LICENSE](LICENSE).
