# ADR 0004: OIDC for service-to-service authentication

## Status

Accepted.

## Context

The shared-key JWT approach has been painful in practice. Key rotation
requires every client to redeploy with the new key, and there is no good
way to revoke a single client without rotating the whole fleet.

## Decision

The serving layer now delegates authentication to the company OIDC
provider. Clients obtain short lived bearer tokens from the provider and
present them to the serving layer, which validates them against the
provider's public keys.

## Consequences

Key rotation is owned by the OIDC provider. Individual clients can be
revoked without affecting the rest of the fleet. Every request now incurs
a public-key validation, which is cheaper than the shared-key flow in
practice because validation can be cached per token.
