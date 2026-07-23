package ui

import (
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stack-research/substrate/internal/substrate"
	"github.com/stack-research/substrate/internal/watcher"
)

func TestThemePaletteIsMonochrome(t *testing.T) {
	for _, dark := range []bool{false, true} {
		theme := newStyles(dark)
		palette := map[string]color.Color{
			"background": theme.background,
			"surface":    theme.surface,
			"text":       theme.text,
			"muted":      theme.muted,
			"faint":      theme.faint,
			"accent":     theme.accent,
			"accent2":    theme.accent2,
			"good":       theme.good,
			"danger":     theme.danger,
		}
		for name, value := range palette {
			red, green, blue, _ := value.RGBA()
			if red != green || green != blue {
				t.Errorf("dark=%v %s is not monochrome: r=%d g=%d b=%d", dark, name, red, green, blue)
			}
		}
		if !theme.subtle.GetFaint() || !theme.timestamp.GetFaint() {
			t.Errorf("dark=%v muted text should use the terminal faint attribute", dark)
		}
	}
}

func testModel(t *testing.T) (*Model, *substrate.Space, substrate.Name) {
	t.Helper()
	space, err := substrate.InitSpace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	me := substrate.MustName("user-name")
	agent := substrate.MustName("claude-a")
	if err := space.AddParticipant(me, substrate.Human); err != nil {
		t.Fatal(err)
	}
	if err := space.AddParticipant(agent, substrate.Agent); err != nil {
		t.Fatal(err)
	}
	thread := substrate.MustName("lab")
	if _, err := substrate.CreateThread(space, thread, "design a calmer room", me, []substrate.Name{agent}); err != nil {
		t.Fatal(err)
	}
	model, err := NewModel(space, me)
	if err != nil {
		t.Fatal(err)
	}
	model.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	return model, space, thread
}

func TestWideViewRendersRoomsConversationAndComposer(t *testing.T) {
	model, _, _ := testModel(t)
	view := model.View().Content
	for _, want := range []string{"SUBSTRATE", "ROOMS", "lab", "design a calmer room", "you have the floor", "ctrl+s send"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestWideViewUsesOpenWorkspaceInsteadOfBoxedPanels(t *testing.T) {
	model, _, _ := testModel(t)
	view := model.View().Content
	if strings.Count(view, "╭") > 1 {
		t.Fatalf("workspace should reserve rounded chrome for the composer:\n%s", view)
	}
	if !strings.Contains(view, "SUBSTRATE") || !strings.Contains(view, "COMPOSE") {
		t.Fatalf("view lacks the new workspace hierarchy:\n%s", view)
	}
}

func TestSubmitAndSlashCommandsUseSharedEngine(t *testing.T) {
	model, space, thread := testModel(t)
	model.focus = focusComposer
	model.composer.SetValue("## Opening\n\nRead the room before speaking.")
	model.submit()
	status, err := substrate.GetTurnStatus(space, thread)
	if err != nil {
		t.Fatal(err)
	}
	if status.Current != substrate.MustName("claude-a") {
		t.Fatalf("current = %s", status.Current)
	}
	if model.composer.Value() != "" {
		t.Fatal("composer should reset")
	}
	if len(model.entries) != 1 || !strings.Contains(model.entries[0].Body, "Read the room") {
		t.Fatalf("entries = %#v", model.entries)
	}

	if err := substrate.SetNext(space, thread, substrate.MustName("user-name")); err != nil {
		t.Fatal(err)
	}
	if err := model.reload(true); err != nil {
		t.Fatal(err)
	}
	model.composer.SetValue("/topic narrower question")
	model.submit()
	status, _ = substrate.GetTurnStatus(space, thread)
	if status.Topic != "narrower question" {
		t.Fatalf("topic = %q", status.Topic)
	}
}

func TestNewRoomFormCreatesAndSelectsRoom(t *testing.T) {
	model, _, _ := testModel(t)
	model.openNewRoom()
	model.newRoom.fields[0].SetValue("second-room")
	model.newRoom.fields[1].SetValue("another question")
	model.newRoom.fields[2].SetValue("claude-a")
	model.createRoom()
	if model.newRoom != nil {
		t.Fatal("form should close")
	}
	if model.rooms[model.selected].Thread != substrate.MustName("second-room") {
		t.Fatalf("selected = %s", model.rooms[model.selected].Thread)
	}
}

func TestNarrowViewCollapsesSidebar(t *testing.T) {
	model, _, _ := testModel(t)
	model.Update(tea.WindowSizeMsg{Width: 70, Height: 24})
	view := model.View().Content
	if strings.Contains(view, "rooms\n") {
		t.Fatalf("sidebar should collapse:\n%s", view)
	}
	if !strings.Contains(view, "design a calmer room") {
		t.Fatal("conversation should remain")
	}
}

func TestFocusAndQuitKeys(t *testing.T) {
	model, space, thread := testModel(t)
	if err := substrate.SetNext(space, thread, substrate.MustName("claude-a")); err != nil {
		t.Fatal(err)
	}
	waiting, err := NewModel(space, substrate.MustName("user-name"))
	if err != nil {
		t.Fatal(err)
	}
	waiting.Init()
	if waiting.focus != focusTranscript || waiting.composer.Focused() {
		t.Fatal("waiting model should initialize on the transcript")
	}

	model.focus = focusComposer
	model.composer.Focus()
	escape := tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape})
	if _, handled := model.handleKey(escape); !handled || model.focus != focusTranscript {
		t.Fatalf("escape handled=%v focus=%v", handled, model.focus)
	}

	model.lastCtrlC = time.Now()
	ctrlC := tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl})
	command, handled := model.handleKey(ctrlC)
	if !handled || command == nil {
		t.Fatal("second Ctrl+C should produce a quit command")
	}
	if _, ok := command().(tea.QuitMsg); !ok {
		t.Fatalf("command returned %T", command())
	}
}

func TestDiskPollReloadsTakenTurn(t *testing.T) {
	model, space, thread := testModel(t)
	if _, err := substrate.WriteEntry(space, thread, substrate.MustName("user-name"), "opening"); err != nil {
		t.Fatal(err)
	}
	if model.rooms[model.selected].Current != substrate.MustName("user-name") {
		t.Fatal("model should still have its pre-poll state")
	}
	model.Update(diskPollMsg{})
	if got := model.rooms[model.selected].Current; got != substrate.MustName("claude-a") {
		t.Fatalf("current after poll = %s, want claude-a", got)
	}
}

func TestPersistentWatcherDeliversSequentialChanges(t *testing.T) {
	root := t.TempDir()
	w, err := watcher.NewRecursive(root)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	waitForChange := func() <-chan tea.Msg {
		result := make(chan tea.Msg, 1)
		go func() { result <- waitForDiskChange(w)() }()
		return result
	}
	assertChange := func(path string) {
		t.Helper()
		result := waitForChange()
		if err := os.WriteFile(path, []byte("change"), 0o644); err != nil {
			t.Fatal(err)
		}
		select {
		case msg := <-result:
			if _, ok := msg.(diskChangedMsg); !ok {
				t.Fatalf("watcher message = %T, want diskChangedMsg", msg)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("watcher did not report the filesystem change")
		}
	}

	assertChange(filepath.Join(root, "first"))
	assertChange(filepath.Join(root, "second"))
}
