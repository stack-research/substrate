package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"github.com/stack-research/substrate/internal/substrate"
)

func (m *Model) render() string {
	if !m.ready {
		return "opening substrate..."
	}
	header := m.renderHeader()
	main := m.renderMain(max(1, m.height-2))
	footer := m.renderFooter()
	page := lipgloss.JoinVertical(lipgloss.Left, header, main, footer)
	page = lipgloss.NewStyle().Background(m.styles.background).Foreground(m.styles.text).Width(m.width).Height(m.height).Render(page)
	if m.showHelp {
		page = m.overlay(page, m.renderHelp())
	}
	if m.showPalette {
		page = m.overlay(page, m.renderPalette())
	}
	if m.newRoom != nil {
		page = m.overlay(page, m.renderNewRoom())
	}
	return page
}

func (m *Model) renderHeader() string {
	root := m.space.Root()
	if absolute, err := filepath.Abs(root); err == nil {
		root = absolute
	}
	spaceName := filepath.Base(root)
	left := m.styles.brand.Render("SUBSTRATE") + m.styles.crumb.Render(spaceName+" / shared room")
	right := m.styles.subtle.Render("present as ") + m.styles.identity.Render(m.me.String())
	gap := max(1, m.width-lipgloss.Width(left)-lipgloss.Width(right))
	return m.styles.topbar.Width(m.width).Render(left + strings.Repeat(" ", gap) + right)
}

func (m *Model) renderMain(height int) string {
	showSidebar := m.showSidebar && m.width >= 80
	if !showSidebar {
		return m.renderConversation(m.width, height)
	}
	sidebarWidth := min(31, max(24, m.width/4))
	sidebar := m.renderSidebar(sidebarWidth, height)
	conversation := m.renderConversation(m.width-sidebarWidth, height)
	return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, conversation)
}

func (m *Model) renderSidebar(width, height int) string {
	style := m.styles.sidebar
	if m.focus == focusRooms {
		style = m.styles.sidebarFocused
	}
	innerWidth := max(1, width-style.GetHorizontalFrameSize())
	lines := []string{
		m.styles.section.Render("ROOMS"),
		m.styles.subtle.Render(fmt.Sprintf("%d conversations", len(m.rooms))),
		"",
	}
	if len(m.rooms) == 0 {
		lines = append(lines, m.styles.subtle.Render("Nothing here yet."), "", m.styles.key.Render("n")+m.styles.subtle.Render("  open a room"))
	}
	for i, room := range m.rooms {
		marker := "  "
		if room.Status == substrate.Ended {
			marker = "- "
		} else if room.Current == m.me {
			marker = "* "
		}
		label := marker + room.Thread.String()
		lineStyle := m.styles.room
		if room.Current == m.me {
			lineStyle = m.styles.roomCurrent
		}
		if i == m.selected {
			lineStyle = m.styles.roomSelected
		}
		lines = append(lines, lineStyle.Width(innerWidth).MaxWidth(innerWidth).Render(label))
	}
	lines = append(lines, "", m.styles.subtle.Render("ctrl+n  new room"), m.styles.subtle.Render("ctrl+b  hide rail"))
	return style.Width(width).Height(height).Render(strings.Join(lines, "\n"))
}

func (m *Model) renderConversation(width, height int) string {
	if len(m.rooms) == 0 {
		content := lipgloss.JoinVertical(lipgloss.Left,
			m.styles.topic.Render("Start with a question worth sharing."),
			"",
			m.styles.subtle.Render("A room is an append-only conversation between humans and agents."),
			m.styles.subtle.Render("Press Ctrl+N to open the first one."),
		)
		return lipgloss.NewStyle().Width(width).Height(height).Padding(3, 4).Render(content)
	}
	room := m.rooms[m.selected]
	topicLine := m.styles.topic.Render(room.Topic)
	turnLabel := "waiting for " + room.Current.String()
	turnStyle := m.styles.turnWaiting
	if room.Status == substrate.Ended {
		turnLabel = "ended"
	} else if room.Paused {
		turnLabel = "moderator pause"
	} else if room.Current == m.me {
		turnLabel = "YOUR TURN"
		turnStyle = m.styles.turnReady
	}
	turn := turnStyle.Render(turnLabel)
	gap := max(1, width-lipgloss.Width(topicLine)-lipgloss.Width(turn)-4)
	bar := m.styles.conversationBar.Width(width).Render(topicLine + strings.Repeat(" ", gap) + turn)

	transcriptStyle := m.styles.transcript
	if m.focus == focusTranscript {
		transcriptStyle = m.styles.transcriptFocus
	}
	composerHeight := m.composer.Height() + 3
	transcriptHeight := max(3, height-lipgloss.Height(bar)-composerHeight)
	transcript := transcriptStyle.Width(width).Height(transcriptHeight).Render(m.viewport.View())

	composerStyle := m.styles.composerWaiting
	composerTitle := "WAITING  " + room.Current.String() + " has the floor"
	if room.Status == substrate.Ended {
		composerTitle = "CLOSED  this room has ended"
	} else if room.Current == m.me {
		composerStyle = m.styles.composerReady
		composerTitle = "COMPOSE  you have the floor"
	}
	if m.focus == focusComposer {
		composerStyle = composerStyle.BorderForeground(m.styles.accent)
	}
	composer := composerStyle.Width(max(1, width-2)).Margin(0, 1).Render(
		m.styles.composerTitle.Render(composerTitle) + "\n" + m.composer.View(),
	)
	return lipgloss.JoinVertical(lipgloss.Left, bar, transcript, composer)
}

