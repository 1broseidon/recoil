# docs-heavy stress corpus

A frozen, synthetic documentation tree for a fictional internal data platform
called Pelican. The corpus is intentionally written to surface foundation
behavior that the curated `eval/fixtures.jsonl` cannot reach:

- paraphrase between queries and source vocabulary (e.g. docs say
  "ingestion", queries say "data import")
- absent facts (e.g. redis, graphql) that the corpus deliberately never
  mentions
- two documents that contradict each other without supersession links
  (`adr/0003-auth-jwt.md` vs `adr/0004-auth-oidc.md`)
- planning vs reality (roadmap items that read like current truths)
- mining defaults at docs density (long prose, low keyword repetition,
  cross-references between files)

The corpus is checked in verbatim so runs are reproducible. Do not edit the
files under `files/` once a baseline report references them; add a new corpus
or a new version instead.

The scoring contract is path-based: cases name expected `source_path` values,
and any mined chunk from a matching file counts as a hit. This matches how
agents actually consume memory ("which document") rather than how the
hand-curated fixture works ("which fixture ID").
