// Package proxy exposes one substrate space to URL-only participants. It is a
// transport adapter: every request re-reads the filesystem and every write goes
// through the same turn engine as the TUI, CLI, and MCP server.
package proxy

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/stack-research/substrate/internal/substrate"
)

type Participant struct {
	Name substrate.Name
	Key  string
}

func RandomKey() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("secure random capability key: %w", err)
	}
	return fmt.Sprintf("%x", bytes), nil
}

func BriefText(space *substrate.Space, thread substrate.Name, forName *substrate.Name, window substrate.Window, urls ...string) (string, error) {
	status, err := substrate.GetTurnStatus(space, thread)
	if err != nil {
		return "", err
	}
	read, err := substrate.ReadTranscriptSnapshot(space, thread, window)
	if err != nil {
		return "", err
	}
	transcript := read.Text
	totalLines := read.Manifest.TotalLines
	fromLine := 1
	if window.FromLine != nil {
		fromLine = *window.FromLine
	} else if read.StartLine > 0 {
		fromLine = read.StartLine
	}
	nextLine := totalLines + 1
	if (window.FromEntry != "" || window.ThroughEntry != "") && read.EndLine > 0 {
		nextLine = read.EndLine + 1
	}
	version := read.Manifest.Version
	participant := "not specified"
	if forName != nil {
		participant = forName.String()
	}
	yours := ""
	if forName != nil {
		switch {
		case status.Status == substrate.Ended:
			yours = " (thread ended)"
		case *forName == status.Current:
			yours = " (you - reply now)"
		default:
			yours = " (not you - wait)"
		}
	}
	order := make([]string, 0, len(status.TurnOrder))
	for _, name := range status.TurnOrder {
		label := name.String()
		if name == status.Moderator {
			label += " [moderator]"
		}
		order = append(order, label)
	}
	var out strings.Builder
	fmt.Fprintf(&out, "SUBSTRATE THREAD\n================\nparticipant: %s\nthread: %s\ntopic: %s\n", participant, thread, status.Topic)
	fmt.Fprintf(&out, "conversation: turn-based group; append-only markdown addressed to everyone\nstatus: %s\n", status.Status.Title())
	fmt.Fprintf(&out, "current turn: %s%s\nturn order: %s\ntranscript lines: %d\nshowing from line: %d\nnext read from line: %d\nthread version: %d\n", status.Current, yours, strings.Join(order, " -> "), totalLines, fromLine, nextLine, version)
	if read.FirstEntry != "" {
		fmt.Fprintf(&out, "actual entries: %s through %s\nactual lines: %d through %d\nbytes returned: %d\n", read.FirstEntry, read.LastEntry, read.StartLine, read.EndLine, read.ByteLength)
		if read.NextEntry != "" {
			fmt.Fprintf(&out, "next entry: %s\n", read.NextEntry)
		} else {
			out.WriteString("next entry: (caught up at captured snapshot)\n")
		}
	}
	if len(urls) == 2 {
		fmt.Fprintf(&out, "\nIMPORTANT: USE A NEW NONCE FOR EVERY REQUEST\n============================================\nBefore EVERY fetch - read or write - replace NONCE with a new random ASCII value. Never reuse a nonce, including for a retry. Reusing one can return an old cached page. The nonce defeats caches; it is not the thread version.\n\nTo read only lines added after this response, fetch this path on the same host as this page:\n%s&from=%d&nonce=NONCE\n\nThe from= value is a stable 1-based transcript line cursor. Keep the newest 'next read from line' value. Omit from= only when you intentionally need the full thread again.\n", urls[0], nextLine)
	}
	fmt.Fprintf(&out, "\nTRANSCRIPT FROM LINE %d (no-op turns omitted)\n=============================================\n", fromLine)
	out.WriteString(transcript)
	if transcript != "" && !strings.HasSuffix(transcript, "\n") {
		out.WriteByte('\n')
	}
	out.WriteString("END TRANSCRIPT\n")
	if len(urls) == 2 {
		out.WriteString("\nYOUR NEXT ACTION\n================\n")
		switch {
		case forName != nil && status.Status == substrate.Ended:
			out.WriteString("This thread has ended. Do not write another entry.\n")
		case forName != nil && *forName == status.Current:
			fmt.Fprintf(&out, "You have the turn. Compose markdown addressed to the whole thread, keep it under about 6KB, and encode it as URL-safe Base64 without padding. Copy the thread version above into turn=, copy the next read cursor into from=, replace NONCE with a brand-new random ASCII value, then fetch:\n%s&turn=%d&from=%d&nonce=NONCE&b64=URL_SAFE_BASE64_REPLY\n\nFor a short URL-encoded reply, replace b64=... with text=URL_ENCODED_REPLY. Send text=pass to yield without a visible entry. The response will contain only transcript lines added since this read.\n", urls[1], version, nextLine)
		default:
			fmt.Fprintf(&out, "Do not write now; the turn belongs to %s. Check the incremental read path above again with a new nonce.\n", status.Current)
		}
	}
	return out.String(), nil
}

func NewHandler(space *substrate.Space, participants []Participant) http.Handler {
	byKey := make(map[string]Participant, len(participants))
	for _, participant := range participants {
		byKey[participant.Key] = participant
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, no-cache, max-age=0, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		participant, ok := byKey[r.URL.Query().Get("key")]
		if !ok {
			http.Error(w, "missing or unknown key", http.StatusForbidden)
			return
		}
		thread, write, ok := route(r.URL)
		if !ok {
			http.Error(w, "routes: /t/<thread>  /t/<thread>/write", http.StatusNotFound)
			return
		}
		if write {
			handleWrite(w, r, space, thread, participant)
			return
		}
		window, err := readWindow(r.URL.Query())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.URL.Query().Get("manifest") == "1" {
			manifest, err := substrate.BuildTranscriptManifest(space, thread)
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			encoder := json.NewEncoder(w)
			encoder.SetIndent("", "  ")
			_ = encoder.Encode(manifest)
			return
		}
		readURL, writeURL := participantURLs(thread, participant)
		text, err := BriefText(space, thread, &participant.Name, window, readURL, writeURL)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(text))
	})
}

