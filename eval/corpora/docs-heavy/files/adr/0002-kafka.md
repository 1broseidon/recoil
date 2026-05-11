# ADR 0002: Kafka for ingestion

## Status

Accepted.

## Context

Every upstream producer needs a durable, ordered, multi-consumer log to
write to. Kinesis was an alternative; in-process queues were considered
and rejected for operational reasons.

## Decision

We use Kafka as the single sanctioned ingestion bus. All producers
publish to Kafka; the ingestion service consumes and writes to the
object store and Postgres.

## Consequences

We maintain a Kafka cluster and the operational burden that comes with
it. In exchange, every producer gets the same retry, replay, and
ordering semantics, and the ingestion service has a single integration
point rather than one per producer.
