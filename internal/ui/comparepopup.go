package ui

import (
	"fmt"
	"strings"

	udiff "github.com/aymanbagabas/go-udiff"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/vulcanshen/kbu/internal/theme"
)

// splitDiffLineLimit caps the per-side input to the line-LCS alignment in
// renderSplitDiff. The DP table is O(n*m) ints; 2000*2000 = 4M ints ≈ 32MB
// — a safe upper bound that finishes inside a frame budget while still
// rendering a useful diff for the realistic worst case (two helm-deployed
// resources whose `kubectl.kubernetes.io/last-applied-configuration`
// annotations carry the full chart manifest). Lines above the limit are
// dropped with a banner announcing the truncation.
const splitDiffLineLimit = 2000

// CompareLayout selects how a diff renders. Split shows old/new side-by-side
// (git diff --side-by-side); Unified shows a single column with -/+ markers
// (git diff default). Persisted in the user config so the choice survives
// a restart.
type CompareLayout int

const (
	CompareLayoutSplit CompareLayout = iota
	CompareLayoutUnified
)

func (l CompareLayout) String() string {
	if l == CompareLayoutUnified {
		return "unified"
	}
	return "split"
}

// CompareYamlPopupModel renders a YAML diff between a locked baseline and a
// chosen comparison target. Driven from the panel-2 Space menu's "Compare
// to this resource" action. Both YAMLs arrive pre-cleaned by
// k8s.MarshalItemYAMLForCompare (status block stripped, managedFields /
// resourceVersion / uid / generation / creationTimestamp wiped). Diff
// computation goes through go-udiff; rendering is a hand-rolled side-by-
// side or +/- unified view, coloured via lipgloss.
//
// Keybindings (popup-only):
//
//	j/k/down/up      scroll one line
//	u/d              scroll half-page
//	g g              jump to top
//	G                jump to bottom
//	Space            open the in-popup action menu
//	Esc              close the popup
//
// Action menu (in-popup, Space-triggered):
//
//	Toggle layout    flip split ↔ unified (persisted via callback)
//	Close            close the popup
//
// The user explicitly asked for compare actions to be discoverable via
// menu rather than hotkey — same rationale as the panel-2 Lock /
// Compare-to / Exit entries — so there's no direct `s`/`u`/`t` toggle.
type CompareYamlPopupModel struct {
	leftYAML   string
	rightYAML  string
	leftLabel  string // "kind/name" of the locked baseline
	rightLabel string // "kind/name" of the comparison target

	layout CompareLayout

	// Rendered display lines for the current layout. Rebuilt whenever
	// layout or popup width changes. scrollOffset indexes into this slice.
	contentLines []string
	scrollOffset int

	// menu is the Space-triggered diff-options sub-popup (Switch view /
	// Close). Lives in its own file (comparemenu.go) with its own
	// PopupAnimator so it falls under the regular popup-audit pattern
	// — see .claude/rules/popup-convention.md.
	menu CompareMenuPopupModel

	width    int
	height   int
	theme    *theme.Theme
	animator PopupAnimator

	// lastBuiltWidth + lastBuiltLayout cache the conditions under which
	// contentLines was built so we only rebuild on actual changes
	// (popup resize, layout toggle).
	lastBuiltWidth  int
	lastBuiltLayout CompareLayout

	pendingG bool

	// onLayoutChange is invoked when the user toggles split ↔ unified.
	// AppModel uses it to persist the new layout into the config file
	// so the choice survives a restart. nil = no persistence.
	onLayoutChange func(CompareLayout)

	layer       int
	borderColor lipgloss.Color
}

// NewCompareYamlPopupModel constructs a compare popup with the default
// (Unified) layout. Unified survives narrow panels and reads like a
// standard `git diff` to anyone who's used one, so it makes the
// safer default than Split. Callers override via SetDefaultLayout
// before Open if the user's config carries a different preference.
func NewCompareYamlPopupModel(t *theme.Theme) CompareYamlPopupModel {
	bc := theme.PopupLayerColor(1)
	return CompareYamlPopupModel{
		theme:       t,
		animator:    NewPopupAnimator("comparepopup", bc),
		menu:        NewCompareMenuPopupModel(t),
		layout:      CompareLayoutUnified,
		borderColor: bc,
		layer:       1,
	}
}

