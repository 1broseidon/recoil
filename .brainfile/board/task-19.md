---
id: task-19
title: "Strict entity filter: demote instead of hard-drop non-matching results"
column: todo
position: 8
priority: medium
tags:
  - audit
  - retrieval
  - bug
relatedFiles:
  - cmd/search.go
parentId: epic-1
createdAt: "2026-06-10T04:41:01.733Z"
---

## Description
cmd/search.go filterStrictEntityResults (353-377) + explicitPersonNameRE (379-395) treat any capitalized bigram as a person name and hard-filter results that lack it; commonNonPersonName has only four hardcoded exception pairs. Queries like 'adopt Visual Studio Code tasks' or 'migrate to North Star schema' drop every non-literal-matching result; the empty-result rescue at 458-466 only covers negative-evidence filtering. Fix: demote with a large score penalty instead of dropping, or only hard-filter when a person-ish cue precedes the match (Dr., my, with, named) as DoctorNameTerms already does.
