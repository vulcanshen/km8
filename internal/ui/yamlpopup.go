package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/vulcanshen/kbu/internal/k8s"
	"github.com/vulcanshen/kbu/internal/theme"
)

// YamlPopupModel renders the full YAML of a resource in a large overlay popup.
//
// v1.7.10 pivot: this popup is a vim-style buffer, not a scroll-only
// viewer. hjkl moves a cursor (auto-scrolling to keep it visible); v
// enters character-wise visual mode; y in visual mode copies the
// selected substring, y outside visual mode copies the full YAML.
// w/b/e word-motion + 0/$ line-endpoints + gg/G buffer-endpoints + u/d
// half-page all follow vim semantics.
//
// Also supports / search (Enter commits; n/N step through matches),
// E to dispatch kubectl edit on the same resource (no confirm — user
// already inspected before pressing), and Esc to close (or exit
// visual mode when active).
type YamlPopupModel struct {
	yaml string
	// rawLines is the source YAML split by newline. searchQuery matches
	// against these; matchLines indices refer to this slice.
	rawLines []string
	// contentLines are the rendered DISPLAY lines — long raw lines are word-
	// wrapped to fit the popup body width, then highlighted. One raw line
	// may produce 1+ display lines (see contentLineRaw).
	contentLines []string
	// contentPlain[i] is the ANSI-stripped version of contentLines[i].
	// Kept parallel to contentLines so cursor-col math (rune-index into
	// the visible characters) can happen in O(1) per line without
	// re-stripping the styled string on every keystroke. Also the
	// authoritative source for visual-mode selection extraction — y
	// copies substrings out of these plain strings.
	contentPlain []string
	// contentLineRaw[i] = index into rawLines that display line i came from.
	// Used to (a) map matchLines (raw indices) to display positions for
	// auto-scroll and (b) extend the current-match background highlight
	// across all wrapped chunks of the same raw line.
	contentLineRaw []int
	// contentChunkStart[i] = rune offset into rawLines[contentLineRaw[i]]
	// where display line i's content begins. For the first (or only)
	// chunk of a raw line this is 0; for a wrapped continuation chunk it
	// is the offset past the earlier chunks + the spaces wrapPlain trimmed
	// at each break. This lets visual-mode yank map a display (line,col)
	// back to a raw (line,col) so the copied text is reconstructed from
	// rawLines — no soft-wrap newlines injected, no wrap-boundary spaces
	// lost. See selectionText.
	contentChunkStart []int
	// lastBuiltWidth caches the body width content was wrapped for, so we
	// only rebuild on actual width changes (not every render call).
	lastBuiltWidth int
	scrollOffset   int
	width          int
	height         int
	theme          *theme.Theme
	animator       PopupAnimator

	// Edit target captured at Open() time.
	resource    k8s.ResourceType
	item        k8s.ResourceItem
	contextName string

	// Search state.
	searching   bool
	searchQuery string
	matchLines  []int
	matchCursor int

	pendingG bool

	// Cursor position — the vim buffer cursor. cursorLine indexes into
	// contentLines / contentPlain; cursorCol is a rune-index within
	// contentPlain[cursorLine]. Both default to 0 so Open() lands the
	// cursor at the top-left of the document.
	cursorLine int
	cursorCol  int

	// Visual mode. visualMode=true switches every motion key from
	// "move cursor" to "extend selection between anchor and cursor",
	// and remaps y from "copy full YAML" to "copy selection + exit
	// visual". Anchor coordinates are captured when v is pressed and
	// don't move as the cursor extends the selection.
	visualMode       bool
	visualAnchorLine int
	visualAnchorCol  int

	layer       int
	borderColor lipgloss.Color
}

// NewYamlPopupModel constructs a YamlPopupModel.
func NewYamlPopupModel(t *theme.Theme) YamlPopupModel {
	bc := theme.PopupLayerColor(1)
	return YamlPopupModel{
		theme:       t,
		animator:    NewPopupAnimator("yamlpopup", bc),
		borderColor: bc,
		layer:       1,
	}
}

// SetLayer stamps nesting depth + derives border / animator color.
func (m *YamlPopupModel) SetLayer(layer int) {
	m.layer = layer
	m.borderColor = theme.PopupLayerColor(layer)
	m.animator.Color = m.borderColor
}

// Open populates the popup with YAML for a specific resource, captures the
// edit target, and begins the open animation.
func (m *YamlPopupModel) Open(yaml string, rt k8s.ResourceType, item k8s.ResourceItem, ctxName string) tea.Cmd {
	m.yaml = strings.TrimRight(yaml, "\n")
	m.resource = rt
	m.item = item
	m.contextName = ctxName
	m.scrollOffset = 0
	m.searching = false
	m.searchQuery = ""
	m.matchLines = nil
	m.matchCursor = 0
	m.pendingG = false
	m.cursorLine = 0
	m.cursorCol = 0
	m.visualMode = false
	m.visualAnchorLine = 0
	m.visualAnchorCol = 0
	m.rebuildContent()
	return m.animator.Open()
}

// Close begins the close animation.
func (m *YamlPopupModel) Close() tea.Cmd { return m.animator.Close() }

// IsActive reports whether the popup is being drawn (including animations).
func (m YamlPopupModel) IsActive() bool { return m.animator.IsActive() }

// IsInteractive reports whether the popup should accept input.
func (m YamlPopupModel) IsInteractive() bool { return m.animator.IsInteractive() }

// HandleTick routes animation tick messages to the popup animator.
func (m *YamlPopupModel) HandleTick(msg AnimTickMsg) tea.Cmd {
	if msg.Target != m.animator.Target {
		return nil
	}
	return m.animator.Tick()
}

