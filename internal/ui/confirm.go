package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/vulcanshen/kbu/internal/theme"
)

type ConfirmAction int

const (
	ConfirmShellExec ConfirmAction = iota
	ConfirmDelete
	ConfirmEdit
	ConfirmSwitch
	ConfirmRollback
)

type ConfirmModel struct {
	animator    PopupAnimator
	action      ConfirmAction
	message     string
	detail      string
	screenW     int
	theme       *theme.Theme
	onConfirm   tea.Cmd
	layer       int
	borderColor lipgloss.Color
}

func (m *ConfirmModel) SetSize(w, h int) {
	m.screenW = w
}

func NewConfirmModel(t *theme.Theme) ConfirmModel {
	bc := theme.PopupLayerColor(1)
	return ConfirmModel{
		theme:       t,
		animator:    NewPopupAnimator("confirm", bc),
		borderColor: bc,
		layer:       1,
	}
}

// SetLayer stamps nesting depth + derives border / animator color.
func (m *ConfirmModel) SetLayer(layer int) {
	m.layer = layer
	m.borderColor = theme.PopupLayerColor(layer)
	m.animator.Color = m.borderColor
}

func (m *ConfirmModel) Show(action ConfirmAction, message, detail string, onConfirm tea.Cmd) tea.Cmd {
	m.action = action
	m.message = message
	m.detail = detail
	m.onConfirm = onConfirm
	return m.animator.Open()
}

func (m *ConfirmModel) Close() tea.Cmd {
	m.onConfirm = nil
	return m.animator.Close()
}

func (m ConfirmModel) IsActive() bool      { return m.animator.IsActive() }
func (m ConfirmModel) IsInteractive() bool { return m.animator.IsInteractive() }

func (m *ConfirmModel) HandleTick(msg AnimTickMsg) tea.Cmd {
	if msg.Target != m.animator.Target {
		return nil
	}
	return m.animator.Tick()
}

func (m ConfirmModel) Update(msg tea.Msg) (ConfirmModel, tea.Cmd) {
	if !m.animator.IsInteractive() {
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter", "y":
			cmd := m.onConfirm
			m.onConfirm = nil
			closeCmd := m.animator.Close()
			return m, tea.Batch(cmd, closeCmd)
		case "esc", "n", " ":
			// Space cancels too — the same key that opens the confirm
			// (Relatives-tab space-jump) re-pressed by reflex should
			// dismiss rather than re-trigger.
			m.onConfirm = nil
			return m, m.animator.Close()
		}
	}
	return m, nil
}

// HandleMouse routes a click against the confirm dialog.
// Right-click inside the popup cancels (mirror of Esc / n / Space).
// Left-click intentionally does NOT confirm — accidental click
// could fire a destructive delete / edit / rollback, so the user
// must commit deliberately via keyboard Enter / y. Outside-popup
// clicks are no-ops.
func (m ConfirmModel) HandleMouse(msg tea.MouseMsg, screenW, screenH int) (ConfirmModel, tea.Cmd) {
	if !m.animator.IsInteractive() || msg.Action != tea.MouseActionPress {
		return m, nil
	}
	if !popupContains(m.renderFullPopup(), msg, screenW, screenH) {
		return m, nil
	}
	if msg.Button == tea.MouseButtonRight {
		m.onConfirm = nil
		return m, m.animator.Close()
	}
	return m, nil
}

func (m ConfirmModel) View() string {
	return ""
}

func (m ConfirmModel) RenderPopup() string {
	return m.animator.RenderFrame(m.renderFullPopup())
}

func (m ConfirmModel) renderFullPopup() string {
	bc := m.borderColor
	bStyle := lipgloss.NewStyle().Foreground(bc)
	tStyle := lipgloss.NewStyle().Foreground(bc).Bold(true)
	msgStyle := lipgloss.NewStyle().Bold(true)
	detailStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Status.Pending))

	title := "󰦕 Confirm"
	hint := " Enter/y: confirm  Space: cancel "

	// Cap inner width at 70% of screen (or 80 chars if no screen size).
	maxInnerW := 80
	if m.screenW > 0 {
		maxInnerW = m.screenW * 70 / 100
		if maxInnerW < 40 {
			maxInnerW = 40
		}
	}

	// Start from content; reserve 2 chars for left/right inner padding.
	innerW := 40
	for _, s := range []string{m.message, m.detail} {
		if w := lipgloss.Width(s) + 2; w > innerW {
			innerW = w
		}
	}
	if w := lipgloss.Width(title) + 4; w > innerW {
		innerW = w
	}
	if w := len(hint) + 4; w > innerW {
		innerW = w
	}
	if innerW > maxInnerW {
		innerW = maxInnerW
	}

	contentW := innerW - 2 // leading + trailing padding
	var lines []string
	for _, l := range wrapWords(m.message, contentW) {
		lines = append(lines, msgStyle.Render(" "+l))
	}
	if m.detail != "" {
		lines = append(lines, "")
		for _, l := range wrapWords(m.detail, contentW) {
			lines = append(lines, detailStyle.Render(" "+l))
		}
	}
	body := strings.Join(lines, "\n")

	dashesAfter := innerW - 1 - lipgloss.Width(title)
	if dashesAfter < 0 {
		dashesAfter = 0
	}

	var b strings.Builder
	b.WriteString(bStyle.Render("╭─") + tStyle.Render(title) + bStyle.Render(strings.Repeat("─", dashesAfter)+"╮") + "\n")

	left := bStyle.Render("│")
	right := bStyle.Render("│")
	padRow := left + strings.Repeat(" ", innerW) + right + "\n"
	b.WriteString(padRow) // top padding row
	for _, line := range strings.Split(body, "\n") {
		lw := lipgloss.Width(line)
		pad := ""
		if lw < innerW {
			pad = strings.Repeat(" ", innerW-lw)
		}
		b.WriteString(left + line + pad + right + "\n")
	}
	b.WriteString(padRow) // bottom padding row

	bottomDashes := innerW - len(hint) - 1
	if bottomDashes < 0 {
		bottomDashes = 0
	}
	b.WriteString(bStyle.Render("╰─") + tStyle.Render(hint) + bStyle.Render(strings.Repeat("─", bottomDashes)+"╯"))

	return b.String()
}
