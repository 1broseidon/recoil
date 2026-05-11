# ADR 0001: Postgres for the curated tier

## Status

Accepted.

## Context

The serving layer needs a hot query store with second-or-better latency
for analytical dashboards and downstream service reads. Reading directly
from the object store is too slow for these access patterns.

We considered Postgres, MySQL, and ClickHouse. ClickHouse offers the best
raw analytical performance but is operationally heavier than the team can
support this year. MySQL would work but the existing operator expertise
is on Postgres.

## Decision

We use Postgres as the curated, hot-query tier behind the serving layer.
The object store remains the canonical record of what happened; Postgres
holds a normalized subset.

## Consequences

We inherit Postgres operational tooling for backups, monitoring, and
upgrades. We accept that very wide analytical queries will be slower than
on a columnar store, and we mitigate by precomputing aggregates in the
transformation layer.
