# Perihelion: A Post-Quantum, Egalitarian Proof-of-Work Currency

**The Perihelion Developers**
Version 1.0 — 9 August 2026

---

## Abstract

We present Perihelion, a proof-of-work cryptocurrency designed around three
properties that existing systems do not simultaneously provide: signatures
that remain secure against quantum adversaries, a mining function on which
commodity processors stay competitive, and a monetary policy that is
deflationary while still funding security indefinitely. Every signature uses
ML-DSA-65, standardised by NIST in FIPS 204, so no migration from
elliptic-curve cryptography is ever required. Proof-of-work is Argon2id with a
64 MiB memory cost, shifting the bottleneck from arithmetic throughput to
memory bandwidth and substantially reducing the advantage of purpose-built
hardware. Block emission decays continuously to a bound below 30,000,000
units; half of every transaction fee is destroyed, and the remainder enters a
pool that pays miners a smoothed, perpetual income. Consensus rules can be
changed only by on-chain miner signalling, and monetary policy is excluded
from that mechanism entirely. The system launched on 9 August 2026 with no
premine, no allocation and no sale.

---

## 1. Introduction

Bitcoin demonstrated that a currency can be secured by verifiable
computational work rather than by an institution [1]. Sixteen years of
operation have also exposed three structural problems that its design cannot
easily correct.

**Quantum vulnerability.** Bitcoin and Ethereum authenticate transactions with
ECDSA over elliptic curves. Shor's algorithm [2] solves the discrete logarithm
problem in polynomial time on a sufficiently large quantum computer, which
would allow an attacker to derive a private key from a public key. Because
public keys are exposed when coins are spent — and permanently exposed for
many older outputs — this is not a theoretical inconvenience but a latent
liability over the lifetime of a long-lived currency. Migrating an established
chain to new signatures is possible in principle but requires coordinated
action by every holder, including those who have lost access or died.

**Mining centralisation.** SHA-256 is inexpensive to implement in silicon and
extremely parallelisable. Application-specific integrated circuits therefore
outperform general-purpose processors by several orders of magnitude in
energy efficiency, concentrating block production among a small number of
industrial operators with privileged access to hardware and electricity. The
consequence is that the population which secures the network is disjoint from
the population that uses it.

**The long-run security budget.** Under a fixed supply, the block subsidy
approaches zero and miner revenue must come entirely from transaction fees.
Fee-only security is unstable: revenue becomes volatile block-to-block, and
because a large fee windfall is claimable by whoever mines the block
containing it, miners face an incentive to reorganise recent history rather
than extend it [3]. Adding perpetual inflation, as Monero does, resolves the
funding problem by abandoning the supply bound.

Perihelion addresses all three. Sections 2 through 4 describe the
construction, Section 5 its monetary policy, Sections 6 and 7 consensus and
governance, and Section 8 states plainly what the system does not guarantee.

---

## 2. Transactions

Perihelion uses an unspent-transaction-output (UTXO) model. A transaction
consumes previous outputs and creates new ones; the difference between input
and output value is the fee. There is no account state and no general-purpose
virtual machine. This is a deliberate restriction: the set of reachable states
is small enough to reason about, and the consensus-critical code is
correspondingly small.

### 2.1 Signatures

Every input is authorised by an ML-DSA-65 signature (FIPS 204, NIST security
category 3), a lattice-based scheme selected through NIST's multi-year
post-quantum standardisation process. No elliptic-curve or RSA construction
appears anywhere in the protocol, so there is no weaker path for an attacker
to target and no future migration to coordinate.

An address is the SHA3-256 hash of a public key, rendered as `per1` followed
by 64 hexadecimal characters and an 8-character checksum (76 characters
total). The checksum rejects transcription errors before a transaction is
constructed.

Both the transaction identifier and the signing digest are computed over the
same canonical encoding, which excludes public keys and signatures. A
signature therefore cannot be altered to produce a different valid identifier
for the same transfer, eliminating transaction malleability by construction.

### 2.2 The cost of post-quantum security

Lattice signatures are large. Measured on the reference implementation:

| Object | Size |
|---|---:|
| Public key | 1,952 bytes |
| Signature | 3,309 bytes |
| Typical transaction (1 input, 2 outputs) | 5,397 bytes |
| Block header | 124 bytes |

