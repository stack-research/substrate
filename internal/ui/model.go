package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"
	"github.com/stack-research/substrate/internal/substrate"
)

type focus int

const (
	focusRooms focus = iota
	focusTranscript
	focusComposer
)

type diskChangedMsg struct{}
type flashExpiredMsg struct{ id uint64 }

type newRoomForm struct {
	fields [3]textarea.Model
	focus  int
}

type Model struct {
	space *substrate.Space
	me    substrate.Name

	width, height int
	dark          bool
	styles        styles
	ready         bool

	rooms    []substrate.TurnStatus
	selected int
	entries  []substrate.Entry
	viewport viewport.Model
	composer textarea.Model
	focus    focus

	showSidebar bool
	showHelp    bool
	showPalette bool
	newRoom     *newRoomForm

	flash       string
	flashIsErr  bool
	flashID     uint64
	lastCtrlC   time.Time
	reloadError error
}

func NewModel(space *substrate.Space, me substrate.Name) (*Model, error) {
	composer := textarea.New()
	composer.Prompt = ""
	composer.Placeholder = "Say what matters. Enter adds a line; Ctrl+S sends."
	composer.ShowLineNumbers = false
	composer.CharLimit = 64 * 1024
	composer.SetHeight(3)
	composer.SetVirtualCursor(true)
	composer.DynamicHeight = true
	composer.MinHeight = 1
	composer.MaxHeight = 5
	model := &Model{
		space: space, me: me, dark: true, styles: newStyles(true),
		viewport: viewport.New(), composer: composer, focus: focusTranscript, showSidebar: true,
	}
	if err := model.reload(false); err != nil {
		return nil, err
	}
	if len(model.rooms) > 0 && model.rooms[model.selected].Current == me {
		model.focus = focusComposer
		model.composer.Focus()
	}
	return model, nil
}

