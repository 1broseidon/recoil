# Architecture Overview

Pelican is a four-layer event platform. This page describes how the layers
fit together at a high level; deeper documents cover each layer in detail.

## Layers

1. **Ingestion.** Producers publish events to a Kafka cluster. Consumers in
   the ingestion layer validate, enrich, and route events into the storage
   layer. The ingestion path is the only sanctioned way to add data; ad hoc
   loads are not supported.
2. **Storage.** Raw events land in the object store. A curated subset is
   normalized into Postgres tables that downstream services read from.
   Long-term retention is the object store; Postgres is for hot queries.
3. **Transformation.** A scheduler runs dbt-like model definitions against
   the curated tables on a configurable cadence. Outputs are written back
   into Postgres for serving and into the object store for archival.
4. **Serving.** Downstream consumers read through a thin HTTP service that
   wraps Postgres with authentication, rate limiting, and audit logging.

## Cross-cutting concerns

Observability, authentication, and deployment are handled uniformly across
layers. See the runbooks and ADR documents for specifics. Pelican does not
run its own identity provider; authentication is delegated, and the current
approach is documented under `adr/`.

## What Pelican is not

Pelican is not a general purpose message bus, not a real-time alerting
system, and not a customer-facing API gateway. It is a platform for moving,
storing, and querying analytics events at internal scale.
