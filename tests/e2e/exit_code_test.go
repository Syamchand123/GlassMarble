package e2e_test

// TestExitCodeContractViaRealBinary drives the REAL compiled gmb binary
// (separate process, real main.go exit handling) and pins the documented
// exit-code contract from docs/commands_master_reference.md §12:
//
//   - every successful command exits 0;
//   - every failing command exits non-zero (main.go maps the producterrs
//     taxonomy to distinct codes: 1 validation, 2 entry missing/not found,
//     3 empty subgraph, 4 render limit — everything else 1);
//   - the 0-vs-nonzero split of each documented case must match reality, so
//     any behavioural regression flips these assertions.
//
// One documented case intentionally does NOT match the docs and is asserted
// by its real behaviour (see the inline comment): `gmb analyze` on an empty
// repository — §12 says analyze exits non-zero only when a phase fails or the
// commit is rejected, and an empty repository commits a healthy empty graph.

import (
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/tests/harness"
)

func TestExitCodeContractViaRealBinary(t *testing.T) {
	bin := harness.BuildBinary(t)

	tests := []struct {
		name    string
		setup   func(sb *harness.Sandbox)
		args    []string
		wantErr bool // false = must exit 0; true = must exit non-zero
		wantOut []string
	}{
		{
			name:    "version exits 0",
			args:    []string{"version"},
			wantOut: []string{"v0.1.0"},
		},
		{
			// The styled Fang help surface (the one shipped in the README) is
			// only reachable through the real binary's cmd.Execute, so this is
			// the only place that contracts the styled labels and the full
			// Use-line command list.
			name: "--help exits 0 and renders the styled surface",
			args: []string{"--help"},
			wantOut: []string{
				"COMMANDS", "USAGE", "FLAGS",
				"ai [command] [question] [--flags]",
				"analyze [--flags]",
				"compare [base_graph.json] [head_graph.json] [--flags]",
				"completion [bash|zsh|fish|powershell]",
				"dependency [target_file_or_symbol] [--flags]",
				"diff [--flags]",
				"doctor [--flags]",
				"drift [--flags]",
				"export [--flags]",
				"help [command]",
				"hooks [install|uninstall] [--flags]",
				"hotspot [--flags]",
				"housekeeping [--flags]",
				"import [graph.json] [--flags]",
				"init [--flags]",
				"inspect [node_id] [--flags]",
				"memory [query] [--flags]",
				"patterns [--flags]",
				"snapshot [--flags]",
				"stats [--flags]",
				"status [--flags]",
				"timeline [--flags]",
				"tree [--flags]",
				"version",
				"visualize [diagram_type] [--flags]",
				"watch [--flags]",
				"why [question]",
			},
		},
		{
			name: "status on uninitialized sandbox exits 0",
			args: []string{"status"},
		},
		{
			name:  "doctor on healthy state exits 0",
			setup: func(sb *harness.Sandbox) { sb.WriteAKGState(harness.TinyGraph()) },
			args:  []string{"doctor"},
		},
		{
			name: "doctor on corrupt state exits non-zero",
			setup: func(sb *harness.Sandbox) {
				sb.WriteFile(".glassmarble/akg.json", "{not valid json")
			},
			args:    []string{"doctor"},
			wantErr: true,
			wantOut: []string{"FAILED"},
		},
		{
			name:  "visualize dependency on valid state exits 0",
			setup: func(sb *harness.Sandbox) { sb.WriteAKGState(dependencyState()) },
			args:  []string{"visualize", "dependency"},
		},
		{
			name:    "visualize unknown diagram exits non-zero",
			setup:   func(sb *harness.Sandbox) { sb.WriteAKGState(harness.TinyGraph()) },
			args:    []string{"visualize", "nosuchdiagram"},
			wantErr: true,
		},
		{
			name:    "visualize sequence without --entry exits non-zero",
			setup:   func(sb *harness.Sandbox) { sb.WriteAKGState(harness.TinyGraph()) },
			args:    []string{"visualize", "sequence"},
			wantErr: true,
		},
		{
			name:    "export without --output exits non-zero",
			setup:   func(sb *harness.Sandbox) { sb.WriteAKGState(harness.TinyGraph()) },
			args:    []string{"export"},
			wantErr: true,
		},
		{
			name:    "import of a missing file exits non-zero",
			args:    []string{"import", "does-not-exist.json"},
			wantErr: true,
		},
		{
			name:    "why without any AI configuration exits non-zero",
			args:    []string{"why", "anything"},
			wantErr: true,
		},
		{
			// §12: `completion` exits non-zero when the shell is unknown.
			// The unknown-shell path returns a validation error (exit 1).
			name:    "completion bogus exits non-zero (unknown shell)",
			args:    []string{"completion", "bogus"},
			wantErr: true,
		},
		{
			// §12: `analyze` exits non-zero only when a phase fails or the
			// commit is rejected. An empty repository commits a healthy
			// empty graph, so exit 0 is the documented behaviour here.
			name: "analyze on empty repository exits 0 (healthy empty commit)",
			args: []string{"analyze"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sb := harness.NewSandbox(t)
			if tc.setup != nil {
				tc.setup(sb)
			}
			stdout, stderr, code := harness.RunBinary(t, bin, sb.Root, nil, tc.args...)
			all := stdout + stderr
			for _, w := range tc.wantOut {
				if !strings.Contains(all, w) {
					t.Errorf("gmb %v output missing %q\n--- output ---\n%s", tc.args, w, all)
				}
			}
			if tc.wantErr {
				if code == 0 {
					t.Errorf("gmb %v: expected non-zero exit code, got 0\n--- output ---\n%s", tc.args, all)
				}
				return
			}
			if code != 0 {
				t.Errorf("gmb %v: expected exit code 0, got %d\n--- stderr ---\n%s", tc.args, code, stderr)
			}
		})
	}
}
