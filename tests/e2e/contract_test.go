package e2e_test

// Contract tests pin the public CLI surface to the documented contracts:
//
//  1. Root help must list every product subcommand and the documented global
//     flags (surfaced through the same Fang/StyledHelp the README shows);
//  2. `gmb version` must print the pinned version string;
//  3. The ontology prefixes (gm:/ext:) must stay stable — the vocabulary
//     invariant guard_test.go depends on them;
//  4. Every product error sentinel (internal/product/errors) must stay
//     unique, non-empty, and must keep Tagged/Annotate classification
//     working through standard producterrs.Is;
//  5. Representative CLI failures must classify as the documented sentinels
//     (validation vs empty-subgraph vs entry-missing) — no string matching.
//
// All commands run IN PROCESS (harness.RunGmb), so nothing here may call
// t.Parallel().

import (
	"errors"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/cmd"
	"github.com/Syamchand123/GlassMarble/internal/akg"
	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
	"github.com/Syamchand123/GlassMarble/internal/product/ont"
	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// TestRootHelpContract pins the public command surface. NOTE: in-process
// runs print cobra's plain help (the Fang styled skin — section labels and
// full Use lines — is applied only by cmd.Execute in the real binary and is
// pinned by TestExitCodeContractViaRealBinary/--help).
func TestRootHelpContract(t *testing.T) {
	sb := harness.NewSandbox(t)
	out := gmbWant(t, sb, []string{"Usage:", "Available Commands:", "Flags:"}, "--help")

	// The rendered help must list every registered top-level command, name
	// and Short description — nothing may be registered but unreachable.
	root := cmd.RootCmdForTesting()
	root.Commands() // materialize the auto-added help command
	if n := len(root.Commands()); n < 20 {
		t.Errorf("root has only %d subcommands, want the full product surface", n)
	}
	for _, c := range root.Commands() {
		if c.Hidden {
			continue
		}
		if !strings.Contains(out, c.Name()) {
			t.Errorf("root help output missing command name %q", c.Name())
		}
		if !strings.Contains(out, c.Short) {
			t.Errorf("root help output missing description of %q: %q", c.Name(), c.Short)
		}
	}

	for _, flag := range []string{
		"--config",
		"--debug",
		"--max-json-mb",
		"--root-dir",
		"--verbose",
	} {
		if !strings.Contains(out, flag) {
			t.Errorf("root help missing flag %q", flag)
		}
	}
	if strings.Contains(out, "--dir") {
		// --dir is the hidden alias the test harness uses; it must not leak
		// into the documented surface.
		t.Errorf("root help unexpectedly exposes --dir")
	}
}

func TestVersionContract(t *testing.T) {
	sb := harness.NewSandbox(t)
	out := gmbWant(t, sb, []string{"v0.1.0", "GlassMarble"}, "version")
	if !strings.HasPrefix(strings.TrimSpace(out), "GlassMarble") {
		t.Errorf("version output should start with the product name:\n%s", out)
	}
}

func TestOntologyPrefixContract(t *testing.T) {
	if ont.PrefixGM != "gm:" {
		t.Errorf("ont.PrefixGM = %q, want %q", ont.PrefixGM, "gm:")
	}
	if ont.PrefixExt != "ext:" {
		t.Errorf("ont.PrefixExt = %q, want %q", ont.PrefixExt, "ext:")
	}
}

func TestErrorSentinelsContract(t *testing.T) {
	sentinels := []error{
		producterrs.ErrValidation,
		producterrs.ErrEmptySubgraph,
		producterrs.ErrEntryMissing,
		producterrs.ErrEntryNotFound,
		producterrs.ErrScopeEmpty,
		producterrs.ErrRenderLimit,
	}

	seen := make(map[string]error, len(sentinels))
	for _, s := range sentinels {
		if s.Error() == "" {
			t.Errorf("sentinel %v has an empty Error() string", s)
		}
		if prev, dup := seen[s.Error()]; dup {
			t.Errorf("sentinel %v and %v share the Error() text %q", prev, s, s.Error())
		}
		seen[s.Error()] = s
	}

	// Tagged: message preserved verbatim, classification intact.
	tagged := producterrs.Tagged("unsupported diagram type 'x'", producterrs.ErrValidation)
	if tagged.Error() != "unsupported diagram type 'x'" {
		t.Errorf("Tagged changed the message: %q", tagged.Error())
	}
	if !errors.Is(tagged, producterrs.ErrValidation) {
		t.Errorf("errors.Is(Tagged(...), ErrValidation) = false")
	}
	if errors.Is(tagged, producterrs.ErrEmptySubgraph) {
		t.Errorf("Tagged messages must not classify as unrelated sentinels")
	}

	// Annotate: inner chain preserved, both classifications reachable.
	inner := errors.New("boom")
	annotated := producterrs.Annotate(inner, producterrs.ErrValidation)
	if !errors.Is(annotated, producterrs.ErrValidation) || !errors.Is(annotated, inner) {
		t.Errorf("errors.Is(Annotate(inner, ErrValidation), ...) broke classification")
	}

	// Classification must survive double wrapping by callers.
	wrapped := errors.Join(producterrs.Annotate(inner, producterrs.ErrEmptySubgraph), errors.New("context"))
	if !errors.Is(wrapped, producterrs.ErrEmptySubgraph) {
		t.Errorf("producterrs.Is through errors.Join lost the sentinel")
	}

	// Nil-safety of the constructors.
	if err := producterrs.Annotate(nil, producterrs.ErrValidation); err != nil {
		t.Errorf("Annotate(nil, kind) = %v, want nil", err)
	}
	if err := producterrs.Tagged("msg", nil); err == nil {
		t.Errorf("Tagged(msg, nil) = nil, want a plain error")
	}
}

func TestCLIFailuresClassifyAsDocumentedSentinels(t *testing.T) {
	cases := []struct {
		name     string
		setup    func(sb *harness.Sandbox)
		args     []string
		wantKind error
		notKind  error
	}{
		{
			name:     "unknown diagram type is a validation error",
			setup:    func(sb *harness.Sandbox) { sb.WriteAKGState(harness.TinyGraph()) },
			args:     []string{"visualize", "nosuchdiagram"},
			wantKind: producterrs.ErrValidation,
			notKind:  producterrs.ErrEntryMissing,
		},
		{
			name:     "sequence diagram without an entry is an entry-missing error",
			setup:    func(sb *harness.Sandbox) { sb.WriteAKGState(harness.TinyGraph()) },
			args:     []string{"visualize", "sequence"},
			wantKind: producterrs.ErrEntryMissing,
			notKind:  producterrs.ErrValidation,
		},
		{
			name:     "export without --output is a validation error",
			setup:    func(sb *harness.Sandbox) { sb.WriteAKGState(harness.TinyGraph()) },
			args:     []string{"export"},
			wantKind: producterrs.ErrValidation,
		},
		{
			name:     "dependency on an empty database is an empty-subgraph error",
			args:     []string{"dependency"},
			wantKind: producterrs.ErrEmptySubgraph,
		},
		{
			name:     "diff with no database is a validation error",
			args:     []string{"diff"},
			wantKind: producterrs.ErrValidation,
		},
		{
			name:     "ai without a question is a validation error",
			args:     []string{"ai"},
			wantKind: producterrs.ErrValidation,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sb := harness.NewSandbox(t)
			if tc.setup != nil {
				tc.setup(sb)
			}
			out, err := gmbErr(t, sb, tc.args...)
			if err == nil {
				t.Fatalf("gmb %v unexpectedly succeeded\n--- output ---\n%s", tc.args, out)
			}
			if !errors.Is(err, tc.wantKind) {
				t.Errorf("gmb %v error %v does not classify as %v", tc.args, err, tc.wantKind)
			}
			if tc.notKind != nil && errors.Is(err, tc.notKind) {
				t.Errorf("gmb %v error %v misclassifies as %v", tc.args, err, tc.notKind)
			}
		})
	}

	// The sentinels must also remain distinguishable CLI-side with real data:
	// a valid visualize run must succeed while the failure modes above all
	// fail — nothing in the documented taxonomy collapses. TinyGraph's
	// EXECUTABLE nodes match no dependency-diagram kind, so a STRUCT/FILE
	// state is used here (same shape a real analysis persists).
	sb := harness.NewSandbox(t)
	sb.WriteAKGState(dependencyState())
	gmb(t, sb, "visualize", "dependency")
}

// dependencyState seeds a two-node graph whose node kinds match the
// DEPENDENCY_GRAPH extraction filter (STRUCT / FILE / MODULE / ... — bare
// uppercase, exactly as a real analysis writes them). TinyGraph's
// EXECUTABLE nodes are deliberately excluded from that filter, so it would
// produce an empty subgraph for `visualize dependency`.
func dependencyState() *akg.GraphJSON {
	return &akg.GraphJSON{
		SchemaVersion: akg.CurrentSchemaVersion,
		Version:       1,
		CommitHash:    "abcdef1234567890",
		Nodes: []akg.GraphNodeJSON{
			{ID: "internal/cache/cache.go", Kind: "FILE", Name: "cache", FileSpec: akg.LocationMetaJSON{Path: "internal/cache/cache.go"}},
			{ID: "internal/order/order.go", Kind: "FILE", Name: "order", FileSpec: akg.LocationMetaJSON{Path: "internal/order/order.go"}},
		},
		Edges: []akg.GraphEdgeJSON{
			{SourceID: "internal/order/order.go", TargetID: "internal/cache/cache.go", Type: "gm:dependsOn"},
		},
	}
}
