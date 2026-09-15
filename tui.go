package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// tui.go — Bubble Tea state machine (Model / Update / commands).
// All rendering lives in ui.go; all styles and tokens live in styles.go.
//   View 1: input form (URL + research query, live Ollama badge)
//   View 2: execution progress (3 animated stages)
//   View 3: results viewport (scrollable synthesized markdown)
//   View 4: export dialog (m / p / r / q)
// ---------------------------------------------------------------------------

type viewState int

const (
	viewInput viewState = iota
	viewRunning
	viewResults
	viewExport
)

// Step identifiers for the execution view.
const (
	stepScrape = iota
	stepInfer
	stepCompile
	stepCount
)

// runResultMsg is emitted by the background worker when the pipeline ends.
type runResultMsg struct {
	report Report
	err    error
}

// progressTickMsg advances the animated step indicator while a run is live.
type progressTickMsg struct{}

// ollamaStatusMsg carries the outcome of the pre-flight health check.
type ollamaStatusMsg struct {
	up     bool
	model  bool
	detail string
}

type model struct {
	state viewState

	// inputs
	urlInput   textinput.Model
	queryInput textinput.Model
	focusURL   bool

	// ollama badge
	ollamaUp    bool
	ollamaModel bool
	checking    bool

	// execution
	spinner  spinner.Model
	step     int
	stepNote string

	// results
	result    Report
	resultErr error
	viewport  viewport.Model

	width  int
	height int
}

func newModel() model {
	url := textinput.New()
	url.Placeholder = "https://example.com"
	url.Focus()
	url.CharLimit = 2048
	url.Width = 56
	url.PromptStyle = lipgloss.NewStyle().Foreground(colorCyan)
	url.TextStyle = lipgloss.NewStyle().Foreground(colorText)

	query := textinput.New()
	query.Placeholder = "What is this page about? Extract key claims..."
	query.CharLimit = 4096
	query.Width = 56
	query.PromptStyle = lipgloss.NewStyle().Foreground(colorLavender)
	query.TextStyle = lipgloss.NewStyle().Foreground(colorText)

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colorTerracot)

	vp := viewport.New(resultWidth, 20)
	vp.Style = lipgloss.NewStyle()

	return model{
		state:      viewInput,
		urlInput:   url,
		queryInput: query,
		focusURL:   true,
		checking:   true,
		spinner:    sp,
		viewport:   vp,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		checkOllamaCmd(),
		m.spinner.Tick,
	)
}

// ---------------------------------------------------------------------------
// Commands (async)
// ---------------------------------------------------------------------------

// checkOllamaCmd pings the daemon + verifies the vision model exists.
func checkOllamaCmd() tea.Cmd {
	return func() tea.Msg {
		c := NewOllamaClient()
		ctx, cancel := longCtx()
		defer cancel()

		if err := c.Ping(ctx); err != nil {
			return ollamaStatusMsg{up: false, detail: err.Error()}
		}
		ok, err := c.HasModel(ctx, c.Model)
		if err != nil {
			return ollamaStatusMsg{up: true, model: false, detail: err.Error()}
		}
		return ollamaStatusMsg{up: true, model: ok}
	}
}

// progressTick animates the three-stage pipeline display: stage 1 while
// Chrome runs, stage 2 while Ollama inference is in flight, stage 3 just
// before the result lands.
func progressTick() tea.Cmd {
	return tea.Tick(4*time.Second, func(time.Time) tea.Msg {
		return progressTickMsg{}
	})
}

// runPipelineCmd executes scrape -> infer -> compile off the UI goroutine.
func runPipelineCmd(url, query string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := longCtx()
		defer cancel()

		client := NewOllamaClient()

		capRes, err := CaptureScreenshot(ctx, url)
		if err != nil {
			return runResultMsg{err: fmt.Errorf("step 1 (chrome): %w", err)}
		}

		resp, err := client.GenerateVision(ctx, buildPrompt(query), capRes.PNG)
		if err != nil {
			return runResultMsg{err: fmt.Errorf("step 2 (ollama): %w", err)}
		}

		report := Report{
			Meta: ReportMeta{
				TargetURL: url,
				FinalURL:  capRes.FinalURL,
				PageTitle: capRes.Title,
				Query:     query,
				Model:     client.Model,
				RunAt:     time.Now(),
			},
			Markdown:   resp,
			Screenshot: capRes.PNG,
		}
		return runResultMsg{report: report}
	}
}

