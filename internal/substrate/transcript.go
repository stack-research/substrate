package substrate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Window selects part of a transcript. LastN and FromLine are the legacy
// stable line windows. FromEntry and ThroughEntry select complete immutable
// entries; they may be used independently or together. A zero Window means
// the whole transcript.
type Window struct {
	LastN        *int
	FromLine     *int
	FromEntry    string
	ThroughEntry string
}

// ManifestEntry is deterministic metadata for one visible transcript entry.
// ByteLength and SHA256 describe the immutable entry file, not rendered text.
type ManifestEntry struct {
	Filename   string    `json:"filename"`
	Author     Name      `json:"author"`
	Timestamp  time.Time `json:"timestamp"`
	StartLine  int       `json:"start_line"`
	EndLine    int       `json:"end_line"`
	ByteLength int       `json:"byte_length"`
	SHA256     string    `json:"sha256"`
}

// TranscriptManifest indexes the visible entries captured by one directory
// snapshot. Version counts every valid entry file, including hidden no-ops.
type TranscriptManifest struct {
	Thread         Name            `json:"thread"`
	Version        int             `json:"thread_version"`
	TotalLines     int             `json:"total_lines"`
	VisibleEntries int             `json:"visible_entries"`
	NoOpEntries    int             `json:"no_op_entries"`
	OmissionPolicy string          `json:"omission_policy"`
	Entries        []ManifestEntry `json:"entries"`
}

// TranscriptRead is a reproducible transcript result plus the actual range
// selected from its captured manifest. Empty range fields mean no visible
// entry was returned.
type TranscriptRead struct {
	Text             string
	Manifest         TranscriptManifest
	FirstEntry       string
	LastEntry        string
	StartLine        int
	EndLine          int
	ByteLength       int
	NextEntry        string
	LegacyLineWindow bool
}

// RenderTranscript renders entries as attributed plain text, one block each.
func RenderTranscript(entries []Entry) string {
	var out strings.Builder
	for _, entry := range entries {
		fmt.Fprintf(&out, "[%s @ %s]\n", entry.Meta.Author, entry.Meta.Timestamp.UTC().Format("2006-01-02T15:04:05Z"))
		out.WriteString(entry.Body)
		out.WriteString("\n\n")
	}
	return out.String()
}