// SetLayer stamps nesting depth + derives border / animator color.
func (m *CompareYamlPopupModel) SetLayer(layer int) {
	m.layer = layer
	m.borderColor = theme.PopupLayerColor(layer)
	m.animator.Color = m.borderColor
}

// SetDefaultLayout sets the initial layout used by subsequent Open calls.
// Hook for config-driven defaults — AppModel wires this from the loaded
// kbu config.yaml `compare.layout` value at startup.
func (m *CompareYamlPopupModel) SetDefaultLayout(l CompareLayout) {
	m.layout = l
}

// SetOnLayoutChange registers a callback invoked when the user toggles
// the layout via the in-popup menu. AppModel uses this to persist the
// choice into the user config file.
func (m *CompareYamlPopupModel) SetOnLayoutChange(fn func(CompareLayout)) {
	m.onLayoutChange = fn
}

// Open populates the popup with both YAML payloads + the per-instance
// labels rendered in the column / line headers, then begins the open
// animation. Both payloads should already be compare-cleaned (status
// block + per-instance noise stripped — see k8s.MarshalItemYAMLForCompare).
func (m *CompareYamlPopupModel) Open(left, right, leftLabel, rightLabel string) tea.Cmd {
	m.leftYAML = left
	m.rightYAML = right
	m.leftLabel = leftLabel
	m.rightLabel = rightLabel
	m.scrollOffset = 0
	m.menu.Reset()
	m.pendingG = false
	m.rebuildContent()
	return m.animator.Open()
}

func (m *CompareYamlPopupModel) Close() tea.Cmd       { return m.animator.Close() }
func (m CompareYamlPopupModel) IsActive() bool        { return m.animator.IsActive() }
func (m CompareYamlPopupModel) IsInteractive() bool   { return m.animator.IsInteractive() }
func (m CompareYamlPopupModel) ScrollOffset() int     { return m.scrollOffset }
func (m CompareYamlPopupModel) Layout() CompareLayout { return m.layout }
func (m CompareYamlPopupModel) MenuOpen() bool        { return m.menu.IsActive() }

func (m *CompareYamlPopupModel) HandleTick(msg AnimTickMsg) tea.Cmd {
	if msg.Target == m.animator.Target {
		return m.animator.Tick()
	}
	return m.menu.HandleTick(msg)
}

func (m *CompareYamlPopupModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	if m.leftYAML == "" && m.rightYAML == "" {
		return
	}
	if m.bodyWidth() != m.lastBuiltWidth || m.layout != m.lastBuiltLayout {
		m.rebuildContent()
	}
}

func (m CompareYamlPopupModel) Update(msg tea.Msg) (CompareYamlPopupModel, tea.Cmd) {
	if !m.animator.IsInteractive() {
		return m, nil
	}
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if m.menu.IsActive() {
		newMenu, result, cmd := m.menu.Update(keyMsg)
		m.menu = newMenu
		if result == CompareMenuActionCommit {
			switch m.menu.Cursor() {
			case 0:
				if m.layout == CompareLayoutSplit {
					m.layout = CompareLayoutUnified
				} else {
					m.layout = CompareLayoutSplit
				}
				if m.onLayoutChange != nil {
					m.onLayoutChange(m.layout)
				}
				m.scrollOffset = 0
				m.rebuildContent()
				return m, cmd
			case 1:
				return m, tea.Batch(cmd, m.animator.Close())
			}
		}
		return m, cmd
	}
	return m.handlePopupKey(keyMsg)
}

