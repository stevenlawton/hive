# <subject>

**Ticket:** <id>
**Repo:** <repo name>
**Base commit:** <full sha at the time of writing>
**Written:** <YYYY-MM-DD>

## Current behaviour

What the code does today, with `file:line` anchors. Verified by reading, not
assumed.

## Root cause

For a bug: the mechanism, and the evidence for it. For a feature: why the
current shape does not accommodate the requirement.

## The contract

Exact files to change. Exact functions. The full API surface for anything new —
signatures, parameters, return types. Written so an agent that cannot ask a
question can execute it without inventing anything.

## Verification

Runnable commands, and what output means success. Not "run the tests".

## Blast radius

Other call sites, other implementations of a changed interface, persisted data
whose shape changes, tests that encode current behaviour.

## Critic findings

What the critics found and what changed as a result. Where a critic was wrong,
that it was wrong and why.

## Open questions

Anything that could not be settled without a human. **Each needs enough context
to answer cold** — the question, the options, and what you would choose. Empty
is fine; guessing is not.
