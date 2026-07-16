// Package watcher provides filesystem watching for substrate spaces: the
// recursive fsnotify primitives shared with the TUI, plus the blocking
// watch/attend loops behind the CLI commands of the same names.
package watcher

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/stack-research/substrate/internal/substrate"
)

func Watch(ctx context.Context, space *substrate.Space, thread substrate.Name, forName *substrate.Name, command string, out, errOut io.Writer) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()
	if err := w.Add(space.ThreadDir(thread)); err != nil {
		return err
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	var lastCurrent substrate.Name
	var lastStatus substrate.ThreadStatus
	first := true
	for {
		status, err := substrate.GetTurnStatus(space, thread)
		if err != nil {
			return err
		}
		changed := first || status.Current != lastCurrent || status.Status != lastStatus
		if changed {
			ended := status.Status == substrate.Ended
			relevant := ended || forName == nil || *forName == status.Current
			if relevant {
				if ended {
					fmt.Fprintf(out, "%s: ended\n", thread)
				} else {
					paused := ""
					if status.Paused {
						paused = " (moderator — paused)"
					}
					fmt.Fprintf(out, "%s: turn %s%s\n", thread, status.Current, paused)
				}
				if command != "" {
					_ = runHook(ctx, command, map[string]string{
						"SUBSTRATE_SPACE": space.Root(), "SUBSTRATE_THREAD": thread.String(),
						"SUBSTRATE_TURN": status.Current.String(), "SUBSTRATE_STATUS": string(status.Status),
						"SUBSTRATE_TOPIC": status.Topic,
					}, out, errOut)
				}
			}
			lastCurrent, lastStatus, first = status.Current, status.Status, false
		}
		if status.Status == substrate.Ended {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-w.Events:
		case err := <-w.Errors:
			if err != nil {
				fmt.Fprintf(errOut, "watch warning: %v\n", err)
			}
		case <-ticker.C:
		}
	}
}

type runState struct {
	at      time.Time
	version int
}

type AttendOptions struct {
	Command      string
	Context      string
	Room         string
	FromEntry    string
	ThroughEntry string
}

type contextOffer struct {
	window    substrate.Window
	version   int
	lastEntry string
	nextLine  int
	fallback  bool
}

