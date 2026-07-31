# Fixed 200 RPS closed-loop load, not max-throughput discovery

Every combination runs a rate-limited closed-loop generator pinned at **200 RPS**
(5 min ramp, 20 min plateau; measurements taken from the plateau window only).
We are measuring resource footprint *at equal delivered throughput*, so a
consumer can read the table as "same traffic, this much less CPU/memory."

## Considered Options

Open-loop max-throughput discovery was rejected for the primary comparison: if
each client is allowed to run at its own ceiling, the arms deliver different
request volumes, and any CPU/memory difference is then partly just a difference
in work done — the numbers stop being directly comparable. A separate
saturation study answers a genuinely different question ("which client wins at
the limit") and can be added later without invalidating these results.

## Consequences

The table says nothing about peak capacity. If someone asks "how many RPS can
poseidon push before it falls over," this benchmark does not answer that, and
the results must not be presented as if it does.