// SetSize records the screen dimensions used to size the popup and rebuilds
// wrapped content when the body width changes (so reflow happens on resize).
func (m *YamlPopupModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	if m.yaml != "" && m.bodyWidth() != m.lastBuiltWidth {
		m.rebuildContent()
	}
}

// bodyWidth is the column budget for a single YAML content line inside
// the popup — popup inner width minus the 2 borders MINUS the line-
// number gutter. Wrapping decisions in rebuildContent use this value so
// long lines break to the right of the gutter, not through it.
func (m YamlPopupModel) bodyWidth() int {
	w := m.popupWidth() - 2 - m.gutterWidth()
	if w < 10 {
		w = 10
	}
	return w
}

// gutterWidth is the number of cells reserved on the left for the raw
// line number + a single trailing space separator. Zero when there are
// no lines to number. The digit count is derived from len(rawLines),
// so a 42-line YAML gets a 3-cell gutter ("42 "), a 999-line YAML gets
// 4 cells ("999 "). Kept small so it doesn't dominate the popup on
// short YAMLs.
func (m YamlPopupModel) gutterWidth() int {
	n := len(m.rawLines)
	if n == 0 {
		return 0
	}
	digits := 1
	for n >= 10 {
		digits++
		n /= 10
	}
	return digits + 1 // trailing space separator
}

// gutter returns the styled left-gutter cells for display line i.
// Shows the raw line number (1-indexed) on the FIRST display row of
// each raw line; continuation rows (wrapped chunks of the same raw
// line) get blank spaces so the number stays anchored to the logical
// line, not the visual chunk. Fixed width = m.gutterWidth() so cursor
// col math never has to shift past variable-width gutters.
//
// The gutter is intentionally excluded from cursor / selection
// semantics: cursorCol is a rune index into contentPlain[i], which
// contains only content bytes. Visual y copies from contentPlain,
// so line numbers never leak into the clipboard. h at cursorCol=0
// stops there — the gutter is a display artefact, not addressable
// text.
func (m YamlPopupModel) gutter(i int) string {
	gw := m.gutterWidth()
	if gw == 0 || i < 0 || i >= len(m.contentLineRaw) {
		return ""
	}
	// Show the number only on the first display line for this raw
	// line — a wrapped chunk anchoring 3 display rows shouldn't
	// repeat "42" three times.
	showNum := i == 0 || m.contentLineRaw[i] != m.contentLineRaw[i-1]
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7f849c"))
	if !showNum {
		return mutedStyle.Render(strings.Repeat(" ", gw))
	}
	label := fmt.Sprintf("%*d ", gw-1, m.contentLineRaw[i]+1)
	return mutedStyle.Render(label)
}

func (m *YamlPopupModel) rebuildContent() {
	if m.yaml == "" {
		m.rawLines = nil
		m.contentLines = nil
		m.contentPlain = nil
		m.contentLineRaw = nil
		m.contentChunkStart = nil
		m.lastBuiltWidth = 0
		return
	}
	m.rawLines = strings.Split(m.yaml, "\n")
	w := m.bodyWidth()
	m.lastBuiltWidth = w

	m.contentLines = m.contentLines[:0]
	m.contentPlain = m.contentPlain[:0]
	m.contentLineRaw = m.contentLineRaw[:0]
	m.contentChunkStart = m.contentChunkStart[:0]
	for i, raw := range m.rawLines {
		chunks := wrapPlain(raw, w)
		if len(chunks) == 0 {
			// Empty raw line still takes one display row so blank lines in
			// the YAML render as blank rows (don't collapse).
			m.contentLines = append(m.contentLines, "")
			m.contentPlain = append(m.contentPlain, "")
			m.contentLineRaw = append(m.contentLineRaw, i)
			m.contentChunkStart = append(m.contentChunkStart, 0)
			continue
		}
		// Track where each chunk starts within the raw line. wrapPlain
		// trims the run of spaces at each soft-wrap break (TrimRight the
		// chunk end + TrimLeft the next chunk start), so between two
		// consecutive chunks the raw line holds only those trimmed spaces
		// — skip past them to land exactly on the next chunk's first rune.
		// A hard mid-word break leaves no gap, so the skip is a no-op there.
		rawRunes := []rune(raw)
		off := 0
		for _, chunk := range chunks {
			cr := []rune(chunk)
			// Don't skip when the chunk itself starts with a space: that's
			// the leading YAML indentation of the first chunk, which
			// wrapPlain preserves (it only TrimRights the first chunk).
			if len(cr) == 0 || cr[0] != ' ' {
				for off < len(rawRunes) && rawRunes[off] == ' ' {
					off++
				}
			}
			m.contentLines = append(m.contentLines, highlightYAMLLine(chunk, m.theme))
			m.contentPlain = append(m.contentPlain, chunk)
			m.contentLineRaw = append(m.contentLineRaw, i)
			m.contentChunkStart = append(m.contentChunkStart, off)
			off += len(cr)
		}
	}
	// Reflow can shrink line count — clamp cursor so it stays valid.
	m.clampCursor()
}

// firstDisplayLineForRaw returns the smallest display-line index whose source
// raw line == rawIdx, or -1 if not found. Used by search auto-scroll: after
// the user commits a query, we jump the viewport to the first wrapped chunk
// of the matched raw line.
func (m YamlPopupModel) firstDisplayLineForRaw(rawIdx int) int {
	for i, r := range m.contentLineRaw {
		if r == rawIdx {
			return i
		}
	}
	return -1
}

