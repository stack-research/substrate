package proxy

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stack-research/substrate/internal/substrate"
)

func proxySpace(t *testing.T) (*substrate.Space, substrate.Name) {
	t.Helper()
	space, err := substrate.InitSpace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, participant := range []substrate.Participant{
		{Name: substrate.MustName("user-name"), Kind: substrate.Human},
		{Name: substrate.MustName("kagi"), Kind: substrate.Agent},
	} {
		if err := space.AddParticipant(participant.Name, participant.Kind); err != nil {
			t.Fatal(err)
		}
	}
	thread := substrate.MustName("lab")
	if _, err := substrate.CreateThread(space, thread, "proxy test", substrate.MustName("user-name"), []substrate.Name{substrate.MustName("kagi")}); err != nil {
		t.Fatal(err)
	}
	return space, thread
}

func TestHandlerReadWriteAndStaleVersion(t *testing.T) {
	space, thread := proxySpace(t)
	handler := NewHandler(space, []Participant{{Name: substrate.MustName("kagi"), Key: "secret"}})

	response, body := perform(t, handler, "/t/lab?key=secret&nonce=one")
	if !strings.Contains(body, "current turn: user-name (not you - wait)") || response.Header().Get("Cache-Control") == "" {
		t.Fatalf("unexpected read:\n%s", body)
	}
	if _, err := substrate.WriteEntry(space, thread, substrate.MustName("user-name"), "opening"); err != nil {
		t.Fatal(err)
	}

	reply := base64.RawURLEncoding.EncodeToString([]byte("## Kagi\n\nA useful reply."))
	response, body = perform(t, handler, "/t/lab/write?key=secret&turn=1&nonce=two&b64="+url.QueryEscape(reply))
	if response.Code != 200 || !strings.Contains(body, "substrate: entry recorded") || !strings.Contains(body, "A useful reply") {
		t.Fatalf("unexpected write:\n%s", body)
	}

	_, body = perform(t, handler, "/t/lab/write?key=secret&turn=1&nonce=three&text=again")
	if !strings.Contains(body, "thread changed") || substrate.ThreadVersion(space, thread) != 2 {
		t.Fatalf("stale write was not rejected:\n%s", body)
	}
}

func TestUnauthorizedAndSlashThread(t *testing.T) {
	space, _ := proxySpace(t)
	thread := substrate.MustName("nested/room")
	if _, err := substrate.CreateThread(space, thread, "slash", substrate.MustName("user-name"), []substrate.Name{substrate.MustName("kagi")}); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(space, []Participant{{Name: substrate.MustName("kagi"), Key: "secret"}})

	response, _ := perform(t, handler, "/t/lab?key=wrong")
	if response.Code != 403 {
		t.Fatalf("status = %d", response.Code)
	}

	response, body := perform(t, handler, "/t/nested%2Froom?key=secret")
	if response.Code != 200 || !strings.Contains(body, "thread: nested/room") {
		t.Fatalf("slash read: %d\n%s", response.Code, body)
	}
}

func TestIncrementalReadCursorAndWriteRefresh(t *testing.T) {
	space, thread := proxySpace(t)
	handler := NewHandler(space, []Participant{{Name: substrate.MustName("kagi"), Key: "secret"}})
	if _, err := substrate.WriteEntry(space, thread, substrate.MustName("user-name"), "opening context"); err != nil {
		t.Fatal(err)
	}

	response, body := perform(t, handler, "/t/lab?key=secret&nonce=first")
	if response.Code != http.StatusOK || !strings.Contains(body, "opening context") || !strings.Contains(body, "next read from line: 4") {
		t.Fatalf("full read did not advertise its cursor:\n%s", body)
	}
	if !strings.Contains(body, "&from=4&nonce=NONCE") || !strings.Contains(body, "stable 1-based transcript line cursor") {
		t.Fatalf("incremental read instructions missing:\n%s", body)
	}

	reply := base64.RawURLEncoding.EncodeToString([]byte("incremental reply"))
	response, body = perform(t, handler, "/t/lab/write?key=secret&turn=1&from=4&nonce=second&b64="+url.QueryEscape(reply))
	if response.Code != http.StatusOK || !strings.Contains(body, "substrate: entry recorded") || !strings.Contains(body, "incremental reply") {
		t.Fatalf("incremental write response failed:\n%s", body)
	}
	if strings.Contains(body, "opening context") || !strings.Contains(body, "showing from line: 4") {
		t.Fatalf("write response repeated prior transcript:\n%s", body)
	}

	if _, err := substrate.WriteEntry(space, thread, substrate.MustName("user-name"), "moderator follow-up"); err != nil {
		t.Fatal(err)
	}
	_, total, err := substrate.ReadTranscript(space, thread, substrate.Window{})
	if err != nil {
		t.Fatal(err)
	}
	response, body = perform(t, handler, "/t/lab?key=secret&from=4&nonce=third")
	if response.Code != http.StatusOK || strings.Contains(body, "opening context") || !strings.Contains(body, "incremental reply") || !strings.Contains(body, "moderator follow-up") {
		t.Fatalf("incremental read window is wrong:\n%s", body)
	}
	if !strings.Contains(body, fmt.Sprintf("next read from line: %d", total+1)) {
		t.Fatalf("next cursor is wrong, total=%d:\n%s", total, body)
	}

	response, body = perform(t, handler, fmt.Sprintf("/t/lab?key=secret&from=%d&nonce=fourth", total+1))
	if response.Code != http.StatusOK || strings.Contains(body, "incremental reply") || strings.Contains(body, "moderator follow-up") {
		t.Fatalf("caught-up read should have an empty transcript:\n%s", body)
	}
}

func TestInvalidReadCursorIsRejected(t *testing.T) {
	space, _ := proxySpace(t)
	handler := NewHandler(space, []Participant{{Name: substrate.MustName("kagi"), Key: "secret"}})
	for _, raw := range []string{"", "0", "-1", "not-a-number"} {
		response, body := perform(t, handler, "/t/lab?key=secret&from="+url.QueryEscape(raw))
		if response.Code != http.StatusBadRequest || !strings.Contains(body, "use a 1-based transcript line") {
			t.Fatalf("from=%q: status=%d body=%s", raw, response.Code, body)
		}
	}
}

func TestBase64ToleranceAndKeys(t *testing.T) {
	original := "markdown with + and unicode ü"
	standard := base64.StdEncoding.EncodeToString([]byte(original))
	for _, encoded := range []string{standard, strings.TrimRight(standard, "="), base64.RawURLEncoding.EncodeToString([]byte(original)), standard[:4] + "\n" + standard[4:]} {
		got, err := DecodeBase64(encoded)
		if err != nil || got != original {
			t.Fatalf("decode %q = %q, %v", encoded, got, err)
		}
	}
	a, err := RandomKey()
	if err != nil {
		t.Fatal(err)
	}
	b, err := RandomKey()
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 32 || a == b {
		t.Fatalf("keys: %q %q", a, b)
	}
}

func perform(t *testing.T, handler http.Handler, target string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	request := httptest.NewRequest("GET", target, nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	result := recorder.Result()
	defer result.Body.Close()
	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatal(err)
	}
	return recorder, string(body)
}
