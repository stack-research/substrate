package main

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stack-research/substrate/internal/mcpserver"
	"github.com/stack-research/substrate/internal/substrate"
)

func TestMCPHelperProcess(t *testing.T) {
	if os.Getenv("SUBSTRATE_MCP_HELPER") != "1" {
		return
	}
	space := os.Getenv("SUBSTRATE_MCP_TEST_SPACE")
	actor := substrate.MustName("claude-a")
	service := mcpserver.New(mcpserver.SpaceSource{Paths: []string{space}}, &actor, nil)
	if err := service.Server().Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func TestChildProcessStdioProtocol(t *testing.T) {
	space, err := substrate.InitSpace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, participant := range []substrate.Participant{
		{Name: substrate.MustName("user-name"), Kind: substrate.Human},
		{Name: substrate.MustName("claude-a"), Kind: substrate.Agent},
	} {
		if err := space.AddParticipant(participant.Name, participant.Kind); err != nil {
			t.Fatal(err)
		}
	}
	thread := substrate.MustName("lab")
	moderator := substrate.MustName("user-name")
	if _, err := substrate.CreateThread(space, thread, "stdio protocol", moderator, []substrate.Name{substrate.MustName("claude-a")}); err != nil {
		t.Fatal(err)
	}
	if _, err := substrate.WriteEntry(space, thread, moderator, "stdio opening"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.Command(os.Args[0], "-test.run=^TestMCPHelperProcess$")
	command.Env = append(os.Environ(), "SUBSTRATE_MCP_HELPER=1", "SUBSTRATE_MCP_TEST_SPACE="+space.Root())
	client := mcp.NewClient(&mcp.Implementation{Name: "child-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != len(mcpserver.ToolNames) {
		t.Fatalf("tools=%d err=%v", len(tools.Tools), err)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_threads", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if result.IsError || !strings.Contains(text, "thread: lab") || !strings.Contains(text, "topic: stdio protocol") {
		t.Fatalf("result error=%v:\n%s", result.IsError, text)
	}
	result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "transcript_manifest", Arguments: map[string]any{"thread": "lab"}})
	if err != nil {
		t.Fatal(err)
	}
	text = result.Content[0].(*mcp.TextContent).Text
	if result.IsError || !strings.Contains(text, `"thread_version": 1`) || !strings.Contains(text, `"sha256"`) {
		t.Fatalf("manifest error=%v:\n%s", result.IsError, text)
	}
	manifest, err := substrate.BuildTranscriptManifest(space, thread)
	if err != nil {
		t.Fatal(err)
	}
	entry := manifest.Entries[0].Filename
	result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "read_thread", Arguments: map[string]any{
		"thread": "lab", "from_entry": entry, "through_entry": entry,
	}})
	if err != nil {
		t.Fatal(err)
	}
	text = result.Content[0].(*mcp.TextContent).Text
	if result.IsError || !strings.Contains(text, "stdio opening") || !strings.Contains(text, "replay with: from_entry=") {
		t.Fatalf("bounded read error=%v:\n%s", result.IsError, text)
	}
}
