// Package mcpserver exposes substrate over the Model Context Protocol: one
// server, many spaces, with per-call participant identity.
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stack-research/substrate/internal/substrate"
	"github.com/stack-research/substrate/internal/version"
)

const (
	waitDefault  = 120 * time.Second
	waitMax      = 600 * time.Second
	waitFallback = 15 * time.Second
)

// ToolNames lists every tool registered by addTools; keep the two in sync
// (the integration test cross-checks the count against the live server).
var ToolNames = []string{
	"about", "check_turn", "end_thread", "invite", "list_threads", "new_thread", "quiet",
	"read_thread", "reorder_turns", "resume_thread", "set_next", "set_topic", "transcript_manifest", "wait_for_turn", "write_entry",
}

type Service struct {
	Source       SpaceSource
	DefaultActor *substrate.Name
	Logger       *slog.Logger
}

func New(source SpaceSource, defaultActor *substrate.Name, logger *slog.Logger) *Service {
	return &Service{Source: source, DefaultActor: defaultActor, Logger: logger}
}

func (s *Service) Server() *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{Name: "substrate-mcp", Version: version.Full()},
		&mcp.ServerOptions{Instructions: s.instructions(), Logger: s.Logger, Capabilities: &mcp.ServerCapabilities{}},
	)
	s.addTools(server)
	return server
}

func (s *Service) instructions() string {
	return fmt.Sprintf("Your default substrate participant is %q. Substrate is a local-first, turn-based group chalkboard. Call about for the exact loop. Call wait_for_turn before writing; timeouts mean keep waiting. Entries are append-only markdown addressed to the room. Send exactly 'pass' for a hidden no-op. Spaces (%s) are re-read on every call. Identity-bearing tools accept participant_name per call.", s.defaultActorText(), s.Source.Describe())
}

func (s *Service) defaultActorText() string {
	if s.DefaultActor == nil {
		return "(none; pass participant_name per call)"
	}
	return s.DefaultActor.String()
}

func (s *Service) loadSpace(label string) (*substrate.Space, error) {
	set, err := s.Source.Load()
	if err != nil {
		return nil, err
	}
	return set.Resolve(label)
}

func (s *Service) actor(raw string) (substrate.Name, error) {
	if strings.TrimSpace(raw) == "" && s.DefaultActor != nil {
		return *s.DefaultActor, nil
	}
	if strings.TrimSpace(raw) == "" {
		return "", errors.New("participant_name is required because this substrate-mcp server was started without --name. Pass the registered participant to act as, or start the server with --name <participant> as a default")
	}
	return substrate.ParseName(strings.TrimSpace(raw))
}

func success(text string) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
}

func reject(err error) (*mcp.CallToolResult, any, error) {
	message := err.Error()
	var notTurn *substrate.NotYourTurnError
	if errors.As(err, &notTurn) {
		message += "\n→ wait_for_turn will wake you when the floor is yours."
	}
	if errors.Is(err, substrate.ErrEnded) {
		message += "\n→ this thread is finished; no further turns anywhere in it."
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: message}}, IsError: true}, nil, nil
}

func nextMoves(yourTurn, ended bool) string {
	if ended {
		return "→ this thread is finished — nothing more to do here."
	}
	if yourTurn {
		return "→ your move: read_thread (from_line = last total + 1) to catch up, then write_entry to reply — or write_entry with exactly 'pass' to yield quietly."
	}
	return "→ not your turn: call wait_for_turn (a timeout means still waiting — call it again). You are only done with a thread when its status is Ended."
}

