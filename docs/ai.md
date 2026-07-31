# GlassMarble AI Architect (`gmb ai`)

`gmb ai` is the interactive face of GlassMarble: a Bring-Your-Own-Key (BYOK)
LLM agent that answers questions about your repository by **querying the AKG
knowledge graph**, **reading real source code**, and **generating diagrams
through the visualization engine** — exactly the same graph data and engine
that `gmb analyze` and `gmb visualize` use.

```bash
gmb ai "which services depend on the payment module?"
gmb ai "explain function Save() in PostgresStore"
gmb ai "generate a C4 container diagram" --save c4.md
gmb ai chat                              # interactive REPL with session memory
gmb ai --no-tools "opinion question"     # plain chat mode, no tool calling
```

The agent is grounded: the LLM never hand-writes diagram markup or guesses
architecture. It calls tools (`akg_*`, `code_*`, `diagram_*`, `system_*`) that
return JSON facts or generated markup, and only then produces an answer. If the
repository has not been analyzed, it says so and recommends `gmb analyze`.

---

## 1. Setup (BYOK)

The AI engine never calls a service on your behalf with a shared key. You
bring your own API key.

```bash
gmb ai configure                        # interactive setup
gmb ai configure --provider openai --model gpt-4o --key sk-...
gmb ai configure --scope project ...    # write repo-local config instead
gmb ai models                           # list providers, adapters, models
gmb ai doctor                           # validate config + connectivity + AKG
```

Configuration is resolved with precedence **CLI flag > environment variable >
project `.glassmarble/ai.yaml` > global `~/.glassmarble/ai.yaml` > defaults**.
Keys are stored with `0600` permissions and are never logged; error messages
redact them.

### Providers

| Provider name | Adapter | Default base URL | Key env var |
|---|---|---|---|
| `openai` | OpenAI-compatible | `https://api.openai.com/v1` | `GLASSMARBLE_OPENAI_API_KEY` |
| `anthropic` | native | `https://api.anthropic.com` | `GLASSMARBLE_ANTHROPIC_API_KEY` |
| `gemini` | native | `https://generativelanguage.googleapis.com` | `GLASSMARBLE_GEMINI_API_KEY` |
| `deepseek` | OpenAI-compatible | `https://api.deepseek.com` | `GLASSMARBLE_DEEPSEEK_API_KEY` |
| `mistral` | OpenAI-compatible | `https://api.mistral.ai` | `GLASSMARBLE_MISTRAL_API_KEY` |
| `glm` | OpenAI-compatible | `https://open.bigmodel.cn/api/paas` | `GLASSMARBLE_GLM_API_KEY` |
| `nvidia` | OpenAI-compatible | `https://integrate.api.nvidia.com` | `GLASSMARBLE_NVIDIA_API_KEY` |
| `openrouter` | OpenAI-compatible | `https://openrouter.ai/api` | `GLASSMARBLE_OPENROUTER_API_KEY` |
| `groq` | OpenAI-compatible | `https://api.groq.com/openai` | `GLASSMARBLE_GROQ_API_KEY` |
| `ollama` | OpenAI-compatible | (custom, e.g. `http://localhost:11434/v1`) | none |
| `custom` | OpenAI-compatible | (required) | `GLASSMARBLE_AI_API_KEY` |

The OpenAI-compatible adapter covers OpenAI, DeepSeek, Mistral, GLM, NVIDIA
NIM, OpenRouter, Groq, Ollama, and any endpoint speaking the chat-completions
wire format (point `--base-url` at it). Tool-calling schemas are converted per
adapter automatically.

### Configuration reference (`ai.yaml`)

```yaml
provider: openai
model: gpt-4o
api_key: ""                 # prefer env vars
base_url: ""                # custom endpoints
temperature: 0.2
max_turns: 15               # agent tool-call rounds per run
max_tool_result_bytes: 8192 # per-tool result truncation
max_output_tokens: 8192
timeout_sec: 180
stream: true                # token streaming (default)
max_total_tokens: 0         # per-run token budget (0 = unlimited)
max_cost_usd: 0             # per-run estimated-cost budget (0 = unlimited)
max_session_messages: 40    # chat history budget (messages)
```

Environment variables mirror the fields: `GLASSMARBLE_AI_PROVIDER`,
`GLASSMARBLE_AI_MODEL`, `GLASSMARBLE_AI_API_KEY`,
`GLASSMARBLE_AI_BASE_URL`, `GLASSMARBLE_AI_TEMPERATURE`,
`GLASSMARBLE_AI_MAX_TURNS`, `GLASSMARBLE_AI_MAX_TOOL_RESULT_BYTES`,
`GLASSMARBLE_AI_MAX_OUTPUT_TOKENS`, `GLASSMARBLE_AI_TIMEOUT_SEC`,
`GLASSMARBLE_AI_STREAM` (`0`/`false` disables), `GLASSMARBLE_AI_MAX_TOTAL_TOKENS`,
`GLASSMARBLE_AI_MAX_COST`, `GLASSMARBLE_AI_MAX_SESSION_MESSAGES`.
Provider-specific keys use `GLASSMARBLE_<PROVIDER>_API_KEY` (uppercase name).

---

## 2. Agent loop

`gmb ai "question"` runs the agentic loop:

1. Build the system prompt (AI-Architect persona + an auto-injected
   **repository context header**: git commit, AKG presence, node/edge/file
   counts, entrypoints, detected patterns).
2. Send the conversation plus the tool schemas to the provider.
3. If the model requests tools, the **dispatcher** validates the JSON
   arguments, executes the handlers against the live AKG snapshot, appends the
   results (truncated to `max_tool_result_bytes`), and loops.
