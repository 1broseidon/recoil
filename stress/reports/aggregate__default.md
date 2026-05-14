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
| kubernetes | 1.0 | 91 | 116 |
| rails | 1.7 | 175 | 214 |
| turbo | 0.2 | 39 | 48 |
| nextjs | 1.4 | 101 | 140 |
| ruff | 0.6 | 75 | 93 |
| transformers | 12.2 | 866 | 991 |
| rust | 3.7 | 196 | 234 |
| raft | 0.0 | 23 | 29 |
| curl | 2.2 | 149 | 174 |
| bazel | 1.8 | 176 | 206 |
| fastapi | 4.7 | 448 | 510 |
| recoil | 0.1 | 25 | 29 |


## Blockers (< 99%)

_No repos below 99% floor._

