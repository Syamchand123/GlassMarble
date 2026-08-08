package commit_reasoning

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

func TestResolveImpact(t *testing.T) {
	snap := &archmodel.ArchSnapshot{
		Components: []archmodel.DetectedComponent{
			{
				ID:          "auth",
				Name:        "auth-service",
				Directories: []string{"internal/auth"},
				NodeIDs:     []string{"node:auth:login", "node:auth:jwt"},
			},
			{
				ID:          "pay",
				Name:        "payment",
				Directories: []string{"internal/payment/v1"},
			},
			{
				ID:      "billing",
				Name:    "billing",
				NodeIDs: []string{"node:billing:invoice"},
			},
		},
	}
	tests := []struct {
		name  string
		files []string
		want  []string
	}{
		{"exact directory", []string{"internal/auth/login.go"}, []string{"auth-service"}},
		{"nested directory", []string{"internal/payment/v1/handlers/h.go"}, []string{"payment"}},
		{"no directory but node id", []string{"node:billing:invoice"}, []string{"billing"}},
		{"token heuristic fallback", []string{"internal/auth/v1/jwt.go"}, []string{"auth-service"}},
		{"unknown file", []string{"cmd/tool/main.go"}, nil},
		{"nil snapshot", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s *archmodel.ArchSnapshot
			if tt.files != nil {
				s = snap
			}
			got := ResolveImpact(tt.files, s)
			assertStrings(t, "impact", got, tt.want)
		})
	}
}

func TestResolveImpact_EmptyFiles(t *testing.T) {
	if got := ResolveImpact(nil, &archmodel.ArchSnapshot{}); got != nil {
		t.Errorf("empty files must yield nil, got %v", got)
	}
}
