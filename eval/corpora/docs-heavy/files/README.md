# Pelican

Pelican is the internal data platform for the analytics organization. It
collects, stores, transforms, and serves the events emitted by every customer
facing product surface.

The platform has four layers:

- the ingestion layer that accepts events from upstream producers,
- the durable storage layer where raw events and curated tables live,
- the transformation layer that builds derived models on a schedule,
- the serving layer where dashboards and downstream services read from.

This documentation set is the source of truth for engineers working on
Pelican. Start with `onboarding/new-engineer.md` if you are new to the team,
or with `architecture/overview.md` if you are integrating against the
platform from the outside.

For questions that are not answered here, see `faq.md`. For terms used
throughout the docs, see `glossary.md`.
