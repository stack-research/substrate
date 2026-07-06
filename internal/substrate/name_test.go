package substrate

import "testing"

func TestNames(t *testing.T) {
	for _, raw := range []string{
		"user-name", "claude-a", "codex-b", "4o", "a", "x-1-y",
		"claude/opus-4.8", "cursor/glm-5.2", "x.y",
	} {
		if _, err := ParseName(raw); err != nil {
			t.Fatalf("%q should be valid: %v", raw, err)
		}
	}
	for _, raw := range []string{
		"", "Pat", "claude_a", "-bob", ".bob", "bob!", "bob bob", "café",
		"../x", "a//b", "a__b", "/x", "x/", ".x", "x/..", "x.", "x-", "line\nbreak",
	} {
		if _, err := ParseName(raw); err == nil {
			t.Fatalf("%q should be invalid", raw)
		}
	}
	if _, err := ParseName(string(make([]byte, MaxNameLen+1))); err == nil {
		t.Fatal("long name should be invalid")
	}
}

func TestNamePathRoundTrip(t *testing.T) {
	name := MustName("claude/opus-4.8")
	component := name.ToPathComponent()
	if component != "claude%2Fopus-4.8" {
		t.Fatalf("unexpected component: %s", component)
	}
	got, err := NameFromPathComponent(component)
	if err != nil || got != name {
		t.Fatalf("round trip: got %q, %v", got, err)
	}
	for _, bad := range []string{"claude%2fopus-4.8", "bad/path", "bad%name"} {
		if _, err := NameFromPathComponent(bad); err == nil {
			t.Fatalf("%q should be invalid", bad)
		}
	}
}
