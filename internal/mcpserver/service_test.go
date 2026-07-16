package mcpserver

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stack-research/substrate/internal/substrate"
	"github.com/stack-research/substrate/internal/version"
)

func setupSpace(t *testing.T) (*substrate.Space, substrate.Name) {
	t.Helper()
	space, err := substrate.InitSpace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, participant := range []substrate.Participant{
		{Name: substrate.MustName("user-name"), Kind: substrate.Human},
		{Name: substrate.MustName("claude-a"), Kind: substrate.Agent},
		{Name: substrate.MustName("codex-b"), Kind: substrate.Agent},
	} {
		if err := space.AddParticipant(participant.Name, participant.Kind); err != nil {
			t.Fatal(err)
		}
	}
	thread := substrate.MustName("lab")
	if _, err := substrate.CreateThread(space, thread, "protocol test", substrate.MustName("user-name"), []substrate.Name{substrate.MustName("claude-a"), substrate.MustName("codex-b")}); err != nil {
		t.Fatal(err)
	}
	return space, thread
}

func connect(t *testing.T, service *Service) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := service.Server().Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "substrate-test", Version: "1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clientSession.Close(); _ = serverSession.Close() })
	return clientSession
}

func call(t *testing.T, session *mcp.ClientSession, tool string, args map[string]any) (string, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	if len(result.Content) == 0 {
		t.Fatalf("%s returned no content", tool)
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("%s content = %T", tool, result.Content[0])
	}
	return text.Text, result.IsError
}

func TestAdvertisesProtocolAndConversationTools(t *testing.T) {
	space, thread := setupSpace(t)
	actor := substrate.MustName("claude-a")
	session := connect(t, New(SpaceSource{Paths: []string{space.Root()}}, &actor, nil))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(listed.Tools))
	for i, tool := range listed.Tools {
		names[i] = tool.Name
	}
	sort.Strings(names)
	want := append([]string(nil), ToolNames...)
	sort.Strings(want)
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("tools = %v, want %v", names, want)
	}

	text, failed := call(t, session, "about", map[string]any{})
	if failed || !strings.Contains(text, "server version: "+version.Full()) || !strings.Contains(text, "TIMEOUT MEANS STILL WAITING") || !strings.Contains(text, "Moderator playbook") {
		t.Fatalf("about failed=%v:\n%s", failed, text)
	}
	text, failed = call(t, session, "check_turn", map[string]any{"thread": thread.String()})
	if failed || !strings.Contains(text, "current turn: user-name") || !strings.Contains(text, "your turn: no") {
		t.Fatalf("check failed=%v:\n%s", failed, text)
	}

	if _, err := substrate.WriteEntry(space, thread, substrate.MustName("user-name"), "moderator opening"); err != nil {
		t.Fatal(err)
	}
	text, failed = call(t, session, "wait_for_turn", map[string]any{"thread": "lab", "timeout_secs": 1})
	if failed || !strings.Contains(text, "your turn: yes") {
		t.Fatalf("wait failed=%v:\n%s", failed, text)
	}
	text, failed = call(t, session, "write_entry", map[string]any{"thread": "lab", "content": "Opening thought from claude-a."})
	if failed || !strings.Contains(text, "next turn: codex-b") {
		t.Fatalf("write failed=%v:\n%s", failed, text)
	}
	text, failed = call(t, session, "write_entry", map[string]any{"thread": "lab", "content": "double dip"})
	if !failed || !strings.Contains(text, "codex-b") || !strings.Contains(text, "wait_for_turn") {
		t.Fatalf("double write failed=%v:\n%s", failed, text)
	}
	text, failed = call(t, session, "write_entry", map[string]any{"thread": "lab", "participant_name": "codex-b", "content": "pass"})
	if failed || !strings.Contains(text, "no-op") {
		t.Fatalf("no-op failed=%v:\n%s", failed, text)
	}
	text, failed = call(t, session, "read_thread", map[string]any{"thread": "lab"})
	if failed || !strings.Contains(text, "Opening thought") || strings.Contains(text, "\npass\n") || !strings.Contains(text, "captured snapshot") {
		t.Fatalf("read failed=%v:\n%s", failed, text)
	}
	manifest, err := substrate.BuildTranscriptManifest(space, thread)
	if err != nil {
		t.Fatal(err)
	}
	text, failed = call(t, session, "read_thread", map[string]any{"thread": "lab", "from_entry": manifest.Entries[0].Filename, "through_entry": manifest.Entries[0].Filename})
	if failed || !strings.Contains(text, "moderator opening") || strings.Contains(text, "Opening thought") || !strings.Contains(text, "replay with: from_entry=") {
		t.Fatalf("bounded read failed=%v:\n%s", failed, text)
	}
	text, failed = call(t, session, "transcript_manifest", map[string]any{"thread": "lab"})
	if failed || !strings.Contains(text, `"thread_version": 3`) || !strings.Contains(text, `"sha256"`) {
		t.Fatalf("manifest failed=%v:\n%s", failed, text)
	}
	text, failed = call(t, session, "read_thread", map[string]any{"thread": "lab", "from_line": 0})
	if !failed || !strings.Contains(text, "1-based") {
		t.Fatalf("zero cursor failed=%v:\n%s", failed, text)
	}
}

