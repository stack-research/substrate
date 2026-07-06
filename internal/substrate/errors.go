package substrate

import (
	"errors"
	"fmt"
	"path/filepath"
)

var (
	ErrEnded                = errors.New("the thread has ended; no further entries")
	ErrNotEnded             = errors.New("the thread is still active — nothing to resume")
	ErrNotModerator         = errors.New("only the moderator may do that")
	ErrCannotQuietModerator = errors.New("the moderator cannot be quieted")
	ErrTooFewParticipants   = errors.New("a thread needs at least two participants")
)

type InvalidNameError struct {
	Name   string
	Reason string
}

func (e *InvalidNameError) Error() string {
	return fmt.Sprintf("invalid name %q: %s", e.Name, e.Reason)
}

type DuplicateParticipantError struct{ Name Name }

func (e *DuplicateParticipantError) Error() string {
	return fmt.Sprintf("%q is already registered in this space", e.Name)
}

type UnknownParticipantError struct{ Name string }

func (e *UnknownParticipantError) Error() string {
	return fmt.Sprintf("%q is not a registered participant", e.Name)
}

type UnknownThreadError struct{ Name string }

func (e *UnknownThreadError) Error() string { return fmt.Sprintf("thread %q not found", e.Name) }

type ThreadExistsError struct{ Name Name }

func (e *ThreadExistsError) Error() string { return fmt.Sprintf("thread %q already exists", e.Name) }

type NotYourTurnError struct{ Current Name }

func (e *NotYourTurnError) Error() string {
	return fmt.Sprintf("not your turn: %q holds the floor", e.Current)
}

type NotInThreadError struct{ Name Name }

func (e *NotInThreadError) Error() string {
	return fmt.Sprintf("%q is not a participant in this thread", e.Name)
}

type NotASpaceError struct{ Root string }

func (e *NotASpaceError) Error() string {
	return fmt.Sprintf("not a substrate space (no .substrate/config.yaml): %s", filepath.Clean(e.Root))
}
