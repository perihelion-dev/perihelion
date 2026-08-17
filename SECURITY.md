# Security

Perihelion is experimental software securing real, if currently unpriced,
value. Treat it as such: it has not had an independent audit, and a young
chain with modest hashrate is inexpensive to attack.

## Reporting a vulnerability

**Please do not open a public issue for a security problem.** A consensus bug
disclosed in the open can be exploited before anyone can respond.

Send reports to **perihelion-dev@proton.me**. Include what you found, how to
reproduce it, and what you believe the impact is. You will get an
acknowledgement within 72 hours and a substantive reply within 14 days.

If the report is valid, the fix is developed privately, tested, and released
with credit to the reporter (unless anonymity is preferred). Once the network
has had reasonable time to update, the details are published in full — the
project's own worst findings are part of its record, not hidden from it.

There is no bug bounty fund: the project has no treasury by design. What it
offers is credit, a fix, and honest disclosure.

## What counts

In rough order of severity:

1. **Consensus** — anything that lets coins be created outside the emission
   rules, spent twice, spent without the key, or lets a minority rewrite
   history cheaply. One such bug (duplicate inputs minting value) was found
   and fixed before exploitation; the class is real.
2. **Cryptography** — signature verification that accepts what it should not;
   key derivation or wallet encryption that leaks or weakens keys.
3. **Network** — remote crashes, unbounded resource consumption, eclipse or
   partition attacks that a single peer can mount cheaply.
4. **Wallet** — anything that exposes keys or the recovery phrase, or that
   lets a malformed file or clipboard interaction cause loss.

Out of scope: the low hashrate itself, the absence of an audit, and the
concentration of early mining — all documented and acknowledged in
`docs/ROADMAP.md`.

## Where the guarantees are stated

- `README.md` — what is and is not claimed, especially about "post-quantum".
- `docs/ROADMAP.md` — known weaknesses and the order in which they are being
  addressed.
- `docs/test-vectors.json` — exact consensus encodings an independent
  implementation must reproduce.
- `perihelion audit` — verifies on any node that no coin exists outside the
  rules.

## Supported versions

Only the current `main` branch. There are no releases yet and nothing to
backport to.
