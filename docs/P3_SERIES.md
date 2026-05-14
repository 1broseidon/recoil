# Recoil P3 — Decision Verdict Maturity

This spec supersedes the P3 stub in `docs/P_SERIES.md`. It is the next concrete
tranche of work after P2.5 (Decision Relevance) and is grounded in a multi-day
dogfooding pass across nine projects and six content domains.

This document was revised after a first-pass review caught that the original
draft misstated the P2.5 baseline and conflicted with shipped fixtures. The
revision preserves all P2.5 behavior and frames each track around the genuine
gaps the dogfooding surfaced.

## Evidence Base

The dogfooding pass exercised `recoil decide`, `recoil check`, and
`recoil wake --include-decisions` on:

| Project              | Domain                 | Decisions | Tier 1 predicate(s) attempted   |
|----------------------|------------------------|-----------|---------------------------------|
| recoil (own)         | Go                     | 7         | —                               |
| cymbal (own)         | Go                     | 4         | `package_version`, `lint_config_value` (unwired) |
| ketch (own)          | Go                     | 5         | —                               |
| brainfile-cli (own)  | TypeScript             | 5         | `tsconfig_value` (unwired)      |
| cecli (contributor)  | Python                 | 5         | `python_version` (unwired)      |
| Documents            | Research + journal     | 5         | —                               |
| ai-consulting        | Business operations    | 4         | —                               |
| leveldesk            | Legal / tax procedure  | 5         | `date_expiry` (kind not wired)  |
| taxes                | Tax filing             | 2         | —                               |

Total: 42 captured decisions, five distinct Tier 1 predicate kinds attempted
in writes — none of which dispatched to a wired evaluator under the names used.

## P2.5 Baseline (What Is Already Wired)

Before stating the gaps, the existing state must be acknowledged:

- `recoil check` already evaluates three deterministic predicate kinds:
  `valid_until`, `source_unchanged`, and `claim_key_status`. They produce
  `predicate_status: holds | broken | unknown` and reasons such as
  `valid_until_expired`, `source_hash_changed`, and `claim_key_status_missing`.
  See `cmd/decision_predicate.go:226` and `README.md:275`.
- `recoil decide --valid-until <date>` normalizes to `kind=valid_until` when
  no explicit kind is set (`cmd/decision_predicate.go:76` and `:154`).
- The existing eval fixture `eval/workflows/decision-relevance.jsonl` asserts
  the contract: expired `valid_until` returns
  `predicate_status: broken, reason: valid_until_expired`. Any P3 change must
  preserve those exact strings or migrate the fixture deliberately.

## Findings That Drive P3

1. **Some Tier 1 evaluators are wired; others surfaced as missing.** The
   dogfooded predicate kinds `package_version`, `tsconfig_value`,
   `python_version`, and `lint_config_value` have no dispatch entry; they
   fall through to `external_predicate`. These are the real evaluator gaps.
2. **Predicate-kind capture is too easy to get wrong.** The leveldesk DTF-4
   capture set `kind=date_expiry` via `--predicate` while also setting
   `--valid-until`. The explicit kind override beat the `--valid-until`
   normalization, so the predicate landed under a kind with no dispatch.
   The fix is a guard at decide time, not a new evaluator kind.
3. **`check` does not detect opposition between request and decision.** A
   request that *agrees with* a stored decision and a request that *reverses
   the same decision* both return `verdict: use, reason: single_current_decision`.
   Reproduced identically across all six content domains.
4. **Tier 1 bound-checks alone do not satisfy `holds_while`.** When a wired
   predicate evaluates to `holds`, any non-empty `holds_while` constraint
   on the same decision is dropped from the verdict response. The cecli
   Python-version case showed how a satisfied bound can still leave a
   contested semantic constraint silent.
5. **Matching and voice preservation are solid.** 27 of 27
   contradiction-catcher queries hit the right `claim_key`, including on
   legal, research, and business terminology. These are the foundation P3
   builds on, not gaps.

## Boundary