func participantURLs(thread substrate.Name, participant Participant) (readURL, writeURL string) {
	base := "/t/" + url.PathEscape(thread.String())
	key := url.QueryEscape(participant.Key)
	return base + "?key=" + key, base + "/write?key=" + key
}

func readWindow(query url.Values) (substrate.Window, error) {
	window := substrate.Window{}
	if query.Has("from") {
		raw := strings.TrimSpace(query.Get("from"))
		from, err := strconv.Atoi(raw)
		if err != nil || from < 1 {
			return substrate.Window{}, fmt.Errorf("invalid from=%q: use a 1-based transcript line such as from=1", raw)
		}
		window.FromLine = &from
	}
	if query.Has("from_entry") {
		window.FromEntry = strings.TrimSpace(query.Get("from_entry"))
		if window.FromEntry == "" {
			return substrate.Window{}, errors.New("invalid empty from_entry: pass an immutable entry filename from the manifest")
		}
	}
	if query.Has("through_entry") {
		window.ThroughEntry = strings.TrimSpace(query.Get("through_entry"))
		if window.ThroughEntry == "" {
			return substrate.Window{}, errors.New("invalid empty through_entry: pass an immutable entry filename from the manifest")
		}
	}
	if window.FromLine != nil && (window.FromEntry != "" || window.ThroughEntry != "") {
		return substrate.Window{}, errors.New("from line and entry cursors are mutually exclusive")
	}
	return window, nil
}

func route(u *url.URL) (substrate.Name, bool, bool) {
	path := u.EscapedPath()
	if !strings.HasPrefix(path, "/t/") {
		return "", false, false
	}
	rest := strings.TrimPrefix(path, "/t/")
	write := strings.HasSuffix(rest, "/write")
	if write {
		rest = strings.TrimSuffix(rest, "/write")
	}
	decoded, err := url.PathUnescape(rest)
	if err != nil {
		return "", false, false
	}
	thread, err := substrate.ParseName(decoded)
	return thread, write, err == nil
}

func handleWrite(w http.ResponseWriter, r *http.Request, space *substrate.Space, thread substrate.Name, participant Participant) {
	window, windowErr := readWindow(r.URL.Query())
	if windowErr != nil {
		http.Error(w, windowErr.Error(), http.StatusBadRequest)
		return
	}
	refreshed := func(outcome string) string {
		readURL, writeURL := participantURLs(thread, participant)
		brief, err := BriefText(space, thread, &participant.Name, window, readURL, writeURL)
		if err != nil {
			brief, _ = BriefText(space, thread, &participant.Name, substrate.Window{}, readURL, writeURL)
		}
		return outcome + "\n\n" + brief
	}
	query := r.URL.Query()
	content := query.Get("text")
	if encoded, present := query["b64"]; present && len(encoded) > 0 {
		decoded, err := decodeBase64(encoded[0])
		if err != nil {
			writePage(w, "substrate: could not decode your reply", refreshed("the b64 parameter did not decode. Re-encode your reply, or use &text= with percent-encoding instead."))
			return
		}
		content = decoded
	}
	if content == "" && !query.Has("text") {
		writePage(w, "substrate: missing reply", refreshed("pass your reply as &b64=… or &text=…"))
		return
	}
	if rawVersion := query.Get("turn"); rawVersion != "" {
		version, err := strconv.Atoi(rawVersion)
		current := substrate.ThreadVersion(space, thread)
		if err != nil || version != current {
			writePage(w, "substrate: thread changed — entry NOT recorded", refreshed(fmt.Sprintf("someone wrote since you read (version is now %d, you replied to %s). Read the refreshed thread, then resend.", current, rawVersion)))
			return
		}
	}
	written, err := substrate.WriteEntry(space, thread, participant.Name, content)
	if err == nil {
		noOp := ""
		if written.NoOp {
			noOp = " as a no-op"
		}
		paused := ""
		if written.Paused {
			paused = " (moderator — the thread is paused)"
		}
		writePage(w, "substrate: entry recorded", refreshed(fmt.Sprintf("recorded%s — next turn: %s%s", noOp, written.Next, paused)))
		return
	}
	var notTurn *substrate.NotYourTurnError
	switch {
	case errors.As(err, &notTurn):
		writePage(w, "substrate: not your turn — entry NOT recorded", refreshed(err.Error()+". Wait, then fetch the thread page again."))
	case errors.Is(err, substrate.ErrEnded):
		writePage(w, "substrate: thread has ended", err.Error()+" — no further entries are possible.")
	default:
		writePage(w, "substrate: rejected", refreshed(err.Error()))
	}
}

func writePage(w http.ResponseWriter, title, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "<!DOCTYPE html><html><head><meta charset=\"utf-8\"><title>%s</title></head><body><h1>%s</h1><pre>%s</pre></body></html>", html.EscapeString(title), html.EscapeString(title), html.EscapeString(body))
}

func decodeBase64(input string) (string, error) {
	clean := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
			return -1
		}
		return r
	}, input)
	clean = strings.TrimRight(clean, "=")
	decoded, err := base64.RawURLEncoding.DecodeString(clean)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(clean)
	}
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}
