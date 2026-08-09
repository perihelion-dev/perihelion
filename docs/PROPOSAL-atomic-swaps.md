# Proposal: conditional outputs for atomic swaps

**Status:** draft, not implemented, not activated.
**Decided by:** the network, through miner signalling. Publishing this changes
nothing on its own.

## What it is for

Two people who have never met should be able to exchange PER for Bitcoin
without an exchange, a custodian, or anyone's permission — and without either
having to send first and hope.

The construction is standard. Alice locks PER so that it can be claimed by
whoever reveals a secret; Bob locks Bitcoin under the hash of the same secret.
When Alice claims Bob's coins she must reveal the secret, which is exactly
what Bob needs to claim hers. If either abandons the trade, both refunds
unlock after a deadline. Either both transfers happen or neither does.

Bitcoin, Litecoin and several other chains already support their half. This
proposal adds Perihelion's half.

## Why Perihelion cannot do this today

An output carries a value and a recipient, and nothing else:

```go
type TxOutput struct {
    Value uint64   // peri
    Addr  [32]byte // SHA3-256 of the recipient's public key
}
```

There is no deadline and no way to make spending depend on revealing a
secret, so half the construction has nowhere to live.

## What is deliberately not proposed

**A scripting language.** Bitcoin's script is a general machine, and a general
machine on a chain this young is a large surface for a small benefit. This
project's argument for itself is that its rules are small enough for one
person to read in an afternoon. One purpose-built output type is enough for
swaps, and can be reasoned about completely.

**Adaptor signatures.** The modern, more private way to build swaps rests on
Schnorr or ECDSA algebra. Perihelion signs with ML-DSA, a lattice scheme with
no such structure, and no adaptor construction for it exists. This is a real
cost of choosing post-quantum signatures: swaps here must use the older
hash-lock form, in which the secret becomes publicly visible on both chains
once claimed, and observers can link the two trades. Privacy is not what this
proposal buys — tradeability is.

## Design

### The output does not change

A swap output is an ordinary output whose address commits to the terms of the
swap rather than to a public key:

```
Addr = H("PER:swap", hashlock, timeout, claimAddr, refundAddr)
```

where

| field | size | meaning |
|---|---|---|
| `hashlock` | 32 bytes | SHA3-256 of the secret |
| `timeout` | 8 bytes | block height from which the refund branch opens |
| `claimAddr` | 32 bytes | address that may claim by revealing the secret |
| `refundAddr` | 32 bytes | address that may reclaim after `timeout` |

Nothing about `TxOutput` changes, and a swap output is indistinguishable from
an ordinary one until it is spent. The terms are revealed by whoever spends
it, and are checked against the commitment then — the same shape as Bitcoin's
pay-to-script-hash, for the same reason: the chain stores a hash, not the
conditions.

### The input gains one optional field

```go
type TxInput struct {
    Prev      OutPoint
    PubKey    []byte
    Signature []byte
    Redeem    []byte // empty for ordinary spends
}
```

`Redeem` encodes, when present:

```
branch    1 byte    0x01 = claim, 0x02 = refund
hashlock  32 bytes
timeout   8 bytes
claimAddr 32 bytes
refundAddr 32 bytes
secret    variable  (claim branch only; at most 64 bytes)
```

### Validation

For an input with non-empty `Redeem`, in addition to the existing checks:

1. `H("PER:swap", hashlock, timeout, claimAddr, refundAddr)` equals the spent
   output's `Addr`. A wrong term is a wrong address.
2. **Claim branch:** `SHA3-256(secret)` equals `hashlock`, and the input's
   public key hashes to `claimAddr`.
3. **Refund branch:** the height of the block containing this transaction is
   at least `timeout`, and the input's public key hashes to `refundAddr`.
4. The signature verifies as it does for any other input.

An input with empty `Redeem` is validated exactly as today, so ordinary
transactions are untouched.

### Serialisation

`Redeem` is included in `serializeCore` — and therefore in both the
transaction id and the signing digest — **only when at least one input carries
one**. A transaction without swaps serialises byte-for-byte as it does today,
so existing transaction ids and signatures are unaffected and nothing needs
re-signing.

Committing to `Redeem` in the digest matters: were the branch selector or the
secret outside the signature, the same transaction could be re-encoded into a
second valid form with a different id. Perihelion has no transaction
malleability today and this must not introduce any.

## How a trade runs

Alice has PER and wants Bitcoin. Bob has Bitcoin and wants PER.

1. Alice invents a secret and tells Bob only its hash.
2. Alice locks PER to `(hash, T_per, claim=Bob, refund=Alice)`.
3. Bob checks the Perihelion chain, then locks Bitcoin to `(hash,
   T_btc, claim=Alice, refund=Bob)` with **`T_btc` well before `T_per`**.
4. Alice claims the Bitcoin, revealing the secret on Bitcoin's chain.
5. Bob reads the secret there and claims the PER.

If Bob never locks, Alice refunds after `T_per`. If Alice never claims, both
refund. Nobody can take without giving.

### The deadlines are the dangerous part

**`T_btc` must expire well before `T_per`.** Otherwise Alice can wait for
Bob's Bitcoin refund to open, claim the Bitcoin at the last moment, and claim
her own PER refund too — taking both sides. The margin must cover the slower
chain's block-time variance with room to spare; a factor of two on the
expected settlement time is the usual guidance, not a tight fit.

Perihelion aims at 60-second blocks against Bitcoin's ten minutes, so a
Perihelion deadline is far more precise. The asymmetry favours a Perihelion
holder as the party who moves second, and any implementation should default
the deadlines rather than let a user invent them.

### What it does not fix

The initiator holds a free option: having seen the price move, Alice can
simply not claim, and Bob's capital is locked until his deadline. This is
inherent to atomic swaps everywhere and is not solved here.

## Security notes

- **Secrets are public once used.** Claiming reveals the secret on both
  chains, so the two transactions can be linked by anyone watching. Never
  reuse a secret across trades.
- **A short secret is a guessable secret.** It must be 32 bytes from a
  cryptographic source; the field is capped at 64 bytes so a swap cannot carry
  arbitrary payload data.
- **The refund branch is a consensus deadline, not a wall-clock one.** Block
  timestamps have limited accuracy; the height comparison is exact and is what
  the rule uses.
- **Reorganisations move deadlines.** A claim confirmed shortly before
  `timeout` could, after a deep reorganisation, land after it. Neither side
  should cut a deadline fine on a chain whose recent history is not settled.
- **No new denial-of-service surface.** Validation adds one hash and one
  comparison per input, and the secret is length-bounded.

## Activation

This is a consensus change. Nodes that do not adopt it will reject swap
spends as invalid and follow a different chain, so it must not be switched on
by whoever publishes software.

The mechanism already exists: a deployment bit, counted over a window of
10,080 blocks, activating one window after 90% of blocks signal support.
Miners who accept the change signal it; miners who do not, do not. If it never
reaches the threshold, it never activates, and that is a legitimate outcome.

Ordinary transactions are unaffected either way, since an input without a
`Redeem` field serialises and validates exactly as before.

## Effort

Roughly 300–500 lines including tests: the commitment, the two branches, the
conditional serialisation, and a wallet flow that constructs and settles a
swap with sane default deadlines. The tests that matter are the adversarial
ones — claiming without the secret, refunding early, mismatched terms,
re-encoding a spend into a second valid form, and a full swap against a
simulated counterparty chain.
