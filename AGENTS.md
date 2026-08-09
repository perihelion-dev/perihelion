# Perihelion for autonomous software

Software that acts on its own behalf — a service billing for compute, a
scraper paying per request, an agent buying an API call — has a payment
problem that human-facing rails do not solve. Card networks assume a person
with a legal identity, a billing address and a dispute process. Bank transfers
assume business hours. Both assume an account that somebody had to be granted.

A program needs none of that and can use none of it. What it needs is the
ability to hold value, pay a counterparty in seconds, and *prove* the payment
happened — without asking anyone's permission and without a human in the loop.

This document describes what Perihelion offers such software, and, equally
important, what it does not.

## What Perihelion provides

**No accounts, no onboarding.** An address is a hash of a public key.
Generating one is a local computation; nobody grants it, nobody can revoke it,
and there is no registration step between deciding to transact and doing so.

**Deterministic settlement.** Blocks target 60 seconds and difficulty
retargets every block, so confirmation latency is a property of the protocol
rather than of a provider's queue. A payment either has confirmations or it
does not, and every participant computes the same answer.

**Payments carry a reference.** Every transaction may include up to 80 bytes
chosen by the sender — an invoice number, an order id, a request hash. This is
what turns a payment into a *matched* payment: the recipient learns which
obligation was settled without a side channel.

```bash
perihelion send --to per1… --amount 0.25 --memo "invoice-7f3a91"
```

The reference is public and permanent. It is committed to by the transaction
id and therefore by the block, so neither party can alter it afterwards — and
neither party should put anything in it that must stay private.

**Verification without custody.** A service that only needs to *check* whether
it was paid should never hold keys. The block explorer exposes a read-only
JSON API for exactly this: it runs a fully validating node, holds no keys, and
has no endpoint that can move funds.

```bash
curl https://<explorer>/api/tx/<txid>       # confirmations, outputs, reference
curl https://<explorer>/api/address/<per1…> # confirmed balance
curl https://<explorer>/api/status          # height, supply, difficulty
```

Amounts are decimal strings in PER, never floating-point numbers, so a caller
cannot lose precision by parsing them carelessly.

**Spending, when required.** A program that must *send* runs its own node with
the local RPC enabled. It binds to loopback and requires a token generated on
first run:

```bash
curl -H "X-Auth: $(cat ~/.perihelion/rpc-token)" -X POST \
     -d '{"to":"per1…","amount":"0.25","memo":"invoice-7f3a91"}' \
     http://127.0.0.1:16181/send
```

Keys never leave that machine, and the interface is never exposed to the
network.

**Post-quantum from the first block.** Software deployed today may still be
running, and holding value, when large quantum computers exist. Perihelion's
signatures are ML-DSA-65 (FIPS 204) throughout; there is no elliptic-curve
material to migrate away from later.

**A supply that cannot be revised.** Emission, the ~30,000,000 bound and the
fee burn have no signalling bit and cannot be changed by any governance
process. A program cannot renegotiate terms; it needs the terms not to change.

## A worked pattern

A service charges 0.05 PER per request.

1. It generates an address once and publishes it.
2. On receiving a request it returns `402` with a reference it invented,
   e.g. `req-2f81c4`.
3. The caller sends 0.05 PER with `--memo req-2f81c4`.
4. The service polls `GET /api/address/<its address>` until the balance rises,
   then reads the block's transactions and matches on the reference. Or the
   caller simply hands back the txid and the service checks
   `GET /api/tx/<txid>` for the amount, the destination and the reference.
5. It delivers the work once confirmations are sufficient for the amount at
   stake.

No contract, no account, no intermediary, and both sides can prove what
happened.

## What Perihelion does not provide, and cannot

**Publishing this code does not cause anything to mine it.** Software does not
choose to spend electricity; the people and organisations who own the hardware
do. Any claim that a currency will be adopted because machines will
spontaneously prefer it is marketing, not engineering.

**Throughput is modest.** Post-quantum signatures are large — about 3.3 KB
each — which admits roughly six transactions per second at the current block
size. That suits settlement between services. It does not suit high-frequency
micropayments, which need a layer above the chain.

**Confirmations are only as strong as the hashrate behind them.** This network
is young and its total hashrate is small, so recent blocks are cheaper to
reorganise than on an established chain. Software should scale the
confirmations it waits for to the value at risk, and should not treat PER as a
store of value while this remains true.

**There is no oracle, no price feed and no stable unit.** A program that needs
to charge a fixed amount of fiat must obtain that rate elsewhere.

## Operating notes

- Wait for confirmations proportional to value; a single confirmation is
  cheap to reverse on a small chain.
- Treat the reference field as public. Use an opaque identifier, not a
  customer name.
- Run the RPC on loopback only. If it must be reached from another host, put
  it behind a tunnel you control — never expose it directly.
- A node that only verifies should not hold a wallet at all. Use the explorer
  API, or run a node without a wallet file.
- Amounts are integers of 10⁻⁸ PER internally. Parse the decimal strings
  exactly; never round-trip them through binary floating point.