// Update handles keyboard input. Vim-buffer semantics: hjkl / w / b / e /
// 0 / $ / gg / G / u / d all move the cursor (auto-scrolling to keep it
// on screen); v toggles character-wise visual mode; y is mode-aware
// (visual → selection copy + exit; normal → full YAML). Esc exits
// visual mode first, then closes the popup.
func (m YamlPopupModel) Update(msg tea.Msg) (YamlPopupModel, tea.Cmd) {
	if !m.animator.IsInteractive() {
		return m, nil
	}
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if m.searching {
		return m.handleSearchKey(keyMsg)
	}

	switch keyMsg.String() {
	case "esc":
		// Esc peels overlays in order: visual mode → locked search →
		// close popup. Mirrors vim (Esc in visual returns to normal)
		// and preserves the "one Esc backs out one layer" mental model
		// so the user is never surprised by a stray Esc closing the
		// popup while search results are still on screen. Locked search
		// = matches showing but not typing (m.searching=false + a
		// non-empty query); the typing case is handled inside
		// handleSearchKey so this branch never sees it.
		if m.visualMode {
			m.visualMode = false
			m.pendingG = false
			return m, nil
		}
		if m.searchQuery != "" || len(m.matchLines) > 0 {
			m.searchQuery = ""
			m.matchLines = nil
			m.matchCursor = 0
			m.pendingG = false
			return m, nil
		}
		m.pendingG = false
		return m, m.animator.Close()
	case " ":
		// Space keeps closing the popup — it was the pre-vim-buffer
		// close-shortcut and users may have muscle memory for it. Not
		// used in visual mode either (no need for another exit path).
		m.pendingG = false
		return m, m.animator.Close()
	case "h", "left":
		m = m.moveCursorLeft()
		m.pendingG = false
	case "l", "right":
		m = m.moveCursorRight()
		m.pendingG = false
	case "j", "down":
		m = m.moveCursorDown()
		m.pendingG = false
	case "k", "up":
		m = m.moveCursorUp()
		m.pendingG = false
	case "0":
		m.cursorCol = 0
		m.pendingG = false
		m.ensureCursorVisible()
	case "$":
		m.cursorCol = m.lastColOf(m.cursorLine)
		m.pendingG = false
		m.ensureCursorVisible()
	case "w":
		m = m.moveWordForward()
		m.pendingG = false
	case "b":
		m = m.moveWordBackward()
		m.pendingG = false
	case "e":
		m = m.moveWordEnd()
		m.pendingG = false
	case "d":
		half := m.contentHeight() / 2
		if half < 1 {
			half = 1
		}
		m.cursorLine += half
		if m.cursorLine > m.lastLine() {
			m.cursorLine = m.lastLine()
		}
		m.clampCol()
		m.ensureCursorVisible()
		m.pendingG = false
	case "u":
		half := m.contentHeight() / 2
		if half < 1 {
			half = 1
		}
		m.cursorLine -= half
		if m.cursorLine < 0 {
			m.cursorLine = 0
		}
		m.clampCol()
		m.ensureCursorVisible()
		m.pendingG = false
	case "G":
		m.cursorLine = m.lastLine()
		m.clampCol()
		m.ensureCursorVisible()
		m.pendingG = false
	case "g":
		if m.pendingG {
			m.cursorLine = 0
			m.clampCol()
			m.ensureCursorVisible()
			m.pendingG = false
		} else {
			m.pendingG = true
		}
	case "v":
		// Toggle character-wise visual mode. Entering: anchor to
		// current cursor so the initial selection is a single character
		// under the cursor. Re-pressing v exits visual mode without
		// copying — same as Esc but keeps hands over hjkl territory.
		if m.visualMode {
			m.visualMode = false
		} else {
			m.visualMode = true
			m.visualAnchorLine = m.cursorLine
			m.visualAnchorCol = m.cursorCol
		}
		m.pendingG = false
	case "/":
		m.searching = true
		m.searchQuery = ""
		m.matchLines = nil
		m.matchCursor = 0
		m.pendingG = false
	case "n":
		if len(m.matchLines) > 0 {
			m.matchCursor = (m.matchCursor + 1) % len(m.matchLines)
			m.scrollToMatch()
			m.landCursorOnMatch()
		}
		m.pendingG = false
	case "N":
		if len(m.matchLines) > 0 {
			m.matchCursor = (m.matchCursor - 1 + len(m.matchLines)) % len(m.matchLines)
			m.scrollToMatch()
			m.landCursorOnMatch()
		}
		m.pendingG = false
	case "E":
		// Direct edit dispatch. User already inspected the YAML in this popup
		// so we skip the confirm step that the table-level `E` uses.
		if m.item.Name == "" {
			return m, nil
		}
		// Honour the same kind-level edit gate as the table `E` — read-only
		// kinds (Contexts, Events) must not reach kubectl edit even via the
		// YAML popup.
		if !resourceAllowsEdit(m.resource) {
			return m, nil
		}
		closeCmd := m.animator.Close()
		rt, item, ctx := m.resource, m.item, m.contextName
		return m, tea.Batch(closeCmd, func() tea.Msg {
			return startEditMsg{resource: rt, item: item, contextName: ctx}
		})
	case "y":
		// Two modes: visual mode → copy the character-wise selection
		// between anchor and cursor, then exit visual. Normal mode →
		// copy the full YAML (paste-back use cases want the whole
		// document). OSC 52 via copyToClipboardCmd works through tmux /
		// SSH without xclip / pbcopy.
		if m.visualMode {
			text := m.selectionText()
			m.visualMode = false
			if text == "" {
				return m, nil
			}
			return m, copyToClipboardCmd(text)
		}
		if m.yaml == "" {
			return m, nil
		}
		return m, copyToClipboardCmd(m.yaml)
	}
	return m, nil
}

// --- cursor helpers -------------------------------------------------------

// lastLine returns the last valid cursorLine index (len-1) or 0 for empty.
func (m YamlPopupModel) lastLine() int {
	if len(m.contentPlain) == 0 {
		return 0
	}
	return len(m.contentPlain) - 1
}