- **In scope:** verdict-logic improvements, narrow Tier 1 evaluators that
  read a single project artifact, capture-side guards on predicate-kind
  conflicts, eval coverage for the new paths.
- **Out of scope:** LLM-based opposition inference, exotic project
  introspection (delegates to hooks per `architecture.predicate.evaluation`),
  team sync, hosted features, daemon refresh of evaluator state, new CLI
  surfaces beyond the existing `decide` / `check` / `wake` verbs.
- **Hard compatibility gate:** the existing `decision-relevance.jsonl`
  fixture and every other P2.5 fixture must pass unchanged. P3 may add
  new fixtures; it must not modify existing assertions.

## Product P3

### Track A — Capture Hygiene For Date Predicates

The dogfooded DTF-4 failure was UX, not evaluator coverage. `valid_until`
is already wired and produces the right `broken` / `valid_until_expired`
shape when the date has passed. Track A makes it harder to capture a date
predicate under a kind the dispatcher does not recognize.

1. In `recoil decide`, when `--valid-until` is set, the kind resolves to
   `valid_until` unless `--predicate kind=<X>` is explicitly set to
   something else. Today's behavior already does this when no `--predicate`
   is provided.
2. When `--valid-until` is set AND `--predicate kind=<X>` is set to a kind
   other than `valid_until` or the accepted alias `date_expiry`, emit a
   non-zero exit and a clear error:
   "conflicting predicate metadata: --valid-until implies kind=valid_until,
   but --predicate kind=X was given." No silent precedence either way.
3. Accept `date_expiry` as a write-time alias that normalizes to
   `valid_until` at storage time. Anyone who reaches for "date expiry"
   instinctively gets the wired evaluator; the canonical kind name in
   storage remains `valid_until` and the dispatch table stays one row.
4. Document the canonical kind in `recoil decide --help` and in
   `README.md`: "date-bound decisions use `--valid-until <date>`; the
   stored predicate kind is `valid_until`."
5. No new status names or reasons. The existing
   `predicate_status: broken, reason: valid_until_expired` contract is
   preserved exactly.

### Track B — Opposition Detection

The largest behavior improvement, content-agnostic, must not introduce an
LLM dependency in the `check` path.

1. Add an optional `stance` field to decisions. Values:
   `prefers`, `rejects`, `requires`, `forbids`. Expose at write time as
   `recoil decide --stance <value> --subject <subject>`. Decisions without
   a stance continue to produce P2.5 verdicts. No existing fixture changes.
2. Add a `requested_action` inference step in `recoil check`. A small
   deterministic rule set classifies the query by what it wants to do to the
   decision subject:
   - `affirm_decision` — explicit meta-cues such as "keep the decision",
     "stick with the call", "follow the policy", or stance-cued phrases
     such as "keep avoiding X" / "continue requiring X". This means "honor
     the remembered judgment", not merely "use X".
   - `adopt_subject` — cues that introduce or use the subject: "use / add /
     introduce / enable / switch to / file / send / register / treat as"
     plus the subject. This is the class that catches "use Redis" against a
     `rejects Redis` decision.
   - `remove_subject` — cues that remove or avoid the subject: "drop /
     disable / turn off / switch off / stop / avoid / do not use / keep out"
     plus the subject.
   - `replace_subject` — cues that move away from the subject: "switch from
     X", "replace X with Y", or "move from X to Y". If the query replaces
     something else with the decision subject, classify it as
     `adopt_subject`, not `replace_subject`.
   - `unknown` — no cue matched, or cues matched without the decision subject.
