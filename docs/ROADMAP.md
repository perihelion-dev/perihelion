# Roadmap

Adopted 2026-08-17. This supersedes any earlier ordering of work.

The guiding ratio: **70% security and credibility, 20% usability, 10% new
features.** The largest mistake available right now would be to pursue an
exchange listing or marketing before it is demonstrable that the network, the
distribution and the money supply are sound.

## What the early distribution actually looks like

Stated here plainly because it should be, and because anyone can verify it:

| Range | Miners | Largest share |
|---|---|---|
| Blocks 1–90 (fixed initial difficulty of 16) | **1** | **100%** — 899.99 PER to one address |
| Blocks 1–1,000 | 6 | 63% |
| Blocks 1–12,700 (all) | ~60 | ~10% and falling |

The first 90 blocks were mined by a single participant — the author — at a
difficulty a single CPU thread clears in seconds. That is not a premine: the
rules were public, the code was public, and anyone could have joined. But it
is a concentrated early distribution, and calling it anything softer would be
dishonest. It is a consequence of the initial difficulty being set far too
low, which is item 3 below.

Whether the network is better served by living with this history or by a
cleanly announced restart after a proper public testnet is a real question. It
becomes harder to answer well the longer it is deferred: eight days of history
is cheap to lose; eight months is not.

## Phase 1 — done (2026-08-17)

1. ✅ **Testnet and regtest, strictly separated from mainnet.** Own genesis,
   own address prefix (`tper1…`), own port, own network magic, trivial
   difficulty, `--regtest` for fully local runs, and a loud warning when a
   mainnet node is mining in isolation. Today a private fork from the same
   genesis produces coins indistinguishable from real PER until they vanish
   on reconnection — that must be impossible to confuse.
2. ✅ **Chain-ID mandatory at block 15,000** (from 60,000). With almost no
   transactions and no market value, the deferred switchover buys nothing and
   leaves a replay window open. Publish signature test vectors and an exact
   serialisation spec.
3. ✅ **Automatic money-supply invariants on every block.** `perihelion audit`
   run by hand is not enough. Enforce in validation: inputs = outputs + fees;
   coinbase ≤ permitted; no duplicate input; no integer overflow; supply ≤
   emission. A previously fixed critical bug — duplicate inputs minting coins
   from nothing — is exactly the class these catch.
4. ✅ **CI, fuzzing, race detector, static analysis, dependency scanning.**
   Continuous fuzzing of every network and transaction parser.
5. ✅ **`SECURITY.md` and a responsible-disclosure channel.**
6. ✅ **Precise labelling as experimental software**, at the top of the README.

## Phase 2 — after Phase 1 holds

7. Independent audit — through a grant if possible; the project has no
   treasury by design.
8. Several independent seed nodes: different operators, countries and
   providers; DNS seeds; a public node directory; reachability measurement.
9. Signed, reproducible builds for macOS, Windows and Linux with published
   checksums and an SBOM — self-compilation deters almost everyone.
10. Wallet: standard backup format instead of a project-specific word list,
    multiple addresses, automatic change addresses, watch-only, offline
    signing, encrypted backups, QR codes, adjustable fees.
11. Public mining benchmarks (Apple M, Ryzen, Intel, ARM, GPUs; hash per watt
    and per euro). Until then the honest claim is "mining with ordinary CPUs
    is possible and more competitive than under SHA-256" — not "ASIC-
    resistant" and not "on equal footing".

## Phase 3 — only when 1 and 2 stand

12. Payment SDKs and an HTTP-402 demonstration: machine-to-machine payment,
    invoice matching via memo, webhooks, proof-of-payment without private
    keys. A real reason for PER to exist.
13. Atomic swaps via hash- and time-locks, activated through the governance
    process. No bridges, no wrapped PER.
14. Careful, late market formation.

## Already done, and kept

- Fair launch: no premine, no allocation, no sale, no treasury.
- Post-quantum signatures from block one; memory-hard PoW; per-block
  difficulty; deflationary emission under a hard bound.
- Fork choice with atomic reorgs; peer discovery with eclipse mitigations;
  per-host connection limits; peer misbehaviour scoring; sync-before-mine.
- Duplicate-input inflation bug found and fixed before exploitation; supply
  audit tool; public explorer with holder and miner distribution.
- Checkpoints for fast initial sync — PoW assumed only inside checkpointed
  history, every value rule still enforced, wrong branches refused at the
  checkpoint (tested).
- Governance: rule changes only by miner signalling; monetary policy outside
  the process entirely.
