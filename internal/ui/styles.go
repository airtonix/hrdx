package ui

import "github.com/charmbracelet/lipgloss"

var (
	colorAccent = lipgloss.Color("81")  // cyan
	colorAlt    = lipgloss.Color("212") // pink
	colorMuted  = lipgloss.Color("243")
	colorFaint  = lipgloss.Color("238")
	colorGood   = lipgloss.Color("78")
	colorBusy   = lipgloss.Color("214")
	colorBad    = lipgloss.Color("203")
	colorBarBg  = lipgloss.Color("234")
	colorBarFg  = lipgloss.Color("250")
	colorInk    = lipgloss.Color("232")

	// Header and footer bars.
	styleBar      = lipgloss.NewStyle().Background(colorBarBg)
	styleBarMuted = lipgloss.NewStyle().Background(colorBarBg).Foreground(colorMuted)
	styleBarText  = lipgloss.NewStyle().Background(colorBarBg).Foreground(colorBarFg)
	styleBarError = lipgloss.NewStyle().Background(colorBarBg).Foreground(colorBad).Bold(true)
	styleBarInfo  = lipgloss.NewStyle().Background(colorBarBg).Foreground(colorAccent).Bold(true)
	styleLogo     = lipgloss.NewStyle().Background(colorAccent).Foreground(colorInk).Bold(true)

	styleBadgeTerm = lipgloss.NewStyle().Background(colorFaint).Foreground(colorBarFg).Bold(true)
	// The tab bar is a solid dark strip like the header; active tab pops
	// in accent, idle tabs stay muted on the dark background.
	styleTabBar      = lipgloss.NewStyle().Background(colorInk)
	styleTabActive   = lipgloss.NewStyle().Background(colorAccent).Foreground(colorInk).Bold(true)
	styleTabIdle     = lipgloss.NewStyle().Background(colorInk).Foreground(colorMuted)
	styleBadgePrefix = lipgloss.NewStyle().Background(colorAlt).Foreground(colorInk).Bold(true)
	styleBadgeInput  = lipgloss.NewStyle().Background(colorGood).Foreground(colorInk).Bold(true)

	// Sidebar.
	styleSection   = lipgloss.NewStyle().Foreground(colorMuted).Bold(true)
	styleSpaceSel  = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	styleSpaceDim  = lipgloss.NewStyle().Foreground(colorBarFg)
	stylePaneSel   = lipgloss.NewStyle().Foreground(colorAccent)
	stylePaneDim   = lipgloss.NewStyle().Foreground(colorMuted)
	styleDotOn     = lipgloss.NewStyle().Foreground(colorGood)
	styleDotBusy   = lipgloss.NewStyle().Foreground(colorBusy)
	styleDotOff    = lipgloss.NewStyle().Foreground(colorBad)
	styleNewButton = lipgloss.NewStyle().Foreground(colorAccent)
	styleMuted     = lipgloss.NewStyle().Foreground(colorMuted)
	styleError     = lipgloss.NewStyle().Foreground(colorBad)
)