func (m CompareYamlPopupModel) handlePopupKey(keyMsg tea.KeyMsg) (CompareYamlPopupModel, tea.Cmd) {
	switch keyMsg.String() {
	case "esc":
		m.pendingG = false
		return m, m.animator.Close()
	case " ":
		m.pendingG = false
		// Sub-popup layer = parent layer + 1. Compare popup is
		// typically layer 1, so the menu opens at layer 2
		// (lavenphire50) — visibly deeper than the parent.
		m.menu.SetLayer(m.layer + 1)
		return m, m.menu.Open(m.menuItems())
	case "j", "down":
		if m.scrollOffset < m.maxScrollOffset() {
			m.scrollOffset++
		}
		m.pendingG = false
	case "k", "up":
		if m.scrollOffset > 0 {
			m.scrollOffset--
		}
		m.pendingG = false
	case "d":
		half := m.contentHeight() / 2
		if half < 1 {
			half = 1
		}
		m.scrollOffset += half
		if m.scrollOffset > m.maxScrollOffset() {
			m.scrollOffset = m.maxScrollOffset()
		}
		m.pendingG = false
	case "u":
		half := m.contentHeight() / 2
		if half < 1 {
			half = 1
		}
		m.scrollOffset -= half
		if m.scrollOffset < 0 {
			m.scrollOffset = 0
		}
		m.pendingG = false
	case "g":
		if m.pendingG {
			m.scrollOffset = 0
			m.pendingG = false
		} else {
			m.pendingG = true
		}
	case "G":
		m.scrollOffset = m.maxScrollOffset()
		m.pendingG = false
	default:
		m.pendingG = false
	}
	return m, nil
}

// menuItems builds the in-popup Space menu. Returned as labels because
// the menu is two items — a slice indexed by menuCursor is the simplest
// possible structure.
func (m CompareYamlPopupModel) menuItems() []string {
	other := CompareLayoutSplit
	if m.layout == CompareLayoutSplit {
		other = CompareLayoutUnified
	}
	return []string{
		fmt.Sprintf("Switch to %s view", other.String()),
		"Close",
	}
}

// rebuildContent recomputes contentLines for the current layout +
// popup body width. Called whenever Open / SetSize-width-changed /
// layout toggle happens.
func (m *CompareYamlPopupModel) rebuildContent() {
	bodyW := m.bodyWidth()
	m.lastBuiltWidth = bodyW
	m.lastBuiltLayout = m.layout
	if m.layout == CompareLayoutUnified {
		m.contentLines = renderUnifiedDiff(m.leftYAML, m.rightYAML, m.leftLabel, m.rightLabel, bodyW, m.theme, m.borderColor)
	} else {
		m.contentLines = renderSplitDiff(m.leftYAML, m.rightYAML, m.leftLabel, m.rightLabel, bodyW, m.theme, m.borderColor)
	}
	if m.scrollOffset > m.maxScrollOffset() {
		m.scrollOffset = m.maxScrollOffset()
	}
}

func (m CompareYamlPopupModel) bodyWidth() int {
	w := m.popupWidth() - 2
	if w < 20 {
		w = 20
	}
	return w
}

func (m CompareYamlPopupModel) popupWidth() int {
	if m.width <= 0 {
		return 80
	}
	w := m.width - 2*popupHMargin
	if w < 40 {
		w = 40
	}
	return w
}

func (m CompareYamlPopupModel) popupHeight() int {
	if m.height <= 0 {
		return 20
	}
	h := m.height - 2*popupVMargin
	if h < 10 {
		h = 10
	}
	return h
}

func (m CompareYamlPopupModel) contentHeight() int {
	h := m.popupHeight() - 2 // top + bottom border
	if h < 1 {
		h = 1
	}
	return h
}

func (m CompareYamlPopupModel) maxScrollOffset() int {
	max := len(m.contentLines) - m.contentHeight()
	if max < 0 {
		return 0
	}
	return max
}

// capDiffInput truncates each side to splitDiffLineLimit lines and
// reports per-side truncation. Shared by renderSplitDiff and
// renderUnifiedDiff so the OOM/perf protection applies regardless of
// which layout the user picks — fix #1 originally only capped the
// split path's line-LCS, leaving the unified path's udiff.Unified call
// as an uncapped escape hatch on the same large input.
func capDiffInput(left, right string) (cappedLeft, cappedRight string, truncL, truncR bool) {
	if left == "" && right == "" {
		return "", "", false, false
	}
	ll := strings.Split(strings.TrimRight(left, "\n"), "\n")
	rl := strings.Split(strings.TrimRight(right, "\n"), "\n")
	if left == "" {
		ll = nil
	}
	if right == "" {
		rl = nil
	}
	truncL = len(ll) > splitDiffLineLimit
	truncR = len(rl) > splitDiffLineLimit
	if truncL {
		ll = ll[:splitDiffLineLimit]
	}
	if truncR {
		rl = rl[:splitDiffLineLimit]
	}
	return strings.Join(ll, "\n"), strings.Join(rl, "\n"), truncL, truncR
}

