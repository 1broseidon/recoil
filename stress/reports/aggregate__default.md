# Stress report — label `default`

**Overall accuracy:** 100.00% (216/216)  
**Floor:** 99%  
**Repos run:** 12 / 12

## Matrix

| repo | overall | onboarding | security | tests | release | adversarial-paraphrase | noise-resistance |
|---|---|---|---|---|---|---|---|
| kubernetes | 100.0% (20/20) | 5/5 | 3/3 | 3/3 | 2/2 | 5/5 | 2/2 |
| rails | 100.0% (20/20) | 5/5 | 3/3 | 3/3 | 2/2 | 5/5 | 2/2 |
| turbo | 100.0% (20/20) | 5/5 | 3/3 | 3/3 | 2/2 | 5/5 | 2/2 |
| nextjs | 100.0% (16/16) | 5/5 | — | 3/3 | 2/2 | 4/4 | 2/2 |
| ruff | 100.0% (16/16) | 5/5 | — | 3/3 | 2/2 | 4/4 | 2/2 |
| transformers | 100.0% (20/20) | 5/5 | 3/3 | 3/3 | 2/2 | 5/5 | 2/2 |
| rust | 100.0% (16/16) | 5/5 | — | 3/3 | 2/2 | 4/4 | 2/2 |
| raft | 100.0% (14/14) | 4/4 | — | 3/3 | 2/2 | 3/3 | 2/2 |
| curl | 100.0% (20/20) | 5/5 | 3/3 | 3/3 | 2/2 | 5/5 | 2/2 |
| bazel | 100.0% (20/20) | 5/5 | 3/3 | 3/3 | 2/2 | 5/5 | 2/2 |
| fastapi | 100.0% (20/20) | 5/5 | 3/3 | 3/3 | 2/2 | 5/5 | 2/2 |
| recoil | 100.0% (14/14) | 4/4 | — | 3/3 | 2/2 | 3/3 | 2/2 |


## Latency

| repo | mine s | search p50 ms | search p95 ms |
|---|---|---|---|
| kubernetes | 0.9 | 97 | 116 |
| rails | 1.7 | 179 | 219 |
| turbo | 0.2 | 39 | 51 |
| nextjs | 1.4 | 103 | 140 |
| ruff | 0.6 | 76 | 97 |
| transformers | 12.1 | 842 | 974 |
| rust | 3.5 | 196 | 232 |
| raft | 0.0 | 22 | 25 |
| curl | 2.1 | 147 | 167 |
| bazel | 1.8 | 173 | 205 |
| fastapi | 4.7 | 421 | 509 |
| recoil | 0.1 | 27 | 29 |


## Blockers (< 99%)

_No repos below 99% floor._

