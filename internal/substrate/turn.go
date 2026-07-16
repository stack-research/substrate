package substrate

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
)

// TurnStatus is a read-only snapshot of a thread's floor state.
type TurnStatus struct {
	Thread    Name
	Topic     string
	Status    ThreadStatus
	Current   Name
	Moderator Name
	Paused    bool
	TurnOrder []Name
	Quieted   map[Name]uint32
}

// Written reports the outcome of a successful WriteEntry.
type Written struct {
	Filename string
	NoOp     bool
	Next     Name
	Paused   bool
}

// GetTurnStatus loads a thread's current floor state from disk.
func GetTurnStatus(space *Space, thread Name) (TurnStatus, error) {
	cfg, err := LoadThread(space, thread)
	if err != nil {
		return TurnStatus{}, err
	}
	return TurnStatus{
		Thread: thread, Topic: cfg.Topic, Status: cfg.Status, Current: cfg.Current(),
		Moderator: cfg.Moderator, Paused: cfg.Paused(),
		TurnOrder: append([]Name(nil), cfg.TurnOrder...), Quieted: maps.Clone(cfg.Quieted),
	}, nil
}

// WriteEntry appends one entry under the thread lock, enforcing that the floor
// is the author's, then advances the turn (skipping quieted participants).
func WriteEntry(space *Space, thread, author Name, content string) (Written, error) {
	var written Written
	err := withThreadLock(space, thread, func(cfg *ThreadConfig) error {
		if cfg.Status == Ended {
			return ErrEnded
		}
		if !slices.Contains(cfg.TurnOrder, author) {
			return &NotInThreadError{Name: author}
		}
		if cfg.Current() != author {
			return &NotYourTurnError{Current: cfg.Current()}
		}
		noOp := IsNoOp(content)
		timestamp, err := nextTimestamp(space.ThreadDir(thread))
		if err != nil {
			return err
		}
		filename := EntryFilename(timestamp, author, noOp)
		data, err := renderEntryFile(EntryMeta{Author: author, Timestamp: timestamp}, content)
		if err != nil {
			return err
		}
		_, beforeData, err := loadThreadFile(space, thread)
		if err != nil {
			return err
		}
		advance(cfg)
		afterData, err := encodeThreadConfig(*cfg)
		if err != nil {
			return err
		}
		transaction, err := beginEntryTransaction(space, thread, filename, beforeData, afterData)
		if err != nil {
			return err
		}
		if err := runWriteEntryPhaseHook("after-intent"); err != nil {
			return err
		}
		if err := writeAtomic(filepath.Join(space.ThreadDir(thread), filename), data); err != nil {
			return err
		}
		if err := runWriteEntryPhaseHook("after-entry"); err != nil {
			return err
		}
		if err := writeAtomic(filepath.Join(space.ThreadDir(thread), ThreadConfigFile), afterData); err != nil {
			return fmt.Errorf("entry %s was recorded but advancing the floor failed: %w", filename, err)
		}
		if err := runWriteEntryPhaseHook("after-config"); err != nil {
			return err
		}
		if err := finishEntryTransaction(space, thread, transaction, "committed"); err != nil {
			return fmt.Errorf("entry %s and floor advance were recorded but finalizing recovery metadata failed: %w", filename, err)
		}
		written = Written{Filename: filename, NoOp: noOp, Next: cfg.Current(), Paused: cfg.Paused()}
		return nil
	})
	return written, err
}

func withThreadLock(space *Space, thread Name, mutate func(*ThreadConfig) error) error {
	// Validate before flock.New so an unknown name never creates a directory.
	if _, _, err := loadThreadFile(space, thread); err != nil {
		return err
	}
	return withFileLock(filepath.Join(space.ThreadDir(thread), ".turn.lock"), func() error {
		if err := recoverEntryTransactionsLocked(space, thread); err != nil {
			return err
		}
		cfg, _, err := loadThreadFile(space, thread)
		if err != nil {
			return err
		}
		return mutate(&cfg)
	})
}