// truncationBanner formats the peach ⚠ row that announces which sides
// got clipped at splitDiffLineLimit. Returns "" when neither side was
// truncated so callers can omit the row entirely.
func truncationBanner(truncL, truncR bool, width int) string {
	if !truncL && !truncR {
		return ""
	}
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#fab387")).Bold(true)
	var msg string
	switch {
	case truncL && truncR:
		msg = fmt.Sprintf(" ⚠ both sides truncated to first %d lines (diff cap; Esc + Y on the row for full YAML)", splitDiffLineLimit)
	case truncL:
		msg = fmt.Sprintf(" ⚠ left truncated to first %d lines (diff cap; Esc + Y on the row for full YAML)", splitDiffLineLimit)
	default:
		msg = fmt.Sprintf(" ⚠ right truncated to first %d lines (diff cap; Esc + Y on the row for full YAML)", splitDiffLineLimit)
	}
	return warnStyle.Render(ansiTruncate(msg, width))
}

// renderUnifiedDiff returns display lines for unified-diff mode. Wraps
// go-udiff's Unified() output with lipgloss colouring: red on `-`,
// green on `+`, dimmed on `@@` hunk headers, default on context.
func renderUnifiedDiff(left, right, leftLabel, rightLabel string, width int, t *theme.Theme, accent lipgloss.Color) []string {
	cappedLeft, cappedRight, truncL, truncR := capDiffInput(left, right)
	diff := udiff.Unified(leftLabel, rightLabel, cappedLeft, cappedRight)
	if diff == "" {
		// Even when udiff produces nothing, surface the truncation
		// notice — otherwise the user sees "(identical — no config
		// diff)" against a silently-clipped input and misreads it.
		if banner := truncationBanner(truncL, truncR, width); banner != "" {
			return []string{banner, centerNoDiff(width, t)}
		}
		return []string{centerNoDiff(width, t)}
	}
	addStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Status.Running))
	delStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Status.Error))
	hunkStyle := lipgloss.NewStyle().Foreground(accent).Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7f849c"))
	var out []string
	if banner := truncationBanner(truncL, truncR, width); banner != "" {
		out = append(out, banner)
	}
	for _, raw := range strings.Split(strings.TrimRight(diff, "\n"), "\n") {
		// Truncate to body width to avoid wrap drift between hunks.
		truncated := ansiTruncate(raw, width)
		switch {
		case strings.HasPrefix(raw, "+++") || strings.HasPrefix(raw, "---"):
			out = append(out, dimStyle.Render(truncated))
		case strings.HasPrefix(raw, "@@"):
			out = append(out, hunkStyle.Render(truncated))
		case strings.HasPrefix(raw, "+"):
			out = append(out, addStyle.Render(truncated))
		case strings.HasPrefix(raw, "-"):
			out = append(out, delStyle.Render(truncated))
		default:
			out = append(out, truncated)
		}
	}
	return out
}

