package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetMCPFlags() {
	mcpTransportFlag = "stdio"
	mcpPortFlag = 8088
	mcpConfigClientFlag = ""
	mcpMaxJSONMBFlag = 256
	mcpStrictPathsFlag = true
}

func TestMCPCommand_Help(t *testing.T) {
	resetMCPFlags()
	b := bytes.NewBufferString("")
	mcpCmd.SetOut(b)
	mcpCmd.SetErr(b)
	err := mcpCmd.Help()
	require.NoError(t, err)
	out := b.String()
	assert.Contains(t, out, "Model Context Protocol")
	assert.Contains(t, out, "--transport")
	assert.Contains(t, out, "--config-client")
}

func TestMCPCommand_ConfigClient_Claude(t *testing.T) {
	resetMCPFlags()
	mcpConfigClientFlag = "claude"
	b := bytes.NewBufferString("")
	mcpCmd.SetOut(b)
	mcpCmd.SetErr(b)

	err := mcpCmd.RunE(mcpCmd, []string{})
	require.NoError(t, err)
	out := b.String()
	assert.Contains(t, out, "claude_desktop_config.json")
	assert.Contains(t, out, "glassmarble")
}

func TestMCPCommand_ConfigClient_Cursor(t *testing.T) {
	resetMCPFlags()
	mcpConfigClientFlag = "cursor"
	b := bytes.NewBufferString("")
	mcpCmd.SetOut(b)
	mcpCmd.SetErr(b)

	err := mcpCmd.RunE(mcpCmd, []string{})
	require.NoError(t, err)
	out := b.String()
	assert.Contains(t, out, "Cursor")
	assert.Contains(t, out, "glassmarble")
}

func TestMCPCommand_ConfigClient_All(t *testing.T) {
	resetMCPFlags()
	mcpConfigClientFlag = "all"
	b := bytes.NewBufferString("")
	mcpCmd.SetOut(b)
	mcpCmd.SetErr(b)

	err := mcpCmd.RunE(mcpCmd, []string{})
	require.NoError(t, err)
	out := b.String()
	assert.Contains(t, out, "Claude Desktop")
	assert.Contains(t, out, "Cursor")
	assert.Contains(t, out, "Zed")
	assert.Contains(t, out, "Continue.dev")
}

func TestMCPCommand_PrintConfig(t *testing.T) {
	resetMCPFlags()
	mcpPrintConfigFlag = true
	b := bytes.NewBufferString("")
	mcpCmd.SetOut(b)
	mcpCmd.SetErr(b)

	err := mcpCmd.RunE(mcpCmd, []string{})
	require.NoError(t, err)
	out := b.String()
	assert.Contains(t, out, "Claude Desktop")
	assert.Contains(t, out, "Cursor")
}

