package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// ---------------------------------------------------------------------------
// Export — Markdown (.md) writer and styled PDF report compiler.
// ---------------------------------------------------------------------------

// ReportMeta describes a single research run, embedded in every export.
type ReportMeta struct {
	TargetURL   string
	FinalURL    string
	PageTitle   string
	Query       string
	Model       string
	RunAt       time.Time
	ScreenshotB bool // did the run include a screenshot
}

// Report is the full artifact handed to both exporters.
type Report struct {
	Meta       ReportMeta
	Markdown   string // synthesized notes in Markdown
	Screenshot []byte // PNG bytes (may be nil)
}

// SafeName builds a filesystem-friendly filename fragment from the target.
func SafeName(url string) string {
	s := strings.TrimSpace(url)
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '.', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, s)
	if len(s) > 60 {
		s = s[:60]
	}
	return strings.Trim(s, "-._")
}

// timestamp returns a compact stamp used in filenames.
func timestamp(t time.Time) string {
	return t.Format("2006-01-02_150405")
}

// headerLine renders the standard Markdown header block.
func headerLine(m ReportMeta) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# SPECTRA Research Report — %s\n\n", m.PageTitle))
	b.WriteString(fmt.Sprintf("> **Target:** %s\n", m.TargetURL))
	if m.FinalURL != "" && m.FinalURL != m.TargetURL {
		b.WriteString(fmt.Sprintf("> **Resolved URL:** %s\n", m.FinalURL))
	}
	b.WriteString(fmt.Sprintf("> **Query:** %s\n", m.Query))
	b.WriteString(fmt.Sprintf("> **Vision model:** %s\n", m.Model))
	b.WriteString(fmt.Sprintf("> **Generated:** %s\n\n---\n\n", m.RunAt.Format(time.RFC1123)))
	return b.String()
}

// WriteMarkdown exports the report as a .md file next to the executable.
func WriteMarkdown(r Report) (string, error) {
	name := fmt.Sprintf("spectra_%s_%s.md", SafeName(r.Meta.TargetURL), timestamp(r.Meta.RunAt))
	path := filepath.Join(".", "exports", name)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("export: create dir: %w", err)
	}

	var b strings.Builder
	b.WriteString(headerLine(r.Meta))
	b.WriteString(r.Markdown)
	b.WriteString("\n")

	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return "", fmt.Errorf("export: write markdown: %w", err)
	}
	return path, nil
}

// ---------------------------------------------------------------------------
// PDF export
// ---------------------------------------------------------------------------

// pdf palette (matches the TUI design tokens, adapted for print)
var (
	pdfBase     = rgb(0x0a, 0x0a, 0x0c) // near-black headers
	pdfCyan     = rgb(0x7a, 0xa2, 0xf7) // electric cyan accent
	pdfLavender = rgb(0xbb, 0x9a, 0xf7) // lavender accent
	pdfEmerald  = rgb(0x73, 0xda, 0xca) // success accent
	pdfZinc     = rgb(0x3b, 0x42, 0x61) // muted border
	pdfBody     = rgb(0x2a, 0x2c, 0x32) // body text (dark gray, print-safe)
)

// rgb returns an [3]int color triple usable by fpdf setters.
func rgb(r, g, b uint8) [3]int { return [3]int{int(r), int(g), int(b)} }

// mdBlock is one flattened block of parsed Markdown for the PDF renderer.
type mdBlock struct {
	Kind    string // "h1","h2","h3","p","li","code","quote","hr"
	Text    string
	Level   int
	Bullet  bool
	CodeStr string
}

// parseMarkdownBlocks flattens the synthesized Markdown into printable
// blocks. We deliberately render inline styles as plain text — the goal is
// a clean, readable report rather than a full MD-to-PDF typesetter.
//
// NOTE: goldmark AST segments index into the ORIGINAL source buffer, so
// every .Text()/.Value() call must receive the source bytes, never an
// empty scratch slice (that panics with slice-out-of-range).
func parseMarkdownBlocks(src string) []mdBlock {
	source := []byte(src)
	md := goldmark.New()
	doc := md.Parser().Parse(text.NewReader(source))

	var blocks []mdBlock
	var listBullet bool

	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			// Leaving a list resets bullet affinity.
			if _, ok := n.(*ast.List); ok {
				listBullet = false
			}
			return ast.WalkContinue, nil
		}

		switch node := n.(type) {
		case *ast.Heading:
			blocks = append(blocks, mdBlock{Kind: fmt.Sprintf("h%d", node.Level), Level: node.Level,
				Text: strings.TrimSpace(string(n.Text(source)))})
			return ast.WalkSkipChildren, nil

		case *ast.Paragraph:
			blocks = append(blocks, mdBlock{Kind: "p", Bullet: listBullet,
				Text: strings.TrimSpace(string(n.Text(source)))})
			return ast.WalkSkipChildren, nil

		case *ast.List:
			listBullet = true
			return ast.WalkContinue, nil

		case *ast.ListItem:
			blocks = append(blocks, mdBlock{Kind: "li", Bullet: true,
				Text: "• " + strings.TrimSpace(string(n.Text(source)))})
			return ast.WalkSkipChildren, nil

		case *ast.FencedCodeBlock:
			code := string(node.Lines().Value(source))
			blocks = append(blocks, mdBlock{Kind: "code", CodeStr: code})
			return ast.WalkSkipChildren, nil

		case *ast.Blockquote:
			blocks = append(blocks, mdBlock{Kind: "quote",
				Text: "> " + strings.TrimSpace(string(n.Text(source)))})
			return ast.WalkSkipChildren, nil

		case *ast.ThematicBreak:
			blocks = append(blocks, mdBlock{Kind: "hr"})
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})

	return blocks
}

