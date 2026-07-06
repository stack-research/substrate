package substrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadsRustEraVersionOneSpaceWithoutMigration(t *testing.T) {
	root := t.TempDir()
	threadDir := filepath.Join(root, ThreadsDir, "legacy-room")
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		t.Fatal(err)
	}
	spaceYAML := "version: 1\nparticipants:\n- name: user-name\n  kind: human\n- name: claude-a\n  kind: agent\n"
	threadYAML := "topic: legacy files stay readable\ncreated_at: 2026-06-10T17:23:50.780Z\nmoderator: user-name\nturn_order:\n- user-name\n- claude-a\nnext_index: 1\nquieted: {}\nstatus: active\n"
	entry := "---\nauthor: user-name\ntimestamp: 2026-06-10T17:23:50.780Z\n---\n\nOpening from the Rust build.\n"
	if err := os.WriteFile(filepath.Join(root, SpaceConfigFile), []byte(spaceYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(threadDir, ThreadConfigFile), []byte(threadYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(threadDir, "20260610T172350780Z__user-name.md"), []byte(entry), 0o644); err != nil {
		t.Fatal(err)
	}

	space, err := OpenSpace(root)
	if err != nil {
		t.Fatal(err)
	}
	status, err := GetTurnStatus(space, MustName("legacy-room"))
	if err != nil {
		t.Fatal(err)
	}
	if status.Current != MustName("claude-a") || status.Topic != "legacy files stay readable" {
		t.Fatalf("status = %#v", status)
	}
	transcript, lines, err := ReadTranscript(space, MustName("legacy-room"), Window{})
	if err != nil {
		t.Fatal(err)
	}
	if lines != 3 || !strings.Contains(transcript, "[user-name @ 2026-06-10T17:23:50Z]") || !strings.Contains(transcript, "Opening from the Rust build.") {
		t.Fatalf("lines=%d transcript=%q", lines, transcript)
	}
}