// lineRunes returns the rune slice for the given display line's plain
// text. Isolated so cursor col math ignores multi-byte character
// boundary issues — vim cursor semantics are "grid cell" but for MVP
// we treat one rune as one cell (fine for YAML which is ASCII in
// practice, mildly off for the rare non-BMP glyph in a comment).
func (m YamlPopupModel) lineRunes(line int) []rune {
	if line < 0 || line >= len(m.contentPlain) {
		return nil
	}
	return []rune(m.contentPlain[line])
}

// lastColOf returns the last valid cursorCol for the given line (0
// for empty lines so cursor still lands somewhere).
func (m YamlPopupModel) lastColOf(line int) int {
	rr := m.lineRunes(line)
	if len(rr) == 0 {
		return 0
	}
	return len(rr) - 1
}

func (m *YamlPopupModel) clampCol() {
	if m.cursorCol < 0 {
		m.cursorCol = 0
		return
	}
	if last := m.lastColOf(m.cursorLine); m.cursorCol > last {
		m.cursorCol = last
	}
}

func (m *YamlPopupModel) clampCursor() {
	if m.cursorLine < 0 {
		m.cursorLine = 0
	}
	if last := m.lastLine(); m.cursorLine > last {
		m.cursorLine = last
	}
	m.clampCol()
}

func (m YamlPopupModel) moveCursorLeft() YamlPopupModel {
	switch {
	case m.cursorCol > 0:
		m.cursorCol--
	case m.cursorLine > 0:
		// Wrap to the end of the previous line so the cursor can
		// walk backwards through wrapped continuation rows and
		// short lines without dead-ending at col 0. At (0, 0) the
		// switch falls through and the cursor stays put — no crash
		// on the buffer-top corner.
		m.cursorLine--
		m.cursorCol = m.lastColOf(m.cursorLine)
	}
	m.ensureCursorVisible()
	return m
}

func (m YamlPopupModel) moveCursorRight() YamlPopupModel {
	switch last := m.lastColOf(m.cursorLine); {
	case m.cursorCol < last:
		m.cursorCol++
	case m.cursorLine < m.lastLine():
		// Symmetric wrap to the start of the next line — matches
		// the left-wrap behavior above so `l` at end-of-line and
		// `h` at start-of-line both cross the boundary.
		m.cursorLine++
		m.cursorCol = 0
	}
	m.ensureCursorVisible()
	return m
}

func (m YamlPopupModel) moveCursorDown() YamlPopupModel {
	if m.cursorLine < m.lastLine() {
		m.cursorLine++
		m.clampCol()
	}
	m.ensureCursorVisible()
	return m
}

func (m YamlPopupModel) moveCursorUp() YamlPopupModel {
	if m.cursorLine > 0 {
		m.cursorLine--
		m.clampCol()
	}
	m.ensureCursorVisible()
	return m
}

// isWordRune classifies a rune as part of a word for w/b/e motions.
// Vim's default `iskeyword` treats alphanumeric + underscore as word
// chars; we follow suit — matches YAML key names ([A-Za-z0-9_-]).
// Hyphen is included because it's common in K8s field names.
func isWordRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z',
		r >= 'A' && r <= 'Z',
		r >= '0' && r <= '9',
		r == '_', r == '-':
		return true
	}
	return false
}

// moveWordForward implements vim's `w` — jump to the start of the
// next word. Skips whitespace and non-word runs; wraps to the next
// line when at the end of the current one. Stops at buffer end.
func (m YamlPopupModel) moveWordForward() YamlPopupModel {
	line, col := m.cursorLine, m.cursorCol
	for {
		rr := m.lineRunes(line)
		if col >= len(rr) {
			if line >= m.lastLine() {
				break
			}
			line++
			col = 0
			continue
		}
		// Skip current word first (find end of current word class).
		if col < len(rr) && isWordRune(rr[col]) {
			for col < len(rr) && isWordRune(rr[col]) {
				col++
			}
		} else if col < len(rr) && !isSpace(rr[col]) {
			for col < len(rr) && !isWordRune(rr[col]) && !isSpace(rr[col]) {
				col++
			}
		}
		// Skip spaces to find start of next word.
		for col < len(rr) && isSpace(rr[col]) {
			col++
		}
		if col < len(rr) {
			break
		}
	}
	m.cursorLine, m.cursorCol = line, col
	m.clampCursor()
	m.ensureCursorVisible()
	return m
}

// moveWordBackward implements vim's `b` — jump to the start of the
// current or previous word.
func (m YamlPopupModel) moveWordBackward() YamlPopupModel {
	line, col := m.cursorLine, m.cursorCol
	for {
		if col == 0 {
			if line == 0 {
				break
			}
			line--
			col = len(m.lineRunes(line))
			continue
		}
		rr := m.lineRunes(line)
		col--
		// Skip trailing spaces.
		for col > 0 && isSpace(rr[col]) {
			col--
		}
		// Walk back through the current word class to its start.
		if col > 0 && isWordRune(rr[col]) {
			for col > 0 && isWordRune(rr[col-1]) {
				col--
			}
		} else if col > 0 && !isSpace(rr[col]) {
			for col > 0 && !isWordRune(rr[col-1]) && !isSpace(rr[col-1]) {
				col--
			}
		}
		break
	}
	m.cursorLine, m.cursorCol = line, col
	m.clampCursor()
	m.ensureCursorVisible()
	return m
}