func advance(cfg *ThreadConfig) {
	length := len(cfg.TurnOrder)
	cfg.NextIndex = (cfg.NextIndex + 1) % length
	for range length {
		current := cfg.TurnOrder[cfg.NextIndex]
		remaining, quieted := cfg.Quieted[current]
		if !quieted || remaining == 0 {
			break
		}
		remaining--
		if remaining == 0 {
			delete(cfg.Quieted, current)
		} else {
			cfg.Quieted[current] = remaining
		}
		cfg.NextIndex = (cfg.NextIndex + 1) % length
	}
}

func mutateActive(space *Space, thread Name, fn func(*ThreadConfig) error) error {
	return withThreadLock(space, thread, func(cfg *ThreadConfig) error {
		if cfg.Status == Ended {
			return ErrEnded
		}
		if err := fn(cfg); err != nil {
			return err
		}
		return SaveThread(space, thread, *cfg)
	})
}

// SetTopic replaces an active thread's topic.
func SetTopic(space *Space, thread Name, topic string) error {
	return mutateActive(space, thread, func(cfg *ThreadConfig) error { cfg.Topic = topic; return nil })
}

// ReorderTurns replaces the speaking order, keeping the moderator first and
// the current speaker's floor intact where possible.
func ReorderTurns(space *Space, thread Name, newOrder []Name) error {
	return mutateActive(space, thread, func(cfg *ThreadConfig) error {
		current := cfg.Current()
		order := []Name{cfg.Moderator}
		for _, name := range newOrder {
			if _, err := space.Participant(name); err != nil {
				return err
			}
			if name != cfg.Moderator && !slices.Contains(order, name) {
				order = append(order, name)
			}
		}
		if len(order) < 2 {
			return ErrTooFewParticipants
		}
		for name := range cfg.Quieted {
			if !slices.Contains(order, name) {
				delete(cfg.Quieted, name)
			}
		}
		cfg.NextIndex = 0
		for i, name := range order {
			if name == current {
				cfg.NextIndex = i
				break
			}
		}
		cfg.TurnOrder = order
		return nil
	})
}

// Quiet makes name skip its next turns; zero turns lifts the quiet.
func Quiet(space *Space, thread, name Name, turns uint32) error {
	return mutateActive(space, thread, func(cfg *ThreadConfig) error {
		if name == cfg.Moderator {
			return ErrCannotQuietModerator
		}
		if !slices.Contains(cfg.TurnOrder, name) {
			return &NotInThreadError{Name: name}
		}
		if turns == 0 {
			delete(cfg.Quieted, name)
		} else {
			cfg.Quieted[name] = turns
		}
		return nil
	})
}

// Invite appends a registered participant to the end of the turn order.
func Invite(space *Space, thread, name Name) error {
	return mutateActive(space, thread, func(cfg *ThreadConfig) error {
		if _, err := space.Participant(name); err != nil {
			return err
		}
		if !slices.Contains(cfg.TurnOrder, name) {
			cfg.TurnOrder = append(cfg.TurnOrder, name)
		}
		return nil
	})
}

// SetNext hands the floor directly to name, lifting any quiet on it.
func SetNext(space *Space, thread, name Name) error {
	return mutateActive(space, thread, func(cfg *ThreadConfig) error {
		for i, candidate := range cfg.TurnOrder {
			if candidate == name {
				cfg.NextIndex = i
				delete(cfg.Quieted, name)
				return nil
			}
		}
		return &NotInThreadError{Name: name}
	})
}

// EndThread marks a thread ended; no further entries are accepted.
func EndThread(space *Space, thread Name) error {
	return mutateActive(space, thread, func(cfg *ThreadConfig) error { cfg.Status = Ended; return nil })
}

// ResumeThread reactivates an ended thread with the floor on the moderator.
func ResumeThread(space *Space, thread Name) error {
	return withThreadLock(space, thread, func(cfg *ThreadConfig) error {
		if cfg.Status != Ended {
			return ErrNotEnded
		}
		cfg.Status = Active
		cfg.NextIndex = 0
		return SaveThread(space, thread, *cfg)
	})
}

// RequireModerator errors with ErrNotModerator unless actor moderates thread.
func RequireModerator(space *Space, thread, actor Name) error {
	cfg, err := LoadThread(space, thread)
	if err != nil {
		return err
	}
	if cfg.Moderator != actor {
		return fmt.Errorf("%w: %s is the moderator", ErrNotModerator, cfg.Moderator)
	}
	return nil
}
