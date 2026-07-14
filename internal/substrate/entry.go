package substrate

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const timestampBaseLayout = "20060102T150405"

// EntryMeta is the YAML front matter of an entry file.
type EntryMeta struct {
	Author    Name      `yaml:"author"`
	Timestamp time.Time `yaml:"timestamp"`
}

// Entry is one turn's markdown entry, loaded from its file in the thread dir.
type Entry struct {
	Meta     EntryMeta
	Body     string
	NoOp     bool
	Filename string
}

// IsNoOp reports whether content is a hidden pass ("pass", "no-op", or "...").
func IsNoOp(content string) bool {
	switch strings.ToLower(strings.TrimSpace(content)) {
	case "no-op", "pass", "...":
		return true
	default:
		return false
	}
}

func FormatTimestamp(t time.Time) string {
	t = t.UTC()
	return fmt.Sprintf("%s%03dZ", t.Format(timestampBaseLayout), t.Nanosecond()/int(time.Millisecond))
}

func ParseTimestamp(raw string) (time.Time, error) {
	if len(raw) != 19 || raw[18] != 'Z' {
		return time.Time{}, fmt.Errorf("invalid timestamp %q", raw)
	}
	base, err := time.Parse(timestampBaseLayout, raw[:15])
	if err != nil {
		return time.Time{}, err
	}
	millis, err := strconv.Atoi(raw[15:18])
	if err != nil {
		return time.Time{}, err
	}
	return base.Add(time.Duration(millis) * time.Millisecond), nil
}

func EntryFilename(t time.Time, author Name, noOp bool) string {
	if noOp {
		return fmt.Sprintf("%s__%s__no-op.md", FormatTimestamp(t), author.ToPathComponent())
	}
	return fmt.Sprintf("%s__%s.md", FormatTimestamp(t), author.ToPathComponent())
}

func ParseEntryFilename(filename string) (time.Time, Name, bool, bool) {
	if !strings.HasSuffix(filename, ".md") {
		return time.Time{}, "", false, false
	}
	parts := strings.Split(strings.TrimSuffix(filename, ".md"), "__")
	noOp := false
	if len(parts) == 3 && parts[2] == "no-op" {
		noOp = true
	} else if len(parts) != 2 {
		return time.Time{}, "", false, false
	}
	timestamp, err := ParseTimestamp(parts[0])
	if err != nil {
		return time.Time{}, "", false, false
	}
	author, err := NameFromPathComponent(parts[1])
	if err != nil {
		return time.Time{}, "", false, false
	}
	return timestamp, author, noOp, true
}

func renderEntryFile(meta EntryMeta, body string) ([]byte, error) {
	data, err := yaml.Marshal(meta)
	if err != nil {
		return nil, err
	}
	return []byte("---\n" + string(data) + "---\n\n" + strings.TrimSpace(body) + "\n"), nil
}

func parseEntryFile(filename string, data []byte) (Entry, bool) {
	timestamp, author, noOp, ok := ParseEntryFilename(filename)
	if !ok {
		return Entry{}, false
	}
	body := stripFrontmatter(string(data))
	return Entry{
		Meta: EntryMeta{Author: author, Timestamp: timestamp},
		Body: strings.TrimSpace(body), NoOp: noOp, Filename: filename,
	}, true
}

func stripFrontmatter(content string) string {
	if !strings.HasPrefix(content, "---\n") {
		return content
	}
	rest := strings.TrimPrefix(content, "---\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return content
	}
	after := rest[end+4:]
	if after == "" {
		return ""
	}
	if strings.HasPrefix(after, "\n") {
		return strings.TrimPrefix(after, "\n")
	}
	return content
}

func nextTimestamp(dir string) (time.Time, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return time.Time{}, err
	}
	var last time.Time
	for _, entry := range entries {
		t, _, _, ok := ParseEntryFilename(entry.Name())
		if ok && t.After(last) {
			last = t
		}
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	if !last.IsZero() && !now.After(last) {
		return last.Add(time.Millisecond), nil
	}
	return now, nil
}

// LoadEntries reads a thread's visible entries in timestamp order, skipping
// no-op turns and unparseable files.
func LoadEntries(space *Space, thread Name) ([]Entry, error) {
	if _, err := LoadThread(space, thread); err != nil {
		return nil, err
	}
	dir := space.ThreadDir(thread)
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	filenames := make([]string, 0, len(dirEntries))
	for _, entry := range dirEntries {
		filenames = append(filenames, entry.Name())
	}
	sort.Strings(filenames)
	entries := make([]Entry, 0, len(filenames))
	for _, filename := range filenames {
		data, err := os.ReadFile(filepath.Join(dir, filename))
		if err != nil {
			continue
		}
		entry, ok := parseEntryFile(filename, data)
		if ok && !entry.NoOp {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

// ThreadVersion counts a thread's entry files; it monotonically increases with
// every turn taken, so callers can use it as a cheap change detector.
func ThreadVersion(space *Space, thread Name) int {
	entries, err := os.ReadDir(space.ThreadDir(thread))
	if err != nil {
		return 0
	}
	count := 0
	for _, entry := range entries {
		if _, _, _, ok := ParseEntryFilename(entry.Name()); ok {
			count++
		}
	}
	return count
}
