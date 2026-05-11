# ADR 0003: JWT for service-to-service authentication

## Status

Accepted.

## Context

The serving layer needs to authenticate calls from downstream services.
Mutual TLS is operationally heavy at this team size, and an external
identity provider would add a network hop on every call.

## Decision

We use JSON Web Tokens signed by a shared internal key as the credential
format for service to service calls into the serving layer. Tokens are
short lived and rotated daily.

## Consequences

The serving layer must verify token signatures on every request. Each
client service must hold a current signing key. Key rotation requires
coordination across all clients.
