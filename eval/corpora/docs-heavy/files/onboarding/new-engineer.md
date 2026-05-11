# New engineer onboarding

Welcome to Pelican. This document is meant to get you productive in your
first week. It is not exhaustive; it is the smallest set of pointers that
lets you make a useful change.

## Day one

Read `architecture/overview.md` end to end. Do not skim. The four-layer
model underpins every conversation you will have on this team.

Get your laptop set up: clone the platform monorepo, run the bootstrap
script, and confirm you can run the test suite locally. The bootstrap
script installs every dependency you need; do not install things by hand.

## First week

Read the ADRs in order. They are short and they explain why the system
looks the way it does. If an ADR confuses you, ask in the team channel;
ADRs that confuse new engineers are a signal that the ADR is wrong.

Shadow an on-call rotation. You will not be on call in your first month,
but you should see one incident from the inside before you have to handle
one yourself.

## First month

Make a real change. The platform team keeps a list of starter tasks
tagged `good-first-issue` in the issue tracker; pick one and ship it end
to end, including the rollout described in `runbooks/deployment.md`.
