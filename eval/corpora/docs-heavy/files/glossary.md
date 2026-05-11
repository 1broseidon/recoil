# Glossary

Terms used throughout the Pelican docs.

- **Blob.** An immutable object in the object store. Blobs are the unit
  of durable storage for raw events.
- **Curated table.** A normalized table in Postgres derived from the
  object store. Hot queries read from curated tables.
- **Dead letter topic.** A Kafka topic where events that fail validation
  are sent for inspection.
- **Ingestion service.** The component that consumes from Kafka,
  validates, enriches, and writes to durable storage.
- **Producer.** Any upstream service that emits events to Kafka.
- **Serving layer.** The HTTP service that wraps Postgres for
  downstream consumers.
- **Transformation layer.** The scheduler that runs derived model
  definitions against curated tables.
