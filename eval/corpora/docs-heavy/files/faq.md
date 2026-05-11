# Frequently asked questions

This page collects answers to questions that come up often enough to
deserve a stable home. If you find yourself asking the same question
twice, add it here.

## Why is Postgres so small?

Because Postgres is the hot-query tier, not the source of truth. The
object store is the source of truth. See `architecture/storage.md` for
details.

## How do I get my events into Pelican?

Publish them to Kafka through the producer SDK. The ingestion layer
will pick them up. See `architecture/ingestion.md`.

## How do I deploy?

Tag, run the release pipeline against staging, then production. The
full procedure lives in `runbooks/deployment.md`.

## What is the on-call rotation?

Weekly, Monday to Monday. Handover happens in the on-call channel on
Monday morning.

## Can I run a one-off SQL query against Postgres?

Yes, through the read replica. Do not run one-off queries against the
primary. The replica connection string is in the team wiki.
