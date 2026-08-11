<!--
================================================================================
  FINDINGS.md — GlassMarble QA Battle-Test Findings Log
================================================================================

  WHAT THIS FILE IS FOR
  --------------------
  This is the SINGLE source of truth for every flaw, gap, logical bug, quality
  issue, and false-alarm surfaced by the battle-test suite under tests/.
  After each `go test ./tests/...` run, every failure, flake, warning, and
  suspicious-but-green behavior discovered by the tests must be transcribed
  here, in the format defined below, BEFORE any code change is made.

  WORKFLOW (follow in order, per run)
  ----------------------------------
  1. RUN      : `go test ./tests/...` (+ `-race`, `-count=2`, `-run <focus>`)
  2. TRIAGE   : classify each failure — REAL BUG vs TEST BUG vs FLAKE vs
                PRE-EXISTING vs UNVERIFIED. See the "Severity & Status" table.
  3. LOG      : append one Finding entry (schema below). NEVER edit or delete
                an existing entry — append only. Fixing a bug marks the entry
                status, it does not remove it (audit trail).
  4. FIX      : only after the finding is logged. Fix the product code first,
                then the test (if the test itself was wrong).
  5. VERIFY   : re-run the affected package, confirm green, set status=RESOLVED,
                capture the re-run evidence.
  6. REVIEW   : every finding must be reviewed by a human before the status is
                flipped to RESOLVED — auto-flipping is forbidden.

  CONTRIBUTORS
  ------------
  The suite is authored by multiple sub-agents. Attribution lives in each test
  file header. The TRIAGE column must record WHO classified the finding and
  WHETHER the evidence was independently reproduced.

  FILING RULES (hard requirements)
  --------------------------------
  - File ONE entry per distinct issue. Never merge unrelated symptoms.
  - File the entry in the same session that discovered it; never "later".
  - Quote the EXACT failing assertion / panic / error text (no paraphrase).
  - Record the FULL command line that reproduces (or will reproduce) it.
  - State the environment (OS, Go version, git state, machine) exactly as
    tested. A finding without an environment header is invalid.
  - Before filing, CHECK FOR DUPLICATES: same symptom + same root cause +
    same location = duplicate. Reference the existing ID and close.
  - Every entry carries an ID from the ID NAMESPACE scheme below. Never reuse.
  - Never delete, overwrite, renumber, or rewrite history of an entry; append
    a VERIFY line to the same entry instead.
  - Mark the entry UNVERIFIED if the test could not be executed yet — never
    leave a guessed severity on an unrun test.

  ID NAMESPACE SCHEME
  -------------------
  IDs are monotonically increasing, four-digit, zero-padded, prefixed by the
  suite that discovered the finding. They are NEVER reused:

      E2E-0001    e2e suite (tests/e2e)             user flows, CLI contracts
      STG-0001    stages suite (tests/stages)       stages 1-12 pipeline logic
      NFL-0001    nonfunctional (tests/nonfunctional)  perf, determinism,
                                                      concurrency, resilience,
                                                      fallback
      EDG-0001    edgecases (tests/edgecases)       empty repo, no-git, config,
                                                      input, non-TTY
      QA-0001     qa suite (tests/qa)               golden output, JSON schema,
                                                      contract, exit codes
      INF-0001    infrastructure findings           harness bugs, fixture bugs,
                                                      missing test coverage

  FINDING ENTRY SCHEMA (copy this block verbatim per entry)
  --------------------------------------------------------
  ------------------------------------------------------------------------
  FINDING:  <ID>
  ------------------------------------------------------------------------
  TITLE:    <One-line, imperative summary. e.g. "analyze crashes on file
             whose name is only a dot ('.')">
  TARGET:   <Product area: command, stage, package, artifact. e.g.
             "cmd/analyze.go / stage2_normalize / akg.json writer">
  TEST:     <Owning test function + file, e.g. "TestStage2DotFile
             (tests/stages/stage2_normalize_test.go)">

  ------------------------------------------------------------------------
  EVIDENCE
  ------------------------------------------------------------------------
  COMMAND:    <Exact reproduction command(s)>
  EXPECTED:   <What the test contract says should happen, quote the assertion>
  ACTUAL:     <What actually happened — quote the FULL error/panic/output.
               No paraphrase, no truncation of the error string>
  LOGS:       <Panic tracebacks, stderr excerpts, or pointer to artifact
               paths. NULL if none>
  ENVIRONMENT:
    OS:            <e.g. Windows 11 / win32 amd64>
    GO VERSION:    <e.g. go1.26.4>
    GIT STATE:     <branch + dirty/clean + last commit SHA>
    TEST MODE:     <go test | go test -race | go test -count=2 | go vet>
    LLM BACKEND:   <mock | real (provider/model) — AI tests MUST state this>

  ------------------------------------------------------------------------
  TRIAGE
  ------------------------------------------------------------------------
  CLASSIFICATION:  <REAL BUG | TEST BUG | FLAKE | PRE-EXISTING |
                    UNVERIFIED | FALSE ALARM>
  SEVERITY:        <CRITICAL | HIGH | MEDIUM | LOW | INFO>
  IMPACT:          <What user-visible behavior breaks, and who hits it.
                    "none" only if INFO>
  ROOT CAUSE:      <Guaranteed or best hypothesis, with code reference
                    (package/file:line). NULL if unknown — say "UNKNOWN">
  RELATED:         <IDs of duplicates/companions, or NULL>

  ------------------------------------------------------------------------
  LIFECYCLE
  ------------------------------------------------------------------------
  FILED BY:     <agent name / "human" / CI>
  FILED AT:     <YYYY-MM-DD HH:MM UTC + test-run identifier>
  STATUS:       <NEW | CONFIRMED | INVESTIGATING | FIX IN PROGRESS |
                 FIXED-UNVERIFIED | RESOLVED | WILL NOT FIX | CANNOT
                 REPRODUCE | DUPLICATE OF <ID>>
  VERIFY:       <one line per re-run: date, command, result, evidence
                 hash/path. Appended, never overwritten>
  REVIEWED:     <human sign-off: date + name — REQUIRED before RESOLVED>

  ------------------------------------------------------------------------

  SEVERITY & STATUS DEFINITIONS (authoritative)
  ---------------------------------------------
  SEVERITY
    CRITICAL   Data loss, crashes, silent corruption of persisted artifacts,
               wrong answers presented as fact, security exposure.
    HIGH       Core feature broken for a documented use case; wrong exit
               codes/exit-code contracts; schema drift on disk artifacts.
    MEDIUM     Edge case misbehavior, degraded UX, wrong optional output,
               non-contract behavior.
    LOW        Cosmetic, stylistic, wording, minor perf, dead code surfaced
               by tests.
    INFO       Observation, not a defect; documented behavior worth pinning.

  CLASSIFICATION
    REAL BUG      Product code violates its own contract; reproduced.
    TEST BUG      The test asserts the wrong contract; product is right.
                  (Fix the test, log BOTH the test bug and — if any — the
                  underlying product gap it was hunting for.)
    FLAKE         Intermittent; passes on re-run without changes. Log it
                  (5 failures = 1 suite-blocking flake problem).
    PRE-EXISTING  Fails on a clean checkout before this suite ran; not
                  introduced by the test suite.
    UNVERIFIED    Test written but not yet executed (or executed in a
                  different environment). MUST be marked so — do not
                  guess results.
    FALSE ALARM   Investigation proved the test wrong or the behavior
                  intended; closed with evidence.

  STATUS
    NEW                Filed, not yet triaged in depth.
    CONFIRMED          Independently reproduced (state who/how).
    INVESTIGATING      Root cause hunt in progress.
    FIX IN PROGRESS    Owner assigned, code being changed.
    FIXED-UNVERIFIED   Fix committed but re-run evidence pending.
    RESOLVED           Fix verified by re-run + human review.
    WILL NOT FIX       Triaged out; rationale recorded.
    CANNOT REPRODUCE   Two independent attempts failed; evidence logged.
    DUPLICATE OF <ID>  Points to the canonical entry.

  ------------------------------------------------------------------------
  KNOWN FLAW SURFACES (pre-flagged suspects the suite was built to probe)
  ------------------------------------------------------------------------
  These are NOT findings — they are documented suspicions that tests will
  either confirm or clear. If a test confirms one, file a finding with the
  listed ID bucket and mark the suspicion CONFIRMED here:
    1. `claims.jsonl` is believed to never be written by the pipeline
       (claims live in the memory.json aggregate).          -> QA bucket
    2. `gmb visualize dependency` requires UPPERCASE kinds (STRUCT/FILE);
       lowercase or EXECUTABLE-only graphs may yield empty subgraphs.
                                                             -> E2E bucket
    3. `gmb completion bogus` is believed to exit 0 (should be non-zero).
                                                             -> E2E bucket
    4. `gmb analyze` on an empty repo is believed to exit 0. -> E2E bucket
    5. `gmb version` prints "v0.1.0" (v prefix) — pin exact banner.
                                                             -> QA bucket
    6. drift.Analyze signature was corrected to take config.DriftConfig
       directly; tests must match the REAL signature (drift.go:87).
                                                             -> STG bucket
    7. Empty-graph serialization bug (nodes:null) was FIXED in
       internal/akg/graph_json.go:149; regression guard shipped in
       tests/qa/json_schema_test.go. Re-verify on every run. -> QA bucket

  ------------------------------------------------------------------------
  CURRENT SNAPSHOT (fill after every run; keep the latest entry on top)
  ------------------------------------------------------------------------
  LAST RUN:        <not run yet>
  LAST RUN MODE:   <go test ./tests/... | -race | -count=2 | package scope>
  RESULT:          <PASS | FAIL | NOT RUN — list failing packages>
  OPEN FINDINGS:   <count>
  RESOLVED:        <count>
  NEW SINCE LAST:  <count>
  NOTES:           <one-liner on anomalies, flakes observed, coverage gaps>

================================================================================
-->
