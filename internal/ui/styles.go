package ui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// styles deliberately keeps the room chrome quiet. The transcript is the
// workspace; color and spacing establish hierarchy, with the composer as the
// one prominent bordered surface.
type styles struct {
	background      color.Color
	surface         color.Color
	surfaceRaised   color.Color
	text            color.Color
	muted           color.Color
	faint           color.Color
	accent          color.Color
	accentSecondary color.Color
	good            color.Color
	warn            color.Color
	danger          color.Color

	topbar          lipgloss.Style
	brand           lipgloss.Style
	crumb           lipgloss.Style
	identity        lipgloss.Style
	subtle          lipgloss.Style
	sidebar         lipgloss.Style
	sidebarFocused  lipgloss.Style
	section         lipgloss.Style
	room            lipgloss.Style
	roomCurrent     lipgloss.Style
	roomSelected    lipgloss.Style
	conversationBar lipgloss.Style
	topic           lipgloss.Style
	turnReady       lipgloss.Style
	turnWaiting     lipgloss.Style
	transcript      lipgloss.Style
	transcriptFocus lipgloss.Style
	composerReady   lipgloss.Style
	composerWaiting lipgloss.Style
	composerTitle   lipgloss.Style
	status          lipgloss.Style
	statusError     lipgloss.Style
	statusGood      lipgloss.Style
	author          lipgloss.Style
	authorSelf      lipgloss.Style
	timestamp       lipgloss.Style
	entry           lipgloss.Style
	entrySelf       lipgloss.Style
	key             lipgloss.Style
	modal           lipgloss.Style
}

func newStyles(dark bool) styles {
	choose := lipgloss.LightDark(dark)
	bg := choose(lipgloss.Color("#FBFAFC"), lipgloss.Color("#0D0C12"))
	surface := choose(lipgloss.Color("#F2EFF5"), lipgloss.Color("#14121B"))
	raised := choose(lipgloss.Color("#FFFFFF"), lipgloss.Color("#1E1A27"))
	text := choose(lipgloss.Color("#26212C"), lipgloss.Color("#F6F1F8"))
	muted := choose(lipgloss.Color("#716979"), lipgloss.Color("#AAA0B2"))
	faint := choose(lipgloss.Color("#D2CBD7"), lipgloss.Color("#403A48"))
	accent := choose(lipgloss.Color("#A9369E"), lipgloss.Color("#F28BE7"))
	accent2 := choose(lipgloss.Color("#087A83"), lipgloss.Color("#71D7E0"))
	good := choose(lipgloss.Color("#18744A"), lipgloss.Color("#75D8A8"))
	warn := choose(lipgloss.Color("#976400"), lipgloss.Color("#F3C969"))
	danger := choose(lipgloss.Color("#B42335"), lipgloss.Color("#FF8794"))

	composer := lipgloss.NewStyle().
		Background(raised).
		Foreground(text).
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1)
	entry := lipgloss.NewStyle().
		Border(lipgloss.ThickBorder(), false, false, false, true).
		BorderForeground(faint).
		PaddingLeft(2)

	return styles{
		background: bg, surface: surface, surfaceRaised: raised, text: text, muted: muted,
		faint: faint, accent: accent, accentSecondary: accent2, good: good, warn: warn, danger: danger,
		topbar:          lipgloss.NewStyle().Background(surface).Foreground(text),
		brand:           lipgloss.NewStyle().Background(accent).Foreground(bg).Bold(true).Padding(0, 1),
		crumb:           lipgloss.NewStyle().Foreground(muted).PaddingLeft(1),
		identity:        lipgloss.NewStyle().Foreground(accent2).Bold(true),
		subtle:          lipgloss.NewStyle().Foreground(muted),
		sidebar:         lipgloss.NewStyle().Background(surface).Foreground(text).Padding(1, 1),
		sidebarFocused:  lipgloss.NewStyle().Background(surface).Foreground(text).Border(lipgloss.NormalBorder(), false, true, false, false).BorderForeground(accent).Padding(1, 1),
		section:         lipgloss.NewStyle().Foreground(muted).Bold(true),
		room:            lipgloss.NewStyle().Foreground(muted).Padding(0, 1),
		roomCurrent:     lipgloss.NewStyle().Foreground(accent2).Bold(true).Padding(0, 1),
		roomSelected:    lipgloss.NewStyle().Background(raised).Foreground(text).Bold(true).Border(lipgloss.ThickBorder(), false, false, false, true).BorderForeground(accent).PaddingLeft(1),
		conversationBar: lipgloss.NewStyle().Foreground(text).Padding(1, 2, 0, 2),
		topic:           lipgloss.NewStyle().Foreground(text).Bold(true),
		turnReady:       lipgloss.NewStyle().Background(accent2).Foreground(bg).Bold(true).Padding(0, 1),
		turnWaiting:     lipgloss.NewStyle().Background(surface).Foreground(muted).Padding(0, 1),
		transcript:      lipgloss.NewStyle().Foreground(text).Padding(0, 2),
		transcriptFocus: lipgloss.NewStyle().Foreground(text).Border(lipgloss.NormalBorder(), false, false, false, true).BorderForeground(faint).PaddingLeft(1),
		composerReady:   composer.BorderForeground(accent2),
		composerWaiting: composer.BorderForeground(faint),
		composerTitle:   lipgloss.NewStyle().Foreground(muted).Bold(true),
		status:          lipgloss.NewStyle().Background(surface).Foreground(muted).Padding(0, 1),
		statusError:     lipgloss.NewStyle().Background(surface).Foreground(danger).Padding(0, 1),
		statusGood:      lipgloss.NewStyle().Background(surface).Foreground(good).Padding(0, 1),
		author:          lipgloss.NewStyle().Foreground(accent).Bold(true),
		authorSelf:      lipgloss.NewStyle().Foreground(accent2).Bold(true),
		timestamp:       lipgloss.NewStyle().Foreground(faint),
		entry:           entry,
		entrySelf:       entry.BorderForeground(accent2).Background(surface),
		key:             lipgloss.NewStyle().Foreground(accent).Bold(true),
		modal:           lipgloss.NewStyle().Background(raised).Foreground(text).Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(1, 2),
	}
}