// renderSplitDiff returns display lines for side-by-side mode. Walks the
// go-udiff edit list, building a synchronized left/right pair of lines
// for each contiguous chunk. Half-width column per side, separated by a
// vertical bar.
func renderSplitDiff(left, right, leftLabel, rightLabel string, width int, t *theme.Theme, accent lipgloss.Color) []string {
	// 1 separator column ` │ `, 3 chars.
	colW := (width - 3) / 2
	if colW < 8 {
		colW = 8
	}
	addStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Status.Running))
	delStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Status.Error))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7f849c"))
	sepStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7f849c"))
	headerStyle := lipgloss.NewStyle().Foreground(accent).Bold(true)

	// capDiffInput caps both sides to splitDiffLineLimit and reports
	// per-side truncation. Shared with the unified path so the OOM /
	// perf protection applies regardless of which layout is active.
	cappedLeft, cappedRight, truncatedLeft, truncatedRight := capDiffInput(left, right)
	leftLines := strings.Split(strings.TrimRight(cappedLeft, "\n"), "\n")
	rightLines := strings.Split(strings.TrimRight(cappedRight, "\n"), "\n")
	if cappedLeft == "" {
		leftLines = nil
	}
	if cappedRight == "" {
		rightLines = nil
	}

	// Compute line-level LCS alignment directly. go-udiff's Edit list
	// is byte-oriented and breaks down for sub-line changes (a single
	// `accent: '#0066FF'` → `accent: '#00CC66'` edit yielded `+ CC`
	// fragments instead of a clean full-line replacement). Line-LCS
	// is the right abstraction for a side-by-side YAML diff anyway.
	pairs := alignSplitDiff(leftLines, rightLines)

	// fit clips s to colW and right-pads with spaces — split-view column
	// alignment depends on EVERY cell being exactly colW wide. The shared
	// padRight in help.go only pads (no truncate), so long YAML lines
	// (e.g. last-applied-configuration JSON, base64 cert blobs) used to
	// blow past the column and the separator vanished.
	fit := func(s string) string {
		if lipgloss.Width(s) > colW {
			s = ansiTruncate(s, colW)
		}
		return padRight(s, colW)
	}
	out := make([]string, 0, len(pairs)+3)
	// Header row: locked label on the left, compare label on the right.
	out = append(out, headerStyle.Render(fit(leftLabel))+
		sepStyle.Render(" │ ")+
		headerStyle.Render(fit(rightLabel)))
	out = append(out, sepStyle.Render(strings.Repeat("─", colW))+
		sepStyle.Render("─┼─")+
		sepStyle.Render(strings.Repeat("─", colW)))
	// Truncation banner sits between the header row and the diff body so
	// the user immediately sees that the LCS bypassed lines beyond the
	// cap. truncationBanner() emits "" when neither side was clipped,
	// so the append is unconditional but cheap.
	if banner := truncationBanner(truncatedLeft, truncatedRight, width); banner != "" {
		out = append(out, banner)
	}

	if len(pairs) == 0 {
		out = append(out, centerNoDiff(width, t))
		return out
	}
	for _, p := range pairs {
		var ls, rs string
		switch p.kind {
		case splitPairInsert:
			ls = dimStyle.Render(fit(""))
			rs = addStyle.Render(fit("+ " + p.right))
		case splitPairDelete:
			ls = delStyle.Render(fit("- " + p.left))
			rs = dimStyle.Render(fit(""))
		case splitPairChanged:
			ls = delStyle.Render(fit("- " + p.left))
			rs = addStyle.Render(fit("+ " + p.right))
		default: // splitPairContext
			ls = fit("  " + p.left)
			rs = fit("  " + p.right)
		}
		out = append(out, ls+sepStyle.Render(" │ ")+rs)
	}
	return out
}

// splitPairKind tags a splitPair so the renderer doesn't have to infer
// the row's role from empty-string heuristics. The earlier `changed
// bool` + `left/right == ""` predicates silently swallowed a perfectly
// valid case: an inserted/deleted line whose own content was the empty
// string (e.g. a blank separator line added to one side). With the
// kind-tag the renderer's switch is exhaustive — blank insertions
// render as `+` rows on the right column instead of disappearing into
// the default context branch.
type splitPairKind byte

const (
	splitPairContext splitPairKind = iota // unchanged on both sides
	splitPairChanged                      // present on both sides but different
	splitPairInsert                       // right-only line (left column gets the dim placeholder)
	splitPairDelete                       // left-only line (right column gets the dim placeholder)
)

// splitPair is one synchronized row in the split-diff view. left/right
// is the raw line text; kind drives renderer styling.
type splitPair struct {
	left  string
	right string
	kind  splitPairKind
}

// changed reports whether the pair is the both-present-but-different
// case. Kept as a method so existing tests that assert `p.changed`
// don't have to be rewritten for the kind enum.
func (p splitPair) changed() bool { return p.kind == splitPairChanged }

