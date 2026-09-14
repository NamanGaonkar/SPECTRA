package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/chromedp/chromedp"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

func main() {
	// Non-TUI modes: --check verifies plumbing, --smoke runs the full
	// pipeline headlessly and prints the report to stdout.
	args := os.Args[1:]
	switch {
	case len(args) > 0 && args[0] == "--check":
		if err := runCheck(); err != nil {
			fmt.Fprintf(os.Stderr, "CHECK FAILED: %v\n", err)
			os.Exit(1)
		}
		return
	case len(args) > 1 && args[0] == "--smoke":
		if err := runSmoke(args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "SMOKE FAILED: %v\n", err)
			os.Exit(1)
		}
		return
	}

	fmt.Fprintln(os.Stdout, "SPECTRA starting...")

	// Quick headless check of dependencies before entering the TUI.
	if err := preflight(); err != nil {
		fmt.Fprintf(os.Stderr, "preflight: %v\n", err)
		os.Exit(1)
	}

	p := tea.NewProgram(newModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "spectra: %v\n", err)
		os.Exit(1)
	}
}

// runCheck verifies browser + Ollama + model without running a capture.
func runCheck() error {
	fmt.Println("[1/3] Browser detection...")
	if p := findBrowser(); p != "" {
		fmt.Printf("      OK: %s\n", p)
	} else {
		fmt.Println("      WARN: no known browser path; chromedp will try its own detection")
	}

	fmt.Println("[2/3] Ollama daemon...")
	c := NewOllamaClient()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	if err := c.Ping(ctx); err != nil {
		return err
	}
	fmt.Printf("      OK: %s\n", c.BaseURL)

	fmt.Printf("[3/3] Vision model (%s)...\n", c.Model)
	ok, err := c.HasModel(ctx, c.Model)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("model %s not pulled — run: ollama pull %s", c.Model, c.Model)
	}
	fmt.Println("      OK")
	fmt.Println("\nAll systems nominal. Launch `spectra` for the TUI.")
	return nil
}

// runSmoke executes the full research pipeline without the TUI.
func runSmoke(targetURL string) error {
	targetURL = normalizeURL(targetURL)
	if err := validTargetURL(targetURL); err != nil {
		return err
	}
	query := "Summarize this page and list the key facts visible on it."

	fmt.Printf("[1/3] Capturing %s ...\n", targetURL)
	ctx, cancel := longCtx()
	defer cancel()

	capRes, err := CaptureScreenshot(ctx, targetURL)
	if err != nil {
		return err
	}
	fmt.Printf("      OK: title=%q in %.1fs (%d KB png)\n", capRes.Title, capRes.LoadTime, len(capRes.PNG)/1024)

	fmt.Println("[2/3] Running vision model (this can take a while)...")
	client := NewOllamaClient()
	resp, err := client.GenerateVision(ctx, buildPrompt(query), capRes.PNG)
	if err != nil {
		return err
	}
	fmt.Printf("      OK: %d chars of synthesis\n", len(resp))

	fmt.Println("[3/3] Exporting...")
	report := Report{
		Meta: ReportMeta{
			TargetURL: targetURL,
			FinalURL:  capRes.FinalURL,
			PageTitle: capRes.Title,
			Query:     query,
			Model:     client.Model,
			RunAt:     time.Now(),
		},
		Markdown:   resp,
		Screenshot: capRes.PNG,
	}
	mdPath, err := WriteMarkdown(report)
	if err != nil {
		return err
	}
	pdfPath, err := WritePDF(report)
	if err != nil {
		return err
	}
	fmt.Printf("      markdown: %s\n      pdf:      %s\n\n", mdPath, pdfPath)
	fmt.Println("--- Report preview ---")
	fmt.Println(resp)
	return nil
}

// longCtx returns a generous context for slow local-LLM + network work.
func longCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Minute)
}

// preflight validates we can find Chrome and prints a warning (not fatal)
// if Ollama isn't up yet — the TUI shows a live badge either way.
func preflight() error {
	if err := chromedpAvailable(); err != nil {
		return fmt.Errorf("headless Chrome not found: %w", err)
	}
	if err := ollamaReachable(); err != nil {
		fmt.Fprintln(os.Stderr, "warning: "+err.Error())
		fmt.Fprintln(os.Stderr, "         the TUI will still start; check the badge on the form view")
	}
	return nil
}

// chromedpAvailable verifies a Chromium-family browser exists for capture.
func chromedpAvailable() error {
	if p := findBrowser(); p != "" {
		fmt.Printf("browser: %s\n", p)
		return nil
	}
	// Last resort: let chromedp try its own detection.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, chromedp.DefaultExecAllocatorOptions[:]...)
	defer allocCancel()

	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	defer browserCancel()

	if err := chromedp.Run(browserCtx); err != nil {
		return fmt.Errorf("no Chrome/Edge/Chromium found; install one or set CHROME_PATH (%v)", err)
	}
	return nil
}

// ollamaReachable does a quick daemon ping; used only by preflight.
func ollamaReachable() error {
	client := NewOllamaClient()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return client.Ping(ctx)
}

// renderMarkdownSafe converts synthesized markdown into plain text for the
// results viewport. We convert through goldmark and strip residual HTML so
// the bubbletea viewport never chokes on raw tags.
func renderMarkdownSafe(src string) string {
	var out strings.Builder
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM, extension.Table),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		goldmark.WithRendererOptions(html.WithHardWraps()),
	)
	if err := md.Convert([]byte(src), &out); err != nil {
		// Fall back to raw text if markdown conversion ever fails.
		return src
	}
	return stripHTMLTags(out.String())
}

// stripHTMLTags removes any residual HTML tags left after conversion.
func stripHTMLTags(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}
