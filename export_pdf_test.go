package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// TestPDFSanitize guards the CP1252 transliteration: fpdf core fonts render
// raw UTF-8 above 0x7f as mojibake, so every class of model output must map
// to a representable sequence.
func TestPDFSanitize(t *testing.T) {
	cases := map[string]string{
		"plain ascii":        "plain ascii",
		"em\u2014dash":       "em--dash",
		"en\u2013dash":       "en-dash",
		"\u2022bullet":       "-bullet",
		"caf\u00e9":          "caf\u00e9", // é is directly representable
		"\u201cquoted\u201d": "\"quoted\"",
		"ellips\u2026s":      "ellips...s",
		"arrow \u2192 x":     "arrow -> x",
		"check \u2713 ok":    "check * ok",
		"emoji \U0001F680!":  "emoji !",   // emoji dropped, neighbors kept
		"zero\u200bwidth":    "zerowidth", // zero-width space dropped
		"line\nbreak":        "line\nbreak",
	}
	for in, want := range cases {
		if got := pdfSanitize(in); got != want {
			t.Errorf("pdfSanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestParseMarkdownBlocksTable verifies GFM tables flatten into structured
// rows instead of degenerating into pipe-soup paragraphs.
func TestParseMarkdownBlocksTable(t *testing.T) {
	md := "| Metric | Value |\n|---|---|\n| Price | $42 |\n| Users | 1,204 |\n\nTrailing paragraph.\n"
	blocks := parseMarkdownBlocks(md)

	var tbl *mdBlock
	paras := 0
	for i := range blocks {
		if blocks[i].Kind == "table" {
			tbl = &blocks[i]
		}
		if blocks[i].Kind == "p" {
			paras++
		}
	}
	if tbl == nil {
		t.Fatalf("no table block parsed; got kinds: %v", kinds(blocks))
	}
	if len(tbl.Table) != 3 {
		t.Fatalf("expected 3 rows (header + 2), got %d: %v", len(tbl.Table), tbl.Table)
	}
	if tbl.Table[0][0] != "Metric" || tbl.Table[0][1] != "Value" {
		t.Errorf("header row wrong: %v", tbl.Table[0])
	}
	if tbl.Table[1][1] != "$42" {
		t.Errorf("data cell wrong: %v", tbl.Table[1])
	}
	if paras != 1 {
		t.Errorf("expected 1 trailing paragraph, got %d", paras)
	}
}

func kinds(bs []mdBlock) []string {
	out := make([]string, len(bs))
	for i, b := range bs {
		out[i] = b.Kind
	}
	return out
}

// TestWritePDFUnicodeReport runs the full exporter against a report loaded
// with exactly the characters MiniCPM-V emits (em dashes, bullets, check
// marks, emoji, accented words) plus a real screenshot. It fails on any
// fpdf panic, CP1252 metric error, or unwritable output.
func TestWritePDFUnicodeReport(t *testing.T) {
	dir := t.TempDir()
	restore := chdir(t, dir)
	defer restore()

	md := "# Summary\n\n" +
		"Caf\u00e9 pricing \u2014 up 12% YoY \u2022 strong quarter \u2713\n\n" +
		"| Item | Cost |\n|---|---|\n| Widget \u00e9dition | \u20ac49.99 |\n\n" +
		"- bullet \u2014 one\n- bullet \u2014 two \U0001F680\n\n" +
		"## Key Findings\n\nArrow math: a \u2192 b \u2264 c \u2265 d.\n"

	report := Report{
		Meta: ReportMeta{
			TargetURL: "https://example.com/p\u00e1ge",
			PageTitle: "Ex\u00e4mple \u2014 T\u00eftle",
			Query:     "what does this cost?",
			Model:     "minicpm-v4.6:latest",
			RunAt:     time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC),
		},
		Markdown:   md,
		Screenshot: tinyPNG(t),
	}

	path, err := WritePDF(report)
	if err != nil {
		t.Fatalf("WritePDF failed: %v", err)
	}
	if !strings.HasPrefix(filepath.Base(path), "spectra_example.com") {
		t.Errorf("unexpected filename: %s", path)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("pdf not readable: %v", err)
	}
	if !strings.HasPrefix(string(raw[:5]), "%PDF-") {
		t.Errorf("output is not a PDF: %q", raw[:8])
	}
	if len(raw) < 2000 {
		t.Errorf("suspiciously small pdf (%d bytes)", len(raw))
	}
}

// TestCapsLockQuickKeys proves the results-view quick keys fire when the
// terminal reports uppercase runes (Caps Lock on): M and P must save.
func TestCapsLockQuickKeys(t *testing.T) {
	dir := t.TempDir()
	restore := chdir(t, dir)
	defer restore()

	m := newModel()
	m.state = viewResults
	m.result = Report{
		Meta: ReportMeta{
			TargetURL: "https://example.com",
			PageTitle: "t",
			Query:     "q",
			Model:     "m",
			RunAt:     time.Now(),
		},
		Markdown: "# T\n\nbody",
	}

	upm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	m2 := upm.(model)
	if !strings.Contains(m2.stepNote, "saved") {
		t.Errorf("CapsLock 'M' did not save; stepNote=%q", m2.stepNote)
	}
	if strings.Contains(m2.stepNote, "failed") {
		t.Errorf("CapsLock 'M' errored: %q", m2.stepNote)
	}

	upm, _ = m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	m3 := upm.(model)
	if !strings.Contains(m3.stepNote, "saved") || !strings.Contains(m3.stepNote, ".pdf") {
		t.Errorf("CapsLock 'P' did not save a pdf; stepNote=%q", m3.stepNote)
	}

	// Sanity: entries were actually written under ./exports of the temp dir.
	entries, err := os.ReadDir(filepath.Join(dir, "exports"))
	if err != nil || len(entries) != 2 {
		t.Errorf("expected 2 exports on disk, got %d (err=%v)", len(entries), err)
	}
}

// --- helpers -------------------------------------------------------------

// chdir moves the test into dir (WriteMarkdown/WritePDF write relative to
// the process working directory) and returns a restore func.
func chdir(t *testing.T, dir string) func() {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	return func() { _ = os.Chdir(old) }
}

// tinyPNG encodes a 4x4 opaque PNG so the screenshot branch runs for real.
func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for x := 0; x < 4; x++ {
		for y := 0; y < 4; y++ {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	var buf strings.Builder
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	return []byte(buf.String())
}
