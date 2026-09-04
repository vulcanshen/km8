package ui

import (
	"math/rand"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/vulcanshen/kbu/internal/version"
)

type splashTickMsg struct{}
type splashIdentityMsg struct{} // fires KubeUI + version + tagline + credit together
type splashHintMsg struct{}

// SplashModel renders the kbu logo as a hidden easter egg.
type SplashModel struct {
	active          bool
	pixelOrder      []int // reveal order: full background sheet (top-down), U frame (bottom-up), then K/B letters (shuffled)
	revealedCount   int
	bgCount         int  // end of stage 1 — first bgCount indices are the full background sheet (every cell)
	uEnd            int  // end of stage 2 — indices [bgCount:uEnd] are U frame pixels
	pausedBg        bool // beat consumed at the D→U boundary
	pausedU         bool // beat consumed at the U→K/B boundary
	identityVisible bool // "KubeUI" line
	taglineVisible  bool // "A single-pane Kubernetes workspace"
	versionVisible  bool
	creditVisible   bool // "developed by" + author email
	hintVisible     bool
}

func NewSplashModel() SplashModel {
	return SplashModel{}
}

func (m SplashModel) IsActive() bool { return m.active }

func (m *SplashModel) Show() tea.Cmd {
	m.active = true
	m.revealedCount = 0
	m.pausedBg = false
	m.pausedU = false
	m.identityVisible = false
	m.taglineVisible = false
	m.versionVisible = false
	m.creditVisible = false
	m.hintVisible = false

	// Reveal phases:
	// (1) background - a full dark-gray sheet, row-major top-to-bottom sweep.
	// (2) beat - brief hold at the background->U boundary.
	// (3) U frame (navy) - bottom-to-top, rising from the base, over the sheet.
	// (4) beat - brief hold at the U->K/B boundary.
	// (5) K/B letters (gold) - shuffled, painted over the sheet.
	// (6) 400ms hold -> KubeUI + version + tagline + credit appear together.
	// (7) 500ms hold -> esc hint appears.
	rows, cols := len(logoPixels), len(logoPixels[0])
	// Background pass covers EVERY cell so the dark-gray sheet fills solid;
	// the U and K/B passes come later and paint over it (overwrite, not gaps).
	bg := make([]int, 0, rows*cols)
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			bg = append(bg, r*cols+c)
		}
	}
	// U frame collected bottom-to-top so it rises from the base.
	var frame []int
	for r := rows - 1; r >= 0; r-- {
		for c := 0; c < cols; c++ {
			if logoPixels[r][c] == 'U' {
				frame = append(frame, r*cols+c)
			}
		}
	}
	// K/B letters shuffled.
	var letters []int
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			if b := logoPixels[r][c]; b == 'K' || b == 'B' {
				letters = append(letters, r*cols+c)
			}
		}
	}
	rand.Shuffle(len(letters), func(i, j int) { letters[i], letters[j] = letters[j], letters[i] })
	m.pixelOrder = append(append(bg, frame...), letters...)
	m.bgCount = len(bg)
	m.uEnd = len(bg) + len(frame)

	return tea.Tick(10*time.Millisecond, func(time.Time) tea.Msg {
		return splashTickMsg{}
	})
}

// kbu logo — generated from docs/icon.svg (25x23). The mark spells
// KBU: K and B in gold, U as the navy frame (side rails + base) that
// wraps them. D=dark-gray background, U=navy frame, K/B=gold letters.
var logoPixels = [23]string{
	"DDDDDDDDDDDDDDDDDDDDDDDDD",
	"DDDDDDDDDDDDDDDDDDDDDDDDD",
	"DDUUUDKKDDKKDBBBBBDDUUUDD",
	"DDDUUDKKDDKKDBBBBBBDUUDDD",
	"DDDUUDKKDDKKDBBDDBBDUUDDD",
	"DDDUUDKKDDKKDBBDDBBDUUDDD",
	"DDDUUDKKDKKKDBBDDBBDUUDDD",
	"DDDUUDKKKKKKDBBDDBBDUUDDD",
	"DDDUUDKKKKKDDBBDDBBDUUDDD",
	"DDDUUDKKDDDDDBBBBBDDUUDDD",
	"DDDUUDKKDDDDDBBBBBDDUUDDD",
	"DDDUUDKKKKKDDBBDDBBDUUDDD",
	"DDDUUDKKKKKKDBBDDBBDUUDDD",
	"DDDUUDKKDKKKDBBDDBBDUUDDD",
	"DDDUUDKKDDKKDBBDDBBDUUDDD",
	"DDDUUDKKDDKKDBBDDBBDUUDDD",
	"DDDUUDKKDDKKDBBBBBBDUUDDD",
	"DDDUUDKKDDKKDBBBBBDDUUDDD",
	"DDDUUDDDDDDDDDDDDDDDUUDDD",
	"DDUUUUUUUUUUUUUUUUUUUUUDD",
	"DUUUUUUUUUUUUUUUUUUUUUUUD",
	"DDDDDDDDDDDDDDDDDDDDDDDDD",
	"DDDDDDDDDDDDDDDDDDDDDDDDD",
}

const (
	logoBg   = "#313244" // dark-gray background (catppuccin surface0)
	logoNavy = "#205090" // U frame — side rails + base (icon.svg)
	logoGold = "#f2b753" // K / B letters (icon.svg)

	// authorEmail is credited under the tagline, above the Esc hint.
	authorEmail = "vulcan.shen.2304@gmail.com"
)