func (s *Service) statusText(space *substrate.Space, thread, actor substrate.Name) (string, error) {
	status, err := substrate.GetTurnStatus(space, thread)
	if err != nil {
		return "", err
	}
	_, lines, err := substrate.ReadTranscript(space, thread, substrate.Window{})
	if err != nil {
		return "", err
	}
	order := make([]string, 0, len(status.TurnOrder))
	for _, name := range status.TurnOrder {
		if name == status.Moderator {
			order = append(order, name.String()+" (moderator)")
		} else {
			order = append(order, name.String())
		}
	}
	yourTurn := status.Current == actor && status.Status == substrate.Active
	text := fmt.Sprintf("thread: %s\ntopic: %s\nstatus: %s\nparticipant: %s\ncurrent turn: %s\nyour turn: %s\npaused on moderator: %s\nturn order: %s\ntranscript lines: %d\n", thread, status.Topic, status.Status.Title(), actor, status.Current, yesNo(yourTurn), yesNo(status.Paused), strings.Join(order, " → "), lines)
	if remaining := status.Quieted[actor]; remaining > 0 {
		text += fmt.Sprintf("you are quieted for your next %d turn(s)\n", remaining)
	}
	return text + nextMoves(yourTurn, status.Status == substrate.Ended), nil
}

func (s *Service) moderator(space *substrate.Space, thread, actor substrate.Name) error {
	status, err := substrate.GetTurnStatus(space, thread)
	if err != nil {
		return err
	}
	if status.Moderator != actor {
		return fmt.Errorf("only the moderator may do that — %s moderates %q, not you (%s). You can still take part on your turn with write_entry", status.Moderator, thread, actor)
	}
	return nil
}

func (s *Service) moderatorOK(space *substrate.Space, thread, actor substrate.Name, confirmation string) (*mcp.CallToolResult, any, error) {
	status, err := s.statusText(space, thread, actor)
	if err != nil {
		return reject(err)
	}
	return success(confirmation + "\n\n" + status)
}

type aboutParams struct{}
type actorParams struct {
	ParticipantName string `json:"participant_name,omitempty" jsonschema:"registered participant; defaults to the server --name"`
}
type threadParams struct {
	Space           string `json:"space,omitempty" jsonschema:"space label; required only with several spaces"`
	Thread          string `json:"thread" jsonschema:"thread slug shown by list_threads"`
	ParticipantName string `json:"participant_name,omitempty" jsonschema:"registered participant; defaults to the server --name"`
}
type readParams struct {
	Space        string  `json:"space,omitempty"`
	Thread       string  `json:"thread"`
	LastN        *uint64 `json:"last_n,omitempty" jsonschema:"last N transcript lines; incompatible with entry cursors"`
	FromLine     *uint64 `json:"from_line,omitempty" jsonschema:"legacy 1-based transcript line; incompatible with entry cursors"`
	FromEntry    string  `json:"from_entry,omitempty" jsonschema:"immutable entry filename at which a complete-entry context offer begins"`
	ThroughEntry string  `json:"through_entry,omitempty" jsonschema:"immutable entry filename at which the captured context offer ends"`
}
type manifestParams struct {
	Space  string `json:"space,omitempty"`
	Thread string `json:"thread"`
}
type writeParams struct {
	Space           string `json:"space,omitempty"`
	Thread          string `json:"thread"`
	ParticipantName string `json:"participant_name,omitempty"`
	Content         string `json:"content"`
}
type waitParams struct {
	Space           string `json:"space,omitempty"`
	Thread          string `json:"thread"`
	ParticipantName string `json:"participant_name,omitempty"`
	TimeoutSecs     uint64 `json:"timeout_secs,omitempty"`
}
type newThreadParams struct {
	Space           string   `json:"space,omitempty"`
	ParticipantName string   `json:"participant_name,omitempty"`
	Name            string   `json:"name"`
	Topic           string   `json:"topic"`
	Moderator       string   `json:"moderator"`
	TurnOrder       []string `json:"turn_order"`
}
type modNameParams struct {
	Space           string `json:"space,omitempty"`
	Thread          string `json:"thread"`
	ParticipantName string `json:"participant_name,omitempty"`
	Name            string `json:"name"`
}
type quietParams struct {
	Space           string `json:"space,omitempty"`
	Thread          string `json:"thread"`
	ParticipantName string `json:"participant_name,omitempty"`
	Name            string `json:"name"`
	Turns           uint32 `json:"turns"`
}
type orderParams struct {
	Space           string   `json:"space,omitempty"`
	Thread          string   `json:"thread"`
	ParticipantName string   `json:"participant_name,omitempty"`
	Order           []string `json:"order"`
}
type topicParams struct {
	Space           string `json:"space,omitempty"`
	Thread          string `json:"thread"`
	ParticipantName string `json:"participant_name,omitempty"`
	Topic           string `json:"topic"`
}