// buildPrompt wraps the user's research objective in an extraction template.
func buildPrompt(query string) string {
	var b strings.Builder
	b.WriteString("You are a research analyst. You are shown a 1280x800 screenshot of a web page.\n\n")
	b.WriteString("Research objective: ")
	b.WriteString(query)
	b.WriteString("\n\nProduce structured research notes in GitHub-Flavored Markdown with these sections:\n")
	b.WriteString("## Summary\n## Key Findings (bulleted, cite on-screen evidence)\n")
	b.WriteString("## Notable Entities (people, products, companies, tools)\n")
	b.WriteString("## Extraction Points (any numbers, dates, prices, or names visible)\n")
	b.WriteString("## Open Questions\n\n")
	b.WriteString("Be concise and factual. Only report what is visible on the page.")
	return b.String()
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = max(20, min(resultWidth, m.width-4))
		m.viewport.Height = max(8, m.height-14)

	case ollamaStatusMsg:
		m.checking = false
		m.ollamaUp = msg.up
		m.ollamaModel = msg.model

	case progressTickMsg:
		if m.state == viewRunning && m.step < stepCompile {
			m.step++
			cmds = append(cmds, progressTick())
		}

	case runResultMsg:
		if msg.err != nil {
			m.resultErr = msg.err
			m.state = viewResults
			return m, nil
		}
		m.result = msg.report
		m.step = stepCompile // mark final stage done in the log
		m.state = viewResults
		content := renderMarkdownSafe(msg.report.Markdown)
		m.viewport.SetContent(content)
		m.viewport.GotoTop()
		return m, nil

	case spinner.TickMsg:
		if m.state == viewRunning {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			cmds = append(cmds, cmd)
		}

	case tea.KeyMsg:
		// ---- View 1: input form -----------------------------------------
		// The form claims EVERY key it doesn't use itself, so pasted URLs
		// containing e/m/p/r/q (or any other bound letter) reach the input
		// untouched instead of being silently swallowed by global keymap
		// cases.
		if m.state == viewInput {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "tab", "shift+tab":
				m.focusURL = !m.focusURL
				if m.focusURL {
					m.urlInput.Focus()
					m.queryInput.Blur()
				} else {
					m.urlInput.Blur()
					m.queryInput.Focus()
				}
			case "enter":
				return m, m.startRun()
			default:
				// Route typing and pasting to the focused input.
				var cmd tea.Cmd
				if m.focusURL {
					m.urlInput, cmd = m.urlInput.Update(msg)
				} else {
					m.queryInput, cmd = m.queryInput.Update(msg)
				}
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		}

		// Compare lowercase: with Caps Lock on, msg.String() reports "M",
		// "P", "E"… and every shortcut below would silently miss. (Shift
		// intentionally does NOT trigger shortcuts — shift+m types a literal
		// "M" into inputs, matching standard terminal-app behavior.)
		key := strings.ToLower(msg.String())

		switch key {
		case "ctrl+c":
			return m, tea.Quit

		case "enter":
			switch m.state {
			case viewResults:
				if m.resultErr != nil {
					// Failed run: enter starts a fresh one.
					return m, m.resetForNewRun()
				}
			case viewExport:
				// Treat enter as "back to results".
				m.state = viewResults
			}

		case "esc":
			switch m.state {
			case viewExport:
				m.state = viewResults
			case viewResults:
				m.state = viewInput
			}

		// ---- View 3: results -> export dialog -----------------------------
		case "e":
			if m.state == viewResults && m.resultErr == nil {
				m.state = viewExport
				m.stepNote = ""
			}

		// ---- Export actions (View 4, and quick-keys on View 3) ------------
		// m/p work from BOTH the export dialog and the results view — no
		// detour through the dialog required.
		case "m":
			if (m.state == viewExport || m.state == viewResults) && m.resultErr == nil {
				path, err := WriteMarkdown(m.result)
				if err != nil {
					m.stepNote = errStyle("markdown save failed: " + err.Error())
				} else {
					m.stepNote = okStyle("saved " + path)
				}
				m.state = viewResults // dialog acts, then returns to results
			}

		case "p":
			if (m.state == viewExport || m.state == viewResults) && m.resultErr == nil {
				path, err := WritePDF(m.result)
				if err != nil {
					m.stepNote = errStyle("pdf export failed: " + err.Error())
				} else {
					m.stepNote = okStyle("saved " + path)
				}
				m.state = viewResults
			}

		case "r":
			if m.state == viewExport || m.state == viewResults {
				return m, m.resetForNewRun()
			}

		case "q":
			if m.state == viewExport || m.state == viewResults {
				return m, tea.Quit
			}
		}

		// ---- View 3: results viewport scrolling ---------------------------
		if m.state == viewResults {
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			cmds = append(cmds, cmd)
		}
	}

	return m, tea.Batch(cmds...)
}

// startRun validates the form and kicks off the pipeline.
func (m *model) startRun() tea.Cmd {
	url := normalizeURL(m.urlInput.Value())
	query := strings.TrimSpace(m.queryInput.Value())

	if url == "" {
		m.stepNote = errStyle("target URL is required")
		return nil
	}
	if err := validTargetURL(url); err != nil {
		m.stepNote = errStyle(err.Error())
		return nil
	}
	m.urlInput.SetValue(url)
	if query == "" {
		m.stepNote = errStyle("research objective is required")
		return nil
	}

	m.stepNote = ""
	m.step = stepScrape
	m.state = viewRunning
	return tea.Batch(m.spinner.Tick, progressTick(), runPipelineCmd(url, query))
}

// resetForNewRun returns to the input form, clearing prior results.
func (m *model) resetForNewRun() tea.Cmd {
	m.state = viewInput
	m.result = Report{}
	m.resultErr = nil
	m.stepNote = ""
	m.step = stepScrape
	m.focusURL = true
	m.urlInput.Focus()
	m.queryInput.Blur()
	m.urlInput.SetValue("")
	m.queryInput.SetValue("")
	return textinput.Blink
}