With a 2 MiB block limit and 60-second blocks, this admits roughly 370
transactions per block, or about 6 transactions per second. An
elliptic-curve chain of the same block size and interval would sustain
considerably more. We regard this as the correct trade: throughput can be
extended by layered protocols, whereas a broken signature scheme cannot be
repaired retroactively. Any chain that adopts post-quantum signatures pays a
comparable cost; Perihelion pays it from the first block rather than later.

---

## 3. Proof of Work

Blocks are produced by finding a header whose Argon2id [4] hash falls below a
target. The header is the message and the previous block hash is the salt, so
every attempt is bound to a specific chain tip and no work can be precomputed
or reused across forks.

Argon2id is memory-hard: each attempt allocates 64 MiB and accesses it in a
data-dependent pattern. The scarce resource is memory bandwidth and capacity,
not arithmetic units. Specialised hardware cannot avoid this cost, because it
is a property of the function rather than of the implementation, and the
memory it would require is the same commodity memory available to everyone.
The efficiency gap between an ASIC and a general-purpose processor therefore
narrows from several orders of magnitude to a small factor. A laptop, a
desktop, or a low-power machine attached to a domestic solar installation can
mine on comparable terms.

This does not make specialised hardware impossible, and we do not claim it
does. It makes such hardware unprofitable to develop relative to the advantage
obtained — which is the property that matters for keeping block production
distributed.

### 3.1 Difficulty adjustment

The target is recomputed for **every block** using a linearly weighted moving
average (LWMA) over the preceding 90 blocks, with per-block solve times
clamped to guard against timestamp manipulation. The target block interval is
60 seconds.

Per-block retargeting is a functional requirement rather than a refinement.
Bitcoin adjusts once per 2,016 blocks, roughly every two weeks; a large
withdrawal of hashrate leaves its blocks slow for the remainder of the period.
A network whose participants are ordinary computers — switched on in the
evening, or powered by sunlight — experiences hashrate swings on a daily
cycle. Continuous adjustment absorbs them.

---

## 4. Consensus

Nodes accept the valid chain with the greatest cumulative work, where a
block's work is derived from its target. Competing branches are stored and
evaluated; when a branch overtakes the active chain, the node disconnects
blocks back to the fork point and connects the new branch within a single
atomic database transaction, restoring the previous state exactly if any step
fails. Transactions from disconnected blocks return to the memory pool.

Every block is independently validated against the full rule set before it can
affect state: proof-of-work, an independently recomputed difficulty target,
timestamp bounds, coinbase structure and value, absence of duplicate
transactions, the Merkle commitment, size limits, and every signature. A node
never trusts a peer; it trusts only its own verification.

Coinbase outputs mature after 20 blocks, so rewards from a branch that is
later abandoned cannot have been spent.

---

## 5. Monetary Policy

Perihelion's emission combines three mechanisms. Each exists elsewhere; the
combination does not.

### 5.1 Continuous decay under a hard bound

The subsidy of block *h* is

    S(h) = 10 · f^(h−1) PER,    f = 2999999 / 3000000

evaluated in fixed-point integer arithmetic, so every node computes bit-identical
values on every platform. The genesis block pays nothing.

The subsidy halves every 2,079,441 blocks — approximately 3.95 years — but
does so *continuously*. Bitcoin's step halvings remove half of the mining
industry's revenue overnight, forcing periodic capitulation cycles among
operators whose margins straddle the threshold. A smooth curve delivers the
same long-run scarcity without the discontinuity.

The infinite sum of the schedule is bounded:

    Σ S(h) < 10 · 3,000,000 = 30,000,000 PER

| Year | Subsidy | Circulating supply | % of bound |
|---:|---:|---:|---:|
| 1 | 8.39 | 4,824,348 | 16.1 |
| 2 | 7.04 | 8,872,884 | 29.6 |
| 4 | 4.96 | 15,121,499 | 50.4 |
| 8 | 2.46 | 22,621,007 | 75.4 |
| 12 | 1.22 | 26,340,388 | 87.8 |
| 20 | 0.30 | 29,099,858 | 97.0 |
| 40 | 0.009 | 29,972,992 | 99.9 |

### 5.2 Fee burn