func (m *Model) renderEntries(width int) string {
	if len(m.entries) == 0 {
		return m.styles.subtle.Render("This room is quiet. The moderator has the first piece of chalk.")
	}
	renderer, err := glamour.NewTermRenderer(glamour.WithWordWrap(max(20, width-8)), glamour.WithStandardStyle(map[bool]string{true: "dark", false: "light"}[m.dark]))
	if err != nil {
		renderer = nil
	}
	var blocks []string
	for _, entry := range m.entries {
		authorStyle := m.styles.author
		entryStyle := m.styles.entry
		if entry.Meta.Author == m.me {
			authorStyle = m.styles.authorSelf
			entryStyle = m.styles.entrySelf
		}
		header := authorStyle.Render(entry.Meta.Author.String()) + "  " + m.styles.timestamp.Render(entry.Meta.Timestamp.Local().Format("Jan 02 15:04"))
		body := entry.Body
		if renderer != nil {
			if rendered, err := renderer.Render(entry.Body); err == nil {
				body = strings.TrimSpace(rendered)
			}
		}
		blocks = append(blocks, entryStyle.Width(max(1, width-1)).Render(header+"\n"+body))
	}
	return strings.Join(blocks, "\n\n")
}

func (m *Model) renderFooter() string {
	if m.flash != "" {
		if m.flashIsErr {
			return m.styles.statusError.Width(m.width).Render(m.flash)
		}
		return m.styles.statusGood.Width(m.width).Render(m.flash)
	}
	if m.reloadError != nil {
		return m.styles.statusError.Width(m.width).Render("reload: " + m.reloadError.Error())
	}
	focusName := []string{"rooms", "conversation", "composer"}[m.focus]
	left := m.styles.key.Render(strings.ToUpper(focusName))
	right := m.styles.subtle.Render("tab focus   ctrl+s send   ctrl+k commands   ? help")
	gap := max(1, m.width-lipgloss.Width(left)-lipgloss.Width(right)-2)
	return m.styles.status.Width(m.width).Render(left + strings.Repeat(" ", gap) + right)
}

func (m *Model) overlay(background, foreground string) string {
	w := lipgloss.Width(foreground)
	h := lipgloss.Height(foreground)
	x := max(0, (m.width-w)/2)
	y := max(0, (m.height-h)/2)
	return lipgloss.NewCompositor(
		lipgloss.NewLayer(background).Z(0),
		lipgloss.NewLayer(foreground).X(x).Y(y).Z(1),
	).Render()
}

func (m *Model) renderHelp() string {
	rows := []string{
		m.styles.brand.Render("SUBSTRATE KEYS"), "",
		m.styles.key.Render("Tab / Shift+Tab") + "   move focus",
		m.styles.key.Render("Ctrl+S") + "           send entry or slash command",
		m.styles.key.Render("Ctrl+N") + "           open a room",
		m.styles.key.Render("Ctrl+B") + "           toggle room rail",
		m.styles.key.Render("Ctrl+E") + "           show/hide ended rooms",
		m.styles.key.Render("[ / ]") + "            previous / next room",
		m.styles.key.Render("PgUp / PgDn") + "      scroll transcript",
		m.styles.key.Render("Ctrl+K") + "           command palette",
		m.styles.key.Render("Ctrl+C twice") + "     quit",
		"", m.styles.subtle.Render("Press Esc to close."),
	}
	return m.styles.modal.Width(min(62, m.width-8)).Render(strings.Join(rows, "\n"))
}

func (m *Model) renderPalette() string {
	rows := []string{
		m.styles.brand.Render("ROOM COMMANDS"), "",
		"/pass", "/topic <text>", "/next <name>", "/invite <name>",
		"/quiet <name> [turns]", "/unquiet <name>", "/order <name>,<name>",
		"/end", "/resume", "/help", "",
		m.styles.subtle.Render("Moderator commands are role-checked by the engine."),
		m.styles.subtle.Render("Type a command in the composer and press Ctrl+S."),
	}
	return m.styles.modal.Width(min(68, m.width-8)).Render(strings.Join(rows, "\n"))
}

func (m *Model) renderNewRoom() string {
	labels := []string{"name", "topic", "turn order (comma-separated)"}
	var rows []string
	rows = append(rows, m.styles.brand.Render("OPEN A ROOM"), "")
	for i, field := range m.newRoom.fields {
		label := m.styles.subtle.Render(labels[i])
		if i == m.newRoom.focus {
			label = m.styles.key.Render(labels[i])
		}
		rows = append(rows, label, field.View())
	}
	rows = append(rows, "", m.styles.subtle.Render("Tab moves  Ctrl+S opens  Esc cancels"))
	return m.styles.modal.Width(min(74, m.width-8)).Render(strings.Join(rows, "\n"))
}