// moveWordEnd implements vim's `e` — jump to the end of the current
// or next word.
func (m YamlPopupModel) moveWordEnd() YamlPopupModel {
	line, col := m.cursorLine, m.cursorCol
	// Start by advancing one rune so we don't stick at the same
	// end-of-word position when already there.
	rr := m.lineRunes(line)
	if col < len(rr) {
		col++
	}
	for {
		rr = m.lineRunes(line)
		if col >= len(rr) {
			if line >= m.lastLine() {
				col = len(rr)
				if col > 0 {
					col--
				}
				break
			}
			line++
			col = 0
			continue
		}
		// Skip spaces.
		for col < len(rr) && isSpace(rr[col]) {
			col++
		}
		if col >= len(rr) {
			continue
		}
		// Walk through the current word class to its final rune.
		if isWordRune(rr[col]) {
			for col+1 < len(rr) && isWordRune(rr[col+1]) {
				col++
			}
		} else {
			for col+1 < len(rr) && !isWordRune(rr[col+1]) && !isSpace(rr[col+1]) {
				col++
			}
		}
		break
	}
	m.cursorLine, m.cursorCol = line, col
	m.clampCursor()
	m.ensureCursorVisible()
	return m
}

func isSpace(r rune) bool { return r == ' ' || r == '\t' }

// ensureCursorVisible adjusts scrollOffset so the cursor sits inside
// the visible viewport. Called after every cursor mutation.
func (m *YamlPopupModel) ensureCursorVisible() {
	h := m.contentHeight()
	if h <= 0 {
		return
	}
	if m.cursorLine < m.scrollOffset {
		m.scrollOffset = m.cursorLine
	} else if m.cursorLine >= m.scrollOffset+h {
		m.scrollOffset = m.cursorLine - h + 1
	}
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
	if max := m.maxScrollOffset(); m.scrollOffset > max {
		m.scrollOffset = max
	}
}

// landCursorOnMatch snaps the cursor onto the leading line of the
// current search match after n/N. Keeps cursor + search focus in
// lock-step so a subsequent v-then-y captures the match line.
func (m *YamlPopupModel) landCursorOnMatch() {
	if len(m.matchLines) == 0 {
		return
	}
	rawTarget := m.matchLines[m.matchCursor]
	target := m.firstDisplayLineForRaw(rawTarget)
	if target < 0 {
		return
	}
	m.cursorLine = target
	m.cursorCol = 0
	m.clampCol()
}

// selectionRange normalises (anchor, cursor) into (startLine, startCol,
// endLine, endCol) in forward reading order, so callers don't have to
// think about which end was set first. Inclusive on both ends.
func (m YamlPopupModel) selectionRange() (sL, sC, eL, eC int) {
	sL, sC = m.visualAnchorLine, m.visualAnchorCol
	eL, eC = m.cursorLine, m.cursorCol
	if sL > eL || (sL == eL && sC > eC) {
		sL, sC, eL, eC = eL, eC, sL, sC
	}
	return
}

// displayToRaw maps a display (line, col) — a cursor/anchor position over
// the wrapped view — back to the source (raw line, raw col). rawCol is the
// rune index into rawLines[rawLine]. Out-of-range inputs are clamped so
// callers get a valid raw coordinate.
func (m YamlPopupModel) displayToRaw(dl, dc int) (rawLine, rawCol int) {
	if dl < 0 {
		dl = 0
	}
	if dl >= len(m.contentLineRaw) {
		dl = len(m.contentLineRaw) - 1
	}
	if dl < 0 {
		return 0, 0
	}
	rawLine = m.contentLineRaw[dl]
	if dc < 0 {
		dc = 0
	}
	return rawLine, m.contentChunkStart[dl] + dc
}

// selectionText extracts the character-wise selection and reconstructs it
// from rawLines — the ORIGINAL YAML — not from the wrapped display chunks.
// This is why: a long raw line is soft-wrapped into several display lines
// for rendering, but those wrap points are not real newlines, and wrapPlain
// also drops the space at word-wrap breaks. Joining display chunks would
// therefore inject newlines the source never had and lose wrap-boundary
// spaces. Mapping the selection endpoints back to raw (line, col) via
// displayToRaw and slicing rawLines yields exactly what the user sees,
// byte-for-byte with the source. Empty when no lines exist.
func (m YamlPopupModel) selectionText() string {
	if len(m.contentPlain) == 0 {
		return ""
	}
	sL, sC, eL, eC := m.selectionRange()
	rawS, startCol := m.displayToRaw(sL, sC)
	rawE, endCol := m.displayToRaw(eL, eC)

	clampEnd := func(rawIdx, col int) int {
		rr := []rune(m.rawLines[rawIdx])
		if col >= len(rr) {
			col = len(rr) - 1
		}
		return col
	}

	if rawS == rawE {
		rr := []rune(m.rawLines[rawS])
		if len(rr) == 0 {
			return ""
		}
		endCol = clampEnd(rawE, endCol)
		if startCol > endCol {
			return ""
		}
		return string(rr[startCol : endCol+1])
	}

	var b strings.Builder
	// First raw line: from startCol to end.
	first := []rune(m.rawLines[rawS])
	if startCol < len(first) {
		b.WriteString(string(first[startCol:]))
	}
	b.WriteByte('\n')
	// Middle raw lines: whole line (real newlines between them).
	for i := rawS + 1; i < rawE; i++ {
		b.WriteString(m.rawLines[i])
		b.WriteByte('\n')
	}
	// Last raw line: from 0 to endCol.
	last := []rune(m.rawLines[rawE])
	if len(last) > 0 {
		endCol = clampEnd(rawE, endCol)
		b.WriteString(string(last[0 : endCol+1]))
	}
	return b.String()
}

// selectionCoversLine reports whether display line `i` overlaps the
// current visual-mode selection. Used at render time to decide which
// rows carry the selection-bg highlight.
func (m YamlPopupModel) selectionCoversLine(i int) bool {
	if !m.visualMode {
		return false
	}
	sL, _, eL, _ := m.selectionRange()
	return i >= sL && i <= eL
}

// --- search handler --------------------------------------------------------

