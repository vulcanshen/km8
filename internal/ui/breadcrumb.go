package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/vulcanshen/kbu/internal/k8s"
	"github.com/vulcanshen/kbu/internal/theme"
)

// Nerd Font glyphs shared with the Relatives tab so the breadcrumb and the
// drill arrow read as the same vocabulary. Both markers are
// "<glyph><space>" so visual width stays 2 cells, matching the existing
// markerW=2 layout math. Width math will break if the user's terminal
// font renders these glyphs as 2-cell — they're intended for single-
// width Nerd Font / Nerd Font Mono variants.
const (
	breadcrumbChainMarker   = " " //  — step indicator (same as Relatives drill arrow)
	breadcrumbCurrentMarker = "● " // ● — you-are-here dot; visually distinct from the chain glyph
	relativesDrillArrow     = " " //  — appears after each drillable Relatives entry
)

// BreadcrumbPopupModel lists the Relatives-tab drill chain and lets the user
// jump back to any ancestor level via j/k + Enter. Opened by `b` on the
// Relatives tab at depth > 1; closed by Esc / b.
//
// On Enter the model emits RelativeJumpMsg{Level: 1-indexed level} so
// AppModel can call detail.JumpToDrillLevel — popping intermediate
// frames in one step.
type BreadcrumbPopupModel struct {
	animator    PopupAnimator
	chain       []k8s.RefTarget
	cursor      int
	screenW     int
	theme       *theme.Theme
	layer       int
	borderColor lipgloss.Color
}

func NewBreadcrumbPopupModel(t *theme.Theme) BreadcrumbPopupModel {
	bc := theme.PopupLayerColor(1)
	return BreadcrumbPopupModel{
		theme:       t,
		animator:    NewPopupAnimator("breadcrumb", bc),
		borderColor: bc,
		layer:       1,
	}
}

// SetLayer stamps nesting depth + derives border / animator color.
func (m *BreadcrumbPopupModel) SetLayer(layer int) {
	m.layer = layer
	m.borderColor = theme.PopupLayerColor(layer)
	m.animator.Color = m.borderColor
}

// Open shows the popup with `chain` as the displayed levels (root first,
// current top last). Cursor lands on the current level so Enter is a
// no-op by default — user must move first to commit a jump.
func (m *BreadcrumbPopupModel) Open(chain []k8s.RefTarget) tea.Cmd {
	m.chain = append([]k8s.RefTarget(nil), chain...)
	if len(m.chain) > 0 {
		m.cursor = len(m.chain) - 1
	} else {
		m.cursor = 0
	}
	return m.animator.Open()
}

func (m *BreadcrumbPopupModel) Close() tea.Cmd { return m.animator.Close() }

func (m *BreadcrumbPopupModel) SetSize(w, _ int) { m.screenW = w }

func (m BreadcrumbPopupModel) IsActive() bool      { return m.animator.IsActive() }
func (m BreadcrumbPopupModel) IsInteractive() bool { return m.animator.IsInteractive() }

func (m *BreadcrumbPopupModel) HandleTick(msg AnimTickMsg) tea.Cmd {
	if msg.Target != m.animator.Target {
		return nil
	}
	return m.animator.Tick()
}

func (m BreadcrumbPopupModel) Update(msg tea.Msg) (BreadcrumbPopupModel, tea.Cmd) {
	if !m.animator.IsInteractive() {
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			if len(m.chain) > 0 {
				m.cursor = (m.cursor + 1) % len(m.chain)
			}
			return m, nil
		case "k", "up":
			if len(m.chain) > 0 {
				m.cursor = (m.cursor - 1 + len(m.chain)) % len(m.chain)
			}
			return m, nil
		case "g":
			m.cursor = 0
			return m, nil
		case "G":
			m.cursor = len(m.chain) - 1
			return m, nil
		case "enter":
			// v1.5.x: Enter commits the cursor row as a panel 1+2 switch
			// (matches the new mental model where Enter = "into / commit
			// choice"). RequestSwitchToResourceMsg goes through the
			// confirm-gate code path shared with other switch entries.
			if m.cursor < 0 || m.cursor >= len(m.chain) {
				return m, nil
			}
			ref := m.chain[m.cursor]
			return m, func() tea.Msg { return RequestSwitchToResourceMsg{Ref: ref} }
		case " ":
			// v1.5.x: Space mirrors the open key — close the popup
			// without committing. Aligns with the global rule "any menu
			// popup Space = close".
			return m, m.animator.Close()
		case "esc":
			return m, m.animator.Close()
		}
	}
	return m, nil
}

