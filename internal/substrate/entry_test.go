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

func TestEntryAlignedSnapshotAndManifest(t *testing.T) {
	space := groupSpace(t)
	thread := MustName("bounded")
	moderator := MustName("user-name")
	agent := MustName("claude-a")
	if _, err := CreateThread(space, thread, "bounded reads", moderator, []Name{agent}); err != nil {
		t.Fatal(err)
	}
	first, err := WriteEntry(space, thread, moderator, "first line\nsecond line")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := WriteEntry(space, thread, agent, "pass"); err != nil {
		t.Fatal(err)
	}
	last, err := WriteEntry(space, thread, moderator, "final entry")
	if err != nil {
		t.Fatal(err)
	}

	manifest, err := BuildTranscriptManifest(space, thread)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Version != 3 || manifest.VisibleEntries != 2 || manifest.NoOpEntries != 1 {
		t.Fatalf("manifest counts: %#v", manifest)
	}
	if manifest.Entries[0].Filename != first.Filename || manifest.Entries[1].Filename != last.Filename {
		t.Fatalf("manifest order: %#v", manifest.Entries)
	}
	if manifest.Entries[0].StartLine != 1 || manifest.Entries[0].EndLine != 4 || manifest.Entries[1].StartLine != 5 {
		t.Fatalf("manifest lines: %#v", manifest.Entries)
	}
	if manifest.Entries[0].ByteLength == 0 || len(manifest.Entries[0].SHA256) != 64 {
		t.Fatalf("manifest identity: %#v", manifest.Entries[0])
	}

	read, err := ReadTranscriptSnapshot(space, thread, Window{FromEntry: first.Filename, ThroughEntry: first.Filename})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read.Text, "first line") || strings.Contains(read.Text, "final entry") {
		t.Fatalf("bounded transcript: %q", read.Text)
	}
	if read.FirstEntry != first.Filename || read.LastEntry != first.Filename || read.NextEntry != last.Filename || read.StartLine != 1 || read.EndLine != 4 {
		t.Fatalf("bounded metadata: %#v", read)
	}
	if read.Manifest.Version != 3 || read.ByteLength != len([]byte(read.Text)) {
		t.Fatalf("snapshot metadata: %#v", read)
	}

	read, err = ReadTranscriptSnapshot(space, thread, Window{FromEntry: last.Filename})
	if err != nil || strings.Contains(read.Text, "first line") || !strings.Contains(read.Text, "final entry") || read.NextEntry != "" {
		t.Fatalf("tail read: %#v err=%v", read, err)
	}
	if _, err := ReadTranscriptSnapshot(space, thread, Window{FromEntry: "missing.md"}); err == nil {
		t.Fatal("missing entry cursor should fail visibly")
	}
}