func (m *Model) Init() tea.Cmd {
	commands := []tea.Cmd{watchSpace(m.space.SubstrateDir()), tea.RequestBackgroundColor}
	if m.focus == focusComposer {
		commands = append(commands, m.composer.Focus())
	} else {
		m.composer.Blur()
	}
	return tea.Batch(commands...)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height, m.ready = msg.Width, msg.Height, true
		m.resize()
	case tea.BackgroundColorMsg:
		m.dark = msg.IsDark()
		m.styles = newStyles(m.dark)
		m.refreshTranscript(true)
	case diskChangedMsg:
		atBottom := m.viewport.AtBottom()
		if err := m.reload(atBottom); err != nil {
			m.reloadError = err
		}
		cmds = append(cmds, watchSpace(m.space.SubstrateDir()))
	case flashExpiredMsg:
		if msg.id == m.flashID {
			m.flash = ""
		}
	case tea.KeyPressMsg:
		cmd, handled := m.handleKey(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		if handled {
			return m, tea.Batch(cmds...)
		}
	}

	if m.newRoom != nil {
		field := m.newRoom.fields[m.newRoom.focus]
		field, cmd := field.Update(msg)
		m.newRoom.fields[m.newRoom.focus] = field
		cmds = append(cmds, cmd)
		return m, tea.Batch(cmds...)
	}
	if m.focus == focusComposer {
		var cmd tea.Cmd
		m.composer, cmd = m.composer.Update(msg)
		cmds = append(cmds, cmd)
	} else if m.focus == focusTranscript {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()
	if key == "ctrl+c" {
		now := time.Now()
		if now.Sub(m.lastCtrlC) < 1500*time.Millisecond {
			return tea.Quit, true
		}
		m.lastCtrlC = now
		return m.setFlash("press Ctrl+C again to leave", false), true
	}
	if m.newRoom != nil {
		switch key {
		case "esc", "escape":
			m.newRoom = nil
			return nil, true
		case "tab", "shift+tab":
			m.newRoom.fields[m.newRoom.focus].Blur()
			if key == "tab" {
				m.newRoom.focus = (m.newRoom.focus + 1) % 3
			} else {
				m.newRoom.focus = (m.newRoom.focus + 2) % 3
			}
			return m.newRoom.fields[m.newRoom.focus].Focus(), true
		case "ctrl+s":
			return m.createRoom(), true
		}
		return nil, false
	}
	if m.showHelp || m.showPalette {
		if key == "esc" || key == "escape" || key == "?" || key == "ctrl+k" {
			m.showHelp, m.showPalette = false, false
		}
		return nil, true
	}
	switch key {
	case "ctrl+k":
		m.showPalette = true
		return nil, true
	case "?":
		if m.focus != focusComposer || m.composer.Value() == "" {
			m.showHelp = true
			return nil, true
		}
	case "ctrl+b":
		m.showSidebar = !m.showSidebar
		m.resize()
		return nil, true
	case "tab":
		return m.setFocus((m.focus + 1) % 3), true
	case "shift+tab":
		return m.setFocus((m.focus + 2) % 3), true
	case "ctrl+s", "ctrl+enter":
		if m.focus == focusComposer {
			return m.submit(), true
		}
	case "esc", "escape":
		if m.focus == focusComposer {
			return m.setFocus(focusTranscript), true
		}
	case "ctrl+n":
		return m.openNewRoom(), true
	}
	if m.focus == focusRooms {
		switch key {
		case "q":
			return tea.Quit, true
		case "j", "down":
			m.moveSelection(1)
			return nil, true
		case "k", "up":
			m.moveSelection(-1)
			return nil, true
		case "enter", "l", "right":
			return m.setFocus(focusTranscript), true
		case "n":
			return m.openNewRoom(), true
		case "r":
			if err := m.reload(true); err != nil {
				return m.setFlash(err.Error(), true), true
			}
			return m.setFlash("reloaded from disk", false), true
		}
	}
	if m.focus == focusTranscript {
		switch key {
		case "q":
			return tea.Quit, true
		case "i", "a":
			return m.setFocus(focusComposer), true
		case "[":
			m.moveSelection(-1)
			return nil, true
		case "]":
			m.moveSelection(1)
			return nil, true
		case "g":
			m.viewport.GotoTop()
			return nil, true
		case "G":
			m.viewport.GotoBottom()
			return nil, true
		}
	}
	return nil, false
}

func (m *Model) setFocus(next focus) tea.Cmd {
	m.focus = next
	if next == focusComposer {
		return m.composer.Focus()
	}
	m.composer.Blur()
	return nil
}

func (m *Model) moveSelection(delta int) {
	if len(m.rooms) == 0 {
		return
	}
	m.selected = (m.selected + delta + len(m.rooms)) % len(m.rooms)
	_ = m.loadSelected(true)
}

func (m *Model) submit() tea.Cmd {
	if len(m.rooms) == 0 {
		return m.setFlash("open a room before writing", true)
	}
	content := strings.TrimSpace(m.composer.Value())
	if content == "" {
		return m.setFlash("the composer is empty", true)
	}
	if strings.HasPrefix(content, "/") {
		if handled, err := m.runCommand(content); handled {
			if err != nil {
				return m.setFlash(err.Error(), true)
			}
			m.composer.Reset()
			_ = m.reload(true)
			return m.setFlash("room updated", false)
		}
	}
	room := m.rooms[m.selected]
	written, err := substrate.WriteEntry(m.space, room.Thread, m.me, content)
	if err != nil {
		return m.setFlash(err.Error(), true)
	}
	m.composer.Reset()
	_ = m.reload(true)
	message := "entry recorded — next: " + written.Next.String()
	if written.NoOp {
		message = "no-op recorded — next: " + written.Next.String()
	}
	return m.setFlash(message, false)
}

func (m *Model) runCommand(input string) (bool, error) {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return false, nil
	}
	room := m.rooms[m.selected]
	thread := room.Thread
	moderatorOnly := func() error { return substrate.RequireModerator(m.space, thread, m.me) }
	switch parts[0] {
	case "/pass":
		_, err := substrate.WriteEntry(m.space, thread, m.me, "pass")
		return true, err
	case "/topic":
		if err := moderatorOnly(); err != nil {
			return true, err
		}
		if len(parts) < 2 {
			return true, errors.New("usage: /topic <new topic>")
		}
		return true, substrate.SetTopic(m.space, thread, strings.TrimSpace(strings.TrimPrefix(input, "/topic")))
	case "/next":
		if err := moderatorOnly(); err != nil {
			return true, err
		}
		name, err := commandName(parts, "/next <name>")
		if err != nil {
			return true, err
		}
		return true, substrate.SetNext(m.space, thread, name)
	case "/invite":
		if err := moderatorOnly(); err != nil {
			return true, err
		}
		name, err := commandName(parts, "/invite <name>")
		if err != nil {
			return true, err
		}
		if _, err := m.space.Participant(name); err != nil {
			var unknown *substrate.UnknownParticipantError
			if !errors.As(err, &unknown) {
				return true, err
			}
			if err := m.space.AddParticipant(name, substrate.Agent); err != nil {
				return true, err
			}
		}
		return true, substrate.Invite(m.space, thread, name)
	case "/quiet":
		if err := moderatorOnly(); err != nil {
			return true, err
		}
		if len(parts) < 2 || len(parts) > 3 {
			return true, errors.New("usage: /quiet <name> [turns]")
		}
		name, err := substrate.ParseName(parts[1])
		if err != nil {
			return true, err
		}
		turns := uint64(1)
		if len(parts) == 3 {
			turns, err = strconv.ParseUint(parts[2], 10, 32)
			if err != nil {
				return true, err
			}
		}
		return true, substrate.Quiet(m.space, thread, name, uint32(turns))
	case "/unquiet":
		if err := moderatorOnly(); err != nil {
			return true, err
		}
		name, err := commandName(parts, "/unquiet <name>")
		if err != nil {
			return true, err
		}
		return true, substrate.Quiet(m.space, thread, name, 0)
	case "/order":
		if err := moderatorOnly(); err != nil {
			return true, err
		}
		raw := strings.TrimSpace(strings.TrimPrefix(input, "/order"))
		if raw == "" {
			return true, errors.New("usage: /order <name>,<name>,...")
		}
		var order []substrate.Name
		for _, value := range strings.Split(raw, ",") {
			name, err := substrate.ParseName(strings.TrimSpace(value))
			if err != nil {
				return true, err
			}
			order = append(order, name)
		}
		return true, substrate.ReorderTurns(m.space, thread, order)
	case "/end":
		if err := moderatorOnly(); err != nil {
			return true, err
		}
		return true, substrate.EndThread(m.space, thread)
	case "/resume":
		if err := moderatorOnly(); err != nil {
			return true, err
		}
		return true, substrate.ResumeThread(m.space, thread)
	case "/help":
		m.showHelp = true
		return true, nil
	default:
		return false, nil
	}
}

