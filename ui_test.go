package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// stripANSI removes escape sequences so width math is testable.
func stripANSI(s string) string {
	return ansi.Strip(s)
}

// TestBannerDimensions guards the segmented-block banner geometry.
func TestBannerDimensions(t *testing.T) {
	lines := strings.Split(bannerSPECTRA, "\n")
	if len(lines) != 5 {
		t.Fatalf("banner must have 5 rows, got %d", len(lines))
	}
	t.Logf("\n%s", bannerSPECTRA)
	for i, l := range lines {
		if w := lipgloss.Width(l); w != bannerWidth {
			t.Errorf("banner row %d width = %d, want %d", i, w, bannerWidth)
		}
	}
}

// TestHeaderContainsBannerAndBadge checks header assembly.
func TestHeaderContainsBannerAndBadge(t *testing.T) {
	m := newModel()
	m.width, m.height = 120, 40
	m.checking = false
	m.ollamaUp = true
	m.ollamaModel = true

	h := stripANSI(m.header("vision-assisted terminal research"))
	if !strings.Contains(h, "█████") {
		t.Error("header missing banner blocks")
	}
	if !strings.Contains(h, "OLLAMA: READY") {
		t.Error("header missing status pill")
	}
	if !strings.Contains(h, "vision-assisted terminal research") {
		t.Error("header missing subtitle")
	}
}

// TestInputViewIsCentered verifies the form renders inside a lipgloss.Place
// block that fills the whole viewport (true center layout).
func TestInputViewIsCentered(t *testing.T) {
	m := newModel()
	m.width, m.height = 120, 40
	m.checking = false
	m.ollamaUp = true
	m.ollamaModel = true

	out := stripANSI(m.View())
	lines := strings.Split(out, "\n")

	if len(lines) != 40 {
		t.Fatalf("view must pad to full height (40), got %d lines", len(lines))
	}

	// Find banner rows and confirm they are horizontally centered.
	firstBanner := -1
	for i, l := range lines {
		if strings.Contains(l, "█████") {
			firstBanner = i
			break
		}
	}
	if firstBanner == -1 {
		t.Fatal("no banner row found in view")
	}

	// Blank margins must be symmetric (±1) for a centered layout.
	line := lines[firstBanner]
	leftBlanks := len(line) - len(strings.TrimLeft(line, " "))
	rightBlanks := len(line) - len(strings.TrimRight(line, " "))
	if leftBlanks < 10 {
		t.Errorf("banner left margin = %d, want >= 10 for a centered layout", leftBlanks)
	}
	if abs(leftBlanks-rightBlanks) > 1 {
		t.Errorf("banner not horizontally centered: left=%d right=%d", leftBlanks, rightBlanks)
	}

	// Vertical: banner must not start at row 0 (vertically centered-ish).
	if firstBanner < 3 {
		t.Errorf("banner starts at row %d; expected vertical centering", firstBanner)
	}
}

// TestExportViewKeymap checks the four export actions render.
func TestExportViewKeymap(t *testing.T) {
	m := newModel()
	m.width, m.height = 120, 40
	m.state = viewExport
	m.result = Report{Meta: ReportMeta{TargetURL: "https://x.com"}}

	out := stripANSI(m.View())
	for _, want := range []string{"m", "p", "r", "q", "Markdown", "PDF", "New Research Run"} {
		if !strings.Contains(out, want) {
			t.Errorf("export view missing %q", want)
		}
	}
}

// TestCardWidthConstrained ensures the input card respects the 64-char budget.
func TestCardWidthConstrained(t *testing.T) {
	m := newModel()
	m.width, m.height = 120, 40

	view := m.urlInput.View()
	for _, l := range strings.Split(view, "\n") {
		if w := len(stripANSI(l)); w > cardWidth {
			t.Errorf("input line width %d exceeds card budget %d", w, cardWidth)
		}
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
