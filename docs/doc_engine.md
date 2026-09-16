# Documentation Engine — GlassMarble

> Living documentation, grounded in your actual code, written in plain English by an LLM — not a fact-table dump. This guide is the companion to the [README](../README.md) and the CLI Overview ([docs/cli.md](cli.md)).

---

## 1. What it does

`gmb doc` turns your codebase's Architecture Knowledge Graph (built by `gmb analyze`) into real documentation: exported interfaces, error catalogs, config references, runbooks, ADRs, and more — kept in sync as the code changes. The engine supplies the grounding data (symbols, signatures, doc comments, call graphs, config variables, sentinel errors); an LLM writes the actual explanation in natural language. A deterministic reference table (the grounded facts) is appended underneath every section, so the prose and the data never drift apart.

Managed sections live inside markers in your own Markdown files:

```markdown
## Concurrency & Thread Safety

<!-- gmb:begin:concurrency -->
... LLM-written prose here, with a reference table underneath ...
<!-- gmb:end:concurrency -->
```

Everything outside the markers is yours — the engine never touches it.

## 2. Quick start

```bash
# 1. Build the Architecture Knowledge Graph once (also runs automatically on commit if you set up the git hook — see docs/getting-started.md)
gmb analyze

# 2. Configure an LLM provider — the default path requires one (see §3)
gmb ai configure

# 3. Scaffold a document for a package
gmb doc init docs/store.md --archetype module --scope "internal/store/**"

# 4. Generate it
gmb doc --doc store
```

`gmb doc init` writes both the Markdown skeleton and an entry in `.glassmarble/docs.yaml`. Re-run `gmb doc` any time — only sections whose underlying code actually changed get regenerated (zero LLM calls, zero token cost, on an unchanged section).

## 3. The LLM is mandatory by default — and why

The whole point of this engine is documentation a person can actually read. Handing a reader a raw table of `func Foo(x int) error` signatures with no explanation isn't documentation — it's an index. So by default, `gmb doc` and `gmb docserve` **require a working LLM provider** and refuse to generate anything if one isn't configured, rather than silently degrading to a fact-table dump that looks finished but explains nothing.

If no provider is configured, `gmb doc` tells you exactly what's wrong and how to fix it:

```
gmb ai configure          # interactive provider setup
gmb ai doctor              # full connectivity diagnostic
```

**Opting out:** if you want the grounded reference tables alone — no prose, no LLM, no network call — pass `--no-llm`:

```bash
gmb doc --no-llm
```

This is a deliberate, explicit choice, not a fallback: `--no-llm` output is a complete, standalone document (tables + diagrams), not half of one.

## 4. The 10 built-in archetypes

`gmb doc init --archetype <name>` scaffolds a pre-configured document with sensible sections and grounding directives for a common documentation shape:

| Archetype | For |
|---|---|
| `module` | A single package's exported interface, concurrency model, errors, callers |
| `architecture` | System-wide overview, components, data flow, storage, security boundaries |
| `api` | REST/RPC endpoints, request/response schemas, auth, error codes |
| `config` | Environment variables and runtime configuration |
| `runbook` | Health checks, error triage, dependencies, config matrix — for on-call |
| `security` | Trust boundaries, data flow, cryptography, attack surface |
| `database` | Tables, relationships, indexes, migrations |
| `onboarding` | Repository tour, request lifecycle, key packages, where to start |
| `migration` | Breaking changes, renamed identifiers, config changes, code examples |
| `adr` | Architectural Decision Records: context, decision, consequences, alternatives |

Scope any document as narrowly or broadly as you like via `--scope` (a glob, e.g. `"internal/store/**"`). Keep it to one real package or subsystem — scoping a `module` document across an entire multi-package tree works, but the wider the scope, the more it costs per run (there's a per-run token ceiling, `constraints.max_tokens_per_run` in `docs.yaml`, to keep a broad scope from running away unbounded).

## 5. Key commands

| Command | Does |
|---|---|
| `gmb doc init <target.md>` | Scaffold a new managed document |
| `gmb doc [--doc ID] [--force]` | Generate/update managed sections |
| `gmb doc check` | CI freshness gate — exits non-zero if any document has fallen too far behind |
| `gmb doc diff [target.md]` | Preview what would change, without writing anything |
| `gmb doc status` | Per-document freshness dashboard |
| `gmb doc export` | Export the current fact base (JSONL / RAG-ready) |
| `gmb docserve` | Watch mode: regenerate on file save |

Every subcommand has a man page (`man gmb-doc-init`, etc.) and `--help`; `docs/cli.md` covers the CLI's general conventions (exit codes, flags, JSON output).

## 6. FAQ

**Does a failed section block the whole run?** No — every other document and section still gets attempted. But the run's exit code reflects it: `gmb doc` exits non-zero if any section failed to render, so CI (or your own `&&`) won't mistake a partially-broken run for a clean one.

**What if the LLM provider is flaky?** Each section retries with backoff (up to ~20s total) before giving up. A section that fails this run keeps whatever content it had from its last successful run — it never gets blanked out.

**Can I review before it writes anything?** Yes — `gmb doc diff` computes what would change without touching disk. It can only compare the grounded reference data (not the prose, which needs a real LLM call to know), so "no change" there means the facts are current, not that the prose has been re-verified word for word.

**Will it touch my hand-written prose?** Only inside the `<!-- gmb:begin:X --> ... <!-- gmb:end:X -->` markers for sections marked `managed: true` in `docs.yaml`. Everything else — including whole documents with `managed: false` sections — is left alone.
