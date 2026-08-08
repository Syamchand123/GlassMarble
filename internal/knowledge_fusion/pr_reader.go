package knowledge_fusion

import (
	"context"
)

type PullRequest struct {
	ID           string
	Title        string
	Description  string
	Author       string
	FilesChanged []string
}

type PRAdapter interface {
	Name() string
	FetchRelatedPRs(ctx context.Context, refs []string) ([]PullRequest, error)
}
