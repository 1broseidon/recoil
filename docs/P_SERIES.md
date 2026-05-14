# Recoil Practical Memory P-Series

This plan turns the benchmark research into product work. The goal is not to
benchmax Recoil; it is to prove that local memory makes long-running agentic
workflows more reliable in coding and non-coding projects.

## Boundary Rule

- `recoil eval`: committed product gates. Small, deterministic, offline suites
  that exercise CLI retrieval, wake context, mined docs, profiles, lifecycle,
  and selected session evidence.
- `bench/`: external academic comparisons and research harnesses such as
  LongMemEval, LoCoMo, and BEAM. Useful for learning, not the product gate.
- `stress/`: large real-repo and volume tests. Operational confidence, not a
  default contributor requirement.

## Product P-Series

### P0 - Prove The Practical Loop

1. Position Recoil as local operational memory for long-running agentic
   workflows, with coding as the first wedge rather than the only market.
2. Make agent hooks useful at session start by including bounded wake context
   when a project database already exists.
3. Add practical workflow evals that use product surfaces, not benchmark-only
   code paths.
4. Add a coding workflow pack covering current decisions, rejected paths, mined
   docs, wake safety, and CLI command recall.
5. Add a non-coding workflow pack covering personal/career facts, preferences,
   food/travel context, and entity disambiguation.
6. Add session-evidence selector evals so transcript capture proves durable
   evidence selection instead of raw transcript hoarding.
7. Productize entity profiles with an exact-detail router:
   `search --profiles auto|on|off`.
8. Keep test hygiene boring: FTS5-required Go tests and eval gates must pass
   locally.

### P1 - Turn It Into A Repeatable Product Surface

1. Share retrieval plumbing between search, eval, MCP, profile building, and
   hook/wake context so practical evals measure the product path.
2. Add adapter payload fixtures for Claude Code, Codex, and OpenCode session
   evidence shapes.
3. Treat raw extracted/profile traces as upstream profile material, not as a
   separate retrieval-time channel that competes with answer context.
4. Add normalized eval artifacts with JSON and Markdown output for workflow
   reports.
5. Document non-coding workflows as first-class bounded projects.

### P2 - Integrate Without Expanding The Core

1. Add a minimal stdio MCP server, built on the official
   `github.com/modelcontextprotocol/go-sdk/mcp` Go SDK, exposing read-only
   `recoil_search` and `recoil_wake`.
2. Gate write-capable MCP tools behind `recoil mcp --allow-write`.
3. Keep MCP thin: it should wrap the CLI semantics rather than create a second
   memory product.

### P2.5 - Decision Relevance

1. Treat `claim_key` as the deterministic join axis for decision families.
2. Let `recoil decide` capture optional predicates:
   deterministic predicates that Recoil can evaluate narrowly, and
   semantic/external predicates that Recoil stores as review prompts.
3. Add `recoil check` as a rare, load-bearing decision-family audit with
   verdicts such as `use`, `use_replacement`, `ignore`, `review`, and
   `no_decision`.
4. Add `wake --include-decisions` so handoff context includes claim-keyed
   judgments and predicate status, not only retrieved prose chunks.
5. Gate the behavior with workflow evals for contradiction catching, broken
   predicates, clean handoff, and review discipline.

### P3 - Decision Verdict Maturity

See [P3_SERIES.md](P3_SERIES.md) for the full spec. Closes verdict-logic
and predicate-evaluation gaps surfaced by the P2.5 dogfooding pass across
nine projects and six content domains: date predicate capture hygiene,
opposition detection via decision stance, the remaining narrow Tier 1
evaluators, and Tier 1 / Tier 2 predicate composition.

### P4 - Later, Only If The Loop Holds

1. Optional daemon refresh for mined sources and profiles.
2. Better no-answer thresholds for absent-fact queries.
3. More source adapters after plain docs and session evidence are reliable.
4. Sync, team memory, or hosted features only after local single-user behavior
   stays predictable.

## Eval Consolidation P-Series

### P0 - Consolidate The Gate

1. Keep the default `recoil eval` suite tiny and deterministic.
2. Add named suites:
   `default`, `embeddings`, `workflows`, `session-evidence`, and `cli-hard`.
3. Move the CLI hard gate into `recoil eval --suite cli-hard` while preserving
   the shell script as an end-to-end smoke test.
4. Extend fixtures with `corpus`, `transcript`, `expected_content`, and
   `forbidden_content` records.
5. Track outcome metrics that matter in workflows: recall, MRR, empty-result
   accuracy, stale demotion, wake safety, scope isolation, and wrong-memory
   failures.

### P1 - Make Results Auditable

1. Use one eval runner for single fixtures and named suites.
2. Write normalized result artifacts with `recoil eval --out`.
3. Keep LoCoMo, BEAM, and LongMemEval under `bench/`, including profile/fact
   research tools.
4. Keep large real-repo experiments under `stress/`.
5. Document the boundary so future benchmark tools do not leak into the product
   CLI unless they become small deterministic workflow gates.

### P2 - Expand Carefully

1. Add new workflow fixtures only when they represent a real agent failure mode.
2. Require each new fixture to name the product behavior it protects.
3. Prefer wrong-memory and no-answer tests over only positive recall tests.