Half of every transaction fee is destroyed permanently. A fixed supply merely
stops growing; a burned supply contracts. Perihelion therefore becomes scarcer
in proportion to its use, and the cost of that scarcity is borne by those
consuming block space rather than imposed on holders as dilution. The
mechanism is comparable to Ethereum's EIP-1559 base-fee burn [5], applied here
to a fixed-supply proof-of-work chain.

### 5.3 The reward pool

The other half of each fee is credited to an on-chain pool. Each block pays
its miner 1/14,400 of the pool balance — a smoothing window of approximately
ten days at 60-second blocks — in addition to the subsidy.

This addresses the instability of fee-only security directly. Because a
block's fee income is drawn from a slowly draining reservoir rather than from
the transactions it contains, the value of reorganising recent history to
capture a fee windfall is largely removed: the windfall was already dispersed
across thousands of future blocks. Miner revenue is smoothed rather than
spiky, and it scales with adoption. As the subsidy decays toward zero, the
pool becomes the principal and permanent source of security funding, without
perpetual inflation.

We state the limitation honestly: like any non-inflationary chain, long-run
security ultimately depends on the network being used. The pool changes the
shape and stability of that revenue, not its origin.

---

## 6. Governance

A currency whose rules can be changed by its authors is a currency with an
administrator. Perihelion's rules can be changed only by the network that runs
it.

Each proposed consensus change is assigned a signal bit. Miners who accept the
change set that bit in the blocks they produce. Every node tallies the signals
in windows of 10,080 blocks (approximately one week) from the chain it has
independently verified. If 9,072 blocks in a completed window — 90% — signal
support, the change locks in and takes effect one full window later, giving
every participant about a week of unambiguous notice. A proposal that fails to
reach the threshold before its timeout expires permanently.

Two properties follow. First, publishing software activates nothing: a
maintainer, a company or a government can distribute any code they wish, and
the rules do not move until the network signals. Second, monetary policy is
outside the mechanism entirely — the emission schedule, the supply bound and
the fee burn have no signal bit, and no majority can create one without
producing a different chain that existing nodes reject. A supply promise that
a majority can revoke is not a promise.

Underlying both is the property that makes them enforceable: every node
validates every rule on every block. A participant who rejects an activated
change simply continues on the chain they consider valid. Divergence is not a
failure of this design; it is the final guarantee that no one is compelled.

---

## 7. Launch

Perihelion's genesis block was created at 00:00:00 UTC on 9 August 2026. It
contains no transactions and pays no reward. Its Merkle root commits to the
message

> Perihelion genesis 2026-08-09 — quantum-safe money, mined by everyone, owned by no one

which fixes the chain's identity: a chain built on any other message is a
different currency and can never merge with this one.

    Genesis hash:        348495b860aa61e166f8f822e63f229885e1744f47121b9c1fafc2711a6f62e0
    Genesis Merkle root: 98670790bb71118e28122ebebeba105561abdcf14b530969a22d0f09fed1a3ed

There was no premine, no founder allocation, no presale, no token sale and no
development fund. Every unit in existence was mined after that instant under
rules that applied equally to all participants. The protocol has no mechanism
by which anyone can be paid other than mining.

---

## 8. Security Analysis

### 8.1 What is established

*Signature security.* ML-DSA-65 rests on the hardness of structured lattice
problems, for which no efficient classical or quantum attack is known after
sustained public cryptanalysis, and which NIST standardised specifically as
the replacement for schemes broken by Shor's algorithm.

*Implementation.* The consensus implementation is approximately 3,000 lines of
Go — memory-safe, without unsafe operations. Peer input is treated as hostile:
frames are length-bounded before allocation, deserialisation is bounds-checked
and copy-based, and all structural counts are capped. Dependencies are pinned
by hash and limited to Cloudflare CIRCL, the Go extended cryptography library
and bbolt.

*Key custody.* Wallet keys are derived from 32 bytes of entropy that is also
encoded as a 24-word recovery phrase. The entropy is stored encrypted under
AES-256-GCM with a key derived by Argon2id (64 MiB, t = 3), authenticated
against the wallet's public key, so an incorrect password fails
authentication rather than yielding a usable but incorrect key.

### 8.2 What is not established

*No formal guarantee of unbreakability.* No cryptographic scheme in use
anywhere is proven secure; all rest on problems believed hard. Post-quantum
means that no efficient attack is known, not that none exists. Should ML-DSA
be weakened, a successor can be deployed through the governance process in
Section 6 — a path Perihelion has and legacy chains would have to build under
duress.