func (m YamlPopupModel) handleSearchKey(msg tea.KeyMsg) (YamlPopupModel, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyEscape:
		m.searching = false
		m.searchQuery = ""
		m.matchLines = nil
		m.matchCursor = 0
		return m, nil
	case msg.Type == tea.KeyEnter:
		m.searching = false
		m.computeMatches()
		m.scrollToMatch()
		return m, nil
	case msg.Type == tea.KeyBackspace:
		if len(m.searchQuery) > 0 {
			m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
		}
		return m, nil
	case msg.Type == tea.KeyRunes:
		for _, r := range msg.Runes {
			m.searchQuery += string(r)
		}
		return m, nil
	}
	return m, nil
}

func (m *YamlPopupModel) computeMatches() {
	m.matchLines = nil
	m.matchCursor = 0
	if m.searchQuery == "" {
		return
	}
	q := strings.ToLower(m.searchQuery)
	for i, l := range m.rawLines {
		if strings.Contains(strings.ToLower(l), q) {
			m.matchLines = append(m.matchLines, i)
		}
	}
}

func (m *YamlPopupModel) scrollToMatch() {
	if len(m.matchLines) == 0 {
		return
	}
	rawTarget := m.matchLines[m.matchCursor]
	target := m.firstDisplayLineForRaw(rawTarget)
	if target < 0 {
		return
	}
	contentH := m.contentHeight()
	if target < m.scrollOffset || target >= m.scrollOffset+contentH {
		m.scrollOffset = target - contentH/2
		if m.scrollOffset < 0 {
			m.scrollOffset = 0
		}
		if m.scrollOffset > m.maxScrollOffset() {
			m.scrollOffset = m.maxScrollOffset()
		}
	}
}

// Popup sizing constants — absolute cells, no percentage math. Both popups
// (YAML + Help) leave one row/col of breathing room between their outer
// border and the terminal edge. overlay.Composite centers the popup, so a
// popup outer width of (m.width - 2*popupHMargin) ends up with exactly
// popupHMargin cells of empty space on each side.
const (
	popupHMargin = 1
	popupVMargin = 1
)

func (m YamlPopupModel) popupWidth() int {
	if m.width <= 0 {
		return 60
	}
	w := m.width - 2*popupHMargin
	if w < 40 {
		w = 40
	}
	return w
}

func (m YamlPopupModel) popupHeight() int {
	if m.height <= 0 {
		return 20
	}
	h := m.height - 2*popupVMargin
	if h < 10 {
		h = 10
	}
	return h
}

// contentHeight is how many YAML lines fit in the body, accounting for the
// optional search box at the top. Just borders (2) — no inner padding rows,
// per UX feedback the previous 1-row blank top + 1-row blank bottom was
// wasted space.
func (m YamlPopupModel) contentHeight() int {
	h := m.popupHeight() - 2 // top + bottom border
	if m.searching || m.searchQuery != "" {
		h -= 3 // search box is 3 lines
	}
	if h < 1 {
		h = 1
	}
	return h
}

// MatchCount returns the number of search matches (for tests + indicator).
func (m YamlPopupModel) MatchCount() int { return len(m.matchLines) }

// MatchCursor returns the current match index (for tests).
func (m YamlPopupModel) MatchCursor() int { return m.matchCursor }

// ScrollOffset returns the current scroll position (for tests).
func (m YamlPopupModel) ScrollOffset() int { return m.scrollOffset }

// SearchQuery returns the current search query (for tests).
func (m YamlPopupModel) SearchQuery() string { return m.searchQuery }

// IsSearching reports whether the popup is in search-input mode (for tests).
func (m YamlPopupModel) IsSearching() bool { return m.searching }

func (m YamlPopupModel) maxScrollOffset() int {
	n := len(m.contentLines) - m.contentHeight()
	if n < 0 {
		return 0
	}
	return n
}

// HandleMouse routes a click against the YAML viewer. Wheel scroll
// is already translated to u/d at the AppModel layer; this method
// only handles discrete buttons. Right-click inside the popup
// closes it (mirror of Esc). Left-click is no-op — YAML has
// no row cursor.
func (m YamlPopupModel) HandleMouse(msg tea.MouseMsg, screenW, screenH int) (YamlPopupModel, tea.Cmd) {
	if !m.animator.IsInteractive() || msg.Action != tea.MouseActionPress {
		return m, nil
	}
	if !popupContains(m.renderFullPopup(), msg, screenW, screenH) {
		return m, nil
	}
	if msg.Button == tea.MouseButtonRight {
		return m, m.animator.Close()
	}
	return m, nil
}

// View is a no-op; rendering happens via RenderPopup + overlay composition.
func (m YamlPopupModel) View() string { return "" }

// RenderPopup returns the popup frame (respecting animation state).
func (m YamlPopupModel) RenderPopup() string {
	return m.animator.RenderFrame(m.renderFullPopup())
}