// HandleMouse routes a click against the rendered breadcrumb popup.
// Left-click on a chain entry commits a jump to that level (same as
// cursor+Enter — emits RequestSwitchToResourceMsg). Right-click
// inside the popup closes it. Long labels that wrap across multiple
// display lines all map back to their single entry, mirroring how
// cursor navigation treats them as one row.
func (m BreadcrumbPopupModel) HandleMouse(msg tea.MouseMsg, screenW, screenH int) (BreadcrumbPopupModel, tea.Cmd) {
	if !m.animator.IsInteractive() || msg.Action != tea.MouseActionPress {
		return m, nil
	}
	popup := m.renderFullPopup()
	if !popupContains(popup, msg, screenW, screenH) {
		return m, nil
	}
	if msg.Button == tea.MouseButtonRight {
		return m, m.animator.Close()
	}
	if msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	// Find the entry under the click. Each entry can span N display
	// lines (label-wrap path); walk lines per entry to find the
	// match.
	lines := strings.Split(popup, "\n")
	w := lipgloss.Width(lines[0])
	py := (screenH - len(lines)) / 2
	rowOffset := msg.Y - py - 2 // skip top border + top padding row
	if rowOffset < 0 {
		return m, nil
	}
	innerW := w - 2
	cur := 0
	for i := range m.chain {
		n := m.entryDisplayLines(i, innerW)
		if rowOffset >= cur && rowOffset < cur+n {
			m.cursor = i
			ref := m.chain[i]
			return m, func() tea.Msg { return RequestSwitchToResourceMsg{Ref: ref} }
		}
		cur += n
	}
	return m, nil
}

// entryDisplayLines mirrors renderEntry's chunk count without
// re-rendering — needed for the mouse hit-test which has to map a
// clicked display line back to the entry that produced it.
func (m BreadcrumbPopupModel) entryDisplayLines(i int, innerW int) int {
	if i < 0 || i >= len(m.chain) {
		return 0
	}
	labelPrefix := fmt.Sprintf("%d. ", i+1)
	labelPrefixW := lipgloss.Width(labelPrefix)
	const markerW = 2
	labelBudget := innerW - 1 - labelPrefixW - markerW
	if labelBudget < 10 {
		labelBudget = 10
	}
	return len(wrapPlain(refDisplay(m.chain[i]), labelBudget))
}

func (m BreadcrumbPopupModel) View() string { return "" }

func (m BreadcrumbPopupModel) RenderPopup() string {
	return m.animator.RenderFrame(m.renderFullPopup())
}

func (m BreadcrumbPopupModel) renderFullPopup() string {
	bc := m.borderColor
	bStyle := lipgloss.NewStyle().Foreground(bc)
	tStyle := lipgloss.NewStyle().Foreground(bc).Bold(true)
	levelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7f849c"))
	cursorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#1e1e2e")).Background(bc).Bold(true)
	currentMarkStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Status.Pending)).Bold(true)

	title := "󰍒 Breadcrumb"
	hint := " j/k: move  Enter: switch  Space: close "

	// Widened from 70% to 85% so long resource names (RS-hash suffixes,
	// generated Job names, ...) get more horizontal room before the
	// wrap-fallback kicks in.
	maxInnerW := 80
	if m.screenW > 0 {
		maxInnerW = m.screenW * 85 / 100
		if maxInnerW < 40 {
			maxInnerW = 40
		}
	}

	// First pass: pick innerW from label widths so short chains use a
	// snug popup; long chains expand up to maxInnerW. Matches the
	// renderEntry layout: " " + "N. " + marker(2) + label.
	innerW := lipgloss.Width(title) + 4
	for i, ref := range m.chain {
		levelTag := fmt.Sprintf("%d.", i+1)
		w := 1 + lipgloss.Width(levelTag) + 1 + 2 + lipgloss.Width(refDisplay(ref))
		if w > innerW {
			innerW = w
		}
	}
	if w := len(hint) + 4; w > innerW {
		innerW = w
	}
	if innerW > maxInnerW {
		innerW = maxInnerW
	}

	// Second pass: render rows with wrap-fallback for labels that
	// exceed the chosen innerW (e.g. "Deployment/<60-char-name>...").
	// Continuation lines are indented under the label start so the
	// chain remains visually scannable.
	var rows []string
	for i, ref := range m.chain {
		rows = append(rows, m.renderEntry(i, ref, innerW, levelStyle, cursorStyle, currentMarkStyle)...)
	}

	dashesAfter := innerW - 1 - lipgloss.Width(title)
	if dashesAfter < 0 {
		dashesAfter = 0
	}

	var b strings.Builder
	b.WriteString(bStyle.Render("╭─") + tStyle.Render(title) + bStyle.Render(strings.Repeat("─", dashesAfter)+"╮") + "\n")

	left := bStyle.Render("│")
	right := bStyle.Render("│")
	// One row of top/bottom padding so the title/hint don't sit directly
	// on the first/last content row — matches helmdocmenu styling.
	padRow := left + strings.Repeat(" ", innerW) + right + "\n"
	b.WriteString(padRow)
	for _, line := range rows {
		lw := lipgloss.Width(line)
		pad := ""
		if lw < innerW {
			pad = strings.Repeat(" ", innerW-lw)
		}
		b.WriteString(left + line + pad + right + "\n")
	}
	b.WriteString(padRow)

	bottomDashes := innerW - len(hint) - 1
	if bottomDashes < 0 {
		bottomDashes = 0
	}
	b.WriteString(bStyle.Render("╰─") + tStyle.Render(hint) + bStyle.Render(strings.Repeat("─", bottomDashes)+"╯"))

	return b.String()
}