func commandName(parts []string, usage string) (substrate.Name, error) {
	if len(parts) != 2 {
		return "", errors.New("usage: " + usage)
	}
	return substrate.ParseName(parts[1])
}

func (m *Model) openNewRoom() tea.Cmd {
	cfg, err := m.space.Config()
	if err != nil {
		return m.setFlash(err.Error(), true)
	}
	rootName := substrate.LabelFor(m.space.Root())
	turns := make([]string, 0, len(cfg.Participants))
	for _, participant := range cfg.Participants {
		if participant.Name != m.me {
			turns = append(turns, participant.Name.String())
		}
	}
	values := []string{rootName, "", strings.Join(turns, ", ")}
	form := &newRoomForm{focus: 1}
	for i := range form.fields {
		field := textarea.New()
		field.ShowLineNumbers = false
		field.SetHeight(1)
		field.SetVirtualCursor(true)
		field.SetWidth(min(68, max(24, m.width-12)))
		field.SetValue(values[i])
		field.CharLimit = 1024
		form.fields[i] = field
	}
	form.fields[1].Focus()
	m.newRoom = form
	return nil
}

func (m *Model) createRoom() tea.Cmd {
	values := make([]string, 3)
	for i := range m.newRoom.fields {
		values[i] = strings.TrimSpace(m.newRoom.fields[i].Value())
	}
	name, err := substrate.ParseName(values[0])
	if err != nil {
		return m.setFlash(err.Error(), true)
	}
	if values[1] == "" {
		return m.setFlash("topic cannot be empty", true)
	}
	var turns []substrate.Name
	for _, raw := range strings.Split(values[2], ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		participant, err := substrate.ParseName(raw)
		if err != nil {
			return m.setFlash(err.Error(), true)
		}
		if _, err := m.space.Participant(participant); err != nil {
			var unknown *substrate.UnknownParticipantError
			if !errors.As(err, &unknown) {
				return m.setFlash(err.Error(), true)
			}
			if err := m.space.AddParticipant(participant, substrate.Agent); err != nil {
				return m.setFlash(err.Error(), true)
			}
		}
		turns = append(turns, participant)
	}
	if _, err := substrate.CreateThread(m.space, name, values[1], m.me, turns); err != nil {
		return m.setFlash(err.Error(), true)
	}
	m.newRoom = nil
	_ = m.reload(true)
	for i, room := range m.rooms {
		if room.Thread == name {
			m.selected = i
			_ = m.loadSelected(true)
			break
		}
	}
	return m.setFlash("room opened — you have the floor", false)
}