func (m YamlPopupModel) renderFullPopup() string {
	bc := m.borderColor
	bStyle := lipgloss.NewStyle().Foreground(bc)
	tStyle := lipgloss.NewStyle().Foreground(bc).Bold(true)
	// matchRowStyle highlights the line under the search cursor with the same
	// background treatment as panel-view selected rows, so users get a
	// consistent "this is what you're on" cue across the app.
	matchRowStyle := m.theme.TableSelectedRowStyle()

	boxW := m.popupWidth()
	panelH := m.popupHeight()
	innerW := boxW - 2
	contentH := m.contentHeight()

	title := "  YAML — " + m.resource.KubectlName() + "/" + m.item.Name
	if m.item.Namespace != "" {
		title += " (" + m.item.Namespace + ")"
	}
	truncated := false
	if lipgloss.Width(title) > innerW-1 {
		// Reserve 1 cell for "…" — mirrors the comparepopup fix so
		// narrow-terminal title cuts read as cut, not as a literal
		// trailing fragment.
		title = ansiTruncate(title, innerW-2)
		truncated = true
	}
	titleVisualW := lipgloss.Width(title)
	if truncated {
		titleVisualW++ // the "…" we'll append
	}
	dashesAfter := innerW - 1 - titleVisualW
	if dashesAfter < 0 {
		dashesAfter = 0
	}

	var b strings.Builder
	b.WriteString(bStyle.Render("╭─"))
	b.WriteString(tStyle.Render(title))
	if truncated {
		// Append "…" via a separate Render call so the ellipsis carries
		// titleStyle — ansiTruncate's trailing \x1b[0m otherwise drops
		// it to the default fg color.
		b.WriteString(tStyle.Render("…"))
	}
	b.WriteString(bStyle.Render(strings.Repeat("─", dashesAfter) + "╮"))
	b.WriteString("\n")

	leftBorder := bStyle.Render("│")
	rightBorder := bStyle.Render("│")

	var lines []string
	if m.searching || m.searchQuery != "" {
		// renderSearchBox auto-picks amber when !active && query != "" (locked
		// filter), so the call site doesn't need to branch on the two states.
		lines = append(lines, strings.Split(renderSearchBox(m.searchQuery, m.searching, innerW, m.theme), "\n")...)
	}

	// Content slice.
	start := m.scrollOffset
	if start > len(m.contentLines) {
		start = len(m.contentLines)
	}
	end := start + contentH
	if end > len(m.contentLines) {
		end = len(m.contentLines)
	}

	// Current match is identified by its RAW line index; with wrapping a
	// single raw match may span multiple consecutive display lines, so we
	// highlight every display row whose source raw line is the match.
	currentMatchRaw := -1
	if !m.searching && len(m.matchLines) > 0 {
		currentMatchRaw = m.matchLines[m.matchCursor]
	}

	// Visual-mode + cursor overlays share the ANSI-strip approach that
	// search match highlight uses — once selection or cursor lands on a
	// line we drop syntax colouring in favour of the clearer selection
	// bg + cursor cell. Kept as a per-line branch so untouched lines
	// still render the syntax-highlighted version (the vast majority
	// of the buffer for typical selections).
	//
	// Colours:
	//   - Cursor uses lipgloss inverse-video so it reads as a caret
	//     against any bg.
	//   - Visual-mode SELECTION uses lavender (#b4befe) per kbu's
	//     color mindset: lavender = user-state (the user is actively
	//     picking a region), same accent as sidebar Pinned / statusbar
	//     [C]ontext / [N]amespace values, so "you selected this"
	//     reads identically across the app. Distinct from the
	//     search-match highlight (TableSelectedRow, subtext1) so
	//     the two overlays don't visually collide when a selection
	//     spans a match row.
	cursorStyle := lipgloss.NewStyle().Reverse(true)
	selectionStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(theme.Lavender)).
		Foreground(lipgloss.Color("#1e1e2e")). // Catppuccin Mocha base — high contrast on lavender bg
		Bold(true)
	sL, sC, eL, eC := 0, 0, 0, 0
	if m.visualMode {
		sL, sC, eL, eC = m.selectionRange()
	}

	contentW := innerW - m.gutterWidth()
	for i := start; i < end; i++ {
		isMatch := currentMatchRaw >= 0 && i < len(m.contentLineRaw) && m.contentLineRaw[i] == currentMatchRaw
		selected := m.selectionCoversLine(i)
		hasCursor := i == m.cursorLine
		var content string
		switch {
		case selected:
			// Visual-mode selection: keep syntax coloring OUTSIDE the
			// selection range, overlay the selection bg + cursor cell
			// only where they belong. Compute per-line selection
			// endpoints from the normalised anchor/cursor range.
			var lineStart, lineEnd int
			switch {
			case sL == eL:
				lineStart, lineEnd = sC, eC
			case i == sL:
				lineStart, lineEnd = sC, len([]rune(m.contentPlain[i]))-1
			case i == eL:
				lineStart, lineEnd = 0, eC
			default:
				lineStart, lineEnd = 0, len([]rune(m.contentPlain[i]))-1
			}
			content = overlaySelectionOnStyledLine(m.contentLines[i], m.contentPlain[i], lineStart, lineEnd, hasCursor, m.cursorCol, selectionStyle, cursorStyle)
			if lipgloss.Width(content) > contentW {
				content = ansiTruncate(content, contentW)
			}
		case hasCursor && !isMatch:
			// Cursor-only line (no selection, no search match): splice
			// the cursor cell into the styled line so surrounding
			// syntax colours survive.
			content = overlayCursorOnStyledLine(m.contentLines[i], m.contentPlain[i], m.cursorCol, cursorStyle)
			if lipgloss.Width(content) > contentW {
				content = ansiTruncate(content, contentW)
			}
		case isMatch:
			// Search-match line (no selection): full-row match-bg
			// highlight per the pre-visual behaviour. Strips syntax
			// colouring in favour of the "current match" cue.
			plain := m.contentPlain[i]
			if i >= len(m.contentPlain) {
				plain = ansi.Strip(m.contentLines[i])
			}
			if lipgloss.Width(plain) > contentW {
				plain = ansiTruncate(plain, contentW)
			}
			content = matchRowStyle.Width(contentW).Render(plain)
		default:
			content = m.contentLines[i]
			if lipgloss.Width(content) > contentW {
				content = ansiTruncate(content, contentW)
			}
		}
		lines = append(lines, m.gutter(i)+content)
	}
	if len(m.contentLines) == 0 {
		dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#7f849c"))
		lines = append(lines, dim.Render("  (no YAML — resource may still be loading)"))
	}

	for len(lines) < panelH-2 {
		lines = append(lines, "")
	}
	lines = lines[:panelH-2]

	for _, l := range lines {
		lw := lipgloss.Width(l)
		if lw > innerW {
			l = ansiTruncate(l, innerW)
			lw = lipgloss.Width(l)
		}
		pad := ""
		if lw < innerW {
			pad = strings.Repeat(" ", innerW-lw)
		}
		if l == "" {
			b.WriteString(leftBorder + strings.Repeat(" ", innerW) + rightBorder)
		} else {
			b.WriteString(leftBorder + l + pad + rightBorder)
		}
		b.WriteString("\n")
	}

	hint, indicator := m.bottomBarStrings(contentH, innerW-1)
	bottomDashes := innerW - lipgloss.Width(hint) - lipgloss.Width(indicator) - 1
	if bottomDashes < 0 {
		bottomDashes = 0
	}
	b.WriteString(bStyle.Render("╰─"))
	b.WriteString(tStyle.Render(hint))
	b.WriteString(bStyle.Render(strings.Repeat("─", bottomDashes) + indicator + "╯"))

	return b.String()
}

