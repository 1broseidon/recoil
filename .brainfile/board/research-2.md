---
id: research-2
title: Brainfile protocol takeaways for Recoil
type: research
column: reference
position: 7
priority: medium
tags:
  - research
  - brainfile
  - v0
parentId: epic-1
createdAt: "2026-05-11T02:31:23.991Z"
---

## Description
Brainfile v2 uses .brainfile/brainfile.md plus board/ active documents and logs/ completed documents with ledger.jsonl. It supports custom document types, parentId links, contracts, rules, agent instructions, and CLI/MCP tools. Recoil can optionally mine these typed records as high-signal project memory when present.

## Sources

- Local repo: `/Users/george/Projects/core/brainfile/protocol`
- Protocol docs: `https://brainfile.md/reference/protocol`
- CLI docs: `https://brainfile.md/reference/commands`

## Useful Brainfile Ideas

- v2 directory architecture: `.brainfile/brainfile.md`, `board/`, `logs/`.
- Active documents are separate markdown files with YAML frontmatter.
- Completed documents move to logs and append to `logs/ledger.jsonl`.
- Custom document types allow `epic`, `adr`, `decision`, `research`, and more.
- Stable IDs and `parentId` create durable references.
- Contracts encode deliverables, validation commands, and status.
- Agent instructions and rules are local project policy.
- CLI and MCP expose the same underlying protocol.

## Recoil Takeaway

Brainfile should be an optional source adapter. It is valuable because it
already separates current work, reference decisions, and completed logs. That is
exactly the freshness and provenance shape Recoil needs.

But Recoil's core must remain independent:

- no Brainfile dependency in `init`, `add`, `search`, `wake`, or `forget`;
- no assumption that `.brainfile/` exists;
- generic files and transcripts remain supported for everyone;
- Brainfile records are an optional high-signal input when present.
