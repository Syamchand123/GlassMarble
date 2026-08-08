package commit_reasoning

import (
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/git"
)

func TestClusterCommits(t *testing.T) {
	t0 := time.Date(2026, 1, 15, 9, 0, 0, 0, time.UTC)
	hour := time.Hour
	meta := func(name string, ts time.Time, files []string) *git.CommitMeta {
		return &git.CommitMeta{Hash: name, Timestamp: ts, Subject: name, Files: files}
	}

	// A PR: several commits, close in time, sharing the "payment" subject
	// token (even though c3 touches a different file).
	pr := []*git.CommitMeta{
		meta("add payment service", t0, []string{"internal/pay/pay.go"}),
		meta("fix payment model", t0.Add(hour), []string{"internal/pay/pay.go", "internal/pay/model.go"}),
		meta("wire payment handler", t0.Add(2*hour), []string{"internal/pay/handler.go"}),
	}
	// A lone unrelated commit an hour later.
	lone := meta("auth login screen", t0.Add(3*hour), []string{"internal/auth/login.go"})

	clusters := ClusterCommits(append(pr, lone))
	if len(clusters) != 2 {
		t.Fatalf("want 2 clusters, got %d", len(clusters))
	}
	if len(clusters[0].Commits) != 3 {
		t.Errorf("cluster 0 has %d commits, want 3", len(clusters[0].Commits))
	}
	if len(clusters[1].Commits) != 1 {
		t.Errorf("cluster 1 has %d commits, want 1 (singletons are clusters)", len(clusters[1].Commits))
	}
	// Order must stay oldest-first.
	for i := 1; i < len(clusters[0].Commits); i++ {
		if clusters[0].Commits[i].Timestamp.Before(clusters[0].Commits[i-1].Timestamp) {
			t.Error("cluster commits must be ordered oldest first")
		}
	}
}

func TestClusterCommits_GapSplitsCluster(t *testing.T) {
	t0 := time.Date(2026, 1, 15, 9, 0, 0, 0, time.UTC)
	// Same file, but 48h apart — a different working session.
	metas := []*git.CommitMeta{
		{Hash: "c1", Timestamp: t0, Subject: "add payment", Files: []string{"pay.go"}},
		{Hash: "c2", Timestamp: t0.Add(48 * time.Hour), Subject: "fix payment", Files: []string{"pay.go"}},
	}
	clusters := ClusterCommits(metas)
	if len(clusters) != 2 {
		t.Fatalf("48h gap must split clusters, got %d", len(clusters))
	}
}

func TestClusterCommits_SharedSubjectTokenJoins(t *testing.T) {
	t0 := time.Date(2026, 1, 15, 9, 0, 0, 0, time.UTC)
	metas := []*git.CommitMeta{
		{Hash: "c1", Timestamp: t0, Subject: "add billing", Files: []string{"a.go"}},
		{Hash: "c2", Timestamp: t0.Add(time.Hour), Subject: "fix billing", Files: []string{"b.go"}},
		{Hash: "c3", Timestamp: t0.Add(2 * time.Hour), Subject: "wire billing callbacks", Files: []string{"c.go"}},
	}
	clusters := ClusterCommits(metas)
	if len(clusters) != 1 {
		t.Fatalf("shared subject token must join the cluster, got %d", len(clusters))
	}
}

func TestClusterCommits_Deterministic(t *testing.T) {
	t0 := time.Date(2026, 1, 15, 9, 0, 0, 0, time.UTC)
	metas := []*git.CommitMeta{
		{Hash: "c1", Timestamp: t0, Subject: "add billing", Files: []string{"b.go"}},
		{Hash: "c2", Timestamp: t0.Add(time.Hour), Subject: "fix billing", Files: []string{"b.go"}},
		{Hash: "c3", Timestamp: t0.Add(2 * time.Hour), Subject: "add payments", Files: []string{"p.go"}},
	}
	first := ClusterCommits(metas)
	for i := 0; i < 10; i++ {
		got := ClusterCommits(metas)
		if len(got) != len(first) {
			t.Fatalf("run %d: %d clusters, want %d", i, len(got), len(first))
		}
		for j := range got {
			if len(got[j].Commits) != len(first[j].Commits) {
				t.Fatalf("run %d cluster %d size: %d vs %d", i, j, len(got[j].Commits), len(first[j].Commits))
			}
			if len(got[j].Files) != len(first[j].Files) {
				t.Fatalf("run %d cluster %d files: %v vs %v", i, j, got[j].Files, first[j].Files)
			}
		}
	}
}

func TestClusterCommits_Empty(t *testing.T) {
	if got := ClusterCommits(nil); got != nil {
		t.Errorf("nil input must yield nil, got %v", got)
	}
}
