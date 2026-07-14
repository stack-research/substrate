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
					runHook(ctx, command, map[string]string{
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

func Attend(ctx context.Context, name substrate.Name, override string, out, errOut io.Writer) error {
	command := override
	if command == "" {
		configured, ok, err := substrate.LoadAgentCommand(name)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("no command configured for %q — add it to ~/.substrate/agents.yaml:\nagents:\n  %s:\n    run: <one-shot harness command using $SUBSTRATE_PROMPT>\nor pass --exec", name, name)
		}
		command = configured
	}
	fmt.Fprintf(out, "attending as %s — command: %s\n", name, command)
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
				version := substrate.ThreadVersion(space, thread)
				if previous, ok := lastRun[key]; ok && previous.version == version && time.Since(previous.at) < time.Minute {
					continue
				}
				lastRun[key] = runState{at: time.Now(), version: version}
				fmt.Fprintf(out, "[%s] the floor is %s's — running agent\n", key, name)
				prompt := fmt.Sprintf("You are participant %q in substrate. It is your turn in thread %q (space %q, topic: %s). Use the substrate MCP tools: read_thread to catch up, then write_entry with your reply — or exactly 'pass' if you have nothing to add. Take only this one turn, then stop.", name, thread, label, status.Topic)
				runHook(ctx, command, map[string]string{
					"SUBSTRATE_PROMPT": prompt, "SUBSTRATE_SPACE": path, "SUBSTRATE_SPACE_LABEL": label,
					"SUBSTRATE_THREAD": thread.String(), "SUBSTRATE_TOPIC": status.Topic,
				}, out, errOut)
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

func runHook(ctx context.Context, command string, env map[string]string, out, errOut io.Writer) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Env = append([]string{}, os.Environ()...)
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	cmd.Stdout, cmd.Stderr = out, errOut
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(errOut, "command failed: %v\n", err)
	}
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
