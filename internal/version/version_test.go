package version

import "testing"

func TestFormatBuildIdentity(t *testing.T) {
	if got := format("0.2.0-dev", "775CC588F9E206492080C9E593AECE7B50FD621D", true); got != "0.2.0-dev+775cc588f9e2.dirty" {
		t.Fatalf("dirty build = %q", got)
	}
	if got := format("0.2.0", "abc123", false); got != "0.2.0+abc123" {
		t.Fatalf("clean build = %q", got)
	}
	if got := format("0.2.0-dev", "", true); got != "0.2.0-dev" {
		t.Fatalf("unknown revision = %q", got)
	}
}
