# How Perihelion could become tradeable

An assessment of the routes by which PER could acquire a market, and what
each would cost. Written because "get it on an exchange" is the obvious next
thought and the obvious answer is the wrong one.

## The constraint that rules out the easy path

Perihelion is an independent chain with its own consensus, not a token issued
on someone else's. That difference decides everything below.

A token on Ethereum or Solana can be listed on a decentralised exchange in an
afternoon: the exchange is a contract on the same chain the token lives on, so
it can hold and move the token directly. Uniswap can trade any ERC-20 because
Uniswap and the ERC-20 execute in the same machine.

Nothing of the sort can hold PER. A decentralised exchange on another chain
has no way to take custody of a coin on this one. **PER cannot be listed on
Uniswap, PancakeSwap or any comparable venue — not for want of effort, but
because the mechanism does not reach across chains.**

So the question is not which exchange to approach, but which of three
genuinely different mechanisms to pursue.

---

## Route 1 — Person to person

Two people agree a price and settle: one sends PER, the other sends money or
goods. No venue, no permission, no fee.

This is how Bitcoin's price came into existence. A rate published in October
2009 was derived from the cost of the electricity needed to mine a coin; the
first trade for a real good — two pizzas — followed in May 2010. Neither
involved an exchange, because none existed.

**Cost:** nothing. **Time:** immediate. **What it gives:** a first price, and
evidence that anyone wants PER at all. **What it does not give:** continuous
price discovery, or any protection — one side must send first and trust the
other.

This is the only route available today, and the honest starting point.

---

## Route 2 — Atomic swaps

Two parties exchange coins on two different chains such that either both
transfers happen or neither does. No exchange, no custodian, no counterparty
risk, and nobody's permission required.

The construction is standard. Alice locks PER so that it can be claimed by
whoever reveals a secret; Bob locks Bitcoin under the hash of the same secret.
When Alice claims Bob's coins she must reveal the secret, which is exactly
what Bob needs to claim hers. If either walks away, both refunds unlock after
a timeout.

**Perihelion cannot do this today.** An output here is a value and a recipient
address and nothing else:

```go
type TxOutput struct {
    Value uint64   // peri
    Addr  [32]byte // SHA3-256 of the recipient's public key
}
```

There is no timelock and no way to make spending conditional on revealing a
secret, so half the construction has nowhere to live.

### What would have to be added

Not a scripting language. Bitcoin's script is a large attack surface, and this
project's argument for itself is that its rules are small enough to read. One
purpose-built output type is enough:

- claimable by a recipient who reveals a preimage of a given hash, or
- claimable by the sender once a stated block height has passed.

That is perhaps 300–500 lines including tests. It is a consensus change, so it
would be the first real use of the governance mechanism: proposed openly,
activated by the network, not by whoever publishes the software.

**Cost:** development effort, no money. **Time:** weeks, including the
activation window. **What it gives:** PER becomes tradeable against Bitcoin,
Litecoin, Monero and anything else supporting the same construction, with no
venue and nobody able to refuse. **Risk:** a consensus change on a live chain,
which is exactly the class of work where mistakes are expensive.

This is the route that matches what Perihelion is trying to be.

---

## Route 3 — A centralised exchange

An exchange runs a Perihelion node, credits deposits, and matches orders in
its own ledger.

What it requires from them is real engineering: node operation, wallet
infrastructure, deposit monitoring, withdrawal signing, cold storage. Exchanges
charge for that, and the figures quoted publicly for smaller venues run from
low five figures upward, with larger ones effectively unavailable at any
advertised price. They also expect demonstrable trading volume, a market maker
providing liquidity, and usually an audit.

**Cost:** substantial money. **Time:** months. **What it gives:** a continuous
public price and the visibility that follows from it.

The difficulty is not the money. It is that this route runs backwards.
Exchanges list assets that people already want, because their revenue is
trading fees. Approaching one with a chain nobody trades yet is asking them to
create demand rather than serve it. And Perihelion has, by deliberate design,
no treasury to pay from: no premine, no allocation, no sale. The credibility
that comes from that is the same property that leaves nothing to spend.

---

## Route 4 — A wrapped token, and why not

A token on Ethereum backed one-for-one by PER held in reserve would trade on
any decentralised exchange immediately.

It should not be built. Someone must hold the reserve, and that someone would
have to be trusted not to spend it — reintroducing precisely the dependency
this project exists to remove. Bridges of this kind are the most frequently
and most expensively compromised construct in the field. And the wrapped
representation would be secured by the elliptic-curve signatures whose
vulnerability to quantum attack is the reason Perihelion exists; the traded
form would carry the flaw the underlying asset was built to avoid.

Fast, and wrong.

---

## Assessment

| | Cost | Time | Needs permission | Fits the project |
|---|---|---|---|---|
| Person to person | none | now | no | yes |
| Atomic swaps | development | weeks | no | yes |
| Centralised exchange | five figures + | months | yes | tolerable |
| Wrapped token | low | days | no | **no** |

The sequence that follows from this is person-to-person first, because it
costs nothing and is the only way a first price has ever come about; atomic
swaps next, because they are the only route that gives a real market without
asking anyone's permission; and an exchange listing last, if demand ever makes
one worth their while.

The order matters. An exchange listing is a consequence of people wanting a
coin, never a cause of it.