func TestPerCallIdentityNewThreadAndModeratorTools(t *testing.T) {
	space, _ := setupSpace(t)
	session := connect(t, New(SpaceSource{Paths: []string{space.Root()}}, nil, nil))
	text, failed := call(t, session, "list_threads", map[string]any{})
	if !failed || !strings.Contains(text, "participant_name is required") {
		t.Fatalf("identity failed=%v:\n%s", failed, text)
	}
	text, failed = call(t, session, "new_thread", map[string]any{
		"participant_name": "claude-a", "name": "fresh-lab", "topic": "mcp-created room",
		"moderator": "user-name", "turn_order": []string{"claude-a", "user-name", "codex-b"},
	})
	if failed || !strings.Contains(text, "created thread: fresh-lab") || !strings.Contains(text, "opening floor: user-name") || !strings.Contains(text, "moderator next steps") {
		t.Fatalf("new failed=%v:\n%s", failed, text)
	}
	text, failed = call(t, session, "set_topic", map[string]any{"thread": "fresh-lab", "participant_name": "claude-a", "topic": "nope"})
	if !failed || !strings.Contains(text, "user-name moderates") {
		t.Fatalf("role gate failed=%v:\n%s", failed, text)
	}
	text, failed = call(t, session, "invite", map[string]any{"thread": "fresh-lab", "participant_name": "user-name", "name": "new-agent"})
	if failed || !strings.Contains(text, "registered as a new agent") {
		t.Fatalf("invite failed=%v:\n%s", failed, text)
	}
	if _, err := space.Participant(substrate.MustName("new-agent")); err != nil {
		t.Fatal(err)
	}
	text, failed = call(t, session, "set_next", map[string]any{"thread": "fresh-lab", "participant_name": "user-name", "name": "claude-a"})
	if failed || !strings.Contains(text, "current turn: claude-a") {
		t.Fatalf("next failed=%v:\n%s", failed, text)
	}
	text, failed = call(t, session, "end_thread", map[string]any{"thread": "fresh-lab", "participant_name": "user-name"})
	if failed || !strings.Contains(text, "status: Ended") {
		t.Fatalf("end failed=%v:\n%s", failed, text)
	}
	text, failed = call(t, session, "resume_thread", map[string]any{"thread": "fresh-lab", "participant_name": "user-name"})
	if failed || !strings.Contains(text, "current turn: user-name") {
		t.Fatalf("resume failed=%v:\n%s", failed, text)
	}
}

func TestRegistryIsReloadedAndMultiSpaceRequiresLabel(t *testing.T) {
	home := t.TempDir()
	registryPath := filepath.Join(home, "spaces.yaml")
	source := SpaceSource{RegistryFile: registryPath}
	set, err := source.Load()
	if err != nil || len(set.Spaces) != 0 {
		t.Fatalf("empty registry: %#v %v", set, err)
	}
	a, _ := substrate.InitSpace(filepath.Join(t.TempDir(), "a"))
	b, _ := substrate.InitSpace(filepath.Join(t.TempDir(), "b"))
	registry := substrate.SpacesRegistry{Spaces: map[string]string{"a": a.Root(), "b": b.Root()}}
	if err := registry.Save(registryPath); err != nil {
		t.Fatal(err)
	}
	set, err = source.Load()
	if err != nil || len(set.Spaces) != 2 {
		t.Fatalf("reloaded registry: %#v %v", set, err)
	}
	if _, err := set.Resolve(""); err == nil || !strings.Contains(err.Error(), "pass `space`") {
		t.Fatalf("resolve = %v", err)
	}
	if got, err := set.Resolve("b"); err != nil || got.Root() != b.Root() {
		t.Fatalf("b = %v %v", got, err)
	}
}
