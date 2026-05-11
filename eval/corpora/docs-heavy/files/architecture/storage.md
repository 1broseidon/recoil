# Storage

This document covers where Pelican keeps data. There are two storage tiers
and a clear rule for which goes where.

## Object store

Raw events and long-lived archives live in the object store. Every event
that enters the ingestion layer is written here as a blob, partitioned by
producer and arrival hour. The object store is the canonical record of
what happened; if Postgres is rebuilt from scratch tomorrow, the object
store is what we rebuild it from.

Blobs are immutable. Reprocessing is done by reading existing blobs and
writing new ones; never by mutating an old blob in place.

## Postgres

A curated subset of the object store is normalized into Postgres tables.
Postgres exists for two reasons: hot queries from the serving layer, and
operational dashboards. It is not the source of truth and should not be
treated as one.

The Postgres footprint is intentionally small. If a query can be answered
from the object store with reasonable latency, it should be answered from
the object store and not from Postgres.

## Retention

Object store blobs are retained indefinitely unless legal review requires
otherwise. Postgres tables follow the table-level retention policy declared
in the table's metadata file.

## What we do not use

Pelican does not use a key value cache layer. There is no Redis or
Memcached in the storage path; if a query is hot enough to need caching,
it gets a materialized view in Postgres instead.