func (m *Model) setFlash(message string, isError bool) tea.Cmd {
	m.flash, m.flashIsErr = message, isError
	m.flashID++
	id := m.flashID
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg { return flashExpiredMsg{id: id} })
}

func (m *Model) reload(stickBottom bool) error {
	threads, err := m.space.ListThreads()
	if err != nil {
		return err
	}
	selectedName := substrate.Name("")
	if len(m.rooms) > 0 && m.selected < len(m.rooms) {
		selectedName = m.rooms[m.selected].Thread
	}
	rooms := make([]substrate.TurnStatus, 0, len(threads))
	for _, thread := range threads {
		status, err := substrate.GetTurnStatus(m.space, thread)
		if err == nil {
			rooms = append(rooms, status)
		}
	}
	m.rooms = rooms
	if len(rooms) == 0 {
		m.selected = 0
		m.entries = nil
		m.viewport.SetContent("")
		return nil
	}
	m.selected = min(m.selected, len(rooms)-1)
	for i, room := range rooms {
		if room.Thread == selectedName {
			m.selected = i
			break
		}
	}
	return m.loadSelected(stickBottom)
}

func (m *Model) loadSelected(stickBottom bool) error {
	if len(m.rooms) == 0 {
		return nil
	}
	entries, err := substrate.LoadEntries(m.space, m.rooms[m.selected].Thread)
	if err != nil {
		return err
	}
	m.entries = entries
	m.refreshTranscript(stickBottom)
	return nil
}

func (m *Model) refreshTranscript(stickBottom bool) {
	width := max(m.viewport.Width(), 40)
	m.viewport.SetContent(m.renderEntries(width))
	if stickBottom {
		m.viewport.GotoBottom()
	}
}

func (m *Model) resize() {
	if !m.ready {
		return
	}
	showSidebar := m.showSidebar && m.width >= 80
	sidebarWidth := 0
	if showSidebar {
		sidebarWidth = min(31, max(24, m.width/4))
	}
	mainWidth := max(30, m.width-sidebarWidth)
	composerHeight := min(5, max(3, m.height/6))
	viewportHeight := max(3, m.height-composerHeight-7)
	m.viewport.SetWidth(max(20, mainWidth-5))
	m.viewport.SetHeight(viewportHeight)
	m.composer.SetWidth(max(20, mainWidth-6))
	m.composer.SetHeight(composerHeight)
	if m.newRoom != nil {
		for i := range m.newRoom.fields {
			m.newRoom.fields[i].SetWidth(min(68, max(24, m.width-12)))
		}
	}
	m.refreshTranscript(true)
}

func watchSpace(path string) tea.Cmd {
	return func() tea.Msg {
		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			time.Sleep(time.Second)
			return diskChangedMsg{}
		}
		defer watcher.Close()
		if err := addRecursive(watcher, path); err != nil {
			time.Sleep(time.Second)
			return diskChangedMsg{}
		}
		for {
			select {
			case event := <-watcher.Events:
				if event.Op&fsnotify.Create != 0 {
					if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
						_ = watcher.Add(event.Name)
					}
				}
				return diskChangedMsg{}
			case <-watcher.Errors:
				return diskChangedMsg{}
			}
		}
	}
}

func addRecursive(watcher *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			return watcher.Add(path)
		}
		return nil
	})
}

func Run(space *substrate.Space, me substrate.Name) error {
	model, err := NewModel(space, me)
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(model).Run()
	return err
}

func (m *Model) View() tea.View {
	view := tea.NewView(m.render())
	view.AltScreen = true
	view.WindowTitle = "substrate"
	return view
}