// linkPattern matches [text](url) markdown links and bare URLs.
var (
	mdLinkPattern = regexp.MustCompile(`\[([^\]]*)\]\((https?://[^)\s]+)\)`)
	bareURLPatt   = regexp.MustCompile(`https?://[^\s)>\]"']+`)
)

// extractLinks pulls unique, sorted http(s) URLs out of the markdown so the
// PDF can cite them in a References section (links are not clickable in the
// fpdf body, so we make them recoverable instead).
func extractLinks(md string) []string {
	seen := map[string]bool{}

	for _, m := range mdLinkPattern.FindAllStringSubmatch(md, -1) {
		seen[m[2]] = true
	}
	// Bare URLs that are not already inside a markdown link.
	stripped := mdLinkPattern.ReplaceAllString(md, "")
	for _, u := range bareURLPatt.FindAllString(stripped, -1) {
		seen[strings.TrimRight(u, ".,;:!")] = true
	}

	out := make([]string, 0, len(seen))
	for u := range seen {
		if len(u) < 12 { // skip junk like http://x
			continue
		}
		out = append(out, u)
	}
	sort.Strings(out)
	return out
}

// WritePDF compiles the report into a styled, paginated A4 PDF.
func WritePDF(r Report) (string, error) {
	name := fmt.Sprintf("spectra_%s_%s.pdf", SafeName(r.Meta.TargetURL), timestamp(r.Meta.RunAt))
	path := filepath.Join(".", "exports", name)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("export: create dir: %w", err)
	}

	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(18, 18, 18)
	pdf.SetAutoPageBreak(true, 20)
	pdf.AddPage()

	// --- Cover header band -------------------------------------------------
	pdf.SetFillColor(pdfBase[0], pdfBase[1], pdfBase[2])
	pdf.Rect(0, 0, 210, 34, "F")

	pdf.SetTextColor(255, 255, 255)
	pdf.SetFont("Helvetica", "B", 20)
	pdf.SetXY(18, 10)
	pdf.CellFormat(120, 10, "SPECTRA", "", 2, "L", false, 0, "")

	pdf.SetFont("Helvetica", "", 10)
	pdf.SetTextColor(pdfCyan[0], pdfCyan[1], pdfCyan[2])
	pdf.CellFormat(120, 6, "Vision-Assisted Terminal Research Report", "", 0, "L", false, 0, "")

	// Timestamp, right-aligned in the band.
	pdf.SetXY(130, 12)
	pdf.SetTextColor(pdfZinc[0]+60, pdfZinc[1]+60, pdfZinc[2]+60)
	pdf.SetFont("Courier", "", 8)
	pdf.CellFormat(60, 6, r.Meta.RunAt.Format("2006-01-02 15:04 MST"), "", 0, "R", false, 0, "")

	pdf.Ln(16)

	// --- Metadata table ----------------------------------------------------
	metaRow := func(label, value string) {
		pdf.SetFont("Helvetica", "B", 9)
		pdf.SetTextColor(pdfLavender[0], pdfLavender[1], pdfLavender[2])
		pdf.CellFormat(32, 6, label, "", 0, "L", false, 0, "")
		pdf.SetFont("Helvetica", "", 9)
		pdf.SetTextColor(pdfBody[0], pdfBody[1], pdfBody[2])
		pdf.MultiCell(0, 6, value, "", "L", false)
	}
	metaRow("TARGET", r.Meta.TargetURL)
	if r.Meta.FinalURL != "" && r.Meta.FinalURL != r.Meta.TargetURL {
		metaRow("RESOLVED", r.Meta.FinalURL)
	}
	metaRow("PAGE", r.Meta.PageTitle)
	metaRow("QUERY", r.Meta.Query)
	metaRow("MODEL", r.Meta.Model)

	pdf.Ln(2)

	// Accent rule
	pdf.SetDrawColor(pdfEmerald[0], pdfEmerald[1], pdfEmerald[2])
	pdf.SetLineWidth(0.8)
	pdf.Line(18, pdf.GetY(), 192, pdf.GetY())
	pdf.Ln(6)

	// --- Screenshot (optional) ---------------------------------------------
	if len(r.Screenshot) > 0 {
		imgTmp := filepath.Join(os.TempDir(), "spectra_shot.png")
		if err := os.WriteFile(imgTmp, r.Screenshot, 0o644); err == nil {
			defer os.Remove(imgTmp)
			pdf.SetFont("Helvetica", "B", 11)
			pdf.SetTextColor(pdfCyan[0], pdfCyan[1], pdfCyan[2])
			pdf.CellFormat(0, 8, "Captured Viewport (1280x800)", "", 1, "L", false, 0, "")
			// 174mm printable width; 1280x800 aspect -> ~108.75mm tall
			pdf.ImageOptions(imgTmp, 18, pdf.GetY(), 174, 0, false, fpdf.ImageOptions{ImageType: "PNG"}, 0, "")
			pdf.Ln(108.75 + 6)
		}
	}

	// --- Body ---------------------------------------------------------------
	blocks := parseMarkdownBlocks(r.Markdown)

	for _, blk := range blocks {
		switch blk.Kind {
		case "h1":
			pdf.Ln(2)
			pdf.SetFont("Helvetica", "B", 16)
			pdf.SetTextColor(pdfCyan[0], pdfCyan[1], pdfCyan[2])
			pdf.MultiCell(0, 8, blk.Text, "", "L", false)
			pdf.Ln(1)

		case "h2":
			pdf.Ln(2)
			pdf.SetFont("Helvetica", "B", 13)
			pdf.SetTextColor(pdfLavender[0], pdfLavender[1], pdfLavender[2])
			pdf.MultiCell(0, 7, blk.Text, "", "L", false)
			pdf.Ln(1)

		case "h3":
			pdf.SetFont("Helvetica", "BI", 11)
			pdf.SetTextColor(pdfEmerald[0], pdfEmerald[1], pdfEmerald[2])
			pdf.MultiCell(0, 6, blk.Text, "", "L", false)
			pdf.Ln(1)

		case "p":
			pdf.SetFont("Helvetica", "", 10)
			pdf.SetTextColor(pdfBody[0], pdfBody[1], pdfBody[2])
			pdf.MultiCell(0, 5.5, blk.Text, "", "L", false)
			pdf.Ln(1.5)

		case "li":
			pdf.SetFont("Helvetica", "", 10)
			pdf.SetTextColor(pdfBody[0], pdfBody[1], pdfBody[2])
			x := pdf.GetX()
			pdf.SetX(x + 4)
			pdf.MultiCell(0, 5.5, blk.Text, "", "L", false)
			pdf.SetX(x)

		case "quote":
			pdf.SetFont("Helvetica", "I", 10)
			pdf.SetTextColor(pdfZinc[0]+40, pdfZinc[1]+40, pdfZinc[2]+40)
			pdf.MultiCell(0, 5.5, blk.Text, "", "L", false)

		case "code":
			pdf.SetFont("Courier", "", 9)
			pdf.SetFillColor(24, 24, 28)
			pdf.SetTextColor(pdfEmerald[0], pdfEmerald[1], pdfEmerald[2])
			pdf.MultiCell(0, 5, blk.CodeStr, "", "L", true)
			pdf.Ln(2)

		case "hr":
			pdf.SetDrawColor(pdfZinc[0], pdfZinc[1], pdfZinc[2])
			pdf.SetLineWidth(0.3)
			pdf.Line(18, pdf.GetY(), 192, pdf.GetY())
			pdf.Ln(4)
		}
	}

	// --- References (link citations survive the PDF) ------------------------
	refs := extractLinks(r.Markdown)
	if len(refs) > 0 {
		pdf.Ln(2)
		pdf.SetFont("Helvetica", "B", 12)
		pdf.SetTextColor(pdfCyan[0], pdfCyan[1], pdfCyan[2])
		pdf.CellFormat(0, 8, "References", "", 1, "L", false, 0, "")
		pdf.SetFont("Helvetica", "", 8)
		pdf.SetTextColor(pdfZinc[0]+30, pdfZinc[1]+30, pdfZinc[2]+30)
		for i, ref := range refs {
			pdf.MultiCell(0, 4.5, fmt.Sprintf("[%d] %s", i+1, ref), "", "L", false)
		}
	}

	// --- Footer page numbers -------------------------------------------------
	total := pdf.PageCount()
	for i := 1; i <= total; i++ {
		pdf.SetPage(i)
		pdf.SetY(-14)
		pdf.SetFont("Helvetica", "", 8)
		pdf.SetTextColor(pdfZinc[0], pdfZinc[1], pdfZinc[2])
		pdf.CellFormat(0, 8, fmt.Sprintf("SPECTRA — %d / %d", i, total), "", 0, "C", false, 0, "")
	}

	if err := pdf.OutputFileAndClose(path); err != nil {
		return "", fmt.Errorf("export: write pdf: %w", err)
	}
	return path, nil
}