func Attend(ctx context.Context, name substrate.Name, options AttendOptions, out, errOut io.Writer) error {
	configured, configuredOK, err := substrate.LoadAgentConfig(name)
	if err != nil {
		return err
	}
	command := options.Command
	if command == "" {
		if !configuredOK || configured.Run == "" {
			return fmt.Errorf("no command configured for %q — add it to ~/.substrate/agents.yaml:\nagents:\n  %s:\n    run: <one-shot harness command using $SUBSTRATE_PROMPT>\nor pass --exec", name, name)
		}
		command = configured.Run
	}
	mode := strings.TrimSpace(options.Context)
	if mode == "" {
		mode = strings.TrimSpace(configured.Context)
	}
	if options.FromEntry != "" || options.ThroughEntry != "" {
		mode = "explicit"
	}
	if mode == "" {
		mode = "full"
	}
	if mode != "full" && mode != "incremental" && mode != "explicit" {
		return fmt.Errorf("unknown attend context policy %q (full|incremental|explicit)", mode)
	}
	if mode == "explicit" && options.Room == "" {
		return fmt.Errorf("explicit entry cursors require --room <space-label>/<thread>")
	}
	if mode == "explicit" && options.FromEntry == "" && options.ThroughEntry == "" {
		return fmt.Errorf("explicit context requires --from-entry, --through-entry, or both")
	}
	if options.Room != "" {
		if err := validateAttendRoom(options.Room); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "attending as %s — command: %s — context: %s\n", name, command, mode)
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()
	if home := substrate.HomeDir(); home != "" {
		_ = w.Add(home)
	}
	watched := make(map[string]bool)
	lastRun := make(map[string]runState)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		registry, err := substrate.LoadSpacesRegistry("")
		if err != nil {
			return err
		}
		for label, path := range registry.Spaces {
			space, err := substrate.OpenSpace(path)
			if err != nil {
				continue
			}
			if !watched[space.SubstrateDir()] {
				_ = addRecursive(w, space.SubstrateDir())
				watched[space.SubstrateDir()] = true
			}
			threads, err := space.ListThreads()
			if err != nil {
				return err
			}
			for _, thread := range threads {
				status, err := substrate.GetTurnStatus(space, thread)
				if err != nil || status.Status != substrate.Active || status.Current != name {
					continue
				}
				key := label + "/" + thread.String()
				if options.Room != "" && options.Room != key {
					continue
				}
				version := substrate.ThreadVersion(space, thread)
				if previous, ok := lastRun[key]; ok && previous.version == version && time.Since(previous.at) < time.Minute {
					continue
				}
				offer, err := makeContextOffer(space, thread, name, mode, options)
				if err != nil {
					fmt.Fprintf(errOut, "[%s] context offer failed: %v\n", key, err)
					continue
				}
				lastRun[key] = runState{at: time.Now(), version: offer.version}
				fmt.Fprintf(out, "[%s] the floor is %s's — running agent\n", key, name)
				if offer.fallback {
					fmt.Fprintf(errOut, "[%s] incremental cursor was unavailable; visibly falling back to a full captured offer\n", key)
				}
				readInstruction := contextReadInstruction(offer.window)
				prompt := fmt.Sprintf("You are participant %q in substrate. It is your turn in thread %q (space %q, topic: %s). The runtime selected a %s context offer at thread version %d: %s. This is presentation scope, not a claim that you understood or used it. Use read_thread with those exact bounds, then write_entry with your reply — or exactly 'pass' if you have nothing to add. Take only this one turn, then stop.", name, thread, label, status.Topic, mode, offer.version, readInstruction)
				err = runHook(ctx, command, map[string]string{
					"SUBSTRATE_PROMPT": prompt, "SUBSTRATE_SPACE": path, "SUBSTRATE_SPACE_LABEL": label,
					"SUBSTRATE_THREAD": thread.String(), "SUBSTRATE_TOPIC": status.Topic,
					"SUBSTRATE_CONTEXT_MODE": mode, "SUBSTRATE_FROM_ENTRY": offer.window.FromEntry,
					"SUBSTRATE_THROUGH_ENTRY": offer.window.ThroughEntry, "SUBSTRATE_FROM_LINE": intText(offer.window.FromLine),
					"SUBSTRATE_THREAD_VERSION": fmt.Sprint(offer.version),
				}, out, errOut)
				if err == nil && mode != "explicit" {
					cursor := substrate.AttendCursor{LastEntry: offer.lastEntry, NextLine: offer.nextLine}
					if err := substrate.SaveAttendCursor(name, space.Root()+"::"+thread.String(), cursor); err != nil {
						fmt.Fprintf(errOut, "[%s] save convenience cursor warning: %v\n", key, err)
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case event := <-w.Events:
			TrackNewDirs(w, event)
		case err := <-w.Errors:
			if err != nil {
				fmt.Fprintf(errOut, "attend watch warning: %v\n", err)
			}
		case <-ticker.C:
		}
	}
}

func validateAttendRoom(room string) error {
	label, rawThread, ok := strings.Cut(room, "/")
	if !ok || label == "" || rawThread == "" {
		return fmt.Errorf("invalid room %q: use <space-label>/<thread>", room)
	}
	registry, err := substrate.LoadSpacesRegistry("")
	if err != nil {
		return err
	}
	path, ok := registry.Spaces[label]
	if !ok {
		return fmt.Errorf("no registered space labeled %q", label)
	}
	space, err := substrate.OpenSpace(path)
	if err != nil {
		return err
	}
	thread, err := substrate.ParseName(rawThread)
	if err != nil {
		return err
	}
	if _, err := substrate.LoadThread(space, thread); err != nil {
		return err
	}
	return nil
}

func makeContextOffer(space *substrate.Space, thread, name substrate.Name, mode string, options AttendOptions) (contextOffer, error) {
	if mode == "explicit" {
		window := substrate.Window{FromEntry: options.FromEntry, ThroughEntry: options.ThroughEntry}
		read, err := substrate.ReadTranscriptSnapshot(space, thread, window)
		if err != nil {
			return contextOffer{}, err
		}
		return offerFromRead(window, read, false), nil
	}
	full, err := substrate.ReadTranscriptSnapshot(space, thread, substrate.Window{})
	if err != nil {
		return contextOffer{}, err
	}
	fullWindow := substrate.Window{}
	if full.LastEntry != "" {
		fullWindow.ThroughEntry = full.LastEntry
	}
	if mode == "full" {
		return offerFromRead(fullWindow, full, false), nil
	}

	cursor, ok, err := substrate.LoadAttendCursor(name, space.Root()+"::"+thread.String())
	if err != nil {
		return contextOffer{}, err
	}
	if !ok {
		return offerFromRead(fullWindow, full, true), nil
	}
	if cursor.LastEntry != "" {
		index := -1
		for i, entry := range full.Manifest.Entries {
			if entry.Filename == cursor.LastEntry {
				index = i
				break
			}
		}
		if index < 0 {
			return offerFromRead(fullWindow, full, true), nil
		}
		if index+1 < len(full.Manifest.Entries) {
			window := substrate.Window{
				FromEntry:    full.Manifest.Entries[index+1].Filename,
				ThroughEntry: full.Manifest.Entries[len(full.Manifest.Entries)-1].Filename,
			}
			return offerFromRead(window, full, false), nil
		}
	}
	fromLine := cursor.NextLine
	if fromLine < 1 {
		fromLine = full.Manifest.TotalLines + 1
	}
	window := substrate.Window{FromLine: &fromLine}
	return offerFromRead(window, full, false), nil
}

func offerFromRead(window substrate.Window, read substrate.TranscriptRead, fallback bool) contextOffer {
	lastEntry := ""
	if len(read.Manifest.Entries) > 0 {
		lastEntry = read.Manifest.Entries[len(read.Manifest.Entries)-1].Filename
	}
	return contextOffer{window: window, version: read.Manifest.Version, lastEntry: lastEntry, nextLine: read.Manifest.TotalLines + 1, fallback: fallback}
}

func contextReadInstruction(window substrate.Window) string {
	switch {
	case window.FromEntry != "" && window.ThroughEntry != "":
		return fmt.Sprintf("from_entry=%q through_entry=%q", window.FromEntry, window.ThroughEntry)
	case window.FromEntry != "":
		return fmt.Sprintf("from_entry=%q (through the captured tail)", window.FromEntry)
	case window.ThroughEntry != "":
		return fmt.Sprintf("through_entry=%q (from the beginning)", window.ThroughEntry)
	case window.FromLine != nil:
		return fmt.Sprintf("from_line=%d", *window.FromLine)
	default:
		return "the captured thread is empty; read_thread without a cursor"
	}
}

func intText(value *int) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(*value)
}

func runHook(ctx context.Context, command string, env map[string]string, out, errOut io.Writer) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Env = append([]string{}, os.Environ()...)
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	cmd.Stdout, cmd.Stderr = out, errOut
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(errOut, "command failed: %v\n", err)
		return err
	}
	return nil
}

// NewRecursive returns an fsnotify watcher covering root and every nested
// directory. The caller owns the watcher and must Close it.
func NewRecursive(root string) (*fsnotify.Watcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := addRecursive(w, root); err != nil {
		_ = w.Close()
		return nil, err
	}
	return w, nil
}

// TrackNewDirs adds a just-created directory to the watch set so events from
// inside it keep flowing; other events are ignored.
func TrackNewDirs(w *fsnotify.Watcher, event fsnotify.Event) {
	if event.Op&fsnotify.Create == 0 {
		return
	}
	if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
		_ = w.Add(event.Name)
	}
}

func addRecursive(w *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			return w.Add(path)
		}
		return nil
	})
}
