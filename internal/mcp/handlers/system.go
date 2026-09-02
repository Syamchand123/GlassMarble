package handlers

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// System tool names exposed by the MCP server (Master Plan §6 system category).
const (
	ToolGMBStatus     = "gmb_status"
	ToolGMBServerInfo = "gmb_server_info"
)

// SystemToolNames returns the canonical system tool names.
func SystemToolNames() []string {
	return []string{ToolGMBStatus, ToolGMBServerInfo}
}

// StatusResponse is the structured payload for gmb_status (mirrors server.go handleStatusTool).
type StatusResponse struct {
	Initialized   bool   `json:"initialized"`
	StorageDir    string `json:"storage_dir,omitempty"`
	RootDir       string `json:"root_dir,omitempty"`
	Nodes         int    `json:"nodes,omitempty"`
	Edges         int    `json:"edges,omitempty"`
	IndexedFiles  int    `json:"indexed_files,omitempty"`
	CommitHash    string `json:"commit_hash,omitempty"`
	LastAnalysis  string `json:"last_analysis,omitempty"`
	GeneratedAt   string `json:"generated_at"`
	Error         string `json:"error,omitempty"`
}

// FormatStatusResponse marshals a StatusResponse with indentation.
func FormatStatusResponse(s StatusResponse) (string, error) {
	if s.GeneratedAt == "" {
		s.GeneratedAt = time.Now().Format(time.RFC3339)
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal status: %w", err)
	}
	return string(b), nil
}

// ServerInfoResponse is the payload for gmb_server_info.
type ServerInfoResponse struct {
	Name            string   `json:"name"`
	Version         string   `json:"version"`
	ProtocolVersion string   `json:"protocol_version"`
	RootDir         string   `json:"root_dir"`
	StorageDir      string   `json:"storage_dir"`
	Transport       string   `json:"transport"`
	Capabilities    []string `json:"capabilities"`
	Timestamp       string   `json:"timestamp"`
}

// FormatServerInfo marshals ServerInfoResponse.
func FormatServerInfo(info ServerInfoResponse) (string, error) {
	if info.Timestamp == "" {
		info.Timestamp = time.Now().Format(time.RFC3339)
	}
	b, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal server info: %w", err)
	}
	return string(b), nil
}

// ValidateSystemArgs validates that system tools take no unexpected params.
func ValidateSystemArgs(tool string, args map[string]any) error {
	switch strings.ToLower(tool) {
	case ToolGMBStatus, ToolGMBServerInfo:
		if len(args) != 0 {
			return fmt.Errorf("tool %q takes no arguments", tool)
		}
		return nil
	default:
		return fmt.Errorf("unknown system tool %q", tool)
	}
}