// overlaySelectionOnStyledLine keeps the syntax-highlighted styled
// line intact OUTSIDE the selection range and overlays the selection
// bg only where it belongs. Same principle as overlayCursorOnStyledLine
// but for a range, and with an optional cursor cell nested inside
// the selection (visual-mode cursor always sits at one endpoint).
//
// selStart / selEnd are inclusive rune indices into plain. If the
// caller provides an empty plain string (blank line inside a
// selection), a single selection-styled space is rendered so the
// user still sees "this line is selected".
func overlaySelectionOnStyledLine(styled, plain string, selStart, selEnd int, hasCursor bool, cursorCol int, selectionStyle, cursorStyle lipgloss.Style) string {
	plainRunes := []rune(plain)
	if len(plainRunes) == 0 {
		if hasCursor {
			return cursorStyle.Render(" ")
		}
		return selectionStyle.Render(" ")
	}
	if selStart < 0 {
		selStart = 0
	}
	if selEnd >= len(plainRunes) {
		selEnd = len(plainRunes) - 1
	}
	const largeRight = 1_000_000
	before := ansi.Cut(styled, 0, selStart)
	after := ansi.Cut(styled, selEnd+1, largeRight)

	var block strings.Builder
	for i := selStart; i <= selEnd; i++ {
		cell := string(plainRunes[i])
		if hasCursor && i == cursorCol {
			block.WriteString(cursorStyle.Render(cell))
		} else {
			block.WriteString(selectionStyle.Render(cell))
		}
	}
	return before + block.String() + after
}

// overlayCursorOnStyledLine returns the syntax-highlighted styled
// line with only the cell at cursorCol flipped to reverse video. The
// rest of the line keeps its highlightYAMLLine coloring (dim keys,
// muted comments, list dashes, etc.). Uses ansi.Cut for the prefix
// and suffix splits so ANSI style spans that straddle the cursor
// column don't break — Cut is grapheme + escape aware.
//
// plain is the ANSI-stripped mirror of styled, needed because the
// character under the cursor has to come from the visible-cell view
// (styled contains ANSI bytes that would confuse a []rune index).
// cursorCol out of range for the line renders the cursor as a single
// reverse-video space at the end of the line — matches vim behavior
// on empty lines / past-EOL positions.
func overlayCursorOnStyledLine(styled, plain string, cursorCol int, cursorStyle lipgloss.Style) string {
	plainRunes := []rune(plain)
	if len(plainRunes) == 0 {
		return cursorStyle.Render(" ")
	}
	if cursorCol < 0 {
		cursorCol = 0
	}
	if cursorCol >= len(plainRunes) {
		cursorCol = len(plainRunes) - 1
	}
	cell := string(plainRunes[cursorCol])
	// Cap far past normal line lengths — ansi.Cut needs a right-
	// bound; MaxInt would work but is easy to misread. 1e6 is plenty
	// for any real YAML line and keeps the intent obvious.
	const largeRight = 1_000_000
	before := ansi.Cut(styled, 0, cursorCol)
	after := ansi.Cut(styled, cursorCol+1, largeRight)
	return before + cursorStyle.Render(cell) + after
}

// bottomBarStrings produces the bottom-border hint + indicator pair that fits
// in `available` columns. Vim conventions (j/k u/d gg/G n/N) are intentionally
// omitted — they apply everywhere in kbu and don't need to live in the local
// hint. Falls back to a short hint and finally drops the indicator if the
// popup width is too tight.
func (m YamlPopupModel) bottomBarStrings(contentH, available int) (hint, indicator string) {
	// v enters visual mode (character-wise); y copies selection when
	// visual is active, else the full YAML. hjkl/w/b/e/0/$ move the
	// cursor. Full hint spells it out; short falls back to letter
	// tags when the popup is narrow.
	const hintFull = " v:visual  y:copy  E:edit  /:search  Esc:close "
	const hintShort = " v  y  E  /  Esc "
	hint = hintFull

	total := len(m.contentLines)
	if total > 0 {
		shownEnd := m.scrollOffset + contentH
		if shownEnd > total {
			shownEnd = total
		}
		indicator = fmt.Sprintf(" %d-%d/%d ", m.scrollOffset+1, shownEnd, total)
		if len(m.matchLines) > 0 && !m.searching {
			indicator = fmt.Sprintf(" %d/%d %s", m.matchCursor+1, len(m.matchLines), strings.TrimLeft(indicator, " "))
		}
	}

	fits := func() bool {
		return lipgloss.Width(hint)+lipgloss.Width(indicator) <= available
	}
	if fits() {
		return
	}
	hint = hintShort
	if fits() {
		return
	}
	// Drop the verbose part of the indicator (match counter) but keep range.
	if total > 0 {
		shownEnd := m.scrollOffset + contentH
		if shownEnd > total {
			shownEnd = total
		}
		indicator = fmt.Sprintf(" %d-%d/%d ", m.scrollOffset+1, shownEnd, total)
	}
	if fits() {
		return
	}
	indicator = ""
	return
}
