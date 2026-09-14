package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// styles.go — design tokens, ASCII block banner, and every Lip Gloss style.
// ---------------------------------------------------------------------------

// Palette — Claude-warm accents over a Nord/Tokyo-night base.
var (
	colorBase     = lipgloss.Color("#0a0a0c") // matte dark background
	colorTerracot = lipgloss.Color("#D97757") // warm terracotta banner accent
	colorEmber    = lipgloss.Color("#E06C54") // secondary ember accent
	colorCyan     = lipgloss.Color("#7aa2f7") // electric cyan
	colorLavender = lipgloss.Color("#bb9af7") // muted lavender
	colorZinc     = lipgloss.Color("#3b4261") // slate/zinc borders
	colorEmerald  = lipgloss.Color("#73daca") // mint status
	colorRed      = lipgloss.Color("#f7768e") // error
	colorText     = lipgloss.Color("#c0caf5") // primary text
	colorDim      = lipgloss.Color("#565f89") // dim text
	colorInk      = lipgloss.Color("#0a0a0c") // "black" text on bright pills
)

// glyphFont is a 5x5 segmented block font — every glyph is exactly 5
// columns wide, so joining letters with single spaces yields banner rows
// of provably equal width (no hand-drawn raggedness).
var glyphFont = map[rune][5]string{
	'S': {"█████", "█    ", "█████", "    █", "█████"},
	'P': {"█████", "█   █", "█████", "█    ", "█    "},
	'E': {"█████", "█    ", "████ ", "█    ", "█████"},
	'C': {"█████", "█    ", "█    ", "█    ", "█████"},
	'T': {"█████", "  █  ", "  █  ", "  █  ", "  █  "},
	'R': {"████ ", "█   █", "████ ", "█  █ ", "█   █"},
	'A': {"█████", "█   █", "█████", "█   █", "█   █"},
	' ': {"     ", "     ", "     ", "     ", "     "},
}

// bannerLines builds the segmented-block wordmark for the given text.
func bannerLines(word string) [5]string {
	var rows [5]string
	for i, r := range word {
		g, ok := glyphFont[r]
		if !ok {
			g = glyphFont[' ']
		}
		for row := 0; row < 5; row++ {
			rows[row] += g[row]
			if i < len(word)-1 {
				rows[row] += " "
			}
		}
	}
	return rows
}

// bannerSPECTRA is the rendered wordmark; bannerWidth its display width.
var (
	bannerSPECTRA = func() string {
		r := bannerLines("SPECTRA")
		lines := make([]string, 5)
		for i := range r {
			lines[i] = r[i]
		}
		return strings.Join(lines, "\n")
	}()
	bannerWidth = lipgloss.Width(bannerSPECTRA)
)

// cardWidth is the fixed content width of centered UI cards.
const cardWidth = 64

// resultWidth is the max box width of the results viewport.
const resultWidth = 88

var (
	// --- header -------------------------------------------------------------
	styleBanner = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorTerracot)

	styleSubtitle = lipgloss.NewStyle().
			Foreground(colorLavender)

	// --- status pill ----------------------------------------------------------
	styleBadgeOK = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorInk).
			Background(colorEmerald).
			Padding(0, 1)

	styleBadgeDown = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorInk).
			Background(colorRed).
			Padding(0, 1)

	styleBadgeChecking = lipgloss.NewStyle().
				Foreground(colorDim).
				Padding(0, 1)

	// --- cards ----------------------------------------------------------------
	styleCard = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorZinc).
			Padding(1, 2).
			Width(cardWidth)

	// Wide variant for the execution view (long step labels + spinner).
	styleCardWide = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorZinc).
			Padding(1, 2).
			Width(cardWidth + 6)

	styleResultBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorZinc)

	// --- execution steps --------------------------------------------------------
	styleStepActive  = lipgloss.NewStyle().Bold(true).Foreground(colorTerracot)
	styleStepDone    = lipgloss.NewStyle().Foreground(colorEmerald)
	styleStepPending = lipgloss.NewStyle().Foreground(colorDim)

	// --- misc -----------------------------------------------------------------
	styleError       = lipgloss.NewStyle().Bold(true).Foreground(colorRed)
	styleHelp        = lipgloss.NewStyle().Foreground(colorDim)
	styleMeta        = lipgloss.NewStyle().Foreground(colorDim)
	styleLabel       = lipgloss.NewStyle().Bold(true).Foreground(colorDim)
	styleExportKey   = lipgloss.NewStyle().Bold(true).Foreground(colorInk).Background(colorLavender).Padding(0, 1)
	styleSuccessPath = lipgloss.NewStyle().Foreground(colorEmerald)
)

// ---------------------------------------------------------------------------
// Small render helpers shared by ui.go and tui.go.
// ---------------------------------------------------------------------------

func labelStyle(s string) string { return styleLabel.Render(s) }
func errStyle(s string) string   { return styleError.Render("✗ " + s) }
func okStyle(s string) string    { return styleSuccessPath.Render("✓ " + s) }

// centerLine centers a single line (or pre-rendered block) within a width.
func centerLine(s string, width int) string {
	return lipgloss.NewStyle().Width(width).Align(lipgloss.Center).Render(s)
}