3. The polarity matrix maps `(decision.stance, requested_action)` to a
   verdict. Cells marked `review` always carry
   `reason: request_contradicts_decision`; cells marked `use` always carry
   `reason: request_consistent_with_decision`; the existing
   `single_current_decision` reason remains the fallback when either side
   is unknown.

   | decision.stance \ requested_action | `affirm_decision` | `adopt_subject` | `remove_subject` | `replace_subject` | `unknown` |
   |------------------------------------|-------------------|-----------------|------------------|-------------------|-----------|
   | `prefers`                          | use               | use             | review           | review            | fallback  |
   | `requires`                         | use               | use             | review           | review            | fallback  |
   | `rejects`                          | use               | review          | use              | use               | fallback  |
   | `forbids`                          | use               | review          | use              | use               | fallback  |

   Notes:
   - `requires` and `forbids` differ from `prefers` and `rejects` only in
     the `recommendation` field (`block_action` vs. `ask_operator`), not
     in the verdict.
   - `adopt_subject` against `prefers/requires X` is `use` because using X
     reinforces the decision; `adopt_subject` against `rejects/forbids X`
     is `review` because it reintroduces what was rejected.
   - `replace_subject` means replacing the decision subject with an
     alternative. It is `review` for `prefers/requires X`, but `use` for
     `rejects/forbids X` because moving away from a rejected subject aligns
     with the decision.
4. Carry the existing `recheck_prompt` through unchanged on every verdict.
5. **Backfill path for existing decisions.** Add
   `recoil mark <mem_id> --stance <value> --subject <subject>` so the 42
   dogfooded decisions (and any other P2.5-era captures) can gain a stance
   without re-writing them. Decisions never marked simply do not benefit
   from opposition detection; the demo requires backfill on the specific
   memories used.

### Track C — Narrow Tier 1 Evaluators (Genuine Gaps Only)

Wire the evaluators implied by `architecture.predicate.evaluation` that are
not already present. Each must read a single project artifact and produce
`holds | broken | unknown` without any LLM call. Existing evaluators
(`valid_until`, `source_unchanged`, `claim_key_status`) are not touched.

1. `package_version` evaluator. Parses the project's package manifest based
   on file presence:
   - `go.mod` for subjects `go` and any module path
   - `package.json` for subjects `engines.node` and any dependency name
   - `pyproject.toml` or `setup.cfg` for `requires-python` and any
     declared dependency
   Compares against `operator` and `value`. Multiple manifests in one
   project are evaluated in priority order; the first one matching the
   subject wins. Anything not matched returns `unknown`.
2. `tsconfig_value` evaluator. Walks a dot-separated path
   (e.g. `compilerOptions.strict`) through `tsconfig.json`. Compares the
   leaf to `predicate.value` with `==` or `!=`. Supports `extends` one
   level deep; anything deeper returns `unknown`.
3. `lint_config_value` evaluator. Covers the two configs surfaced during
   dogfooding:
   - `.golangci.yml` (read
     `linters-settings.gocyclo.max-complexity`)
   - `pyproject.toml` `[tool.*]` sections for ruff / mypy / black
   Anything else returns `unknown` so the predicate falls through to
   external; this is intentional — exotic configs delegate to hooks.
4. No new CLI surface for predicate evaluation. Hooks that want to persist
   results for exotic predicates write them into the nested predicate
   metadata before `check` runs, specifically
   `metadata.predicate.status` plus optional `metadata.predicate.prompt` or
   `metadata.predicate.recheck_prompt`. Built-in evaluators still compute
   status at check time; hook-persisted status is only the escape hatch for
   predicates Recoil intentionally does not understand.

### Track D — Tier 1 + Tier 2 Composition

Closes the gap from cecli's Python-version case: a wired Tier 1 predicate
returns `holds`, but a non-empty `holds_while` on the same decision is
still in contest.

1. When a wired predicate evaluates to `holds` AND `holds_while` is
   non-empty, the response gains an `advisory` field carrying the
   `holds_while` text. `predicate_status` remains `holds` (no new status
   names); `reason` remains the existing one (e.g. `valid_until_holds`).
2. The opposition matrix (Track B) runs independently of predicate
   composition. If the request opposes the decision's stance, the verdict
   is `review` even when the Tier 1 predicate holds. `holds_while`
   advisory text accompanies the contradiction explanation rather than
   suppressing it.
3. Document in `recoil decide --help` that `--predicate ...` and
   `--holds-while ...` compose; neither replaces the other.

## Eval P3

### New Fixtures (Additive Only)

Add under `eval/workflows/`. No existing rows are modified.

