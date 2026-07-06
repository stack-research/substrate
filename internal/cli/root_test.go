package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runCLI(t *testing.T, input string, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	app := New()
	app.In = strings.NewReader(input)
	app.Out = &out
	app.ErrOut = &errOut
	root := app.Root()
	root.SetArgs(args)
	err := root.Execute()
	return out.String() + errOut.String(), err
}

func TestScriptableLifecycle(t *testing.T) {
	t.Setenv("SUBSTRATE_HOME", filepath.Join(t.TempDir(), "home"))
	space := filepath.Join(t.TempDir(), "lab")
	steps := [][]string{
		{"--space", space, "init"},
		{"--space", space, "add", "user-name", "--kind", "human"},
		{"--space", space, "add", "claude-a", "--kind", "agent"},
		{"--space", space, "new", "design", "--topic", "storage shape", "--moderator", "user-name", "--turns", "claude-a"},
	}
	for _, args := range steps {
		if output, err := runCLI(t, "", args...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, output)
		}
	}
	output, err := runCLI(t, "", "--space", space, "status", "design")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"thread: design", "topic: storage shape", "turn: user-name", "transcript lines: 0"} {
		if !strings.Contains(output, want) {
			t.Fatalf("status missing %q:\n%s", want, output)
		}
	}
	output, err = runCLI(t, "", "--space", space, "write", "design", "--as", "user-name", "-m", "Opening context")
	if err != nil || !strings.Contains(output, "next: claude-a") {
		t.Fatalf("write: %v\n%s", err, output)
	}
	output, err = runCLI(t, "Agent reply from stdin", "--space", space, "write", "design", "--as", "claude-a", "--stdin")
	if err != nil || !strings.Contains(output, "moderator — paused") {
		t.Fatalf("stdin write: %v\n%s", err, output)
	}
	output, err = runCLI(t, "", "--space", space, "read", "design")
	if err != nil || !strings.Contains(output, "Opening context") || !strings.Contains(output, "Agent reply from stdin") {
		t.Fatalf("read: %v\n%s", err, output)
	}
}

func TestNoOpHiddenSpacesAndDoctor(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	t.Setenv("SUBSTRATE_HOME", home)
	space := filepath.Join(t.TempDir(), "lab")
	for _, args := range [][]string{
		{"--space", space, "init"},
		{"--space", space, "add", "user-name", "--kind", "human"},
		{"--space", space, "add", "agent-a", "--kind", "agent"},
		{"--space", space, "new", "room", "--topic", "t", "--moderator", "user-name", "--turns", "agent-a"},
		{"--space", space, "write", "room", "--as", "user-name", "-m", "pass"},
	} {
		if output, err := runCLI(t, "", args...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, output)
		}
	}
	output, err := runCLI(t, "", "--space", space, "read", "room")
	if err != nil || strings.Contains(output, "pass") {
		t.Fatalf("no-op leaked: %v\n%s", err, output)
	}
	output, err = runCLI(t, "", "spaces", "list")
	if err != nil || !strings.Contains(output, space) {
		t.Fatalf("spaces: %v\n%s", err, output)
	}
	output, err = runCLI(t, "", "--space", space, "doctor")
	if err != nil || !strings.Contains(output, "runtime: go") || !strings.Contains(output, "health: ok") {
		t.Fatalf("doctor: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(home, "spaces.yaml")); err != nil {
		t.Fatal(err)
	}
}

func TestWriteRequiresExactlyOneSource(t *testing.T) {
	t.Setenv("SUBSTRATE_HOME", filepath.Join(t.TempDir(), "home"))
	output, err := runCLI(t, "", "write", "room", "--as", "user-name")
	if err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("expected source error: %v\n%s", err, output)
	}
}
