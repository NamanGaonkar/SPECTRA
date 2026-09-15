package main

import (
	"bytes"
	"fmt"
	"image"
	_ "image/png" // register PNG decoder for screenshot dimensions
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
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
	if runes := []rune(s); len(runes) > 60 {
		s = string(runes[:60]) // rune-safe: byte slicing would split multi-byte chars
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

// renderPDFTable draws a GFM table as a bordered, shaded-header grid that
// wraps within textW. All text passes through pdfSanitize.
func renderPDFTable(pdf *fpdf.Fpdf, rows [][]string, textW float64) {
	if len(rows) == 0 {
		return
	}
	cols := 0
	for _, r := range rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	if cols == 0 {
		return
	}
	colW := textW / float64(cols)

	drawRow := func(cells []string, header bool) {
		height := 7.0
		// Pre-measure: the row is as tall as its tallest wrapped cell.
		pdf.SetFont("Helvetica", "B", 9)
		if !header {
			pdf.SetFont("Helvetica", "", 9)
		}
		for c := 0; c < cols; c++ {
			cell := ""
			if c < len(cells) {
				cell = pdfSanitize(cells[c])
			}
			if lines := pdf.SplitText(cell, colW-4); len(lines) > 1 {
				if need := float64(len(lines)) * 5.2; need > height {
					height = need
				}
			}
		}

		// Keep whole rows intact across page breaks.
		_, pageH := pdf.GetPageSize()
		_, _, _, bottom := pdf.GetMargins()
		if pdf.GetY()+height > pageH-bottom {
			pdf.AddPage()
		}

		if header {
			pdf.SetFillColor(pdfBase[0]+22, pdfBase[1]+22, pdfBase[2]+26)
		}
		pdf.SetDrawColor(pdfZinc[0], pdfZinc[1], pdfZinc[2])
		pdf.SetLineWidth(0.2)
		startX := pdf.GetX()
		startY := pdf.GetY()
		for c := 0; c < cols; c++ {
			cell := ""
			if c < len(cells) {
				cell = pdfSanitize(cells[c])
			}
			if header {
				pdf.SetTextColor(pdfCyan[0], pdfCyan[1], pdfCyan[2])
				pdf.SetFont("Helvetica", "B", 9)
			} else {
				pdf.SetTextColor(pdfBody[0], pdfBody[1], pdfBody[2])
				pdf.SetFont("Helvetica", "", 9)
			}
			x := startX + float64(c)*colW
			pdf.Rect(x, startY, colW, height, "D")
			if header {
				pdf.Rect(x, startY, colW, height, "F")
			}
			pdf.SetXY(x+2, startY+1.5)
			pdf.MultiCell(colW-4, 5.2, cell, "", "L", false)
		}
		pdf.SetXY(startX, startY+height)
	}

	for i, row := range rows {
		drawRow(row, i == 0)
	}
	pdf.Ln(3)
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
	Kind    string // "h1","h2","h3","p","li","code","quote","hr","table"
	Text    string
	Level   int
	CodeStr string
	Table   [][]string // GFM table rows (header row first)
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
	// GFM adds tables (and strikethrough) — the vision model loves both.
	md := goldmark.New(goldmark.WithExtensions(extension.GFM))
	doc := md.Parser().Parse(text.NewReader(source))

	var blocks []mdBlock
	var listOrdered bool
	var itemIndex int

	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			// Leaving a list resets list state.
			if _, ok := n.(*ast.List); ok {
				listOrdered = false
			}
			return ast.WalkContinue, nil
		}

		switch node := n.(type) {
		case *ast.Heading:
			blocks = append(blocks, mdBlock{Kind: fmt.Sprintf("h%d", node.Level), Level: node.Level,
				Text: strings.TrimSpace(string(n.Text(source)))})
			return ast.WalkSkipChildren, nil

		case *ast.Paragraph:
			blocks = append(blocks, mdBlock{Kind: "p",
				Text: strings.TrimSpace(string(n.Text(source)))})
			return ast.WalkSkipChildren, nil

		case *ast.List:
			listOrdered = node.IsOrdered()
			itemIndex = 0
			return ast.WalkContinue, nil

		case *ast.ListItem:
			itemIndex++
			marker := "• "
			if listOrdered {
				marker = fmt.Sprintf("%d. ", itemIndex)
			}
			blocks = append(blocks, mdBlock{Kind: "li",
				Text: marker + strings.TrimSpace(string(n.Text(source)))})
			return ast.WalkSkipChildren, nil

		case *east.Table:
			blocks = append(blocks, mdBlock{Kind: "table", Table: parseTableRows(node, source)})
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

// parseTableRows extracts a GFM table's cells (header row first). The
// alignment row (---|---) is skipped automatically by goldmark's AST.
func parseTableRows(tbl *east.Table, source []byte) [][]string {
	var rows [][]string
	for row := tbl.FirstChild(); row != nil; row = row.NextSibling() {
		var cells []string
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			cells = append(cells, strings.TrimSpace(string(cell.Text(source))))
		}
		if len(cells) > 0 {
			rows = append(rows, cells)
		}
	}
	return rows
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

// cp1252 maps the printable code points above 0x7f that CP1252 can encode;
// everything else in a rune string falls back via pdfSanitize.
var cp1252 = map[rune]string{
	0x80: "€", 0x82: "‚", 0x83: "ƒ", 0x84: "„", 0x85: "…",
	0x86: "†", 0x87: "‡", 0x88: "ˆ", 0x89: "‰", 0x8a: "Š",
	0x8b: "‹", 0x8c: "Œ", 0x91: "'", 0x92: "'", 0x93: "\"",
	0x94: "\"", 0x95: "•", 0x96: "–", 0x97: "—", 0x98: "˜",
	0x99: "™", 0x9a: "š", 0x9b: "›", 0x9c: "œ", 0x9f: "Ÿ",
	0xa1: "¡", 0xa2: "¢", 0xa3: "£", 0xa4: "¤", 0xa5: "¥",
	0xa6: "¦", 0xa7: "§", 0xa8: "¨", 0xa9: "©", 0xaa: "ª",
	0xab: "«", 0xac: "¬", 0xae: "®", 0xaf: "¯", 0xb0: "°",
	0xb1: "±", 0xb4: "´", 0xb5: "µ", 0xb6: "¶", 0xb7: "·",
	0xb8: "¸", 0xb9: "¹", 0xba: "º", 0xbb: "»", 0xbf: "¿", 0xc0: "À", 0xc1: "Á",
	0xc2: "Â", 0xc3: "Ã", 0xc4: "Ä", 0xc5: "Å", 0xc6: "Æ",
	0xc7: "Ç", 0xc8: "È", 0xc9: "É", 0xca: "Ê", 0xcb: "Ë",
	0xcc: "Ì", 0xcd: "Í", 0xce: "Î", 0xcf: "Ï", 0xd1: "Ñ",
	0xd2: "Ò", 0xd3: "Ó", 0xd4: "Ô", 0xd5: "Õ", 0xd6: "Ö",
	0xd8: "Ø", 0xd9: "Ù", 0xda: "Ú", 0xdb: "Û", 0xdc: "Ü",
	0xdd: "Ý", 0xe0: "à", 0xe1: "á", 0xe2: "â", 0xe3: "ã",
	0xe4: "ä", 0xe5: "å", 0xe6: "æ", 0xe7: "ç", 0xe8: "è",
	0xe9: "é", 0xea: "ê", 0xeb: "ë", 0xec: "ì", 0xed: "í",
	0xee: "î", 0xef: "ï", 0xf1: "ñ", 0xf2: "ò", 0xf3: "ó",
	0xf4: "ô", 0xf5: "õ", 0xf6: "ö", 0xf8: "ø", 0xf9: "ù",
	0xfa: "ú", 0xfb: "û", 0xfc: "ü", 0xfd: "ý", 0xff: "ÿ",
}

// pdfSanitize converts a rune string into something fpdf's CP1252 core
// fonts can actually render. Raw UTF-8 fed to fpdf produces the classic
// "â€"" mojibake (em dashes, bullets, box glyphs from the model output).
//
// Strategy: use CP1252 equivalents where they exist, transliterate the
// common typographic/emoji set, drop everything else unrepresentable.
func pdfSanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == 0x0a || r == 0x0d:
			b.WriteByte('\n')
		case r >= 0x20 && r <= 0x7e:
			b.WriteRune(r) // plain ASCII
		case r >= 0x80 && r <= 0xff:
			if enc, ok := cp1252[r]; ok {
				b.WriteString(enc)
			} else {
				b.WriteByte('?')
			}
		default:
			// Drop unrepresentable blocks first: emoji, misc symbols,
			// zero-width chars, control codes.
			if (r >= 0x1f300 && r <= 0x1faff) || (r >= 0x2600 && r <= 0x26ff) ||
				r == 0x200b || r == 0x200c || r == 0x200d || r == 0xfeff || r < 0x20 {
				continue
			}
			switch r {
			case 0x2013: // en dash
				b.WriteByte('-')
			case 0x2014: // em dash
				b.WriteString("--")
			case 0x2018, 0x2019, 0x201a: // quotes
				b.WriteByte('\'')
			case 0x201c, 0x201d, 0x201e: // double quotes
				b.WriteByte('"')
			case 0x2022, 0x25cf, 0x25aa, 0x2023: // bullets
				b.WriteByte('-')
			case 0x2026: // ellipsis
				b.WriteString("...")
			case 0x2192, 0x21d2: // arrows
				b.WriteString("->")
			case 0x2190: // left arrow
				b.WriteString("<-")
			case 0x2191: // up
				b.WriteString("^")
			case 0x2193: // down
				b.WriteString("v")
			case 0x00d7: // multiplication
				b.WriteByte('x')
			case 0x2264:
				b.WriteString("<=")
			case 0x2265:
				b.WriteString(">=")
			case 0x2260:
				b.WriteString("!=")
			case 0x2713, 0x2714, 0x2717, 0x2718: // check/cross marks
				b.WriteByte('*')
			case 0x25b6, 0x25ba, 0x25b8, 0x25c6, 0x25c7, 0x25a0, 0x25a1:
				b.WriteByte('>') // geometric shapes -> simple marker
			default:
				b.WriteByte('?')
			}
		}
	}
	return b.String()
}