func (s *Service) addTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{Name: "about", Description: "Start here: learn every room and moderator tool, why it exists, and the safe operating loop."}, s.about)
	mcp.AddTool(server, &mcp.Tool{Name: "new_thread", Description: "Create and become responsible for a moderated room; returns a compact moderator playbook."}, s.newThread)
	mcp.AddTool(server, &mcp.Tool{Name: "list_threads", Description: "Discover exact thread slugs, spaces, lifecycle state, and who currently owns the floor."}, s.listThreads)
	mcp.AddTool(server, &mcp.Tool{Name: "read_thread", Description: "Read immutable testimony; entry cursors create reproducible bounded context offers while line cursors support incremental catch-up."}, s.readThread)
	mcp.AddTool(server, &mcp.Tool{Name: "transcript_manifest", Description: "Get the mechanical entry index, hashes, line ranges, and captured version needed to assemble or verify a bounded context offer."}, s.transcriptManifest)
	mcp.AddTool(server, &mcp.Tool{Name: "write_entry", Description: "Append testimony only while you own the floor; exact pass, no-op, or ... yields without visible transcript growth."}, s.writeEntry)
	mcp.AddTool(server, &mcp.Tool{Name: "check_turn", Description: "Re-read filesystem truth immediately before acting: floor, pause, order, and transcript size."}, s.checkTurn)
	mcp.AddTool(server, &mcp.Tool{Name: "wait_for_turn", Description: "Block efficiently until your floor; a timeout is not completion, so wait again until your turn or Ended."}, s.waitForTurn)
	mcp.AddTool(server, &mcp.Tool{Name: "set_next", Description: "Moderator only: route the next expensive invocation to the participant suited to the current assignment."}, s.setNext)
	mcp.AddTool(server, &mcp.Tool{Name: "invite", Description: "Moderator only: add a participant to the shared room and speaking order; unknown names become registered agents."}, s.invite)
	mcp.AddTool(server, &mcp.Tool{Name: "quiet", Description: "Moderator only: skip a participant for a bounded number of future rounds without deleting membership or history."}, s.quiet)
	mcp.AddTool(server, &mcp.Tool{Name: "reorder_turns", Description: "Moderator only: replace the recurring speaking order while preserving moderator-first review pauses."}, s.reorderTurns)
	mcp.AddTool(server, &mcp.Tool{Name: "set_topic", Description: "Moderator only: update the room's current public scope without rewriting prior testimony."}, s.setTopic)
	mcp.AddTool(server, &mcp.Tool{Name: "end_thread", Description: "Moderator only: close the room to further entries while preserving its complete append-only history."}, s.endThread)
	mcp.AddTool(server, &mcp.Tool{Name: "resume_thread", Description: "Moderator only: reopen an ended room on the moderator for an explicit new phase."}, s.resumeThread)
}

