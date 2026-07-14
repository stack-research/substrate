package substrate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gopkg.in/yaml.v3"
)

func groupSpace(t *testing.T) *Space {
	t.Helper()
	space, err := InitSpace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, participant := range []Participant{
		{Name: MustName("user-name"), Kind: Human},
		{Name: MustName("pat"), Kind: Human},
		{Name: MustName("claude-a"), Kind: Agent},
		{Name: MustName("codex-b"), Kind: Agent},
		{Name: MustName("gemini-c"), Kind: Agent},
	} {
		if err := space.AddParticipant(participant.Name, participant.Kind); err != nil {
			t.Fatal(err)
		}
	}
	return space
}

func groupThread(t *testing.T, space *Space) Name {
	t.Helper()
	thread := MustName("lab")
	_, err := CreateThread(space, thread, "storage design", MustName("user-name"), []Name{
		MustName("claude-a"), MustName("pat"), MustName("codex-b"), MustName("gemini-c"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return thread
}

func TestSpaceAndThreadValidation(t *testing.T) {
	space := groupSpace(t)
	if _, err := OpenSpace(space.Root()); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenSpace(t.TempDir()); err == nil {
		t.Fatal("expected non-space to fail")
	}
	if err := space.AddParticipant(MustName("user-name"), Other); err == nil {
		t.Fatal("expected duplicate participant")
	}
	thread := MustName("review")
	cfg, err := CreateThread(space, thread, "t", MustName("user-name"), []Name{
		MustName("claude-a"), MustName("user-name"), MustName("pat"), MustName("claude-a"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []Name{MustName("user-name"), MustName("claude-a"), MustName("pat")}
	if !equalNames(cfg.TurnOrder, want) {
		t.Fatalf("order = %v, want %v", cfg.TurnOrder, want)
	}
	if _, err := CreateThread(space, thread, "t", MustName("user-name"), []Name{MustName("pat")}); err == nil {
		t.Fatal("expected duplicate thread")
	}
}

func TestSlashNamesStayInsideThreadRoot(t *testing.T) {
	space, err := InitSpace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	moderator := MustName("user-name")
	agent := MustName("claude/opus-4.8")
	for _, p := range []Participant{{moderator, Human}, {agent, Agent}} {
		if err := space.AddParticipant(p.Name, p.Kind); err != nil {
			t.Fatal(err)
		}
	}
	thread := MustName("harness/model-version")
	if _, err := CreateThread(space, thread, "slash names", moderator, []Name{agent}); err != nil {
		t.Fatal(err)
	}
	if filepath.Base(space.ThreadDir(thread)) != "harness%2Fmodel-version" {
		t.Fatal(space.ThreadDir(thread))
	}
	if _, err := WriteEntry(space, thread, moderator, "opening"); err != nil {
		t.Fatal(err)
	}
	written, err := WriteEntry(space, thread, agent, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(written.Filename, "claude%2Fopus-4.8") {
		t.Fatal(written.Filename)
	}
}

func TestRoundsNoOpsAndTranscript(t *testing.T) {
	space := groupSpace(t)
	thread := groupThread(t, space)
	order := []Name{MustName("user-name"), MustName("claude-a"), MustName("pat"), MustName("codex-b"), MustName("gemini-c")}
	for round := range 2 {
		for i, speaker := range order {
			status, err := GetTurnStatus(space, thread)
			if err != nil {
				t.Fatal(err)
			}
			if status.Current != speaker || status.Paused != (speaker == MustName("user-name")) {
				t.Fatalf("bad status: %#v", status)
			}
			content := "visible"
			if round == 0 && speaker == MustName("pat") {
				content = " PASS "
			}
			written, err := WriteEntry(space, thread, speaker, content)
			if err != nil {
				t.Fatal(err)
			}
			if written.Next != order[(i+1)%len(order)] {
				t.Fatalf("next = %s", written.Next)
			}
		}
	}
	entries, err := LoadEntries(space, thread)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 9 {
		t.Fatalf("entries = %d", len(entries))
	}
	if ThreadVersion(space, thread) != 10 {
		t.Fatalf("version = %d", ThreadVersion(space, thread))
	}
	transcript := RenderTranscript(entries)
	if strings.Contains(transcript, "PASS") {
		t.Fatal("no-op leaked into transcript")
	}
}

func TestQuietAndModeratorOperations(t *testing.T) {
	space := groupSpace(t)
	thread := groupThread(t, space)
	if err := Quiet(space, thread, MustName("pat"), 2); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(Quiet(space, thread, MustName("user-name"), 1), ErrCannotQuietModerator) {
		t.Fatal("moderator quiet should fail")
	}
	for _, step := range []struct {
		speaker Name
		body    string
	}{
		{MustName("user-name"), "opening"}, {MustName("claude-a"), "r1"},
		{MustName("codex-b"), "r1"}, {MustName("gemini-c"), "r1"},
		{MustName("user-name"), "pass"}, {MustName("claude-a"), "r2"},
	} {
		if _, err := WriteEntry(space, thread, step.speaker, step.body); err != nil {
			t.Fatalf("%s: %v", step.speaker, err)
		}
	}
	status, _ := GetTurnStatus(space, thread)
	if status.Current != MustName("codex-b") {
		t.Fatalf("quiet did not skip pat: %s", status.Current)
	}
	if err := SetNext(space, thread, MustName("pat")); err != nil {
		t.Fatal(err)
	}
	status, _ = GetTurnStatus(space, thread)
	if status.Current != MustName("pat") {
		t.Fatal(status.Current)
	}
	if err := SetTopic(space, thread, "narrowed"); err != nil {
		t.Fatal(err)
	}
	if err := ReorderTurns(space, thread, []Name{MustName("pat"), MustName("claude-a")}); err != nil {
		t.Fatal(err)
	}
	if err := Invite(space, thread, MustName("gemini-c")); err != nil {
		t.Fatal(err)
	}
	if err := EndThread(space, thread); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteEntry(space, thread, MustName("pat"), "late"); !errors.Is(err, ErrEnded) {
		t.Fatalf("late write: %v", err)
	}
	if err := ResumeThread(space, thread); err != nil {
		t.Fatal(err)
	}
	status, _ = GetTurnStatus(space, thread)
	if status.Current != MustName("user-name") || !status.Paused {
		t.Fatalf("resume: %#v", status)
	}
}

func TestConcurrentWritersOnlyOneWins(t *testing.T) {
	space := groupSpace(t)
	thread := groupThread(t, space)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := WriteEntry(space, thread, MustName("user-name"), "opening")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	successes, rejected := 0, 0
	for err := range errs {
		if err == nil {
			successes++
		} else if notYourTurn := (*NotYourTurnError)(nil); errors.As(err, &notYourTurn) {
			rejected++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || rejected != 1 {
		t.Fatalf("successes=%d rejected=%d", successes, rejected)
	}
	if ThreadVersion(space, thread) != 1 {
		t.Fatalf("version=%d", ThreadVersion(space, thread))
	}
}

func TestUnknownYAMLFieldsSurviveMutation(t *testing.T) {
	space := groupSpace(t)
	thread := groupThread(t, space)
	path := filepath.Join(space.ThreadDir(thread), ThreadConfigFile)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte("future_field:\n  nested: true\n")...)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetTopic(space, thread, "new topic"); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := yaml.Unmarshal(updated, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["future_field"]; !ok {
		t.Fatalf("unknown field lost:\n%s", updated)
	}
}

func equalNames(a, b []Name) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
