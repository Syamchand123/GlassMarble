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
	if s.shouldRegister("gmb_snapshot_list", "snapshot") {
		snapshotListTool := mcp.NewTool("gmb_snapshot_list",
			mcp.WithDescription("List captured point-in-time architecture snapshots in the repository."),
			mcp.WithNumber("limit",
				mcp.Description("Maximum snapshots to list (default 20, max 100)"),
			),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:           "gmb_snapshot_list",
				ReadOnlyHint:    mcp.ToBoolPtr(true),
				DestructiveHint: mcp.ToBoolPtr(false),
				IdempotentHint:  mcp.ToBoolPtr(true),
				OpenWorldHint:   mcp.ToBoolPtr(false),
			}),
		)
		s.RegisterTool(snapshotListTool, s.handleSnapshotListTool)
	}
	if s.shouldRegister("gmb_snapshot_at", "snapshot") {
		snapshotAtTool := mcp.NewTool("gmb_snapshot_at",
			mcp.WithDescription("Inspect architecture state at a specific snapshot ID, commit hash, or 'HEAD'."),
			mcp.WithString("ref",
				mcp.Required(),
				mcp.Description("Snapshot ID, commit hash, git tag, or 'HEAD'"),
			),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:           "gmb_snapshot_at",
				ReadOnlyHint:    mcp.ToBoolPtr(true),
				DestructiveHint: mcp.ToBoolPtr(false),
				IdempotentHint:  mcp.ToBoolPtr(true),
				OpenWorldHint:   mcp.ToBoolPtr(false),
			}),
		)
		s.RegisterTool(snapshotAtTool, s.handleSnapshotAtTool)
	}
	if s.shouldRegister("gmb_snapshot_diff", "snapshot") {
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
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:           "gmb_snapshot_diff",
				ReadOnlyHint:    mcp.ToBoolPtr(true),
				DestructiveHint: mcp.ToBoolPtr(false),
				IdempotentHint:  mcp.ToBoolPtr(true),
				OpenWorldHint:   mcp.ToBoolPtr(false),
			}),
		)
		s.RegisterTool(snapshotDiffTool, s.handleSnapshotDiffTool)
	}
}
func (s *Server) handleSnapshotListTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if r, cancelled := checkCancellation(ctx); cancelled {
		return r, nil
	}
	store, err := s.bridge.SnapshotStore()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Snapshot store unavailable: %v", err)), nil
	}

	limit := getIntArgClamped(req, "limit", 20, 1, 100)

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
	if r, cancelled := checkCancellation(ctx); cancelled {
		return r, nil
	}
	ref, err := requireStringArg(req, "ref")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if len(ref) > maxIDArgLen {
		return mcp.NewToolResultError(fmt.Sprintf("input too long: argument %q exceeds %d chars (got %d)", "ref", maxIDArgLen, len(ref))), nil
	}
	if len(ref) > maxStringArgLen {
		return mcp.NewToolResultError(fmt.Sprintf("input too long: argument %q exceeds %d chars (got %d)", "ref", maxStringArgLen, len(ref))), nil
	}

	select {
	case <-ctx.Done():
		return mcp.NewToolResultError("cancelled: " + ctx.Err().Error()), nil
	default:
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
	if r, cancelled := checkCancellation(ctx); cancelled {
		return r, nil
	}
	baseRef, err := requireStringArg(req, "base_ref")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if len(baseRef) > maxIDArgLen {
		return mcp.NewToolResultError(fmt.Sprintf("input too long: argument %q exceeds %d chars (got %d)", "base_ref", maxIDArgLen, len(baseRef))), nil
	}

	headRef, err := requireStringArg(req, "head_ref")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if len(headRef) > maxIDArgLen {
		return mcp.NewToolResultError(fmt.Sprintf("input too long: argument %q exceeds %d chars (got %d)", "head_ref", maxIDArgLen, len(headRef))), nil
	}

	select {
	case <-ctx.Done():
		return mcp.NewToolResultError("cancelled: " + ctx.Err().Error()), nil
	default:
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