func (s *Service) about(context.Context, *mcp.CallToolRequest, aboutParams) (*mcp.CallToolResult, any, error) {
	spaces := "(none yet — a moderator creates one with `substrate init`)"
	if set, err := s.Source.Load(); err == nil && len(set.Spaces) > 0 {
		spaces = strings.Join(set.Labels(), ", ")
	}
	return success(fmt.Sprintf("# substrate — shared append-only rooms\n\nserver version: %s\nruntime: %s\nadvertised tools: %s\n\nDefault participant: %s. Spaces: %s. Entries address the whole room; runtime filenames own identity. Call about whenever this fixed control contract is unfamiliar. new_thread creates a room through the domain engine and returns the moderator's next steps.\n\n## Participant loop\n1. list_threads finds the exact slug, lifecycle, and floor.\n2. wait_for_turn blocks efficiently; A TIMEOUT MEANS STILL WAITING.\n3. check_turn re-reads filesystem truth immediately before acting.\n4. read_thread catches up. Use from_line for a small live delta, or from_entry + through_entry for a reproducible complete-entry offer.\n5. write_entry appends your reply; exact pass yields invisibly. Repeat until Ended.\n\n## Moderator playbook\nA moderator schedules attention, not truth. Use transcript_manifest to identify immutable entries and hashes; write an explicit assignment plus bounded context coordinates; then set_next to route the floor. The returned range proves only what was offered, never comprehension. Use invite for membership, quiet for bounded skips, reorder_turns for recurring order, set_topic for current scope, end_thread to close, and resume_thread to begin an explicit new phase. Keep context selection distinct from floor routing and from proxy turn/version stale-write protection.\n\n## Why the read tools differ\nfrom_line is a lightweight cursor into the growing rendered transcript. Entry cursors preserve whole immutable entries and through_entry freezes the offer ceiling. transcript_manifest is mechanical metadata, not a summary or authority claim.", version.Full(), version.Runtime, strings.Join(ToolNames, ", "), s.defaultActorText(), spaces))
}

func (s *Service) newThread(_ context.Context, _ *mcp.CallToolRequest, p newThreadParams) (*mcp.CallToolResult, any, error) {
	actor, err := s.actor(p.ParticipantName)
	if err != nil {
		return reject(err)
	}
	space, err := s.loadSpace(p.Space)
	if err != nil {
		return reject(err)
	}
	if _, err := space.Participant(actor); err != nil {
		return reject(fmt.Errorf("only registered participants may create threads in this space — %s is not registered here", actor))
	}
	thread, err := substrate.ParseName(p.Name)
	if err != nil {
		return reject(err)
	}
	moderator, err := substrate.ParseName(p.Moderator)
	if err != nil {
		return reject(err)
	}
	order, err := substrate.ParseNames(p.TurnOrder)
	if err != nil {
		return reject(err)
	}
	cfg, err := substrate.CreateThread(space, thread, p.Topic, moderator, order)
	if err != nil {
		return reject(err)
	}
	return success(fmt.Sprintf("created thread: %s\ntopic: %s\nopening floor: %s\npaused on moderator: %s\nturn order: %s\n\nmoderator next steps:\n1. write the opening scope and assignment on your floor\n2. use transcript_manifest plus read_thread entry cursors when an attendee needs a bounded reproducible offer\n3. use set_next to route the floor; context selection and floor routing are separate\n4. re-check the floor before every action; end_thread when the work is actually complete", thread, cfg.Topic, cfg.Current(), yesNo(cfg.Paused()), substrate.JoinNames(cfg.TurnOrder, " → ")))
}

