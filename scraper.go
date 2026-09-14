package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

// ---------------------------------------------------------------------------
// Scraper — headless Chrome via chromedp. Produces a PNG screenshot buffer.
// ---------------------------------------------------------------------------

const (
	screenshotWidth  = 1280
	screenshotHeight = 800
	pageLoadTimeout  = 60 * time.Second
)

// knownBrowserPaths lists Chromium-family browsers in preference order.
// Chrome is preferred; Edge (also Chromium) is a fully capable fallback.
var knownBrowserPaths = []string{
	`C:\Program Files\Google\Chrome\Application\chrome.exe`,
	`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
	`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
	`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
	`C:\Users\LocalAppData\Google\Chrome\Application\chrome.exe`, // replaced at runtime below
	"/usr/bin/google-chrome",
	"/usr/bin/google-chrome-stable",
	"/usr/bin/chromium",
	"/usr/bin/chromium-browser",
	"/snap/bin/chromium",
}

// findBrowser returns the first installed Chromium-family browser path,
// or "" to let chromedp use its own default detection.
func findBrowser() string {
	// Windows per-user Chrome install lives under the real LOCALAPPDATA.
	localAppData := os.Getenv("LOCALAPPDATA")
	candidates := make([]string, 0, len(knownBrowserPaths)+1)
	for _, p := range knownBrowserPaths {
		candidates = append(candidates, p)
	}
	if localAppData != "" {
		candidates = append(candidates, localAppData+`\Google\Chrome\Application\chrome.exe`)
	}
	// Per-user installs first would be nicer, but order barely matters:
	// we just need ANY working Chromium. Prefer Chrome over Edge by leaving
	// the slice order as declared, then scan.
	for _, p := range candidates {
		if strings.Contains(p, "LocalAppData") {
			continue // skip the placeholder entry
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	if localAppData != "" {
		p := localAppData + `\Google\Chrome\Application\chrome.exe`
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// CaptureResult carries everything we learned during the run.
type CaptureResult struct {
	PNG      []byte  // raw screenshot bytes
	FinalURL string  // URL after redirects
	Title    string  // document.title
	LoadTime float64 // seconds until render complete
}

// CaptureScreenshot navigates to targetURL with headless Chrome and returns a
// 1280x800 desktop screenshot plus page metadata. The browser is fully
// allocated and torn down per call.
func CaptureScreenshot(ctx context.Context, targetURL string) (*CaptureResult, error) {
	if targetURL == "" {
		return nil, errors.New("scraper: empty target URL")
	}

	// Chrome flags tuned for deterministic, clean captures.
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("hide-scrollbars", true),
		chromedp.Flag("mute-audio", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-default-browser-check", true),
		// Pin the exact desktop viewport required by the spec.
		chromedp.WindowSize(screenshotWidth, screenshotHeight),
		// A common desktop UA so responsive sites serve the desktop layout.
		chromedp.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"),
	)

	// Browser resolution order: CHROME_PATH env > known browser paths >
	// chromedp's built-in detection.
	exec := os.Getenv("CHROME_PATH")
	if exec == "" {
		exec = findBrowser()
	}
	if exec != "" {
		opts = append(opts, chromedp.ExecPath(exec))
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, opts...)
	defer allocCancel()

	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	defer browserCancel()

	// Hard deadline for the whole navigate+render+shoot cycle.
	runCtx, runCancel := context.WithTimeout(browserCtx, pageLoadTimeout)
	defer runCancel()

	start := time.Now()

	var (
		png      []byte
		finalURL string
		title    string
	)

	err := chromedp.Run(runCtx,
		chromedp.Navigate(targetURL),
		chromedp.WaitReady("body", chromedp.ByQuery),
		// Give SPA / lazy-render content a beat to paint.
		chromedp.Sleep(1500*time.Millisecond),
		chromedp.Location(&finalURL),
		chromedp.Title(&title),
		chromedp.FullScreenshot(&png, 100), // full quality at 1280x800
	)
	if err != nil {
		return nil, fmt.Errorf("scraper: chrome run failed for %s: %w", targetURL, err)
	}

	if len(png) == 0 {
		return nil, fmt.Errorf("scraper: chrome produced an empty screenshot for %s", targetURL)
	}

	return &CaptureResult{
		PNG:      png,
		FinalURL: finalURL,
		Title:    title,
		LoadTime: time.Since(start).Seconds(),
	}, nil
}
