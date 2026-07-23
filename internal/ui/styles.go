package ui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// styles deliberately keeps the room chrome quiet. The transcript is the
// workspace; contrast and spacing establish hierarchy, with the composer as
// the one prominent bordered surface.
type styles struct {
	background color.Color
	surface    color.Color
	text       color.Color
	muted      color.Color
	faint      color.Color
	accent     color.Color
	accent2    color.Color
	good       color.Color
	danger     color.Color

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
	bg := choose(lipgloss.Color("#FAFAFA"), lipgloss.Color("#101010"))
	surface := choose(lipgloss.Color("#F1F1F1"), lipgloss.Color("#161616"))
	raised := choose(lipgloss.Color("#FFFFFF"), lipgloss.Color("#1F1F1F"))
	text := choose(lipgloss.Color("#202020"), lipgloss.Color("#E7E7E7"))
	muted := choose(lipgloss.Color("#707070"), lipgloss.Color("#929292"))
	faint := choose(lipgloss.Color("#D0D0D0"), lipgloss.Color("#4B4B4B"))
	accent := choose(lipgloss.Color("#383838"), lipgloss.Color("#D8D8D8"))
	accent2 := choose(lipgloss.Color("#555555"), lipgloss.Color("#BDBDBD"))
	good := choose(lipgloss.Color("#484848"), lipgloss.Color("#CFCFCF"))
	danger := choose(lipgloss.Color("#111111"), lipgloss.Color("#F2F2F2"))
	mutedStyle := lipgloss.NewStyle().Foreground(muted).Faint(true)

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
		background: bg, surface: surface, text: text, muted: muted,
		faint: faint, accent: accent, accent2: accent2, good: good, danger: danger,
		topbar:          lipgloss.NewStyle().Background(surface).Foreground(text),
		brand:           lipgloss.NewStyle().Background(accent).Foreground(bg).Bold(true).Padding(0, 1),
		crumb:           mutedStyle.PaddingLeft(1),
		identity:        lipgloss.NewStyle().Foreground(accent2).Bold(true),
		subtle:          mutedStyle,
		sidebar:         lipgloss.NewStyle().Background(surface).Foreground(text).Padding(1, 1),
		sidebarFocused:  lipgloss.NewStyle().Background(surface).Foreground(text).Border(lipgloss.NormalBorder(), false, true, false, false).BorderForeground(accent).Padding(1, 1),
		section:         mutedStyle.Bold(true),
		room:            mutedStyle.Padding(0, 1),
		roomCurrent:     lipgloss.NewStyle().Foreground(accent2).Bold(true).Padding(0, 1),
		roomSelected:    lipgloss.NewStyle().Background(raised).Foreground(text).Bold(true).Border(lipgloss.ThickBorder(), false, false, false, true).BorderForeground(accent).PaddingLeft(1),
		conversationBar: lipgloss.NewStyle().Foreground(text).Padding(1, 2, 0, 2),
		topic:           lipgloss.NewStyle().Foreground(text).Bold(true),
		turnReady:       lipgloss.NewStyle().Background(accent2).Foreground(bg).Bold(true).Padding(0, 1),
		turnWaiting:     mutedStyle.Background(surface).Padding(0, 1),
		transcript:      lipgloss.NewStyle().Foreground(text).Padding(0, 2),
		transcriptFocus: lipgloss.NewStyle().Foreground(text).Border(lipgloss.NormalBorder(), false, false, false, true).BorderForeground(faint).PaddingLeft(1),
		composerReady:   composer.BorderForeground(accent2),
		composerWaiting: composer.BorderForeground(faint),
		composerTitle:   mutedStyle.Bold(true),
		status:          mutedStyle.Background(surface).Padding(0, 1),
		statusError:     lipgloss.NewStyle().Background(surface).Foreground(danger).Padding(0, 1),
		statusGood:      lipgloss.NewStyle().Background(surface).Foreground(good).Padding(0, 1),
		author:          lipgloss.NewStyle().Foreground(accent).Bold(true),
		authorSelf:      lipgloss.NewStyle().Foreground(accent2).Bold(true),
		timestamp:       lipgloss.NewStyle().Foreground(faint).Faint(true),
		entry:           entry,
		entrySelf:       entry.BorderForeground(accent2).Background(surface),
		key:             lipgloss.NewStyle().Foreground(accent).Bold(true),
		modal:           lipgloss.NewStyle().Background(raised).Foreground(text).Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(1, 2),
	}
}
