package substrate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const entryTransactionsDir = "transactions"

type entryTransaction struct {
	Version      int    `yaml:"version"`
	Entry        string `yaml:"entry"`
	BeforeSHA256 string `yaml:"before_config_sha256"`
	AfterSHA256  string `yaml:"after_config_sha256"`
	AfterConfig  string `yaml:"after_config"`
}

// writeEntryPhaseHook exists only for deterministic crash-injection tests.
var writeEntryPhaseHook func(string) error

func runWriteEntryPhaseHook(phase string) error {
	if writeEntryPhaseHook == nil {
		return nil
	}
	return writeEntryPhaseHook(phase)
}

func transactionID(entry string) string { return strings.TrimSuffix(entry, ".md") }

func transactionDir(space *Space, thread Name) string {
	return filepath.Join(space.ThreadDir(thread), entryTransactionsDir)
}

func transactionIntentPath(space *Space, thread Name, entry string) string {
	return filepath.Join(transactionDir(space, thread), transactionID(entry)+".yaml")
}

func transactionPendingPath(space *Space, thread Name) string {
	return filepath.Join(transactionDir(space, thread), ".pending.yaml")
}

func transactionTerminalPath(space *Space, thread Name, entry, outcome string) string {
	return filepath.Join(transactionDir(space, thread), transactionID(entry)+"."+outcome)
}

func digestHex(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func beginEntryTransaction(space *Space, thread Name, entry string, before, after []byte) (entryTransaction, error) {
	tx := entryTransaction{
		Version: 1, Entry: entry, BeforeSHA256: digestHex(before),
		AfterSHA256: digestHex(after), AfterConfig: string(after),
	}
	data, err := yaml.Marshal(tx)
	if err != nil {
		return entryTransaction{}, err
	}
	// The hidden pointer is coordination state, not history. Publishing it
	// first ensures recovery never needs to scan an unbounded transaction log.
	if err := writeAtomic(transactionPendingPath(space, thread), data); err != nil {
		return entryTransaction{}, err
	}
	if err := runWriteEntryPhaseHook("after-pending"); err != nil {
		return entryTransaction{}, err
	}
	if err := writeAtomic(transactionIntentPath(space, thread, entry), data); err != nil {
		return entryTransaction{}, err
	}
	return tx, nil
}

func finishEntryTransaction(space *Space, thread Name, tx entryTransaction, outcome string) error {
	data, err := yaml.Marshal(map[string]any{"entry": tx.Entry, "outcome": outcome})
	if err != nil {
		return err
	}
	if err := writeAtomic(transactionTerminalPath(space, thread, tx.Entry, outcome), data); err != nil {
		return err
	}
	return clearPendingTransaction(space, thread)
}

func recoverEntryTransactionsLocked(space *Space, thread Name) error {
	pendingPath := transactionPendingPath(space, thread)
	data, err := os.ReadFile(pendingPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var tx entryTransaction
	if err := yaml.Unmarshal(data, &tx); err != nil {
		return fmt.Errorf("read pending entry transaction: %w", err)
	}
	if tx.Version != 1 || tx.Entry == "" || digestHex([]byte(tx.AfterConfig)) != tx.AfterSHA256 {
		return errors.New("pending entry transaction is invalid")
	}
	if _, _, _, ok := ParseEntryFilename(tx.Entry); !ok {
		return fmt.Errorf("pending transaction names invalid entry %q", tx.Entry)
	}
	intentPath := transactionIntentPath(space, thread, tx.Entry)
	if intentData, err := os.ReadFile(intentPath); errors.Is(err, os.ErrNotExist) {
		if err := writeAtomic(intentPath, data); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if !bytes.Equal(intentData, data) {
		return fmt.Errorf("entry transaction intent for %s does not match its pending record", tx.Entry)
	}
	if terminalExists(space, thread, tx.Entry) {
		return clearPendingTransaction(space, thread)
	}
	_, currentData, err := loadThreadFile(space, thread)
	if err != nil {
		return err
	}
	currentHash := digestHex(currentData)
	_, entryErr := os.Stat(filepath.Join(space.ThreadDir(thread), tx.Entry))
	switch {
	case errors.Is(entryErr, os.ErrNotExist) && currentHash == tx.BeforeSHA256:
		return finishEntryTransaction(space, thread, tx, "aborted")
	case entryErr == nil && currentHash == tx.BeforeSHA256:
		if err := writeAtomic(filepath.Join(space.ThreadDir(thread), ThreadConfigFile), []byte(tx.AfterConfig)); err != nil {
			return fmt.Errorf("recover entry %s floor advance: %w", tx.Entry, err)
		}
		return finishEntryTransaction(space, thread, tx, "committed")
	case entryErr == nil && currentHash == tx.AfterSHA256:
		return finishEntryTransaction(space, thread, tx, "committed")
	case entryErr != nil:
		return fmt.Errorf("inspect entry %s during recovery: %w", tx.Entry, entryErr)
	default:
		return fmt.Errorf("cannot recover entry %s: thread config matches neither the before nor after state", tx.Entry)
	}
}

func clearPendingTransaction(space *Space, thread Name) error {
	path := transactionPendingPath(space, thread)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if dir, err := os.Open(filepath.Dir(path)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

func terminalExists(space *Space, thread Name, entry string) bool {
	for _, outcome := range []string{"committed", "aborted"} {
		if _, err := os.Stat(transactionTerminalPath(space, thread, entry, outcome)); err == nil {
			return true
		}
	}
	return false
}
