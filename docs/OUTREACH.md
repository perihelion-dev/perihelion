# Reaching people

A strategy for making Perihelion known. Written for a project that has no
budget, no treasury and nothing to sell — which rules out most of what the
word "marketing" usually means, and leaves a narrower set of things that
actually work.

## Where we start

Measured, not estimated:

| | |
|---|---|
| Age | 8 days |
| Blocks | ~11,400, on target interval |
| Addresses holding a balance | 72 |
| Distinct miners recently | 30–56 |
| Largest holding | 15.9% |
| Founder's holding | 10.9%, falling |
| GitHub stars, forks, issues | **0, 0, 0** |

The last line next to the ones above it is the whole strategic picture.

**People are running the software. Nobody is talking about it.** Dozens of
strangers found the code, compiled it and pointed real hardware at the chain —
and not one has starred the repository, opened an issue or said a word. Two
things follow. First, the product spreads on its own merits; awareness is not
the bottleneck. Second, **there is no room where anything could be discussed**,
so nothing is.

One caveat, stated because it changes what the numbers mean: eight of the
connected addresses are French consumer ISP ranges each holding exactly the
per-host connection limit. That pattern is more consistent with one operator
running many nodes than with eight independent participants. The true number
of people involved is probably smaller than the number of addresses suggests,
and cannot be determined from outside.

## The one thing that is actually different

Not "better than Bitcoin" — that claim is not credible from an eight-day-old
chain and would be dismissed on sight by exactly the people worth reaching.

What is defensible, and rare:

**Post-quantum signatures from the first block.** Bitcoin and Ethereum
authenticate with elliptic curves, which a large quantum computer breaks.
They can migrate only by persuading every holder to move — including those who
have lost access or died. Perihelion never has to, because it never used them.
Very few chains can say this, and none of the large ones.

Everything else follows from that and supports it: mining that ordinary
computers can do, a fair launch with no premine and no sale, and a codebase
small enough to read in an afternoon.

The credibility comes from what the project *doesn't* claim. The README says
the code is unaudited, that a young chain is cheap to attack, and that this is
not a store of value. Keep that. It is the reason a sceptical reader keeps
reading.

## Who to talk to

In order of how well the story lands:

**CPU miners.** People already mining Monero on processors have the hardware,
the software habits, and the ideological objection to specialised mining
hardware that Perihelion is built around. They can join at zero marginal cost.
This is the closest audience there is.

**Post-quantum cryptography people.** Researchers, students, and engineers who
follow NIST standardisation. For them the interesting question is not the coin
but the engineering: what does a chain look like when signatures are 3.3 KB?
That question is genuinely interesting and we can answer it with measurements.

**Fair-launch purists.** A small, sceptical, loud group for whom "no premine"
is the first and often only question. Perihelion passes their test outright,
which almost nothing else does.

**Who not to talk to:** general cryptocurrency audiences and anyone looking
for an investment. They will ask about price, there is none, and the encounter
produces disappointment on one side and a reputation for vapourware on the
other. This audience is not merely unhelpful; it is actively harmful this
early.

## What has to exist first

**A place to talk.** This is the actual gap, and no amount of outreach works
without it. GitHub Discussions costs nothing, needs no infrastructure, and is
where people already are. Without it, someone curious has nowhere to ask a
question and simply leaves — which is, measurably, what has been happening.

**A way to be found on GitHub.** Repository topics (`cryptocurrency`,
`post-quantum`, `proof-of-work`, `blockchain`, `golang`, `mldsa`) are how
GitHub search and recommendations work. Free, one minute, currently unset.

**A one-page technical account.** Not a whitepaper — that exists — but the
kind of thing an engineer reads in five minutes: what the signature size costs
in practice, why Argon2id rather than RandomX, what the measured block
interval looks like over a week. The numbers are already collected.

## Channels, in the order they are worth trying

**1. Where CPU miners already are.** Monero mining communities and the altcoin
sections of long-standing forums. The post that works there is technical and
unexcited: what it is, what it costs to join, what is unfinished. The post
that fails is enthusiastic.

**2. Post-quantum and cryptography venues.** Mailing lists and communities that
follow NIST work. Frame it as an engineering report, not an announcement: "a
proof-of-work chain built entirely on ML-DSA — here is what it costs."

**3. Hacker News, once.** The code quality, the honest README and the
adversarial bug hunt would land well with that readership, which respects
candour and detests hype. It is also merciless about cryptocurrency, so it is
worth exactly one attempt, with the emphasis on the engineering and the
mistakes found — including the inflation bug that was caught and fixed. A
project that publishes its own worst finding earns more trust than one that
never mentions any.

**4. Nothing paid, ever.** Paid promotion and influencer coverage would
destroy the only asset the project has. For this project specifically, being
unmarketed is part of the argument.

## What not to do

- **No price talk.** There is no price. Speculating about one invites the wrong
  audience and, in the EU, edges towards conduct that regulation treats
  differently from publishing code.
- **No "quantum-proof."** The honest phrasing is that no efficient attack is
  known. Overclaiming to an audience that knows the difference costs exactly
  the credibility the claim was meant to buy.
- **No buying coins from participants.** The founder's share falling from 100%
  to 10.9% through open competition is the strongest fact this project has.
  Reversing it for units with no market value would be a poor trade.
- **No urgency.** Nothing here expires. Manufactured scarcity reads as a
  warning sign to precisely the people worth attracting.

## A note on the law

Publishing code and running a network is straightforward. Actively promoting a
cryptocurrency in the EU touches MiCA, and the line matters: a fair launch
with no sale, no allocation and no issuer is the configuration least likely to
create obligations, while promoting an asset one holds a large share of looks
different. This is not legal advice, and if a market ever forms, that is the
point to get some.

## What success looks like

Not a price. Not an exchange listing. In the next months, in order:

1. **Someone who is not the author runs a public seed.** Today both entry
   points belong to one person at one provider; if that account goes, so does
   the way in for everyone new.
2. **Someone opens an issue or a pull request.** The first outside
   contribution is the difference between a published program and a project.
3. **Someone builds something nobody asked for** — a wallet, a monitor, a
   different implementation.
4. **The founder's share keeps falling.** It is the one metric that cannot be
   faked and that no competitor can claim.

Each of these is worth more than any number of readers.
