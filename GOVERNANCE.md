# Perihelion Governance

Perihelion has no owner. This document describes how the rules of the network
can and cannot change. It is not a promise by a maintainer — every mechanism
described here is enforced by the software each participant voluntarily runs,
and can be verified in `core/deployment.go`.

## Principles

1. **The network decides, not the repository.** A code change — merged,
   released, or otherwise — has no effect on the currency until node operators
   choose to run it and, where consensus rules are concerned, until the
   network has activated it through the signalling process below. The
   repository is a coordination point, not an authority.

2. **Every node is a veto.** Each node independently validates every rule on
   every block. No developer, maintainer, miner, exchange or government can
   impose a rule change on a node that has not adopted it. This is a
   mathematical property of the system, not a policy.

3. **Monetary policy is not subject to governance.** The emission schedule,
   the ~30,000,000 PER supply bound, and the fee burn are permanently outside
   the signalling process. There is no mechanism — including the one in this
   document — that can alter them, because a monetary promise that a majority
   can vote away is not a promise. A network that changed them would simply be
   a different currency under a different name.

4. **No plutocracy.** Perihelion has no token voting. Influence over
   activation follows contributed proof-of-work — the same open, permissionless
   resource that secures the chain — and adoption follows the free choice of
   every node operator. Wealth alone confers no say.

5. **The right to refuse is absolute.** Signalling determines *when* a
   prepared rule change takes effect for those who accept it; it never obliges
   anyone to run it. Participants who reject an activated change may continue
   operating under the rules they consider valid. Forks are not a failure mode
   of this system; they are its final guarantee.

## How a consensus rule changes

Any consensus-affecting proposal follows this path. Nothing may skip a step.

1. **Proposal.** A written Perihelion Improvement Proposal (PIP) is published
   openly in the repository: motivation, exact specification, activation
   parameters, and analysis of risks and incompatibilities.

2. **Review.** The proposal is discussed in public. There is no deadline: a
   proposal that cannot survive scrutiny does not proceed.

3. **Implementation.** The change is implemented behind a *deployment*: a
   named entry in `core/deployment.go` with a dedicated signal bit, a start
   height and a timeout height. The new rule is inert until activated —
   shipping the code activates nothing.

4. **Signalling.** Miners who support the proposal set its signal bit in the
   blocks they produce (`--signal <name>`). Signalling is a public, on-chain,
   per-block statement of readiness that anyone can audit.

5. **Activation.** Every node tallies signals over fixed windows of 10,080
   blocks (about one week). If **90%** of the blocks in a completed window
   signal support, the deployment locks in, and takes effect one full window
   later — giving every participant about a week of unambiguous notice. If
   the threshold is never reached before the timeout height, the proposal
   fails permanently and its bit is retired.

6. **Adoption.** Node operators upgrade — or do not. See principle 5.

The 90% threshold is deliberately high. Consensus changes should be rare,
boring and near-unanimous; a change that 30% of the network opposes should
not happen at all.

## Non-consensus changes

Bug fixes, performance work, wallets, tooling, documentation, translations
and other changes that do not affect which blocks are valid follow ordinary
open-source practice: issues and pull requests, public review, maintainer
merge. Security fixes that do not alter consensus rules are released as fast
as possible. None of this requires signalling, because none of it can change
anyone's money.

## Maintainers

Maintainers review and merge code, cut releases and keep infrastructure
running. They hold no protocol authority: they cannot activate a rule change
(only the network can), they cannot bypass the process above, and anyone may
fork the repository at any time. Seed nodes they operate are convenience
bootstrap points, not privileged infrastructure — any node may serve as a
seed for others.

## What there is not

There is no foundation, no treasury, no premine, no developer fund, no
council, and no on-chain voting by balance. Nobody is paid by the protocol,
and nobody can be: the coinbase pays miners, and fees are burned or recycled
to miners, as fixed in consensus. Perihelion's governance budget is zero by
design — projects are captured through their treasuries, and this one has
none to capture.
