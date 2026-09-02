package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/arch_timeline"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerSnapshotTools binds point-in-time architecture snapshot query and diff tools.
func (s *Server) registerSnapshotTools() {
	// 1. gmb_snapshot_list Tool
	snapshotListTool := mcp.NewTool("gmb_snapshot_list",
		mcp.WithDescription("List captured point-in-time architecture snapshots in the repository."),
		mcp.WithNumber("limit",
			mcp.Description("Maximum snapshots to list (default 20, max 100)"),
		),
	)
	s.RegisterTool(snapshotListTool, s.handleSnapshotListTool)

	// 2. gmb_snapshot_at Tool
	snapshotAtTool := mcp.NewTool("gmb_snapshot_at",
		mcp.WithDescription("Inspect architecture state at a specific snapshot ID, commit hash, or 'HEAD'."),
		mcp.WithString("ref",
			mcp.Required(),
			mcp.Description("Snapshot ID, commit hash, git tag, or 'HEAD'"),
		),
	)
	s.RegisterTool(snapshotAtTool, s.handleSnapshotAtTool)

	// 3. gmb_snapshot_diff Tool
	snapshotDiffTool := mcp.NewTool("gmb_snapshot_diff",
		mcp.WithDescription("Compare architectural state (components, metrics, patterns, smells) between two snapshots or commits."),
		mcp.WithString("base_ref",
			mcp.Required(),
			mcp.Description("Base snapshot ID or commit hash"),
		),
		mcp.WithString("head_ref",
			mcp.Required(),
			mcp.Description("Head snapshot ID, commit hash, or 'HEAD'"),
		),
	)
	s.RegisterTool(snapshotDiffTool, s.handleSnapshotDiffTool)
}

func (s *Server) handleSnapshotListTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	store, err := s.bridge.SnapshotStore()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Snapshot store unavailable: %v", err)), nil
	}

	limit := getIntArg(req, "limit", 20)
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	entries := store.List()

	type snapSummary struct {
		ID           string    `json:"id"`
		CommitHash   string    `json:"commit_hash"`
		Timestamp    time.Time `json:"timestamp"`
		TopologyHash string    `json:"topology_hash"`
		Patterns     int       `json:"patterns"`
		Smells       int       `json:"smells"`
	}

	total := len(entries)
	if len(entries) > limit {
		entries = entries[:limit]
	}

	summaries := make([]snapSummary, 0, len(entries))
	for _, e := range entries {
		summaries = append(summaries, snapSummary{
			ID:           e.SnapshotID,
			CommitHash:   e.CommitHash,
			Timestamp:    e.Timestamp,
			TopologyHash: e.TopologyHash,
			Patterns:     e.PatternCount,
			Smells:       e.SmellCount,
		})
	}

	out, err := json.MarshalIndent(map[string]any{
		"total":     total,
		"count":     len(summaries),
		"snapshots": summaries,
	}, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Serialization error: %v", err)), nil
	}

	return mcp.NewToolResultText(string(out)), nil
}

func (s *Server) handleSnapshotAtTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ref, err := requireStringArg(req, "ref")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	store, err := s.bridge.SnapshotStore()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Snapshot store unavailable: %v", err)), nil
	}

	snap, err := resolveSnapshotRef(store, ref)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	out, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Serialization error: %v", err)), nil
	}

	return mcp.NewToolResultText(string(out)), nil
}

func (s *Server) handleSnapshotDiffTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	baseRef, err := requireStringArg(req, "base_ref")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	headRef, err := requireStringArg(req, "head_ref")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	store, err := s.bridge.SnapshotStore()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Snapshot store unavailable: %v", err)), nil
	}

	base, err := resolveSnapshotRef(store, baseRef)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to resolve base ref %q: %v", baseRef, err)), nil
	}

	head, err := resolveSnapshotRef(store, headRef)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to resolve head ref %q: %v", headRef, err)), nil
	}

	result := arch_timeline.Diff(base, head)

	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Serialization error: %v", err)), nil
	}

	return mcp.NewToolResultText(string(out)), nil
}

func resolveSnapshotRef(store *arch_timeline.SnapshotStore, ref string) (*archmodel.ArchSnapshot, error) {
	if strings.EqualFold(ref, "HEAD") || strings.EqualFold(ref, "latest") {
		return store.Latest()
	}
	if snap, err := store.GetBySnapshotID(ref); err == nil {
		return snap, nil
	}
	if snap, err := store.Get(ref); err == nil {
		return snap, nil
	}
	return nil, fmt.Errorf("snapshot not found for ref %q", ref)
}