// ApplyWindow trims text to a legacy line window and returns it with the total
// line count, which callers use as the next read cursor.
func ApplyWindow(text string, window Window) (string, int) {
	if text == "" {
		return "", 0
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	total := len(lines)
	start := 0
	end := total
	if window.LastN != nil {
		n := max(*window.LastN, 0)
		start = max(total-n, 0)
	}
	if window.FromLine != nil {
		start = min(max(*window.FromLine-1, 0), total)
	}
	return strings.Join(lines[start:end], "\n"), total
}

type transcriptSnapshot struct {
	manifest TranscriptManifest
	entries  []Entry
}

func loadTranscriptSnapshot(space *Space, thread Name) (transcriptSnapshot, error) {
	if _, _, err := loadThreadFile(space, thread); err != nil {
		return transcriptSnapshot{}, err
	}
	var snapshot transcriptSnapshot
	err := withFileLock(filepath.Join(space.ThreadDir(thread), ".turn.lock"), func() error {
		if err := recoverEntryTransactionsLocked(space, thread); err != nil {
			return err
		}
		loaded, err := loadTranscriptSnapshotLocked(space, thread)
		snapshot = loaded
		return err
	})
	return snapshot, err
}

func loadTranscriptSnapshotLocked(space *Space, thread Name) (transcriptSnapshot, error) {
	dirEntries, err := os.ReadDir(space.ThreadDir(thread))
	if err != nil {
		return transcriptSnapshot{}, err
	}
	filenames := make([]string, 0, len(dirEntries))
	for _, entry := range dirEntries {
		filenames = append(filenames, entry.Name())
	}
	sort.Strings(filenames)

	snapshot := transcriptSnapshot{manifest: TranscriptManifest{
		Thread: thread, OmissionPolicy: "exact pass, no-op, and ... entries are counted in thread_version but omitted from the rendered transcript and visible manifest",
	}}
	line := 1
	for _, filename := range filenames {
		_, _, noOp, valid := ParseEntryFilename(filename)
		if !valid {
			continue
		}
		snapshot.manifest.Version++
		if noOp {
			snapshot.manifest.NoOpEntries++
			continue
		}
		data, err := os.ReadFile(filepath.Join(space.ThreadDir(thread), filename))
		if err != nil {
			return transcriptSnapshot{}, err
		}
		entry, ok := parseEntryFile(filename, data)
		if !ok {
			continue
		}
		_, count := ApplyWindow(RenderTranscript([]Entry{entry}), Window{})
		digest := sha256.Sum256(data)
		snapshot.entries = append(snapshot.entries, entry)
		snapshot.manifest.Entries = append(snapshot.manifest.Entries, ManifestEntry{
			Filename: filename, Author: entry.Meta.Author, Timestamp: entry.Meta.Timestamp,
			StartLine: line, EndLine: line + count - 1, ByteLength: len(data), SHA256: hex.EncodeToString(digest[:]),
		})
		line += count
	}
	snapshot.manifest.VisibleEntries = len(snapshot.entries)
	if line > 1 {
		snapshot.manifest.TotalLines = line - 1
	}
	return snapshot, nil
}

// BuildTranscriptManifest captures a deterministic visible-entry index.
func BuildTranscriptManifest(space *Space, thread Name) (TranscriptManifest, error) {
	snapshot, err := loadTranscriptSnapshot(space, thread)
	return snapshot.manifest, err
}

// ReadTranscriptSnapshot returns a transcript window and mechanical metadata
// describing the exact snapshot and range that were returned.
func ReadTranscriptSnapshot(space *Space, thread Name, window Window) (TranscriptRead, error) {
	if window.LastN != nil && window.FromLine != nil {
		return TranscriptRead{}, errors.New("last lines and from line are mutually exclusive")
	}
	entryWindow := window.FromEntry != "" || window.ThroughEntry != ""
	if entryWindow && (window.LastN != nil || window.FromLine != nil) {
		return TranscriptRead{}, errors.New("entry cursors and line windows are mutually exclusive")
	}
	snapshot, err := loadTranscriptSnapshot(space, thread)
	if err != nil {
		return TranscriptRead{}, err
	}
	result := TranscriptRead{Manifest: snapshot.manifest, LegacyLineWindow: !entryWindow && (window.LastN != nil || window.FromLine != nil)}
	if !entryWindow {
		result.Text, _ = ApplyWindow(RenderTranscript(snapshot.entries), window)
		result.ByteLength = len([]byte(result.Text))
		if len(snapshot.entries) > 0 && !result.LegacyLineWindow {
			result.FirstEntry = snapshot.entries[0].Filename
			result.LastEntry = snapshot.entries[len(snapshot.entries)-1].Filename
			result.StartLine = snapshot.manifest.Entries[0].StartLine
			result.EndLine = snapshot.manifest.Entries[len(snapshot.entries)-1].EndLine
		}
		return result, nil
	}

	start, end := 0, len(snapshot.entries)-1
	if window.FromEntry != "" {
		start = entryIndex(snapshot.entries, window.FromEntry)
		if start < 0 {
			return TranscriptRead{}, fmt.Errorf("from entry %q is not a visible entry in thread %q", window.FromEntry, thread)
		}
	}
	if window.ThroughEntry != "" {
		end = entryIndex(snapshot.entries, window.ThroughEntry)
		if end < 0 {
			return TranscriptRead{}, fmt.Errorf("through entry %q is not a visible entry in thread %q", window.ThroughEntry, thread)
		}
	}
	if start > end {
		return TranscriptRead{}, fmt.Errorf("from entry %q follows through entry %q", window.FromEntry, window.ThroughEntry)
	}
	if len(snapshot.entries) == 0 {
		return result, nil
	}
	selected := snapshot.entries[start : end+1]
	result.Text, _ = ApplyWindow(RenderTranscript(selected), Window{})
	result.ByteLength = len([]byte(result.Text))
	result.FirstEntry = selected[0].Filename
	result.LastEntry = selected[len(selected)-1].Filename
	result.StartLine = snapshot.manifest.Entries[start].StartLine
	result.EndLine = snapshot.manifest.Entries[end].EndLine
	if end+1 < len(snapshot.entries) {
		result.NextEntry = snapshot.entries[end+1].Filename
	}
	return result, nil
}

func entryIndex(entries []Entry, filename string) int {
	for i := range entries {
		if entries[i].Filename == filename {
			return i
		}
	}
	return -1
}

// ReadTranscript loads a thread's entries and renders the windowed transcript.
// It is retained for line-cursor compatibility; new callers that need a
// reproducible offer should use ReadTranscriptSnapshot.
func ReadTranscript(space *Space, thread Name, window Window) (string, int, error) {
	result, err := ReadTranscriptSnapshot(space, thread, window)
	if err != nil {
		return "", 0, err
	}
	return result.Text, result.Manifest.TotalLines, nil
}
