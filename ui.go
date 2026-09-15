package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// ui.go — pure view layer. Every view is assembled as ONE content block
// (banner + subtitle + status pill + card + footer) and centered on both
// axes inside the terminal via lipgloss.Place.
// ---------------------------------------------------------------------------

// View dispatches on the current state.
func (m model) View() string {
	switch m.state {
	case viewRunning:
		return m.viewRunning()
	case viewResults:
		return m.viewResults()
	case viewExport:
		return m.viewExport()
	default:
		return m.viewInput()
	}
}

// header renders the ASCII block banner, subtitle, and status pill —
// everything centered over the banner width so the column stays balanced.
func (m model) header(subtitle string) string {
	var b strings.Builder
	b.WriteString(centerLine(styleBanner.Render(bannerSPECTRA), bannerWidth))
	b.WriteString("\n")
	b.WriteString(centerLine(styleSubtitle.Render(subtitle), bannerWidth))
	b.WriteString("\n")
	b.WriteString(centerLine(m.badge(), bannerWidth))
	return b.String()
}

// badge renders the Ollama status pill.
func (m model) badge() string {
	switch {
	case m.checking:
		return styleBadgeChecking.Render("◌ OLLAMA: checking…")
	case m.ollamaUp && m.ollamaModel:
		return styleBadgeOK.Render("● OLLAMA: READY")
	case m.ollamaUp && !m.ollamaModel:
		return styleBadgeDown.Render("● OLLAMA: MODEL MISSING")
	default:
		return styleBadgeDown.Render("● OLLAMA: OFFLINE")
	}
}

// viewInput — View 1: banner, status, 64-col input card, centered keymap.
func (m model) viewInput() string {
	card := styleCard.Render(lipgloss.JoinVertical(lipgloss.Left,
		labelStyle("TARGET URL"),
		m.urlInput.View(),
		"",
		labelStyle("RESEARCH OBJECTIVE / QUERY"),
		m.queryInput.View(),
	))

	hints := centerLine(
		styleHelp.Render("[tab] switch field   [enter] begin research run   [ctrl+c] quit"),
		cardWidth+2,
	)

	body := lipgloss.JoinVertical(lipgloss.Center,
		m.header("vision-assisted terminal research"),
		"",
		card,
		hints,
	)

	// System hints when Ollama is not ready, centered under the card.
	var extras []string
	if !m.checking && !m.ollamaUp {
		extras = append(extras, centerLine(styleError.Render("Start the daemon first:  ollama serve"), cardWidth+2))
	}
	if m.ollamaUp && !m.ollamaModel {
		extras = append(extras, centerLine(styleError.Render("Pull the vision model:   ollama pull minicpm-v4.6:latest"), cardWidth+2))
	}
	if m.stepNote != "" {
		extras = append(extras, centerLine(m.stepNote, cardWidth+2))
	}
	if len(extras) > 0 {
		body = lipgloss.JoinVertical(lipgloss.Center,
			append([]string{body, ""}, extras...)...)
	}

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, body)
}

// viewRunning — View 2: animated three-stage pipeline in a wide card.
func (m model) viewRunning() string {
	steps := []struct {
		id   int
		text string
	}{
		{stepScrape, "Navigating and rendering DOM with headless Chrome..."},
		{stepInfer, "Processing visual buffer through MiniCPM-V 4.6 (Ollama)..."},
		{stepCompile, "Compiling research synthesis..."},
	}

	lines := make([]string, 0, len(steps))
	for _, s := range steps {
		switch {
		case s.id < m.step:
			lines = append(lines, styleStepDone.Render("✔  "+s.text))
		case s.id == m.step:
			lines = append(lines, fmt.Sprintf("%s %s", m.spinner.View(),
				styleStepActive.Render(fmt.Sprintf("[%d/3] %s", s.id+1, s.text))))
		default:
			lines = append(lines, styleStepPending.Render("·  "+s.text))
		}
	}

	card := styleCardWide.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	hints := centerLine(styleHelp.Render("ctrl+c to abort"), cardWidth+8)

	body := lipgloss.JoinVertical(lipgloss.Center,
		m.header("research run in progress"),
		"",
		card,
		hints,
	)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, body)
}

// viewResults — View 3: metadata line, scrollable viewport box, keymap.
func (m model) viewResults() string {
	if m.resultErr != nil {
		card := styleCard.Render(lipgloss.JoinVertical(lipgloss.Left,
			styleError.Render("Run failed:"),
			m.resultErr.Error(),
		))
		hints := centerLine(styleHelp.Render("[enter] new run   [q] quit"), cardWidth+2)
		body := lipgloss.JoinVertical(lipgloss.Center,
			m.header("research synthesis"),
			"",
			card,
			hints,
		)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, body)
	}

	meta := fmt.Sprintf("target %s  ·  page %q  ·  model %s",
		m.result.Meta.TargetURL, m.result.Meta.PageTitle, m.result.Meta.Model)

	box := styleResultBox.Render(m.viewport.View())
	hints := centerLine(
		styleHelp.Render("[e] export dialog   [m] quick-save .md   [p] quick-save .pdf   [↑/↓] scroll   [esc] back   [q] quit"),
		resultWidth+2,
	)

	body := lipgloss.JoinVertical(lipgloss.Center,
		m.header("research synthesis"),
		"",
		centerLine(styleMeta.Render(meta), resultWidth+2),
		box,
	)

	// Save/export feedback ("✓ saved exports\\…") renders here so quick-key
	// exports from this view are confirmed on screen.
	if m.stepNote != "" {
		body = lipgloss.JoinVertical(lipgloss.Center,
			body,
			"",
			centerLine(m.stepNote, resultWidth+2),
		)
	}

	body = lipgloss.JoinVertical(lipgloss.Center, body, "", hints)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, body)
}

// viewExport — View 4: export quick-keys card.
func (m model) viewExport() string {
	menu := styleCard.Render(lipgloss.JoinVertical(lipgloss.Left,
		styleExportKey.Render("m / M")+"  Save Markdown (.md)      → ./exports/",
		styleExportKey.Render("p / P")+"  Compile to PDF (.pdf)    → ./exports/",
		styleExportKey.Render("r / R")+"  New Research Run",
		styleExportKey.Render("q / Q")+"  Quit",
	))

	body := lipgloss.JoinVertical(lipgloss.Center,
		m.header("export report"),
		"",
		menu,
	)

	if m.stepNote != "" {
		body = lipgloss.JoinVertical(lipgloss.Center,
			body,
			"",
			centerLine(m.stepNote, cardWidth+2),
		)
	}

	body = lipgloss.JoinVertical(lipgloss.Center,
		body,
		"",
		centerLine(styleHelp.Render("[esc] back to results"), cardWidth+2),
	)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, body)
}