// alignSplitDiff produces side-by-side pairs via a line-level LCS:
// equal lines pair 1:1 as context; runs of left-only / right-only
// lines surface as delete / insert; adjacent delete + insert at the
// same boundary collapse into a single `changed` pair so the renderer
// can paint both columns in their respective colours.
//
// Line-LCS replaced an earlier byte-level approach that walked go-
// udiff's Edit list. The byte approach had two bugs the user saw on
// near-twin ConfigMaps:
//
//  1. Context lines paired leftLines[i] with itself on both sides,
//     so the right column rendered the left content verbatim — a
//     `accent: '#0066FF'` from the LEFT showed up where the RIGHT's
//     `accent: '#00CC66'` should have been.
//  2. Sub-line edits (e.g. replacing `0066FF` with `00CC66` inside
//     one line) produced char-fragment pairs (`+ CC`, `+ f`, `+ r`,
//     ...) because go-udiff's Edit spanned a few bytes within a
//     line, oldChunk was empty, and newChunk was the orphan fragment.
//
// O(n*m) memory/time — fine for typical K8s YAMLs (≤ a few hundred
// lines). If we ever hit pathological inputs we can swap to Myers.
func alignSplitDiff(leftLines, rightLines []string) []splitPair {
	n, m := len(leftLines), len(rightLines)
	// LCS DP table.
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			if leftLines[i-1] == rightLines[j-1] {
				lcs[i][j] = lcs[i-1][j-1] + 1
			} else if lcs[i-1][j] >= lcs[i][j-1] {
				lcs[i][j] = lcs[i-1][j]
			} else {
				lcs[i][j] = lcs[i][j-1]
			}
		}
	}
	// Backtrack — record (context / delete / insert) ops in forward order.
	type opKind byte
	const (
		opContext opKind = iota
		opDelete
		opInsert
	)
	type op struct {
		kind  opKind
		left  string
		right string
	}
	ops := make([]op, 0, n+m)
	i, j := n, m
	for i > 0 && j > 0 {
		switch {
		case leftLines[i-1] == rightLines[j-1]:
			ops = append(ops, op{kind: opContext, left: leftLines[i-1], right: rightLines[j-1]})
			i--
			j--
		case lcs[i-1][j] >= lcs[i][j-1]:
			ops = append(ops, op{kind: opDelete, left: leftLines[i-1]})
			i--
		default:
			ops = append(ops, op{kind: opInsert, right: rightLines[j-1]})
			j--
		}
	}
	for i > 0 {
		ops = append(ops, op{kind: opDelete, left: leftLines[i-1]})
		i--
	}
	for j > 0 {
		ops = append(ops, op{kind: opInsert, right: rightLines[j-1]})
		j--
	}
	for a, b := 0, len(ops)-1; a < b; a, b = a+1, b-1 {
		ops[a], ops[b] = ops[b], ops[a]
	}

	// Collapse adjacent delete+insert runs into `changed` pairs. We
	// pair them positionally: 1st delete ↔ 1st insert, 2nd ↔ 2nd, ...
	// Any leftover from the longer side trails as pure delete or insert.
	pairs := make([]splitPair, 0, len(ops))
	k := 0
	for k < len(ops) {
		if ops[k].kind == opContext {
			pairs = append(pairs, splitPair{left: ops[k].left, right: ops[k].right, kind: splitPairContext})
			k++
			continue
		}
		// Gather the contiguous run of deletes + inserts (in any order).
		var dels, ins []string
		for k < len(ops) && ops[k].kind != opContext {
			switch ops[k].kind {
			case opDelete:
				dels = append(dels, ops[k].left)
			case opInsert:
				ins = append(ins, ops[k].right)
			}
			k++
		}
		paired := len(dels)
		if len(ins) < paired {
			paired = len(ins)
		}
		for p := 0; p < paired; p++ {
			pairs = append(pairs, splitPair{left: dels[p], right: ins[p], kind: splitPairChanged})
		}
		for p := paired; p < len(dels); p++ {
			pairs = append(pairs, splitPair{left: dels[p], kind: splitPairDelete})
		}
		for p := paired; p < len(ins); p++ {
			pairs = append(pairs, splitPair{right: ins[p], kind: splitPairInsert})
		}
	}
	return pairs
}

func centerNoDiff(width int, t *theme.Theme) string {
	msg := "(identical — no config diff)"
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#7f849c"))
	pad := (width - lipgloss.Width(msg)) / 2
	if pad < 0 {
		pad = 0
	}
	return strings.Repeat(" ", pad) + dim.Render(msg)
}

// HandleMouse routes a click against the compare popup. Wheel
// scroll already gets translated to u/d at the AppModel layer, so
// this only needs to cover the discrete button gestures.
// Right-click inside the popup closes it (mirror of Esc). The
// in-popup Space menu stays keyboard-only — left-click is no-op
// on both the diff body and the menu overlay to avoid accidental
// layout toggles.
func (m CompareYamlPopupModel) HandleMouse(msg tea.MouseMsg, screenW, screenH int) (CompareYamlPopupModel, tea.Cmd) {
	if !m.animator.IsInteractive() || msg.Action != tea.MouseActionPress {
		return m, nil
	}
	if !popupContains(m.renderFrame(), msg, screenW, screenH) {
		return m, nil
	}
	if msg.Button == tea.MouseButtonRight {
		return m, m.animator.Close()
	}
	return m, nil
}