1. `decision-opposition.jsonl` — paired contradicting and aligning queries
   against the same `claim_key` for stanced decisions. Each row asserts
   `verdict` and `reason` per the Track B polarity matrix. Includes one
   case per stance value and per requested_action class. Targets Track B.
2. `predicate-coverage.jsonl` — one decision per new wired evaluator
   (`package_version` on go.mod, `tsconfig_value` on tsconfig.json,
   `lint_config_value` on .golangci.yml). Asserts the right
   `predicate_status` and `reason` strings. Targets Track C.
3. `predicate-composition.jsonl` — decisions with both a wired Tier 1
   predicate at `holds` and a non-empty `holds_while`. Asserts the
   `advisory` field appears in the response. Targets Track D.
4. `cross-domain-matching.jsonl` — natural-language queries from research,
   business-ops, and legal/tax domains paired to expected `claim_key`
   hits. Prevents regressions in the matching layer drawn from the
   dogfooding pass. No verdict assertions; purely matching coverage.

### Suite Hooks

1. Add a `decisions` suite to `recoil eval` that runs the four new
   fixtures plus the existing `decision-relevance.jsonl`. Fast,
   deterministic, no network.
2. Promote one row per new fixture into `cli-hard` so the default gate
   catches a complete regression.
3. LoCoMo and BEAM stay under `bench/` per the existing boundary rule.

## Answers To Reviewer Questions

- **Should `date_expiry` be a new predicate kind, or should P3 extend
  `valid_until` with operator support?** Neither. P3 keeps `valid_until`
  as the single canonical kind, accepts `date_expiry` as a write-time
  alias that normalizes to `valid_until` at storage, and adds a guard
  against non-alias conflicting `--predicate kind=...` overrides. Operator
  support is not added because every dogfooded case was implicit `<=` and the
  existing fixture's `valid_until_expired` reason already covers it.
- **Migration for the 42 dogfooded decisions captured before stance.**
  Stance is opt-in. Decisions without stance keep P2.5 behavior with no
  regression. Decisions used in demos need stance backfill via the new
  `recoil mark --stance <value> --subject <subject>` path (Track B item 5).
  No automated migration is shipped; demo curation handles the small
  set that matters.

## What P3 Explicitly Does Not Do

- It does not add an LLM call anywhere in the `check` path. Stance
  inference is heuristic; ambiguous queries fall through to today's
  `reason: single_current_decision` verdict.
- It does not introduce new predicate-status names. `holds | broken |
  unknown` and `needs_review` remain the full set.
- It does not duplicate existing evaluators. `file_hash_changed` is not
  added because `source_unchanged` already covers it; `claim_key_status`
  is not re-implemented.
- It does not add a `recoil predicate eval` CLI surface. Hooks can persist
  external predicate status through `metadata.predicate.status`, as described
  in Track C, without adding a new command.
- It does not modify P2.5 fixtures. New fixtures are additive only.

## Acceptance

P3 is done when:

- The DTF-4-style decision, captured with `--valid-until 2026-03-16` (no
  conflicting `--predicate kind`), returns
  `verdict: review, predicate_status: broken, reason: valid_until_expired`
  once the date has passed — i.e. the existing P2.5 contract, accessed
  via the corrected capture path.
- Attempting to capture a decision with both `--valid-until` and a
  conflicting `--predicate kind=<X>` exits non-zero with a clear error.
- A contradiction-catcher query against a stanced decision returns
  `verdict: review, reason: request_contradicts_decision`; an alignment
  query against the same decision returns
  `verdict: use, reason: request_consistent_with_decision`. Unstanced
  decisions keep their `reason: single_current_decision` behavior.
- The three new evaluators in Track C each produce
  `holds | broken | unknown` on their target artifacts in fixture runs.
- A wired Tier 1 predicate at `holds` plus a non-empty `holds_while`
  produces an `advisory` field in the `check` response without changing
  `predicate_status`.
- The `decisions` suite is green in `recoil eval --suite decisions` and
  one row per new fixture is included in `cli-hard`.
- Every existing P2.5 fixture passes with no change to its assertions.
