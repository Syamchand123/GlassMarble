package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fsnotify/fsnotify"
)

// TestWatchEventRelevant verifies the fsnotify event filter ignores
// bookkeeping paths and keeps source changes.
func TestWatchEventRelevant(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"src/main.go", true},
		{".glassmarble/akg.json", false},
		{"src/.git/index.lock", false},
		{"node_modules/pkg/index.js", false},
		{"vendor/foo.go", false},
		{"src/main_test.go", true},
		{"C:/repo/dist/bundle.js", false},
		{"C:/repo/build/app.js", false},
	}
	for _, tc := range cases {
		ev := fsnotify.Event{Name: tc.path, Op: fsnotify.Write}
		if got := watchEventRelevant(nil, ev); got != tc.want {
			t.Errorf("watchEventRelevant(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// TestWatchTreeSkipsTooling verifies watchTree registers watches while pruning
// common tooling directories.
func TestWatchTreeSkipsTooling(t *testing.T) {
	root := t.TempDir()
	for _, sub := range []string{"src", "node_modules", ".git", ".glassmarble", "vendor"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Close()

	if err := watchTree(watcher, root); err != nil {
		t.Fatal(err)
	}

	found := false
	for _, p := range watcher.WatchList() {
		if p == root {
			found = true
		}
	}
	if !found {
		t.Errorf("root not registered in watcher: %v", watcher.WatchList())
	}
}
