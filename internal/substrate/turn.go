package substrate

import (
	"errors"
	"fmt"
	"path/filepath"
)

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

type Written struct {
	Filename string
	NoOp     bool
	Next     Name
	Paused   bool
}

func GetTurnStatus(space *Space, thread Name) (TurnStatus, error) {
	cfg, err := LoadThread(space, thread)
	if err != nil {
		return TurnStatus{}, err
	}
	return TurnStatus{
		Thread: thread, Topic: cfg.Topic, Status: cfg.Status, Current: cfg.Current(),
		Moderator: cfg.Moderator, Paused: cfg.Paused(),
		TurnOrder: append([]Name(nil), cfg.TurnOrder...), Quieted: cloneQuieted(cfg.Quieted),
	}, nil
}

func WriteEntry(space *Space, thread, author Name, content string) (Written, error) {
	var written Written
	err := withThreadLock(space, thread, func(cfg *ThreadConfig) error {
		if cfg.Status == Ended {
			return ErrEnded
		}
		if !containsName(cfg.TurnOrder, author) {
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
		if err := writeAtomic(filepath.Join(space.ThreadDir(thread), filename), data); err != nil {
			return err
		}
		advance(cfg)
		if err := SaveThread(space, thread, *cfg); err != nil {
			return fmt.Errorf("entry %s was recorded but advancing the floor failed: %w", filename, err)
		}
		written = Written{Filename: filename, NoOp: noOp, Next: cfg.Current(), Paused: cfg.Paused()}
		return nil
	})
	return written, err
}

func withThreadLock(space *Space, thread Name, mutate func(*ThreadConfig) error) error {
	// Validate before flock.New so an unknown name never creates a directory.
	if _, err := LoadThread(space, thread); err != nil {
		return err
	}
	return withFileLock(filepath.Join(space.ThreadDir(thread), ".turn.lock"), func() error {
		cfg, err := LoadThread(space, thread)
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

func SetTopic(space *Space, thread Name, topic string) error {
	return mutateActive(space, thread, func(cfg *ThreadConfig) error { cfg.Topic = topic; return nil })
}

func ReorderTurns(space *Space, thread Name, newOrder []Name) error {
	return mutateActive(space, thread, func(cfg *ThreadConfig) error {
		current := cfg.Current()
		order := []Name{cfg.Moderator}
		for _, name := range newOrder {
			if _, err := space.Participant(name); err != nil {
				return err
			}
			if name != cfg.Moderator && !containsName(order, name) {
				order = append(order, name)
			}
		}
		if len(order) < 2 {
			return ErrTooFewParticipants
		}
		for name := range cfg.Quieted {
			if !containsName(order, name) {
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

func Quiet(space *Space, thread, name Name, turns uint32) error {
	return mutateActive(space, thread, func(cfg *ThreadConfig) error {
		if name == cfg.Moderator {
			return ErrCannotQuietModerator
		}
		if !containsName(cfg.TurnOrder, name) {
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

func Invite(space *Space, thread, name Name) error {
	return mutateActive(space, thread, func(cfg *ThreadConfig) error {
		if _, err := space.Participant(name); err != nil {
			return err
		}
		if !containsName(cfg.TurnOrder, name) {
			cfg.TurnOrder = append(cfg.TurnOrder, name)
		}
		return nil
	})
}

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

func EndThread(space *Space, thread Name) error {
	return mutateActive(space, thread, func(cfg *ThreadConfig) error { cfg.Status = Ended; return nil })
}

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

func IsNotYourTurn(err error) bool {
	var target *NotYourTurnError
	return errors.As(err, &target)
}

func cloneQuieted(in map[Name]uint32) map[Name]uint32 {
	out := make(map[Name]uint32, len(in))
	for name, remaining := range in {
		out[name] = remaining
	}
	return out
}
