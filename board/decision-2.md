---
id: decision-2
title: "Product thesis: recall must be cheaper than guessing"
type: decision
column: reference
position: 1
priority: high
tags:
  - product
  - v0
  - usability
parentId: epic-1
createdAt: "2026-05-11T02:25:43.319Z"
---

## Description
Recoil exists to make the right local evidence cheaper to retrieve than guessing. The v0 habit loop is init, wake, search before historical claims, add durable decisions, and audit with list/show/forget.

## Decision

Recoil is not a general memory platform. It is the local memory command an agent
can reach for at the moment of action.

The product promise:

> Recoil makes an agent's local past searchable when remembering would prevent
> wasted work.

## User Shape

- Operator: wants continuity without surrendering privacy or control.
- Agent: wants a tiny low-cost recall loop.

## V0 Loop

1. `recoil init`
2. `recoil wake`
3. `recoil search "<topic>"`
4. `recoil add --role decision "..."`
5. `recoil list`, `recoil show`, `recoil forget`

## Usability Constraints

- The common path has no flags inside an initialized project.
- Scope must always be visible.
- Memory is evidence, not belief.
- Recall must be cheap enough that agents use it before guessing.