// renderEntry produces the one or more display lines for a single chain
// entry. Long labels wrap to fit innerW; cursor styling spans every
// wrapped line so the highlight reads as one block.
//
// Layout invariant: every line (cursor or not) is exactly innerW wide
// and starts with a single leading space, so the "N." column lines up
// across rows regardless of cursor state. The cursor highlight wraps
// the full innerW-wide content (including the leading space) so it
// reads as a continuous block flush with both inner borders.
func (m BreadcrumbPopupModel) renderEntry(
	i int, ref k8s.RefTarget, innerW int,
	levelStyle, cursorStyle, currentMarkStyle lipgloss.Style,
) []string {
	levelTag := fmt.Sprintf("%d.", i+1)
	labelPrefix := levelTag + " " // "2. "  (NO leading space — that's added once at line level below)
	labelPrefixW := lipgloss.Width(labelPrefix)
	const markerW = 2
	// Every row carries a marker so the visual rhythm matches the Relatives
	// tab's drill arrow. Middle rows show the chain-step glyph (same as
	// Relatives. drill indicator); the bottom row swaps to the
	// you-are-here glyph for unmistakable current-level affordance.
	marker := breadcrumbChainMarker
	if i == len(m.chain)-1 {
		marker = breadcrumbCurrentMarker
	}

	// Width budget for the label chunk. Line layout:
	//   " " + labelPrefix + marker + chunk + pad-to-innerW
	//   ^1    ^labelPrefixW ^markerW ^?
	labelBudget := innerW - 1 - labelPrefixW - markerW
	if labelBudget < 10 {
		labelBudget = 10
	}
	chunks := wrapPlain(refDisplay(ref), labelBudget)
	contIndent := strings.Repeat(" ", labelPrefixW+markerW)
	isCursor := i == m.cursor

	out := make([]string, 0, len(chunks))
	for ci, chunk := range chunks {
		var bodyPlain string
		if ci == 0 {
			bodyPlain = labelPrefix + marker + chunk
		} else {
			bodyPlain = contIndent + chunk
		}
		// Pad the body to fill all the way to the right border so the
		// cursor highlight (when present) becomes a clean rectangle.
		padW := innerW - 1 - lipgloss.Width(bodyPlain)
		if padW < 0 {
			padW = 0
		}
		pad := strings.Repeat(" ", padW)

		if isCursor {
			out = append(out, cursorStyle.Render(" "+bodyPlain+pad))
			continue
		}
		if ci == 0 {
			markerStyled := marker
			if marker == breadcrumbCurrentMarker {
				markerStyled = currentMarkStyle.Render(marker)
			}
			out = append(out, " "+levelStyle.Render(levelTag)+" "+markerStyled+chunk+pad)
		} else {
			out = append(out, " "+bodyPlain+pad)
		}
	}
	return out
}

// refDisplay formats a RefTarget for the breadcrumb row. Cluster-scoped
// refs (empty namespace) drop the ns/ prefix.
func refDisplay(ref k8s.RefTarget) string {
	kind := string(ref.Type)
	if def := k8s.DefaultRegistry.Get(ref.Type); def != nil {
		kind = strings.TrimSuffix(def.DisplayName, "s")
	}
	if ref.Namespace == "" {
		return fmt.Sprintf("%s/%s", kind, ref.Name)
	}
	return fmt.Sprintf("%s/%s in %s", kind, ref.Name, ref.Namespace)
}
