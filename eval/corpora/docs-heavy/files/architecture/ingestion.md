# Ingestion

The ingestion layer is the only sanctioned path for new data to enter
Pelican. This document describes how events move from a producer to a
durable blob.

## Producers

Upstream services emit events to a Kafka cluster. Each producer owns a
set of topics and is responsible for the schema of the messages it
publishes. Producers do not write to Postgres or the object store
directly; everything goes through Kafka.

## Consumers

The ingestion service consumes from Kafka, validates each event against
the registered schema, enriches it with platform-side metadata, and
writes it to the object store. A separate process replicates the curated
subset into Postgres.

## Backpressure and retries

If the object store is slow, the ingestion service slows its consumer
offset rather than buffering unbounded in memory. If validation fails,
the event is written to a dead letter topic for inspection. Producers
should not retry on their own; the platform handles retries inside the
ingestion service.

## Ad hoc loads

There is no ad hoc load path. If you need to backfill historical data,
open a backfill ticket and the platform team will run it through the
ingestion service with a synthetic producer.
