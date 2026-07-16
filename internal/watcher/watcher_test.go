package watcher

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stack-research/substrate/internal/substrate"
)

type lockedBuffer struct {
	mu   sync.Mutex
	text strings.Builder
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.text.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.text.String()
}

func TestWatchReportsInitialFloorAndEnd(t *testing.T) {
	space, err := substrate.InitSpace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	moderator := substrate.MustName("user-name")
	agent := substrate.MustName("agent-a")
	if err := space.AddParticipant(moderator, substrate.Human); err != nil {
		t.Fatal(err)
	}
	if err := space.AddParticipant(agent, substrate.Agent); err != nil {
		t.Fatal(err)
	}
	thread := substrate.MustName("lab")
	if _, err := substrate.CreateThread(space, thread, "watch test", moderator, []substrate.Name{agent}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var out, errOut lockedBuffer
	done := make(chan error, 1)
	go func() { done <- Watch(ctx, space, thread, nil, "", &out, &errOut) }()
	waitFor(t, func() bool { return strings.Contains(out.String(), "turn user-name") })
	if _, err := substrate.WriteEntry(space, thread, moderator, "opening"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return strings.Contains(out.String(), "turn agent-a") })
	if err := substrate.EndThread(space, thread); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("watch did not exit after thread ended")
	}
	if !strings.Contains(out.String(), "lab: ended") {
		t.Fatalf("output:\n%s\nerrors:\n%s", out.String(), errOut.String())
	}
}

func TestAttendExplainsMissingCommand(t *testing.T) {
	t.Setenv("SUBSTRATE_HOME", t.TempDir())
	err := Attend(context.Background(), substrate.MustName("agent-a"), AttendOptions{}, &lockedBuffer{}, &lockedBuffer{})
	if err == nil || !strings.Contains(err.Error(), "agents.yaml") || !strings.Contains(err.Error(), "SUBSTRATE_PROMPT") {
		t.Fatalf("error = %v", err)
	}
}

func TestAttendContextOffersAreCapturedAndIncremental(t *testing.T) {
	t.Setenv("SUBSTRATE_HOME", t.TempDir())
	space, err := substrate.InitSpace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	moderator := substrate.MustName("user-name")
	agent := substrate.MustName("agent-a")
	for _, participant := range []substrate.Participant{{Name: moderator, Kind: substrate.Human}, {Name: agent, Kind: substrate.Agent}} {
		if err := space.AddParticipant(participant.Name, participant.Kind); err != nil {
			t.Fatal(err)
		}
	}
	thread := substrate.MustName("lab")
	if _, err := substrate.CreateThread(space, thread, "offers", moderator, []substrate.Name{agent}); err != nil {
		t.Fatal(err)
	}
	opening, err := substrate.WriteEntry(space, thread, moderator, "opening")
	if err != nil {
		t.Fatal(err)
	}

	full, err := makeContextOffer(space, thread, agent, "full", AttendOptions{})
	if err != nil || full.window.ThroughEntry != opening.Filename || full.version != 1 || full.fallback {
		t.Fatalf("full offer: %#v err=%v", full, err)
	}
	missing, err := makeContextOffer(space, thread, agent, "incremental", AttendOptions{})
	if err != nil || !missing.fallback || missing.window.ThroughEntry != opening.Filename {
		t.Fatalf("first incremental fallback: %#v err=%v", missing, err)
	}
	if err := substrate.SaveAttendCursor(agent, space.Root()+"::"+thread.String(), substrate.AttendCursor{LastEntry: opening.Filename, NextLine: 4}); err != nil {
		t.Fatal(err)
	}
	agentReply, err := substrate.WriteEntry(space, thread, agent, "prior agent reply")
	if err != nil {
		t.Fatal(err)
	}
	followup, err := substrate.WriteEntry(space, thread, moderator, "new assignment")
	if err != nil {
		t.Fatal(err)
	}
	incremental, err := makeContextOffer(space, thread, agent, "incremental", AttendOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if incremental.fallback || incremental.window.FromEntry != agentReply.Filename || incremental.window.ThroughEntry != followup.Filename || incremental.version != 3 {
		t.Fatalf("incremental offer: %#v", incremental)
	}

	explicit, err := makeContextOffer(space, thread, agent, "explicit", AttendOptions{FromEntry: followup.Filename, ThroughEntry: followup.Filename})
	if err != nil || explicit.window.FromEntry != followup.Filename || explicit.window.ThroughEntry != followup.Filename {
		t.Fatalf("explicit offer: %#v err=%v", explicit, err)
	}
}

func TestAttendPassesCapturedOfferToChildProcess(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SUBSTRATE_HOME", home)
	space, err := substrate.InitSpace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	moderator := substrate.MustName("user-name")
	agent := substrate.MustName("agent-a")
	for _, participant := range []substrate.Participant{{Name: moderator, Kind: substrate.Human}, {Name: agent, Kind: substrate.Agent}} {
		if err := space.AddParticipant(participant.Name, participant.Kind); err != nil {
			t.Fatal(err)
		}
	}
	thread := substrate.MustName("lab")
	if _, err := substrate.CreateThread(space, thread, "child env", moderator, []substrate.Name{agent}); err != nil {
		t.Fatal(err)
	}
	opening, err := substrate.WriteEntry(space, thread, moderator, "opening")
	if err != nil {
		t.Fatal(err)
	}
	registry := substrate.SpacesRegistry{Spaces: map[string]string{"test-space": space.Root()}}
	if err := registry.Save(""); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out, errOut lockedBuffer
	done := make(chan error, 1)
	command := `printf '%s|%s|%s|%s\n' "$SUBSTRATE_CONTEXT_MODE" "$SUBSTRATE_THROUGH_ENTRY" "$SUBSTRATE_THREAD_VERSION" "$SUBSTRATE_THREAD"`
	go func() {
		done <- Attend(ctx, agent, AttendOptions{Command: command, Context: "full", Room: "test-space/lab"}, &out, &errOut)
	}()
	want := "full|" + opening.Filename + "|1|lab"
	waitFor(t, func() bool { return strings.Contains(out.String(), want) })
	waitFor(t, func() bool {
		_, ok, err := substrate.LoadAttendCursor(agent, space.Root()+"::"+thread.String())
		return err == nil && ok
	})
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("attend did not stop after cancellation")
	}
	if errOut.String() != "" {
		t.Fatalf("attend errors:\n%s", errOut.String())
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition did not become true")
}