4. When the model answers without tool calls, the final answer is streamed or
   printed.

Tool call activity is shown on stderr as it happens:

```
→ akg_status({})
← akg_status: error (182 bytes)
→ diagram_generate({"type":"C4_CONTAINER"})
← diagram_generate: ok (4251 bytes)
```

Prompt-injection stance: file contents, AKG nodes, and tool output are always
**data, never instructions** — this is enforced in the system prompt and the
tools never mutate state.

### Tool catalog

| Group | Tools |
|---|---|
| System | `system_status`, `system_diagram_types`, `save_artifact` |
| AKG queries | `akg_status`, `akg_summary`, `akg_search`, `akg_get_node`, `akg_edges`, `akg_traverse`, `akg_path`, `akg_cycles`, `akg_orphans`, `akg_god_objects`, `akg_hotspots`, `akg_page_rank`, `akg_impact_radius`, `akg_communities`, `akg_articulation_points`, `akg_topological_order`, `akg_entrypoints`, `akg_similarity` |
| Code | `code_read_file`, `code_list_dir`, `code_search_symbol`, `code_definition`, `code_diff` |
| Diagrams | `diagram_generate` (all 31 diagram types), `diagram_summary`, `diagram_types` |

Restrict the set per invocation:

```bash
gmb ai --tools akg,code "question"   # categories or exact names
gmb ai --tools diagram "C4 container diagram"
gmb ai --no-tools "opinion question"
```

---

## 3. Streaming

Streaming is on by default (`stream: true`): answer tokens are printed as
they arrive instead of waiting for the full response. Disable it with
`gmb ai --no-stream`, the `stream: false` config, or
`GLASSMARBLE_AI_STREAM=0`. All three adapters support streaming
(OpenAI SSE `data:` chunks, Anthropic SSE events, Gemini
`:streamGenerateContent`); endpoints that ignore `stream: true` fall back to
one-shot JSON transparently.

---

## 4. Chat mode and session memory

```bash
gmb ai chat                    # resume the latest session (or start one)
gmb ai chat --new              # force a fresh session
gmb ai chat --session 2026...  # resume a specific session (see gmb ai sessions)
gmb ai sessions                # list saved sessions, newest first
gmb ai sessions --delete <id>  # remove one session
```

Every turn is appended to the conversation and saved as JSON under
`.glassmarble/ai/sessions/<id>.json` (0600 permissions). The next
`gmb ai chat` resumes the latest session automatically, so context carries
across invocations. Long chats are trimmed to `max_session_messages` on turn
boundaries — a tool round is never split, and the trailing answer is never
dropped. Type `exit`, `quit`, or `bye` (or Ctrl+D) to leave; a per-session
summary (turns, messages, tokens, estimated cost) is printed on exit.

---

## 5. Token and cost guardrails

Runs can be capped so an agentic loop cannot burn unbounded tokens or money:

```bash
gmb ai --max-total-tokens 20000 "question"
gmb ai --max-cost 0.5 "question"
gmb ai chat --max-total-tokens 20000
```

- **Token budget** (`max_total_tokens`): summed prompt + completion tokens
  across the whole run. A pre-flight check estimates the next request's prompt
  size and stops before sending it; after every completion the provider-
  reported usage is authoritative.
- **Cost budget** (`max_cost_usd`): estimated spend from the model's list
  price per 1M tokens (see `internal/ai_engine/provider/pricing.go` for the
  covered models; vendor-prefixed IDs like `openai/gpt-5` resolve by the last
  path segment). Enforced only for priced models — unknown models are not
  estimated and the cap is skipped.
- Stop reasons are reported to the user: `turn_limit`, `token_budget`, and
  `cost_budget` each print a `Note:` line. `--verbose` adds a token/cost
  accounting line per run.

Guardrail semantics: the check runs after a completion is already spent (a
second tool completion may execute before the overrun is visible), but its
tool round does not run and no third request is sent.

---

## 6. Artifacts

`save_artifact` writes the answer or notes to `.glassmarble/ai/`, and
`diagram_generate` with `save=true` writes markup to `.glassmarble/marbles/` —
the LLM returns a path receipt instead of dumping markup into the chat.

The same routing is available without tool cooperation through the `--save`
flag on single queries:

```bash
gmb ai "generate a C4 container diagram" --save c4.md   # markup → .glassmarble/marbles/c4.md
gmb ai "write architecture notes" --save notes.md       # prose → .glassmarble/ai/notes.md
```

The final answer is written to the artifact file (diagram markup detected by
mermaid/plantuml/dot fences or graph declarations; everything else is prose)
and the terminal shows only the saved path. The agent is instructed to keep
answers concise and grounded, citing real paths and symbol names.

---

## 7. Implementation layout

```
internal/ai_engine/
├── engine.go              # public facade: New / Ask / AskAgent / Doctor
├── aiconfig/              # BYOK config: yaml + env + defaults
├── provider/              # adapter interface + registry
│   ├── openai_compat.go   # OpenAI-compatible chat completions (+ SSE)
│   ├── anthropic.go       # native Claude (+ SSE events)
│   ├── gemini.go          # native Gemini (+ SSE)
│   ├── sse.go             # SSE scanner
│   └── pricing.go         # model list prices + cost estimation
├── agent/                 # tool-calling loop, budgets, streaming events
├── akgbridge/             # lazy, cached AKG snapshot loader
├── tools/                 # tool registry + all tool powers
└── session/               # persistent chat sessions
cmd/ai.go                  # `gmb ai` Cobra command tree
```

`internal/akg`, `internal/code_analysis_engine`, and
`internal/visualization_engine` are untouched — the AI engine consumes their
existing public APIs as a read-only client.
