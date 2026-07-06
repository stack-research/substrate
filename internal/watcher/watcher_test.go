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
	err := Attend(context.Background(), substrate.MustName("agent-a"), "", &lockedBuffer{}, &lockedBuffer{})
	if err == nil || !strings.Contains(err.Error(), "agents.yaml") || !strings.Contains(err.Error(), "SUBSTRATE_PROMPT") {
		t.Fatalf("error = %v", err)
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