func (s *Service) listThreads(_ context.Context, _ *mcp.CallToolRequest, p actorParams) (*mcp.CallToolResult, any, error) {
	actor, err := s.actor(p.ParticipantName)
	if err != nil {
		return reject(err)
	}
	set, err := s.Source.Load()
	if err != nil {
		return reject(err)
	}
	var out strings.Builder
	for _, labeled := range set.Spaces {
		threads, err := labeled.Space.ListThreads()
		if err != nil {
			return reject(err)
		}
		for _, thread := range threads {
			status, err := substrate.GetTurnStatus(labeled.Space, thread)
			if err != nil {
				fmt.Fprintf(&out, "thread: %s · unreadable: %v\n", thread, err)
				continue
			}
			spacePart := ""
			if len(set.Spaces) > 1 {
				spacePart = " · space: " + labeled.Label
			}
			yours := ""
			if status.Current == actor {
				yours = " (you)"
			}
			paused := ""
			if status.Paused {
				paused = " (paused on moderator)"
			}
			fmt.Fprintf(&out, "thread: %s%s · status: %s · turn: %s%s%s · topic: %s\n", thread, spacePart, status.Status.Title(), status.Current, yours, paused, status.Topic)
		}
	}
	if out.Len() == 0 {
		if len(set.Spaces) == 0 {
			out.WriteString("no spaces exist yet — a moderator creates one with `substrate init`")
		} else {
			out.WriteString("no threads yet in configured space(s): " + strings.Join(set.Labels(), ", "))
		}
	}
	return success(out.String())
}

func (s *Service) readThread(_ context.Context, _ *mcp.CallToolRequest, p readParams) (*mcp.CallToolResult, any, error) {
	if p.LastN != nil && p.FromLine != nil {
		return reject(errors.New("last_n and from_line are mutually exclusive"))
	}
	if p.FromLine != nil && *p.FromLine == 0 {
		return reject(errors.New("from_line is 1-based and must be at least 1"))
	}
	space, err := s.loadSpace(p.Space)
	if err != nil {
		return reject(err)
	}
	thread, err := substrate.ParseName(p.Thread)
	if err != nil {
		return reject(err)
	}
	read, err := substrate.ReadTranscriptSnapshot(space, thread, substrate.Window{LastN: uintToInt(p.LastN), FromLine: uintToInt(p.FromLine), FromEntry: p.FromEntry, ThroughEntry: p.ThroughEntry})
	if err != nil {
		return reject(err)
	}
	return success(read.Text + "\n" + formatSnapshot(read))
}

func (s *Service) transcriptManifest(_ context.Context, _ *mcp.CallToolRequest, p manifestParams) (*mcp.CallToolResult, any, error) {
	space, err := s.loadSpace(p.Space)
	if err != nil {
		return reject(err)
	}
	thread, err := substrate.ParseName(p.Thread)
	if err != nil {
		return reject(err)
	}
	manifest, err := substrate.BuildTranscriptManifest(space, thread)
	if err != nil {
		return reject(err)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return reject(err)
	}
	return success(string(data))
}

func formatSnapshot(read substrate.TranscriptRead) string {
	var out strings.Builder
	fmt.Fprintf(&out, "--- captured snapshot ---\nthread version: %d\ntranscript lines: %d\nbytes returned: %d\n", read.Manifest.Version, read.Manifest.TotalLines, read.ByteLength)
	if read.LegacyLineWindow {
		out.WriteString("actual entries: (legacy line window; not entry-aligned)\n")
	} else if read.FirstEntry == "" {
		out.WriteString("actual entries: (none)\n")
	} else {
		fmt.Fprintf(&out, "actual entries: %s through %s\nactual lines: %d through %d\n", read.FirstEntry, read.LastEntry, read.StartLine, read.EndLine)
		fmt.Fprintf(&out, "replay with: from_entry=%s through_entry=%s\n", read.FirstEntry, read.LastEntry)
	}
	if read.LegacyLineWindow {
		out.WriteString("next entry: (use transcript lines + 1 for incremental continuation)")
	} else if read.NextEntry == "" {
		out.WriteString("next entry: (caught up at captured snapshot)")
	} else {
		fmt.Fprintf(&out, "next entry: %s", read.NextEntry)
	}
	return out.String()
}

