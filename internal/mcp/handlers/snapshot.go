package handlers

import (
	"fmt"
	"strings"
)

// Snapshot tool names (Master Plan §6 snapshot category).
const (
	ToolSnapshotList = "gmb_snapshot_list"
	ToolSnapshotAt   = "gmb_snapshot_at"
	ToolSnapshotDiff = "gmb_snapshot_diff"
)

// SnapshotToolNames returns the snapshot tool set.
func SnapshotToolNames() []string {
	return []string{ToolSnapshotList, ToolSnapshotAt, ToolSnapshotDiff}
}

// SnapshotListArgs holds validated args for gmb_snapshot_list.
type SnapshotListArgs struct {
	Limit int
}

// ValidateSnapshotListArgs validates gmb_snapshot_list args.
func ValidateSnapshotListArgs(args map[string]any) SnapshotListArgs {
	limit := 20
	if v, ok := args["limit"]; ok {
		switch n := v.(type) {
		case float64:
			limit = int(n)
		case int:
			limit = n
		}
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	return SnapshotListArgs{Limit: limit}
}

// ValidateSnapshotAtArgs validates gmb_snapshot_at required ref.
func ValidateSnapshotAtArgs(args map[string]any) (string, error) {
	ref, _ := args["ref"].(string)
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("missing required parameter \"ref\"")
	}
	if len(ref) > 500 {
		return "", fmt.Errorf("ref too long (%d chars, max 500)", len(ref))
	}
	return ref, nil
}

// SnapshotDiffArgs holds validated args for gmb_snapshot_diff.
type SnapshotDiffArgs struct {
	BaseRef string
	HeadRef string
}

// ValidateSnapshotDiffArgs validates gmb_snapshot_diff args.
func ValidateSnapshotDiffArgs(args map[string]any) (SnapshotDiffArgs, error) {
	base, _ := args["base_ref"].(string)
	head, _ := args["head_ref"].(string)
	base = strings.TrimSpace(base)
	head = strings.TrimSpace(head)
	if base == "" {
		return SnapshotDiffArgs{}, fmt.Errorf("missing required parameter \"base_ref\"")
	}
	if head == "" {
		return SnapshotDiffArgs{}, fmt.Errorf("missing required parameter \"head_ref\"")
	}
	return SnapshotDiffArgs{BaseRef: base, HeadRef: head}, nil
}
