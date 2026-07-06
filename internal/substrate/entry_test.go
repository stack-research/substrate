package substrate

import (
	"strings"
	"testing"
	"time"
)

func TestEntryFilenameAndFileRoundTrip(t *testing.T) {
	timestamp := time.Date(2026, 2, 3, 4, 5, 6, 123_000_000, time.UTC)
	author := MustName("claude/opus-4.8")
	filename := EntryFilename(timestamp, author, false)
	if filename != "20260203T040506123Z__claude%2Fopus-4.8.md" {
		t.Fatalf("unexpected filename: %s", filename)
	}
	parsedTime, parsedAuthor, noOp, ok := ParseEntryFilename(filename)
	if !ok || noOp || parsedAuthor != author || !parsedTime.Equal(timestamp) {
		t.Fatalf("bad parse: %v %q %v %v", parsedTime, parsedAuthor, noOp, ok)
	}
	body := "First line.\n\nSecond paragraph with --- inside."
	data, err := renderEntryFile(EntryMeta{Author: author, Timestamp: timestamp}, body)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := parseEntryFile(filename, data)
	if !ok || entry.Body != body || entry.Meta.Author != author {
		t.Fatalf("bad entry: %#v", entry)
	}
	if !strings.HasPrefix(string(data), "---\n") {
		t.Fatal("entry should have frontmatter")
	}
}

func TestNoOpDetection(t *testing.T) {
	for _, yes := range []string{"no-op", "pass", "...", " PASS ", "No-Op", "...\n"} {
		if !IsNoOp(yes) {
			t.Fatalf("%q should be a no-op", yes)
		}
	}
	for _, no := range []string{"I'll pass", "no op", "…", "pass the salt", ""} {
		if IsNoOp(no) {
			t.Fatalf("%q should not be a no-op", no)
		}
	}
}

func TestWindowBoundaries(t *testing.T) {
	last := 2
	from := 3
	if got, total := ApplyWindow("a\nb\nc\nd\n", Window{LastN: &last}); got != "c\nd" || total != 4 {
		t.Fatalf("last: %q %d", got, total)
	}
	if got, total := ApplyWindow("a\nb\nc\nd\n", Window{FromLine: &from}); got != "c\nd" || total != 4 {
		t.Fatalf("from: %q %d", got, total)
	}
	if got, total := ApplyWindow("", Window{}); got != "" || total != 0 {
		t.Fatalf("empty: %q %d", got, total)
	}
}