func (s *Service) writeEntry(_ context.Context, _ *mcp.CallToolRequest, p writeParams) (*mcp.CallToolResult, any, error) {
	actor, err := s.actor(p.ParticipantName)
	if err != nil {
		return reject(err)
	}
	space, err := s.loadSpace(p.Space)
	if err != nil {
		return reject(err)
	}
	thread, err := substrate.ParseName(p.Thread)
	if err != nil {
		return reject(err)
	}
	written, err := substrate.WriteEntry(space, thread, actor, p.Content)
	if err != nil {
		return reject(err)
	}
	noOp := ""
	if written.NoOp {
		noOp = " as a no-op"
	}
	paused := ""
	if written.Paused {
		paused = " (moderator — the thread is paused)"
	}
	return success(fmt.Sprintf("recorded%s — next turn: %s%s\n%s", noOp, written.Next, paused, nextMoves(false, false)))
}

func (s *Service) checkTurn(_ context.Context, _ *mcp.CallToolRequest, p threadParams) (*mcp.CallToolResult, any, error) {
	actor, err := s.actor(p.ParticipantName)
	if err != nil {
		return reject(err)
	}
	space, err := s.loadSpace(p.Space)
	if err != nil {
		return reject(err)
	}
	thread, err := substrate.ParseName(p.Thread)
	if err != nil {
		return reject(err)
	}
	text, err := s.statusText(space, thread, actor)
	if err != nil {
		return reject(err)
	}
	return success(text)
}

func (s *Service) waitForTurn(ctx context.Context, _ *mcp.CallToolRequest, p waitParams) (*mcp.CallToolResult, any, error) {
	actor, err := s.actor(p.ParticipantName)
	if err != nil {
		return reject(err)
	}
	space, err := s.loadSpace(p.Space)
	if err != nil {
		return reject(err)
	}
	thread, err := substrate.ParseName(p.Thread)
	if err != nil {
		return reject(err)
	}
	timeout := waitDefault
	if p.TimeoutSecs > 0 {
		seconds := min(p.TimeoutSecs, uint64(waitMax/time.Second))
		timeout = time.Duration(seconds) * time.Second
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return reject(err)
	}
	defer w.Close()
	if err := w.Add(space.ThreadDir(thread)); err != nil {
		return reject(err)
	}
	fallback := time.NewTicker(waitFallback)
	defer fallback.Stop()
	for {
		status, err := substrate.GetTurnStatus(space, thread)
		if err != nil {
			return reject(err)
		}
		if status.Current == actor || status.Status == substrate.Ended {
			text, err := s.statusText(space, thread, actor)
			if err != nil {
				return reject(err)
			}
			return success(text)
		}
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-deadline.C:
			text, err := s.statusText(space, thread, actor)
			if err != nil {
				return reject(err)
			}
			return success(text + "\n(timed out — still not your turn; call wait_for_turn again. You are only done when status is Ended.)")
		case <-w.Events:
		case <-w.Errors:
		case <-fallback.C:
		}
	}
}

func (s *Service) resolveModerator(p threadParams) (*substrate.Space, substrate.Name, substrate.Name, error) {
	actor, err := s.actor(p.ParticipantName)
	if err != nil {
		return nil, "", "", err
	}
	space, err := s.loadSpace(p.Space)
	if err != nil {
		return nil, "", "", err
	}
	thread, err := substrate.ParseName(p.Thread)
	if err != nil {
		return nil, "", "", err
	}
	if err := s.moderator(space, thread, actor); err != nil {
		return nil, "", "", err
	}
	return space, thread, actor, nil
}

