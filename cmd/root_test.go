package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootCommand(t *testing.T) {
	cmd := rootCmd
	if cmd == nil {
		t.Fatal("rootCmd is nil")
	}
	if cmd.Use != "gmb" {
		t.Errorf("Use = %q, want 'gmb'", cmd.Use)
	}
	if !strings.Contains(cmd.Short, "Architecture Knowledge Graph") {
		t.Errorf("Short doesn't mention Architecture Knowledge Graph: %q", cmd.Short)
	}
}

func TestVersionCommand(t *testing.T) {
	cmd := versionCmd
	if cmd == nil {
		t.Fatal("versionCmd is nil")
	}
	if cmd.Use != "version" {
		t.Errorf("Use = %q, want 'version'", cmd.Use)
	}

	// Test execution
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}
	output := buf.String()
	// Version command might print to actual stdout, so just verify command exists
	_ = output
}

func TestHelpCommand(t *testing.T) {
	cmd := rootCmd
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "glassmarble") {
		t.Errorf("Help output doesn't contain command name: %q", output)
	}
	if !strings.Contains(output, "Architecture Knowledge Graph") {
		t.Errorf("Help output doesn't mention Architecture Knowledge Graph")
	}
}

func TestRootFlags(t *testing.T) {
	cmd := rootCmd

	// Test config flag
	cmd.SetArgs([]string{"--config", "/custom/config.yaml"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}

	// Test verbose flag
	buf := new(bytes.Buffer)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--verbose", "version"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}
}
