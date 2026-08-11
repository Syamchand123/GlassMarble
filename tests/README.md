# GlassMarble End-to-End Test Suite

This directory is the full-product, battle-test suite for GlassMarble. It
treats the product the way a real user does: running real commands (`gmb
init`, `gmb analyze`, `gmb visualize`, `gmb ai`, ...) against real sandbox
repositories, plus direct pipeline-level tests for every stage.

## Layout

| Directory      | Contents |
|----------------|----------|
| `harness/`     | Shared test infrastructure (sandboxes, CLI runner, fixtures, mock LLM). |
| `e2e/`         | Real user flows: the full journey from `init` through `analyze`, `visualize`, `export`, `compare`, `snapshot`, `timeline`, `memory` and `ai`. |
| `stages/`      | Pipeline-level tests for stages 1–12 (ingestion → linking → intelligence → memory → fusion → learning → aging). |
| `nonfunctional/` | Performance budgets, determinism, idempotency, concurrency, resilience, corruption recovery. |
| `edgecases/`   | Empty repos, no-git repos, giant files, unknown languages, corrupt input, fallback behavior. |
| `qa/`          | Golden outputs, JSON schema conformance, output contracts, exit codes. |

## Rules for tests in this tree

1. **Isolation.** Every test builds its own sandbox under `t.TempDir()`;
   the live `G:\GlassMarble\.glassmarble` workspace is never touched.
2. **No `t.Parallel()` in CLI tests.** The in-process runner temporarily
   swaps `os.Stdout` and the process working directory (process-global
   state), so parallel tests would race. Serialize via the harness mutex.
3. **No real LLM.** AI commands talk to `harness.NewMockLLM`, a scriptable
   OpenAI-compatible server on `httptest` (SSE streaming, tool calls,
   failure injection supported).
4. **Real binaries where it matters.** A few flows (watch, hooks,
   cross-process locking, exit codes) exercise the actual compiled binary
   via `harness.BuildBinary`.

## Running

```bash
# everything
go test ./tests/...

# one suite
go test ./tests/e2e/...

# one test, verbose
go test ./tests/e2e/ -run TestUserJourney -v
```

Tests requiring git (analyze family, watch, hooks) skip cleanly when git is
not installed. Tests requiring CGO (tree-sitter parsing) fail the build only
if CGO is disabled, which the Go toolchain reports at compile time.
