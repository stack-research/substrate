package proxy

import (
	"encoding/base64"
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
