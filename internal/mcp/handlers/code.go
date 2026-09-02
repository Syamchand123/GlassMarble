package handlers

import (
	"fmt"
	"strings"
)

// Code tool names (Master Plan §6 code category).
const (
	ToolCodeReadFile     = "code_read_file"
	ToolCodeSearchSymbol = "code_search_symbol"
	ToolCodeListDir      = "code_list_directory"
	ToolCodeListDirAlias = "code_list_dir"
)

// CodeToolNames returns the code tool set (including alias).
func CodeToolNames() []string {
	return []string{ToolCodeReadFile, ToolCodeSearchSymbol, ToolCodeListDir, ToolCodeListDirAlias}
}

// ReadFileArgs holds validated args for code_read_file.
type ReadFileArgs struct {
	Path     string
	MaxBytes int
}

// ValidateReadFileArgs validates code_read_file args.
func ValidateReadFileArgs(args map[string]any) (ReadFileArgs, error) {
	path, _ := args["path"].(string)
	path = strings.TrimSpace(path)
	if path == "" {
		return ReadFileArgs{}, fmt.Errorf("missing required parameter \"path\"")
	}
	if len(path) > 1000 {
		return ReadFileArgs{}, fmt.Errorf("path too long (%d chars, max 1000)", len(path))
	}
	maxBytes := 50000
	if v, ok := args["max_bytes"]; ok {
		switch n := v.(type) {
		case float64:
			maxBytes = int(n)
		case int:
			maxBytes = n
		}
		if maxBytes < 1 {
			maxBytes = 50000
		}
		if maxBytes > 1024*1024 {
			maxBytes = 1024 * 1024
		}
	}
	return ReadFileArgs{Path: path, MaxBytes: maxBytes}, nil
}

// SearchSymbolArgs holds validated args for code_search_symbol.
type SearchSymbolArgs struct {
	Name  string
	Kind  string
	Limit int
}

// ValidateSearchSymbolArgs validates code_search_symbol args.
func ValidateSearchSymbolArgs(args map[string]any) (SearchSymbolArgs, error) {
	name, _ := args["name"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		return SearchSymbolArgs{}, fmt.Errorf("missing required parameter \"name\"")
	}
	kind, _ := args["kind"].(string)
	limit := 20
	if v, ok := args["limit"]; ok {
		switch n := v.(type) {
		case float64:
			limit = int(n)
		case int:
			limit = n
		}
		if limit < 1 {
			limit = 20
		}
		if limit > 100 {
			limit = 100
		}
	}
	return SearchSymbolArgs{Name: name, Kind: strings.TrimSpace(kind), Limit: limit}, nil
}

// ListDirArgs holds validated args for code_list_directory.
type ListDirArgs struct {
	Path      string
	Recursive bool
}

// ValidateListDirArgs validates code_list_directory args.
func ValidateListDirArgs(args map[string]any) (ListDirArgs, error) {
	path, _ := args["path"].(string)
	path = strings.TrimSpace(path)
	if path == "" {
		return ListDirArgs{}, fmt.Errorf("missing required parameter \"path\"")
	}
	recursive, _ := args["recursive"].(bool)
	return ListDirArgs{Path: path, Recursive: recursive}, nil
}