// RenderPopup composes the diff panel onto a centred overlay frame —
// border, title, body, bottom-hint legend.
func (m CompareYamlPopupModel) RenderPopup() string {
	return m.animator.RenderFrame(m.renderFrame())
}

func (m CompareYamlPopupModel) renderFrame() string {
	popupW := m.popupWidth()
	popupH := m.popupHeight()
	borderColor := m.borderColor
	borderStyle := lipgloss.NewStyle().Foreground(borderColor)
	titleStyle := lipgloss.NewStyle().Foreground(borderColor).Bold(true)
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7f849c"))

	title := fmt.Sprintf(" \U000f08aa %s vs %s ",
		m.leftLabel, m.rightLabel)
	titleW := lipgloss.Width(title)
	innerW := popupW - 2
	if innerW < 10 {
		innerW = 10
	}
	leadDashCount := 2
	truncated := false
	if titleW+leadDashCount+1 > innerW {
		// Reserve 1 cell for "…" so a narrow-terminal truncation reads
		// as cut, not as the literal trailing fragment (the long
		// "demo/demo-app-…-xyz (unified)" right-end got snipped at "(u"
		// on small windows, indistinguishable from a real label).
		titleW = innerW - leadDashCount - 1
		title = ansiTruncate(title, titleW-1)
		truncated = true
	}
	leadDashes := strings.Repeat("─", leadDashCount)
	trailLen := innerW - leadDashCount - titleW
	if trailLen < 1 {
		trailLen = 1
	}
	titleRender := titleStyle.Render(title)
	if truncated {
		// Append "…" via a separate Render call so the ellipsis carries
		// titleStyle — ansiTruncate emits a \x1b[0m at the end, which
		// would otherwise drop the ellipsis to the default fg color.
		titleRender += titleStyle.Render("…")
	}
	top := borderStyle.Render("╭"+leadDashes) +
		titleRender +
		borderStyle.Render(strings.Repeat("─", trailLen)+"╮")

	body := m.renderBody(innerW, popupH-2)
	vbar := borderStyle.Render("│")
	bodyRows := strings.Split(body, "\n")
	for i, row := range bodyRows {
		visible := lipgloss.Width(row)
		if visible < innerW {
			row = row + strings.Repeat(" ", innerW-visible)
		} else if visible > innerW {
			row = ansiTruncate(row, innerW)
		}
		bodyRows[i] = vbar + row + vbar
	}

	hint := " Space: menu  j/k: scroll  Esc: close "
	hintW := lipgloss.Width(hint)
	// Bottom border target width = innerW + 2 (matches top: ╭ + innerW
	// dashes-or-title + ╮). The earlier "╰─" lead consumed 2 chars but
	// the trailing-dash count subtracted 2 from innerW, leaving the
	// row 1 char short — and the ╯ corner slid 1 cell left of the
	// right vertical bar. Drop the leading "─" and size trailDashes =
	// innerW - hintW so the total comes out to innerW + 2.
	trailDashes := innerW - hintW
	if trailDashes < 1 {
		trailDashes = 1
	}
	bot := borderStyle.Render("╰") + hintStyle.Render(hint) +
		borderStyle.Render(strings.Repeat("─", trailDashes)+"╯")

	parts := []string{top}
	parts = append(parts, bodyRows...)
	parts = append(parts, bot)
	frame := strings.Join(parts, "\n")
	if m.menu.IsActive() {
		frame = m.menu.Render(frame)
	}
	return frame
}

func (m CompareYamlPopupModel) renderBody(width, height int) string {
	rows := make([]string, 0, height)
	end := m.scrollOffset + height
	if end > len(m.contentLines) {
		end = len(m.contentLines)
	}
	for i := m.scrollOffset; i < end; i++ {
		rows = append(rows, m.contentLines[i])
	}
	for len(rows) < height {
		rows = append(rows, "")
	}
	return strings.Join(rows, "\n")
}
