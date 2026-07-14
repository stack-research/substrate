package substrate

import (
	"fmt"
	"strings"
)

// Window selects part of a transcript: the last N lines, or everything from a
// stable 1-based line cursor. A zero Window means the whole transcript.
type Window struct {
	LastN    *int
	FromLine *int
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

// ApplyWindow trims text to the window and returns it with the total line
// count, which callers use as the next read cursor.
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

// ReadTranscript loads a thread's entries and renders the windowed transcript.
func ReadTranscript(space *Space, thread Name, window Window) (string, int, error) {
	entries, err := LoadEntries(space, thread)
	if err != nil {
		return "", 0, err
	}
	text, total := ApplyWindow(RenderTranscript(entries), window)
	return text, total, nil
}