// pdfLines splits sanitized text on newlines and drops trailing blanks.
func pdfLines(s string) []string {
	s = pdfSanitize(s)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
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
	pageW, _ := pdf.GetPageSize()
	left, _, right, _ := pdf.GetMargins()
	textW := pageW - left - right // 174mm printable on A4 with these margins

	// --- Cover header band -------------------------------------------------
	pdf.SetFillColor(pdfBase[0], pdfBase[1], pdfBase[2])
	pdf.Rect(0, 0, pageW, 46, "F")

	pdf.SetY(10)
	pdf.SetTextColor(255, 255, 255)
	pdf.SetFont("Helvetica", "B", 22)
	pdf.SetX(left)
	pdf.CellFormat(100, 10, "SPECTRA", "", 2, "L", false, 0, "")

	pdf.SetFont("Helvetica", "", 10)
	pdf.SetTextColor(pdfCyan[0], pdfCyan[1], pdfCyan[2])
	pdf.CellFormat(120, 6, "Vision-Assisted Terminal Research Report", "", 0, "L", false, 0, "")

	// Timestamp, right-aligned inside the band.
	pdf.SetXY(pageW-right-62, 14)
	pdf.SetTextColor(pdfZinc[0]+90, pdfZinc[1]+90, pdfZinc[2]+90)
	pdf.SetFont("Courier", "", 8)
	pdf.CellFormat(62, 6, r.Meta.RunAt.Format("2006-01-02 15:04 MST"), "", 0, "R", false, 0, "")

	pdf.SetY(50) // resume below the band — never overlap it

	// --- Metadata table ----------------------------------------------------
	metaRow := func(label, value string) {
		pdf.SetFont("Helvetica", "B", 9)
		pdf.SetTextColor(pdfLavender[0], pdfLavender[1], pdfLavender[2])
		pdf.CellFormat(32, 6, label, "", 0, "L", false, 0, "")
		pdf.SetFont("Helvetica", "", 9)
		pdf.SetTextColor(pdfBody[0], pdfBody[1], pdfBody[2])
		pdf.MultiCell(0, 6, pdfSanitize(value), "", "L", false)
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
	pdf.Line(left, pdf.GetY(), pageW-right, pdf.GetY())
	pdf.Ln(6)

	// --- Screenshot (optional) ---------------------------------------------
	if len(r.Screenshot) > 0 {
		imgTmp := filepath.Join(os.TempDir(), "spectra_shot.png")
		if err := os.WriteFile(imgTmp, r.Screenshot, 0o644); err == nil {
			defer os.Remove(imgTmp)
			pdf.SetFont("Helvetica", "B", 11)
			pdf.SetTextColor(pdfCyan[0], pdfCyan[1], pdfCyan[2])
			pdf.CellFormat(textW, 8, "Captured Viewport", "", 1, "L", false, 0, "")

			// Scale by the image's real pixel aspect ratio (never hardcode).
			w, h := 1280, 800
			if cfg, _, err := image.DecodeConfig(bytes.NewReader(r.Screenshot)); err == nil && cfg.Width > 0 {
				w, h = cfg.Width, cfg.Height
			}
			imgH := textW * float64(h) / float64(w)

			// If the remaining page can't hold the image, start a fresh page
			// instead of letting the auto page-break slice through it.
			_, pageH := pdf.GetPageSize()
			_, top, _, bottom := pdf.GetMargins()
			if avail := pageH - bottom - pdf.GetY(); imgH > avail && avail > top+40 {
				pdf.AddPage()
			}
			pdf.ImageOptions(imgTmp, left, pdf.GetY(), textW, 0, false,
				fpdf.ImageOptions{ImageType: "PNG"}, 0, "")
			pdf.SetY(pdf.GetY() + imgH + 6)
		}
	}

	// --- Body ---------------------------------------------------------------
	blocks := parseMarkdownBlocks(r.Markdown)

	// writeLines renders each sanitized line; newline-aware so headings and
	// paragraphs from the model never fuse into mojibake soup.
	writeLines := func(lines []string, font, style string, size float64, color [3]int, lh float64) {
		pdf.SetFont(font, style, size)
		pdf.SetTextColor(color[0], color[1], color[2])
		for _, l := range lines {
			pdf.MultiCell(0, lh, l, "", "L", false)
		}
	}

	for _, blk := range blocks {
		switch blk.Kind {
		case "h1":
			pdf.Ln(2)
			writeLines(pdfLines(blk.Text), "Helvetica", "B", 16, pdfCyan, 8)
			pdf.Ln(1)

		case "h2":
			pdf.Ln(2)
			writeLines(pdfLines(blk.Text), "Helvetica", "B", 13, pdfLavender, 7)
			pdf.Ln(1)

		case "h3", "h4", "h5", "h6":
			writeLines(pdfLines(blk.Text), "Helvetica", "BI", 11, pdfEmerald, 6)
			pdf.Ln(1)

		case "p":
			writeLines(pdfLines(blk.Text), "Helvetica", "", 10, pdfBody, 5.5)
			pdf.Ln(1.5)

		case "li":
			for _, l := range pdfLines(blk.Text) {
				x := pdf.GetX()
				pdf.SetX(x + 4)
				writeLines([]string{l}, "Helvetica", "", 10, pdfBody, 5.5)
				pdf.SetX(x)
			}

		case "quote":
			x := pdf.GetX()
			pdf.SetX(x + 3)
			writeLines(pdfLines(blk.Text), "Helvetica", "I", 10,
				[3]int{pdfZinc[0] + 40, pdfZinc[1] + 40, pdfZinc[2] + 40}, 5.5)
			pdf.SetX(x)

		case "code":
			for _, l := range pdfLines(blk.CodeStr) {
				pdf.SetFont("Courier", "", 9)
				pdf.SetFillColor(24, 24, 28)
				pdf.SetTextColor(pdfEmerald[0], pdfEmerald[1], pdfEmerald[2])
				pdf.MultiCell(0, 5, l, "", "L", true)
			}
			pdf.Ln(2)

		case "table":
			renderPDFTable(pdf, blk.Table, textW)

		case "hr":
			pdf.SetDrawColor(pdfZinc[0], pdfZinc[1], pdfZinc[2])
			pdf.SetLineWidth(0.3)
			pdf.Line(left, pdf.GetY(), pageW-right, pdf.GetY())
			pdf.Ln(4)
		}
	}

	// --- References (link citations survive the PDF) ------------------------
	refs := extractLinks(r.Markdown)
	if len(refs) > 0 {
		pdf.Ln(2)
		pdf.SetFont("Helvetica", "B", 12)
		pdf.SetTextColor(pdfCyan[0], pdfCyan[1], pdfCyan[2])
		pdf.CellFormat(textW, 8, "References", "", 1, "L", false, 0, "")
		pdf.SetFont("Helvetica", "", 8)
		pdf.SetTextColor(pdfZinc[0]+30, pdfZinc[1]+30, pdfZinc[2]+30)
		for i, ref := range refs {
			pdf.MultiCell(0, 4.5, fmt.Sprintf("[%d] %s", i+1, pdfSanitize(ref)), "", "L", false)
		}
	}

	// --- Footer page numbers -------------------------------------------------
	// ASCII-only caption: CP1252 core fonts render the em dash as mojibake.
	total := pdf.PageCount()
	for i := 1; i <= total; i++ {
		pdf.SetPage(i)
		pdf.SetY(-14)
		pdf.SetFont("Helvetica", "", 8)
		pdf.SetTextColor(pdfZinc[0], pdfZinc[1], pdfZinc[2])
		pdf.CellFormat(0, 8, fmt.Sprintf("SPECTRA  -  %d / %d", i, total), "", 0, "C", false, 0, "")
	}

	if err := pdf.OutputFileAndClose(path); err != nil {
		return "", fmt.Errorf("export: write pdf: %w", err)
	}
	return path, nil
}