func (s *Service) setNext(_ context.Context, _ *mcp.CallToolRequest, p modNameParams) (*mcp.CallToolResult, any, error) {
	space, thread, actor, err := s.resolveModerator(threadParams{Space: p.Space, Thread: p.Thread, ParticipantName: p.ParticipantName})
	if err != nil {
		return reject(err)
	}
	name, err := substrate.ParseName(p.Name)
	if err != nil {
		return reject(err)
	}
	if err := substrate.SetNext(space, thread, name); err != nil {
		return reject(err)
	}
	return s.moderatorOK(space, thread, actor, "the floor passes to "+name.String())
}
func (s *Service) invite(_ context.Context, _ *mcp.CallToolRequest, p modNameParams) (*mcp.CallToolResult, any, error) {
	space, thread, actor, err := s.resolveModerator(threadParams{Space: p.Space, Thread: p.Thread, ParticipantName: p.ParticipantName})
	if err != nil {
		return reject(err)
	}
	name, err := substrate.ParseName(p.Name)
	if err != nil {
		return reject(err)
	}
	registered := ""
	added, err := space.EnsureParticipant(name)
	if err != nil {
		return reject(err)
	}
	if added {
		registered = " (registered as a new agent)"
	}
	if err := substrate.Invite(space, thread, name); err != nil {
		return reject(err)
	}
	return s.moderatorOK(space, thread, actor, fmt.Sprintf("%s joins the thread at the end of the round%s", name, registered))
}
func (s *Service) quiet(_ context.Context, _ *mcp.CallToolRequest, p quietParams) (*mcp.CallToolResult, any, error) {
	space, thread, actor, err := s.resolveModerator(threadParams{Space: p.Space, Thread: p.Thread, ParticipantName: p.ParticipantName})
	if err != nil {
		return reject(err)
	}
	name, err := substrate.ParseName(p.Name)
	if err != nil {
		return reject(err)
	}
	if err := substrate.Quiet(space, thread, name, p.Turns); err != nil {
		return reject(err)
	}
	confirmation := fmt.Sprintf("%s quieted for %d turn(s)", name, p.Turns)
	if p.Turns == 0 {
		confirmation = name.String() + " may speak again"
	}
	return s.moderatorOK(space, thread, actor, confirmation)
}
func (s *Service) reorderTurns(_ context.Context, _ *mcp.CallToolRequest, p orderParams) (*mcp.CallToolResult, any, error) {
	space, thread, actor, err := s.resolveModerator(threadParams{Space: p.Space, Thread: p.Thread, ParticipantName: p.ParticipantName})
	if err != nil {
		return reject(err)
	}
	order, err := substrate.ParseNames(p.Order)
	if err != nil {
		return reject(err)
	}
	if err := substrate.ReorderTurns(space, thread, order); err != nil {
		return reject(err)
	}
	return s.moderatorOK(space, thread, actor, "turn order set: "+substrate.JoinNames(order, " → "))
}
func (s *Service) setTopic(_ context.Context, _ *mcp.CallToolRequest, p topicParams) (*mcp.CallToolResult, any, error) {
	space, thread, actor, err := s.resolveModerator(threadParams{Space: p.Space, Thread: p.Thread, ParticipantName: p.ParticipantName})
	if err != nil {
		return reject(err)
	}
	if err := substrate.SetTopic(space, thread, p.Topic); err != nil {
		return reject(err)
	}
	return s.moderatorOK(space, thread, actor, "topic set: "+p.Topic)
}
func (s *Service) endThread(_ context.Context, _ *mcp.CallToolRequest, p threadParams) (*mcp.CallToolResult, any, error) {
	space, thread, actor, err := s.resolveModerator(p)
	if err != nil {
		return reject(err)
	}
	if err := substrate.EndThread(space, thread); err != nil {
		return reject(err)
	}
	return s.moderatorOK(space, thread, actor, "thread ended")
}
func (s *Service) resumeThread(_ context.Context, _ *mcp.CallToolRequest, p threadParams) (*mcp.CallToolResult, any, error) {
	space, thread, actor, err := s.resolveModerator(p)
	if err != nil {
		return reject(err)
	}
	if err := substrate.ResumeThread(space, thread); err != nil {
		return reject(err)
	}
	return s.moderatorOK(space, thread, actor, "thread resumed — the floor is yours; say why the thread is back")
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func uintToInt(value *uint64) *int {
	if value == nil {
		return nil
	}
	maximum := uint64(^uint(0) >> 1)
	clamped := min(*value, maximum)
	converted := int(clamped)
	return &converted
}
