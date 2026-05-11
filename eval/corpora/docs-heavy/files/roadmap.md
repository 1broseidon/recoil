# Roadmap

This page describes work the platform team intends to do but has not
yet done. Nothing on this page reflects current production behavior.
If a roadmap item is shipped, the corresponding architecture or runbook
document is updated and the item is removed from this page.

## Encryption at rest

We plan to encrypt object store blobs at rest using customer-managed
keys. This is not currently implemented; today blobs are stored without
application-level encryption, relying only on the underlying storage
provider's defaults.

## Schema registry

We plan to introduce a central schema registry so producers can evolve
schemas without coordinating with the ingestion team. Today schemas are
checked into the platform monorepo and producers send pull requests when
they change.

## Self-serve backfills

We plan to expose backfills as a self-serve API so teams do not have to
open a ticket. Today backfills require platform team involvement.