*The chain is young.* Security in proof-of-work is purchased with hashrate. A
network with modest participation can be overwhelmed inexpensively by an
adversary who temporarily directs more computation at it than its honest
participants, allowing recent transactions to be reversed. This is not a
defect of the design but a property of every new chain, and it recedes only as
independent miners join. Memory-hard mining raises the cost of assembling such
capacity — an attacker must acquire memory rather than borrow hashrate from an
existing SHA-256 market — but does not eliminate it.

*The code has not been audited.* An independent security review has not yet
been performed. The implementation should be treated as experimental.

*Software wallets cannot exceed their host.* Keys are protected against theft
of the wallet file, not against a compromised operating system. Secrets are
overwritten in memory where practical, but a managed runtime offers no
guarantee that no copy remains.

### 8.3 Attack surface

A node accepts inbound connections only when configured to do so; the desktop
wallet makes outbound connections exclusively and listens on no port. There is
no peer discovery, no telemetry and no reporting endpoint. The local RPC
interface binds to loopback and requires a token generated on first use.

---

## 9. Comparison

| Property | Bitcoin | Ethereum | Perihelion |
|---|---|---|---|
| Signatures | ECDSA | ECDSA | ML-DSA-65 (post-quantum) |
| Quantum-vulnerable | yes | yes | not by known attacks |
| Consensus | PoW (SHA-256) | Proof-of-stake | PoW (Argon2id) |
| Mining accessible to commodity hardware | no | n/a | yes |
| Block interval | 10 min | 12 s | 60 s |
| Difficulty adjustment | 2,016 blocks | n/a | every block |
| Supply | 21,000,000 fixed | unbounded, burn-adjusted | < 30,000,000, burning |
| Perpetual miner funding | fees only | issuance | fee pool |
| Premine / allocation | none | presale | none |
| Rule changes | social consensus | core development process | on-chain signalling, 90% |

The comparison is offered for orientation, not as a claim of superiority in
aggregate. Bitcoin and Ethereum possess something Perihelion does not and
cannot manufacture: years of adversarial testing, and security budgets funded
by enormous market capitalisations. Those advantages are decisive today. What
Perihelion offers is a design whose foundational choices do not require
revision in the decades ahead.

---

## 10. Implementation

The reference implementation is written in Go and published under the MIT
licence at **github.com/perihelion-dev/perihelion**. It comprises a fully
validating node, a miner, a command-line wallet, a graphical desktop wallet
with an embedded node, and an authenticated local RPC interface for
programmatic use.

Distribution is source-only by design. No precompiled binaries are published:
a wallet binary supplied by someone else is a wallet that must be trusted,
and the project prefers to be verifiable rather than convenient.

---

## 11. Conclusion

Perihelion is an attempt to build the currency the next several decades
require rather than the one the last several produced. Its signatures do not
need replacing when quantum computers arrive. Its mining does not concentrate
into industrial facilities by construction. Its supply contracts with use
while its security remains funded. Its rules cannot be changed by its authors.

None of this guarantees adoption, and adoption is what ultimately secures any
currency. But the properties above cannot be retrofitted onto a system that
did not begin with them, and a network can only begin once.

---

## References

[1] S. Nakamoto. *Bitcoin: A Peer-to-Peer Electronic Cash System.* 2008.

[2] P. W. Shor. *Algorithms for Quantum Computation: Discrete Logarithms and
Factoring.* Proceedings of the 35th Annual Symposium on Foundations of
Computer Science, 1994.

[3] M. Carlsten, H. Kalodner, S. M. Weinberg, A. Narayanan. *On the Instability
of Bitcoin Without the Block Reward.* ACM CCS, 2016.

[4] A. Biryukov, D. Dinu, D. Khovratovich, S. Josefsson. *Argon2 Memory-Hard
Function for Password Hashing and Proof-of-Work Applications.* RFC 9106, 2021.

[5] V. Buterin et al. *EIP-1559: Fee Market Change for ETH 1.0 Chain.*
Ethereum Improvement Proposals, 2019.

[6] National Institute of Standards and Technology. *FIPS 204:
Module-Lattice-Based Digital Signature Standard.* 2024.