func pixelGlyph(byte) string {
	return "\uf0c8" // nf-fa-square: one solid pixel, same glyph for every cell
}

func (m SplashModel) Render(width, height int) string {
	if !m.active {
		return ""
	}

	// Colour each cell by the reveal pass that last touched it. The background
	// pass paints every cell dark gray; the U and K/B passes come later in
	// pixelOrder, so they overwrite the gray where they land (no gaps left).
	cols := len(logoPixels[0])
	cellColor := make([]string, len(logoPixels)*cols)
	for i := 0; i < m.revealedCount; i++ {
		idx := m.pixelOrder[i]
		switch {
		case i < m.bgCount:
			cellColor[idx] = logoBg
		case i < m.uEnd:
			cellColor[idx] = logoNavy
		default:
			cellColor[idx] = logoGold
		}
	}

	var logoLines []string
	for r := 0; r < len(logoPixels); r++ {
		var line strings.Builder
		for c := 0; c < cols; c++ {
			if color := cellColor[r*cols+c]; color != "" {
				line.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(pixelGlyph(0) + " "))
			} else {
				line.WriteString("  ")
			}
		}
		logoLines = append(logoLines, line.String())
	}
	logo := strings.Join(logoLines, "\n")

	// Caption space is always reserved so the logo doesn't shift when text appears.
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7f849c"))
	blueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#89b4fa"))
	logoW := cols * 2
	identityText, taglineText, versionText := " ", " ", " "
	creditText, emailText, hintText := " ", " ", " "
	if m.identityVisible {
		identityText = blueStyle.Bold(true).Render("KubeUI")
	}
	if m.versionVisible {
		versionText = blueStyle.Render(version.Display())
	}
	if m.taglineVisible {
		taglineText = blueStyle.Render("A single-pane kubernetes workspace")
	}
	if m.creditVisible {
		creditText = dimStyle.Render("developed by")
		emailText = dimStyle.Render(authorEmail)
	}
	if m.hintVisible {
		hintText = dimStyle.Render("Press Esc to close")
	}
	caption := "\n\n" +
		lipgloss.PlaceHorizontal(logoW, lipgloss.Center, identityText) +
		"\n" +
		lipgloss.PlaceHorizontal(logoW, lipgloss.Center, versionText) +
		"\n" +
		lipgloss.PlaceHorizontal(logoW, lipgloss.Center, taglineText) +
		"\n\n" +
		lipgloss.PlaceHorizontal(logoW, lipgloss.Center, creditText) +
		"\n" +
		lipgloss.PlaceHorizontal(logoW, lipgloss.Center, emailText) +
		"\n\n" +
		lipgloss.PlaceHorizontal(logoW, lipgloss.Center, hintText)

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, logo+caption)
}

// Update handles key events and animation ticks when splash is active.
func (m SplashModel) Update(msg tea.Msg) (SplashModel, tea.Cmd) {
	if !m.active {
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "enter", " ":
			m.active = false
			m.revealedCount = 0
			m.pixelOrder = nil
			m.bgCount = 0
			m.uEnd = 0
			m.pausedBg = false
			m.pausedU = false
			m.identityVisible = false
			m.taglineVisible = false
			m.versionVisible = false
			m.creditVisible = false
			m.hintVisible = false
		}
	case splashTickMsg:
		// Beat at the D->U boundary - brief hold before the frame rises.
		if m.bgCount > 0 && m.revealedCount == m.bgCount && !m.pausedBg {
			m.pausedBg = true
			return m, tea.Tick(250*time.Millisecond, func(time.Time) tea.Msg {
				return splashTickMsg{}
			})
		}
		// Beat at the U->K/B boundary - brief hold before the letters shuffle in.
		if m.uEnd > m.bgCount && m.revealedCount == m.uEnd && !m.pausedU {
			m.pausedU = true
			return m, tea.Tick(250*time.Millisecond, func(time.Time) tea.Msg {
				return splashTickMsg{}
			})
		}
		if m.revealedCount < len(m.pixelOrder) {
			// Stage 1 (background sheet): one full row per tick — fast top-to-bottom fill.
			// Stage 2 (U frame): 3 px/tick, bottom-to-top sweep.
			// Stage 3 (K/B letters): 2 px/tick, shuffled.
			step := 2
			switch {
			case m.revealedCount < m.bgCount:
				step = len(logoPixels[0])
			case m.revealedCount < m.uEnd:
				step = 3
			}
			newCount := m.revealedCount + step
			// Clamp to each boundary so the beats fire cleanly.
			if m.revealedCount < m.bgCount && newCount > m.bgCount {
				newCount = m.bgCount
			} else if m.revealedCount < m.uEnd && newCount > m.uEnd {
				newCount = m.uEnd
			}
			if newCount > len(m.pixelOrder) {
				newCount = len(m.pixelOrder)
			}
			m.revealedCount = newCount
			return m, tea.Tick(10*time.Millisecond, func(time.Time) tea.Msg {
				return splashTickMsg{}
			})
		}
		// Letters done - schedule the identity caption reveal after a brief hold.
		if !m.identityVisible {
			return m, tea.Tick(400*time.Millisecond, func(time.Time) tea.Msg {
				return splashIdentityMsg{}
			})
		}
	case splashIdentityMsg:
		m.identityVisible = true
		m.versionVisible = true
		m.taglineVisible = true
		m.creditVisible = true
		return m, tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg {
			return splashHintMsg{}
		})
	case splashHintMsg:
		m.hintVisible = true
	}
	return m, nil
}
