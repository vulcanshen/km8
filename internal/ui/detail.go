package ui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/vulcanshen/kbu/internal/k8s"
	"github.com/vulcanshen/kbu/internal/theme"
)

// logLine holds an unrendered log entry. Wrapping is deferred to render time
// so log output reflows correctly when the panel is resized.
//
// pod is empty for single-pod log streams (the user is on a Pod detail view —
// the pod identity is implicit). For aggregate streams (Deployment / workload
// kinds), pod carries the source pod name so the render path can emit a
// three-segment `<pod-hash>│<container>│<text>` prefix.
type logLine struct {
	pod       string
	container string
	text      string
}

// DetailTab identifies which tab is active in the detail panel.
type DetailTab int

const (
	DetailTabInfo DetailTab = iota
	DetailTabEvents
	DetailTabLogs
)

// DetailModel is the Bubble Tea model for the detail panel.
type DetailModel struct {
	activeTab    DetailTab
	tabs         []string
	detail       k8s.ResourceDetail
	events       []k8s.EventItem
	scrollOffset int
	contentLines []string // pre-rendered content lines for current tab
	focused      bool
	width        int
	height       int
	theme        *theme.Theme
	hasData      bool
	pendingG     bool
	logLines     []logLine
	maxLogLines  int
	resourceType k8s.ResourceType
	followTail   bool // Logs tab: stick to bottom on new lines until user scrolls up

	// followEventsTail mirrors followTail for the Events tab. K8s events
	// arrive via SetDetail (not a per-line stream like logs), so the
	// sticky-bottom snap fires inside SetDetail rather than on each
	// append. Same u/scrollUp-pauses / G-resumes contract as Logs.
	// Same live/paused glyph in the tab title. Rationale: Aggregate
	// Events for workload kinds funnels many streams into one view; a
	// long-lived observer wants "always show me the latest" by default
	// with a pause escape hatch when they need to freeze a snapshot.
	followEventsTail bool

	// Relatives tab state: entries are the logical rows (drillable + info +
	// section headers); relativeCursor is the index of the currently-selected
	// entry within the *current level*. Cursor only lands on selectable
	// entries (sections skipped).
	relativeEntries    []relativeEntry
	relativeCursor     int
	relativeCursorLine int // display-line index of cursor row; -1 when none

	// History tab state (Helm releases only). historyCursor indexes into
	// detail.ReleaseHistory. -1 means "no cursor" — set when there's no
	// history loaded yet.
	historyCursor int

	// drillStack is the chain of resources the user has drilled into via
	// the Relatives tab (level 2+). Empty = at level 1 (root = the
	// table-selected resource, whose data is m.detail). When non-empty,
	// rebuildRelativeEntries reads from drillStack[top].detail instead of
	// m.detail. rootCursor preserves m.relativeCursor at level 1 so it can be
	// restored when popping back to root.
	drillStack []drillFrame
	rootCursor int
}

// drillFrame represents one level on the Relatives-tab drill chain. ref is the
// (kind, ns, name) identity used for cycle detection; item carries the
// fetched resource (UID + Raw for YAML); detail is the per-level link
// payload; cursor is the link cursor remembered for this level when the
// user drilled deeper, so back-navigation puts the cursor right where it
// was.
type drillFrame struct {
	ref    k8s.RefTarget
	item   k8s.ResourceItem
	detail k8s.ResourceDetail
	cursor int
}

// IsSearching is kept as a no-op for cross-package API symmetry with
// sidebar / table. Panel 3 has no search by design — cursor-driven tabs
// (Relatives / History) don't tolerate row filtering, and the line-based
// tabs (Logs / Events) read better as plain scrollable views.
func (m DetailModel) IsSearching() bool { return false }

// ClearSearch is a no-op for the same reason as IsSearching — kept so
// AppModel's clearSearchOnLeave() can call it without a type switch.
func (m *DetailModel) ClearSearch() {}

// HasActiveFilter is a no-op (same reason as above).
func (m DetailModel) HasActiveFilter() bool { return false }

// CurrentLevelYAML returns the YAML for the Relatives-tab drill level the
// user is currently viewing — at depth 1 that's the table-selected
// resource's YAML, at deeper levels it's the YAML of the resource the
// user has drilled into. Used as the Y-key fallback on the Relatives tab
// when the cursor isn't on a drillable entry.
func (m DetailModel) CurrentLevelYAML() string {
	if len(m.drillStack) == 0 {
		return m.detail.YAML
	}
	return m.drillStack[len(m.drillStack)-1].detail.YAML
}

// YAMLContent returns the raw YAML for the resource currently shown in the
// detail panel, or "" if no YAML is loaded. Used by the global `Y` key to
// open the YAML popup.
func (m DetailModel) YAMLContent() string { return m.detail.YAML }

// NewDetailModel creates a new detail model with no data and the Relatives tab
// active. SetResourceType refines the tab list (and reorders for Pod/Deploy).
func NewDetailModel(t *theme.Theme) DetailModel {
	return DetailModel{
		activeTab:        DetailTabInfo,
		tabs:             []string{"Relatives", "Events"},
		theme:            t,
		maxLogLines:      1000,
		followTail:       true,
		followEventsTail: true,
		relativeCursor:   -1,
	}
}

// FollowTail reports whether the Logs tab auto-scrolls to the bottom on new lines.
func (m DetailModel) FollowTail() bool { return m.followTail }

// FollowEventsTail reports whether the Events tab auto-scrolls to the
// bottom when a fresh events batch arrives via SetDetail.
func (m DetailModel) FollowEventsTail() bool { return m.followEventsTail }

// Init implements tea.Model.
func (m DetailModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m DetailModel) Update(msg tea.Msg) (DetailModel, tea.Cmd) {
	switch msg := msg.(type) {
	case ResourceDetailMsg:
		m.SetDetail(msg.Detail, msg.Events)
		return m, nil

	case tea.KeyMsg:
		if !m.focused {
			return m, nil
		}
		return m.handleKey(msg)

	case tea.MouseMsg:
		if !m.focused {
			return m, nil
		}
		return m.handleMouse(msg)
	}

	return m, nil
}

func (m DetailModel) handleKey(msg tea.KeyMsg) (DetailModel, tea.Cmd) {
	// Relatives tab uses j/k for cursor navigation (not line scroll) and Enter
	// to drill into the highlighted ref. Other tabs scroll by line — fall
	// through to the standard logic.
	if m.ActiveTabName() == "Relatives" {
		if newModel, handled, cmd := m.handleRelativeKey(msg); handled {
			return newModel, cmd
		}
	}

	// History tab (Helm) — j/k moves the revision cursor, Space is left
	// to AppModel (which dispatches the rollback confirm popup) so this
	// handler only owns navigation.
	if m.ActiveTabName() == "History" {
		if newModel, handled, cmd := m.handleHistoryKey(msg); handled {
			return newModel, cmd
		}
	}

	if m.pendingG {
		m.pendingG = false
		if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == 'g' {
			m.scrollOffset = 0
			m = m.disableFollowOnScroll()
			return m, nil
		}
		// Not a second g — fall through to normal handling.
	}

	switch msg.Type {
	case tea.KeyRunes:
		if len(msg.Runes) != 1 {
			return m, nil
		}
		switch msg.Runes[0] {
		case 'j':
			m = m.scrollDown()
		case 'k':
			m = m.scrollUp()
		case 'g':
			m.pendingG = true
		case 'G':
			m = m.scrollToBottom()
		case ']':
			m = m.nextTab()
		case '[':
			m = m.prevTab()
		case 'd':
			half := m.contentHeight() / 2
			if half < 1 {
				half = 1
			}
			m.scrollOffset += half
			if m.scrollOffset > m.maxScrollOffset() {
				m.scrollOffset = m.maxScrollOffset()
			}
		case 'u':
			half := m.contentHeight() / 2
			if half < 1 {
				half = 1
			}
			m.scrollOffset -= half
			if m.scrollOffset < 0 {
				m.scrollOffset = 0
			}
			m = m.disableFollowOnScroll()
		}

	case tea.KeyDown:
		m = m.scrollDown()
	case tea.KeyUp:
		m = m.scrollUp()
	}

	return m, nil
}

// handleRelativeKey intercepts the keys with Relatives-tab-specific semantics:
//   - j/k (or arrow keys): move the cursor between drillable entries,
//     auto-scrolling the viewport so the cursor stays visible.
//   - Enter: drill into the highlighted ref (push a frame onto the Relatives
//     chain — emits RelativePushMsg, AppModel handles cycle check + fetch).
//   - Esc: pop one level off the chain. No-op at root level.
//
// v1.5.x mental model: `l` no longer drills (Enter is the sole drill key);
// `h` no longer pops (Esc owns that); `b` retired (Space opens the breadcrumb
// popup at the AppModel layer). `h`/`l` mean panel-3 tab switch and are
// handled in app.go before this routine ever sees them.
//
// Returns handled=false to let the caller fall back to the generic per-line
// scroll handlers for everything else.
func (m DetailModel) handleRelativeKey(msg tea.KeyMsg) (DetailModel, bool, tea.Cmd) {
	switch msg.Type {
	case tea.KeyRunes:
		if len(msg.Runes) != 1 {
			return m, false, nil
		}
		switch msg.Runes[0] {
		case 'j':
			m = m.relativeMoveOrScroll(+1)
			return m, true, nil
		case 'k':
			m = m.relativeMoveOrScroll(-1)
			return m, true, nil
		}
	case tea.KeyDown:
		m = m.relativeMoveOrScroll(+1)
		return m, true, nil
	case tea.KeyUp:
		m = m.relativeMoveOrScroll(-1)
		return m, true, nil
	case tea.KeyEnter:
		return m.dispatchRelativePush()
	case tea.KeyEscape:
		// Esc only handled here when we're drilled in; at root, fall
		// through so the generic Esc handler (search-clear) can run.
		if m.Depth() > 1 {
			return m.dispatchRelativePop()
		}
	}
	return m, false, nil
}

// dispatchRelativePush emits RelativePushMsg for the cursor-pointed entry. If
// the cursor isn't on a drillable row, it's a no-op (handled=true so the
// caller doesn't double-process the key).
func (m DetailModel) dispatchRelativePush() (DetailModel, bool, tea.Cmd) {
	ref := m.SelectedRelativeRef()
	if ref == nil {
		return m, true, nil
	}
	target := *ref
	return m, true, func() tea.Msg { return RelativePushMsg{Ref: target} }
}

// dispatchRelativePop pops one level off the chain. No-op at root.
func (m DetailModel) dispatchRelativePop() (DetailModel, bool, tea.Cmd) {
	if m.Depth() <= 1 {
		return m, true, nil
	}
	m.PopDrillFrame()
	return m, true, nil
}

// relativeMoveOrScroll moves the cursor to the next/prev selectable entry
// (dir = +1 / -1). When the cursor is already at the boundary and cannot
// advance, falls through to a plain viewport scroll so the user can still
// reveal trailing/leading non-selectable content (section dividers,
// footer rows) that sits past the last/first selectable entry.
// Without this fallback, lists with non-selectable trailing content get
// stuck at "(maxCursorLine - h + 1) of N" and the last few contentLines
// stay invisible.
func (m DetailModel) relativeMoveOrScroll(dir int) DetailModel {
	prev := m.relativeCursor
	m.relativeCursor = nextSelectableCursor(m.relativeEntries, m.relativeCursor, dir)
	m.buildContentLines()
	if m.relativeCursor != prev {
		return m.scrollRelativeCursorIntoView()
	}
	if dir > 0 {
		return m.scrollDown()
	}
	return m.scrollUp()
}

// scrollRelativeCursorIntoView nudges scrollOffset so the cursor row is
// inside the visible viewport. Mirrors the standard "follow cursor" behavior
// of any selectable list — without it, j/k can move the cursor past the
// bottom of the panel and the user has to manually scroll to see it.
func (m DetailModel) scrollRelativeCursorIntoView() DetailModel {
	if m.relativeCursorLine < 0 {
		return m
	}
	h := m.contentHeight()
	if h <= 0 {
		return m
	}
	if m.relativeCursorLine < m.scrollOffset {
		m.scrollOffset = m.relativeCursorLine
	} else if m.relativeCursorLine >= m.scrollOffset+h {
		m.scrollOffset = m.relativeCursorLine - h + 1
	}
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
	return m
}

// SetCursorAtScreenY moves the Relatives cursor to the entry whose
// rendered span contains the given screen-relative Y (counted from
// the panel's top border, 0 = border, 1 = first content row). On
// other tabs (Logs, Events, YAML, Conditions, History) the click
// only changes focus — there's no row cursor to follow.
//
// Returns nil; the cursor move doesn't fire any selection cmd
// because the Relatives tab doesn't have a panel-wide "row
// selected" side-effect — drilling is its own gesture (Enter / Y /
// double-click) that the user takes explicitly.
func (m *DetailModel) SetCursorAtScreenY(screenY int) tea.Cmd {
	if m.ActiveTabName() != "Relatives" {
		return nil
	}
	contentLine := screenY - 1 // skip the panel's top border row
	if contentLine < 0 {
		return nil
	}
	target := m.scrollOffset + contentLine
	idx := entryAtLine(m.relativeEntries, target)
	if idx < 0 {
		return nil
	}
	if !m.relativeEntries[idx].isSelectable() {
		return nil
	}
	if m.relativeCursor == idx {
		return nil
	}
	m.relativeCursor = idx
	// buildContentLines re-renders with the new cursor row
	// highlighted and updates relativeCursorLine for the
	// scroll-into-view path.
	m.buildContentLines()
	return nil
}

func (m DetailModel) handleMouse(msg tea.MouseMsg) (DetailModel, tea.Cmd) {
	switch msg.Type {
	case tea.MouseWheelUp:
		m = m.scrollUp()
	case tea.MouseWheelDown:
		m = m.scrollDown()
	}
	return m, nil
}

func (m DetailModel) scrollDown() DetailModel {
	maxOffset := m.maxScrollOffset()
	if m.scrollOffset < maxOffset {
		m.scrollOffset++
	}
	return m
}

func (m DetailModel) scrollUp() DetailModel {
	if m.scrollOffset > 0 {
		m.scrollOffset--
	}
	return m.disableFollowOnScroll()
}

func (m DetailModel) scrollToBottom() DetailModel {
	m.scrollOffset = m.maxScrollOffset()
	switch m.ActiveTabName() {
	case "Logs":
		m.followTail = true
	case "Events":
		m.followEventsTail = true
	}
	return m
}

// disableFollowOnScroll turns off follow-tail when the user manually
// scrolls up. Fires for both streaming tabs (Logs / Events) — each
// carries its own follow flag. A no-op on any other tab. Named for
// the WHEN not the WHERE: the trigger is "user scrolled up" regardless
// of which streaming tab they're on. Replaces the earlier
// disableFollowOnScroll, which was Logs-only.
func (m DetailModel) disableFollowOnScroll() DetailModel {
	switch m.ActiveTabName() {
	case "Logs":
		m.followTail = false
	case "Events":
		m.followEventsTail = false
	}
	return m
}

func (m DetailModel) maxScrollOffset() int {
	contentHeight := m.contentHeight()
	max := len(m.contentLines) - contentHeight
	if max < 0 {
		return 0
	}
	return max
}

// clampScroll keeps scrollOffset within [0, maxScrollOffset()]. The scroll
// key handlers clamp per-keystroke against the CURRENT geometry, but a
// previously-valid offset goes stale whenever contentHeight changes (a
// resize or a panel-3 zoom grows the body, shrinking maxScrollOffset) or
// the content length changes (wrap reflow, the log ring-buffer trimming
// its front, a watcher refresh). Left unclamped, the render slice starts
// past the last screenful and the body draws blank. Call this after any
// such change; snap-to-bottom (followTail) paths already set the ceiling
// so a follow-up clamp is a harmless no-op.
func (m *DetailModel) clampScroll() {
	if max := m.maxScrollOffset(); m.scrollOffset > max {
		m.scrollOffset = max
	}
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
}

// contentHeight returns the number of lines available for content.
func (m DetailModel) contentHeight() int {
	if m.height < 0 {
		return 0
	}
	return m.height
}

func (m DetailModel) switchToTab(tab DetailTab) DetailModel {
	if m.activeTab != tab {
		m.activeTab = tab
		m.scrollOffset = 0
		m.buildContentLines()
		// Streaming tabs land at the bottom with follow re-enabled — the
		// user's mental model on entering Logs or Events is "show me the
		// latest", not "start from the top of history". Any prior pause
		// state is dropped, matching how Logs behaved pre-Events.
		switch m.ActiveTabName() {
		case "Logs":
			m.followTail = true
			m.scrollOffset = m.maxScrollOffset()
		case "Events":
			m.followEventsTail = true
			m.scrollOffset = m.maxScrollOffset()
		}
	}
	return m
}

func (m DetailModel) nextTab() DetailModel {
	next := (int(m.activeTab) + 1) % len(m.tabs)
	return m.switchToTab(DetailTab(next))
}

func (m DetailModel) prevTab() DetailModel {
	prev := (int(m.activeTab) - 1 + len(m.tabs)) % len(m.tabs)
	return m.switchToTab(DetailTab(prev))
}

// View implements tea.Model.
func (m DetailModel) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}

	contentHeight := m.contentHeight()
	if contentHeight <= 0 {
		return ""
	}

	// Empty state: centered dim placeholder in the tab body, matching
	// Panel 2's empty-state convention. Applies uniformly to the
	// "no resource selected" case and each tab's own empty message.
	if msg := m.activeTabEmptyMessage(); msg != "" {
		dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#7f849c"))
		return lipgloss.Place(m.width, contentHeight, lipgloss.Center, lipgloss.Center, dim.Render(msg))
	}

	displayLines := m.contentLines

	end := m.scrollOffset + contentHeight
	if end > len(displayLines) {
		end = len(displayLines)
	}

	var lines []string
	for i := m.scrollOffset; i < end; i++ {
		lines = append(lines, displayLines[i])
	}
	return strings.Join(lines, "\n")
}

// activeTabEmptyMessage returns the placeholder text for the current tab
// when it has no data to show, or "" when there is content. View() renders
// the returned message centered + dim; the individual buildXxxLines
// functions still produce the same message as a first-line fallback (dead
// code in practice, but keeps the builders self-contained).
func (m DetailModel) activeTabEmptyMessage() string {
	if !m.hasData {
		return "No resource selected"
	}
	switch m.ActiveTabName() {
	case "Logs":
		if !supportsLogs(m.resourceType) {
			return "Logs not available for this resource type"
		}
		if len(m.logLines) == 0 {
			return "Waiting for logs..."
		}
	case "Events":
		if len(m.events) == 0 {
			return "No events"
		}
	case "Conditions":
		if len(m.detail.Conditions) == 0 {
			return "No conditions"
		}
	case "Relatives":
		if len(m.relativeEntries) == 0 {
			return relativesPlaceholderEmpty
		}
	case "History":
		if len(m.detail.ReleaseHistory) == 0 {
			return "No revision history"
		}
	}
	return ""
}

// SetSize sets the dimensions of the detail panel. When the width changes the
// content lines are rebuilt so wrap points reflow to the new width — this is
// what makes panel expand (= / -) feel correct.
func (m *DetailModel) SetSize(width, height int) {
	widthChanged := width != m.width
	heightChanged := height != m.height
	m.width = width
	m.height = height
	if !m.hasData {
		return
	}
	if widthChanged {
		// Wrap points are width-based — reflow so long lines re-break.
		m.buildContentLines()
	}
	// A width change reflows contentLines (length shifts); a height change
	// (e.g. zooming panel 3 to near full-screen) changes contentHeight and
	// thus maxScrollOffset. Either way a scrolled-up offset can end up past
	// the last screenful and render blank — height changes used to be
	// ignored here entirely. Following tabs snap to the fresh bottom;
	// otherwise clamp the offset back into range.
	if widthChanged || heightChanged {
		switch {
		case m.ActiveTabName() == "Logs" && m.followTail:
			m.scrollOffset = m.maxScrollOffset()
		case m.ActiveTabName() == "Events" && m.followEventsTail:
			m.scrollOffset = m.maxScrollOffset()
		default:
			m.clampScroll()
		}
	}
}

// SetFocused sets whether the detail panel is focused. Rebuilds the
// pre-rendered content for any cursor-bearing tab (Relatives, History) so
// the highlighted row picks the focused vs unfocused style immediately —
// without this, the panel would keep its previous highlight color until
// the next data refresh (3s for Helm releases, much longer for everything
// else).
func (m *DetailModel) SetFocused(focused bool) {
	if m.focused == focused {
		return
	}
	m.focused = focused
	// Every tab now reacts to focus — Relatives / History repaint their
	// cursor highlight, Logs / Events / Conditions collapse to the
	// overlay0 dim treatment used by sidebar / table when unfocused so
	// panel 3 stops pulling the eye while focus sits elsewhere.
	m.buildContentLines()
}

// CopyableContent returns the FOCUS content of the panel-3 detail
// view, following the "focus content" mental model driving the global
// `y` key:
//
//   - Cursor-bearing tabs (Relatives, History) copy the CURSOR ROW's
//     raw fields, tab-separated — matches Panel 2 / Sidebar semantics.
//   - Non-cursor tabs (Logs, Events, Conditions, Info) copy the full
//     visible content (ANSI-stripped) as before — with no cursor,
//     "focus" IS the whole tab.
//
// For raw YAML, the user opens the Y popup and copies from there
// (yamlpopup.go's own y).
func (m DetailModel) CopyableContent() string {
	if !m.hasData {
		return ""
	}
	switch m.ActiveTabName() {
	case "Relatives":
		return m.relativeCursorLineText()
	case "History":
		return m.historyCursorLineText()
	}
	lines := m.contentLines
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = strings.TrimRight(ansi.Strip(l), " ")
	}
	return strings.Join(plain, "\n")
}

// relativeCursorLineText returns the currently-highlighted relative
// entry as "label\tvalue". Section headers and unselectable slots
// yield "" so an accidental y on a non-navigable row is a silent
// no-op rather than copying header text.
func (m DetailModel) relativeCursorLineText() string {
	if m.relativeCursor < 0 || m.relativeCursor >= len(m.relativeEntries) {
		return ""
	}
	e := m.relativeEntries[m.relativeCursor]
	if e.section || !e.isSelectable() {
		return ""
	}
	return e.label + "\t" + e.value
}

// historyCursorLineText returns the highlighted release revision as a
// tab-separated line: revision, updated, status, chart, app_version,
// description. Matches ReleaseRevision's field order so `awk` / `cut`
// positions are stable across kmi releases.
func (m DetailModel) historyCursorLineText() string {
	if m.historyCursor < 0 || m.historyCursor >= len(m.detail.ReleaseHistory) {
		return ""
	}
	r := m.detail.ReleaseHistory[m.historyCursor]
	return fmt.Sprintf("%d\t%s\t%s\t%s\t%s\t%s",
		r.Revision, r.Updated, r.Status, r.Chart, r.AppVersion, r.Description)
}

// ScrollInfo returns scroll position for the detail panel. Position is the
// LAST visible line of contentLines (capped at total), so reaching the
// bottom reads "N of N" — matching the table panel's "cursor of total"
// semantics. The earlier "first visible line" form maxed out at
// "(N-h+1) of N" and never reached N, leaving users unsure whether
// they'd actually scrolled to the bottom.
func (m DetailModel) ScrollInfo() *ScrollInfo {
	lines := m.contentLines
	if len(lines) == 0 {
		return nil
	}
	pos := m.scrollOffset + m.contentHeight()
	if pos > len(lines) {
		pos = len(lines)
	}
	if pos < 1 {
		pos = 1
	}
	return &ScrollInfo{Position: pos, Total: len(lines)}
}

// SetDetail updates the detail data and rebuilds content lines.
//
// Does NOT touch the Relatives drill chain — background watcher refreshes
// keep dispatching detail fetches for the still-selected root row, and a
// stale-arriving ResourceDetailMsg would otherwise wipe the user's in-
// flight drill state and snap them back to level 1. The row-change path
// (RowSelectedMsg) calls ResetDrillStack() explicitly before dispatch;
// namespace/context switches go through ClearDetail() which resets too.
func (m *DetailModel) SetDetail(detail k8s.ResourceDetail, events []k8s.EventItem) {
	// Different underlying resource → reset per-tab cursors AND scroll so
	// the auto-land logic in buildContentLines re-fires for the new item
	// and the viewport starts at the top of the new content. Same UID
	// (watcher polling refresh of the same release / pod / ...) preserves
	// BOTH cursor and scroll position — without the scroll guard, Logs
	// viewing at tail / Relatives mid-list / Events scrolled-down would
	// snap back to top every watcher tick (most visible on Logs of an
	// idle pod, where no incoming line arrives to push scroll back down
	// after the reset).
	sameItem := m.detail.UID != "" && m.detail.UID == detail.UID
	if !sameItem {
		m.historyCursor = -1
		m.scrollOffset = 0
	}
	m.detail = detail
	m.events = events
	m.hasData = true
	m.buildContentLines()
	// Sticky-bottom for Events: mirrors AppendLogLine's Logs branch.
	// Events arrive in batches via SetDetail (unlike Logs' per-line
	// stream), so the snap fires here after buildContentLines. When
	// the user has manually scrolled up (followEventsTail=false), the
	// snap is skipped and their pause point is preserved across
	// subsequent watcher ticks — same contract as Logs.
	if m.ActiveTabName() == "Events" && m.followEventsTail {
		m.scrollOffset = m.maxScrollOffset()
	} else {
		// Preserved scroll (same item) can outrun a shortened body after
		// the rebuild — clamp so a watcher refresh never lands on blank.
		m.clampScroll()
	}
}

// PushDrillFrame appends a level to the Relatives drill chain — used after a
// successful drill fetch (l/Enter on a drillable entry). Saves the
// outgoing level's cursor so back-navigation restores it.
func (m *DetailModel) PushDrillFrame(ref k8s.RefTarget, item k8s.ResourceItem, detail k8s.ResourceDetail) {
	if len(m.drillStack) == 0 {
		m.rootCursor = m.relativeCursor
	} else {
		m.drillStack[len(m.drillStack)-1].cursor = m.relativeCursor
	}
	m.drillStack = append(m.drillStack, drillFrame{
		ref:    ref,
		item:   item,
		detail: detail,
		cursor: -1,
	})
	m.relativeCursor = -1
	m.scrollOffset = 0
	m.buildContentLines()
}

// PopDrillFrame removes the top of the drill chain — used by h/Esc on a
// deeper level. No-op at level 1. Restores the relativeCursor to whatever it
// was on the level we're returning to.
func (m *DetailModel) PopDrillFrame() {
	if len(m.drillStack) == 0 {
		return
	}
	m.drillStack = m.drillStack[:len(m.drillStack)-1]
	if len(m.drillStack) == 0 {
		m.relativeCursor = m.rootCursor
	} else {
		m.relativeCursor = m.drillStack[len(m.drillStack)-1].cursor
	}
	m.scrollOffset = 0
	m.buildContentLines()
}

// JumpToDrillLevel pops frames so the chain is `level` deep. level=1
// returns to root; level=Depth() is a no-op. Used by the breadcrumb popup
// when the user picks an ancestor level. Out-of-range levels are ignored.
func (m *DetailModel) JumpToDrillLevel(level int) {
	if level < 1 || level > m.Depth() {
		return
	}
	for m.Depth() > level {
		m.PopDrillFrame()
	}
}

// ResetDrillStack returns the panel to level 1 without changing m.detail
// — used when the user moves to a different table row, which the chain
// no longer applies to.
func (m *DetailModel) ResetDrillStack() {
	if len(m.drillStack) == 0 {
		return
	}
	m.drillStack = nil
	m.relativeCursor = m.rootCursor
	m.scrollOffset = 0
	m.buildContentLines()
}

// TabTitle returns the tab bar string for embedding in the panel border.
// Active tab is bracketed; the active Logs tab carries an inline live (▶) /
// paused (⏸) glyph suffix to surface the follow-tail state. Relatives gets
// a chain-level suffix when drilled. Embed in Panel 3's border title —
// Panel 2 stays clean with just its breadcrumb.
//
// The follow-tail marker is intentionally a glyph, not a color: kbu's color
// vocabulary is reserved for "this row / cell needs your attention"
// (abnormal status, cursor, lock). Painting the Logs label green to mean
// "follow on" would have overloaded color with a fourth meaning ("a state
// flag is set"), so v1.7.x+ uses the Nerd Font MDI glyphs U+F0753 / U+F0754
// for live / paused — no color, just an inline icon. Falls back gracefully
// in non-NF terminals to a tofu box, which still signals "something is here"
// without misreading as a different color category.
func (m DetailModel) TabTitle() string {
	borderHex := m.theme.Detail.BorderColor
	if m.focused {
		borderHex = m.theme.Sidebar.CategoryFg
	}
	bc := lipgloss.Color(borderHex)
	// Two role-specific colours:
	//   - chipFg (base #1e1e2e) is the DARK text colour rendered
	//     inside the active blue chip. Kept as base for legibility
	//     on top of the blue bg.
	//   - tabBg (crust #11111b) is the TAB AREA bg outside the
	//     active chip. Sitting one Catppuccin surface tier below
	//     base gives the tab area a subtly recessed look vs the
	//     panel body, so the active chip reads as raised.
	chipFg := lipgloss.Color("#1e1e2e")
	tabBg := lipgloss.Color("#11111b")

	// Powerline chip chain (starship-inspired). Only the active tab is
	// highlighted (blue chip); non-active tabs sit on tabBg (crust)
	// with thin E0B1 chevrons between them. When the first tab is
	// active, its chip merges with the [N] chip left open by
	// plainTitlePrefix, becoming one continuous blue segment "[N]
	// Label" (avoids double chevron at the boundary). Otherwise [N]
	// closes with its own cap and every tab renders under the standard
	// tabBg/blue rule.
	chipStyle := lipgloss.NewStyle().Foreground(chipFg).Background(bc).Bold(true)
	closeCap := lipgloss.NewStyle().Foreground(bc).Background(tabBg)
	openCap := lipgloss.NewStyle().Foreground(tabBg).Background(bc)
	baseLabel := lipgloss.NewStyle().Foreground(bc).Background(tabBg)
	// Divider (E0B1 thin chevron) between two non-active tabs —
	// focus-aware, both visible on the crust tab bg but one tier
	// apart so the focused panel's chevron reads slightly brighter
	// than the unfocused panel's:
	//   - Focused panel: surface0 (#313244)
	//   - Unfocused panel: base (#1e1e2e) — one Catppuccin surface
	//     tier below focused
	dividerFg := lipgloss.Color("#1e1e2e")
	if m.focused {
		dividerFg = lipgloss.Color("#313244")
	}
	divider := lipgloss.NewStyle().Foreground(dividerFg).Background(tabBg)
	trailingBase := lipgloss.NewStyle().Foreground(tabBg)
	trailingBlue := lipgloss.NewStyle().Foreground(bc)

	activeIdx := int(m.activeTab)
	tabs := m.tabs

	labelOf := func(i int) string {
		label := m.tabLabel(tabs[i])
		// Single space between label text and follow-tail glyph so
		// the play/pause icon breathes against the trailing "s"
		// rather than butting into it. Same convention for Logs
		// and Events for visual consistency.
		switch tabs[i] {
		case "Logs":
			marker := logsPausedGlyph
			if m.followTail {
				marker = logsLiveGlyph
			}
			label = label + " " + marker
		case "Events":
			marker := logsPausedGlyph
			if m.followEventsTail {
				marker = logsLiveGlyph
			}
			label = label + " " + marker
		}
		return label
	}

	if len(tabs) == 0 {
		return trailingBlue.Render("\uE0B4")
	}

	var b strings.Builder
	// prevBlue tracks whether the previous cell sits on chip bg. Starts
	// true because plainTitlePrefix left the [N] chip open.
	prevBlue := true
	for i := 0; i < len(tabs); i++ {
		isBlue := (i == activeIdx)
		if i == 0 {
			if isBlue {
				// First tab active: merge with [N] chip. No boundary cap
				// -- just continue the blue chip with " Label".
				b.WriteString(chipStyle.Render(" " + labelOf(0)))
			} else {
				// First tab inactive: emit the E0B0 close cap to
				// transition from the [N] chip's blue bg to the base
				// bg tab area. Label sits flush against the wedge \u2014
				// the triangle IS the visual separator, no extra
				// leading space needed.
				b.WriteString(closeCap.Render("\ue0b0"))
				b.WriteString(baseLabel.Render(labelOf(0)))
			}
			prevBlue = isBlue
			continue
		}
		switch {
		case prevBlue && !isBlue:
			b.WriteString(closeCap.Render("\uE0B0"))
		case !prevBlue && isBlue:
			b.WriteString(openCap.Render("\uE0B0"))
		case !prevBlue && !isBlue:
			b.WriteString(divider.Render("\uE0B1"))
		}
		// Non-first tabs sit flush against their leading boundary
		// (chevron / cap) regardless of active/inactive state. Both
		// branches render the label without a leading space so tab
		// width stays STABLE across focus / activeTab changes —
		// without this, the chip's leading padding space would make
		// the whole tab bar shift 1 cell every time the active tab
		// swaps and popup / border alignment jitters.
		if isBlue {
			b.WriteString(chipStyle.Render(labelOf(i)))
		} else {
			b.WriteString(baseLabel.Render(labelOf(i)))
		}
		prevBlue = isBlue
	}
	if prevBlue {
		b.WriteString(trailingBlue.Render("\uE0B4"))
	} else {
		b.WriteString(trailingBase.Render("\uE0B4"))
	}
	return b.String()
}

// Nerd Font Material Design Icons: play (live) + pause (paused). Picked
// over the older ▼ marker because the play/pause pair conveys both states
// instead of just "follow on"; picked over a color-only signal because
// color is reserved for attention-grabbing semantics (see TabTitle doc).
const (
	logsLiveGlyph   = "\U000F0753"
	logsPausedGlyph = "\U000F0754"
)

// ActiveTabTitle is kept as a thin wrapper for callers that still expect
// the single-tab-name format. v1.5.1 moved the full tab bar to Panel 3,
// so most callers should use TabTitle() instead.
func (m DetailModel) ActiveTabTitle() string {
	return m.tabLabel(m.ActiveTabName())
}

// tabLabel returns the per-tab label as it should appear in the tab bar,
// including the drill-level suffix for the Relatives tab. The chain glyph
// matches the per-row drill arrow + the breadcrumb middle markers so
// the three surfaces speak the same vocabulary — "you've gone N levels
// down this chain."
func (m DetailModel) tabLabel(name string) string {
	if name == "Relatives" && m.Depth() > 1 {
		return fmt.Sprintf("Relatives %s%d", relativesDrillArrow, m.Depth())
	}
	return name
}

// BorderTopRightHint returns a short string to render at the top-right
// of panel 3's border, or "" when no hint applies. Currently always "".
// Kept as a method so callers don't break if a future hint surfaces.
func (m DetailModel) BorderTopRightHint() string {
	return ""
}

// BorderBottomLeftHint returns a short hotkey hint for the bottom-left of
// panel 3's border, or "" when no hint applies.
//
// Convention: the border hint surfaces TAB-CONTEXTUAL keys only — keys
// whose meaning is specific to the current tab. Core-keys (Tab / Space /
// Esc / Enter / ?) carry app-wide constant semantics and are not
// repeated here; they live in the ? help cheatsheet. Enter and Esc do
// appear below because their behavior on Relatives is contextual (Enter
// drills into the referenced resource; Esc at depth>1 pops one drill
// level — distinct from the app-wide "dismiss popup" default).
//
// Surfaces:
//   - "enter: drill" on Relatives always; "esc: back" composes on top
//     once depth > 1 (there's a chain to walk back up).
//   - "u/d: page  gg: top  G: live" on Logs so users discover the scroll
//     keys at hand. `G` says "live" rather than "bottom" because
//     scrollToBottom on Logs also re-attaches the live tail
//     (followTail flips true) — losing that nuance would mislead.
//   - Same "u/d: page  gg: top  G: live" hint on Events, and for the
//     same rule-satisfying reason: G on Events re-attaches the events
//     watcher tail (followEventsTail flips true), so its behavior is
//     non-default and the border hint is justified.
func (m DetailModel) BorderBottomLeftHint() string {
	switch m.ActiveTabName() {
	case "Relatives":
		if m.Depth() > 1 {
			return "enter: drill  esc: back"
		}
		return "enter: drill"
	case "Logs", "Events":
		return "u/d: page  gg: top  G: live"
	}
	return ""
}

// ClearDetail clears the detail data and tears down the Relatives drill chain.
// Used by namespace/context switches, which invalidate every drilled-into
// resource (different cluster scope, no guarantee the chain still exists).
func (m *DetailModel) ClearDetail() {
	m.detail = k8s.ResourceDetail{}
	m.events = nil
	m.hasData = false
	m.scrollOffset = 0
	m.contentLines = nil
	m.logLines = nil
	m.drillStack = nil
	m.rootCursor = -1
	m.relativeCursor = -1
}

// SetResourceType sets the current resource type and adjusts available tabs.
//
// Tab order convention — Logs goes first for kinds that have it because
// switching rows in panel 2 is most often a "show me what this thing is
// doing right now" gesture, not "show me what it's related to" (Relatives
// is a deliberate drill action that warrants the extra tab-switch). For
// kinds without Logs, Relatives stays first so the space-hotkey jump
// lands on the same tab the user came from (no visual whiplash).
//
//   - Pods / workloads:   Logs → Relatives → Events
//   - Events:             Relatives alone
//   - !relativesApplicable:   Events only (Namespace — Relatives tab dropped)
//   - everything else:    Relatives → Events
//
// Pod gets the structured Owner/Node/SA/Volumes Relatives; other kinds
// use the generic labels + sections fallback so the panel never renders
// empty.
func (m *DetailModel) SetResourceType(rt k8s.ResourceType) {
	m.resourceType = rt
	switch {
	case rt == k8s.ResourcePods,
		rt == k8s.ResourceDeployments,
		rt == k8s.ResourceStatefulSets,
		rt == k8s.ResourceDaemonSets,
		rt == k8s.ResourceJobs,
		rt == k8s.ResourceCronJobs:
		// All workload kinds get a Logs tab: Pods stream single-pod,
		// the rest funnel their managed Pods into one aggregate stream
		// (Deployment via current ReplicaSet; StatefulSet / DaemonSet /
		// Job via selector; CronJob across all currently-retained Jobs).
		// k8s.PodsForWorkload already routes each kind to its resolver.
		m.tabs = []string{"Logs", "Relatives", "Events"}
	case rt == k8s.ResourceEvents:
		m.tabs = []string{"Relatives"}
	case rt == k8s.ResourceReleases:
		// Helm releases aren't K8s objects themselves so kubectl-style
		// events don't apply; History replaces Events as the second tab.
		m.tabs = []string{"Relatives", "History"}
	case rt == k8s.ResourceContexts:
		// Read-only kubeconfig context: no cluster events / relatives.
		// A single Info tab renders the flattened detail fields.
		m.tabs = []string{"Info"}
	case !relativesApplicable(rt):
		m.tabs = []string{"Events"}
	default:
		m.tabs = []string{"Relatives", "Events"}
	}
	// Conditions tab is appended for kinds that carry .status.conditions —
	// diagnostic view answering "why is this Pending / NotReady / Unavailable".
	// Type-driven (not data-driven) so the tab stays put across refreshes; an
	// empty conditions slice renders "No conditions" rather than removing the
	// tab.
	if resourceHasConditions(rt) {
		m.tabs = append(m.tabs, "Conditions")
	}
	m.activeTab = 0
	m.scrollOffset = 0
	m.relativeCursor = -1
	m.historyCursor = -1
	m.buildContentLines()
}

// resourceHasConditions reports whether kind kind exposes .status.conditions
// worth a tab. Matches the set ExtractConditions handles. CRDs and other
// kinds may have conditions but kbu doesn't surface them here.
func resourceHasConditions(rt k8s.ResourceType) bool {
	switch rt {
	case k8s.ResourcePods,
		k8s.ResourceNodes,
		k8s.ResourcePersistentVolumeClaims,
		k8s.ResourceDeployments,
		k8s.ResourceStatefulSets,
		k8s.ResourceDaemonSets,
		k8s.ResourceJobs,
		k8s.ResourceHorizontalPodAutoscalers,
		k8s.ResourceIngresses:
		return true
	}
	return false
}

// NextTab switches to the next tab.
func (m DetailModel) NextTab() DetailModel {
	return m.nextTab()
}

// PrevTab switches to the previous tab.
func (m DetailModel) PrevTab() DetailModel {
	return m.prevTab()
}

// ActiveTabName returns the name of the currently active tab.
func (m DetailModel) ActiveTabName() string {
	if int(m.activeTab) < len(m.tabs) {
		return m.tabs[m.activeTab]
	}
	return "YAML"
}

// SwitchToTabByName sets the active tab by looking up name in the
// current tab list. Returns true when the switch happened and false
// when the name isn't in tabs — callers that want to restore a saved
// tab (session state) can silently ignore the miss so a stale name
// falls back to the current first-tab default. Used at startup by
// NewAppModel to honor state.yaml's recorded Panel 3 tab; runtime
// tab switches go through the arrow / hjkl handlers or Panel 2 row
// changes which set the tab index directly.
func (m *DetailModel) SwitchToTabByName(name string) bool {
	for i, tab := range m.tabs {
		if tab == name {
			*m = m.switchToTab(DetailTab(i))
			return true
		}
	}
	return false
}

// AppendLogLine appends a formatted log line to the log buffer.
// If the buffer exceeds maxLogLines, the oldest lines are trimmed.
// If the Logs tab is active, content lines are rebuilt.
//
// Pass pod = "" for single-pod streams. For aggregate streams (Deployment
// and other workload kinds), pass the source pod name so the render path can
// label each line with its origin.
//
// text is passed through sanitizeLogText so carriage-return progress
// redraws (apt update / npm install / pip install / docker pull) can't
// leak `\r` into the terminal and jump the cursor to column 0 outside
// the panel — see sanitizeLogText's docstring for the semantics.
func (m *DetailModel) AppendLogLine(pod, container, text string) {
	m.logLines = append(m.logLines, logLine{pod: pod, container: container, text: sanitizeLogText(text)})
	if len(m.logLines) > m.maxLogLines {
		m.logLines = m.logLines[len(m.logLines)-m.maxLogLines:]
	}
	if m.ActiveTabName() == "Logs" {
		m.buildContentLines()
		if m.followTail {
			m.scrollOffset = m.maxScrollOffset()
		} else {
			// Paused mid-scroll: the ring buffer may have trimmed lines off
			// the front (or a rebuild changed the wrapped count), so re-clamp
			// rather than leave the offset pointing past the shortened body.
			m.clampScroll()
		}
	}
}

// buildContentLines rebuilds the pre-rendered content lines for the current tab.
func (m *DetailModel) buildContentLines() {
	switch m.ActiveTabName() {
	case "Relatives":
		m.rebuildRelativeEntries()
		lines, _, cursorLine := renderRelativeEntries(m.relativeEntries, m.relativeCursor, m.width, m.theme, relativesPlaceholderEmpty, m.focused)
		m.contentLines = lines
		m.relativeCursorLine = cursorLine
	case "Info":
		m.contentLines = m.buildInfoLines()
	case "Logs":
		m.contentLines = m.buildLogLines()
	case "Events":
		m.contentLines = m.buildEventLines()
	case "Conditions":
		m.contentLines = m.buildConditionsLines()
	case "History":
		// First entry into the History tab with data loaded: land the
		// cursor on the current deployed revision so the user sees an
		// immediate highlight rather than an unmarked table. Subsequent
		// j/k movements are preserved (we only auto-land when cursor=-1).
		if m.historyCursor < 0 && len(m.detail.ReleaseHistory) > 0 {
			for i, r := range m.detail.ReleaseHistory {
				if r.Status == "deployed" {
					m.historyCursor = i
					break
				}
			}
			// Fall back to the last revision if no row reports deployed
			// (mid-rollback states). Better than -1 invisible cursor.
			if m.historyCursor < 0 {
				m.historyCursor = len(m.detail.ReleaseHistory) - 1
			}
		}
		m.contentLines = m.buildHistoryLines()
	}
}

// RootRef returns the RefTarget identifying the root (table-selected)
// resource — used by AppModel's cycle-check on drill push.
func (m DetailModel) RootRef() k8s.RefTarget {
	return k8s.RefTarget{
		Type:      m.resourceType,
		Name:      m.detail.Name,
		Namespace: m.detail.Namespace,
	}
}

// DrillChain returns the full identity chain from root to current top
// — root first, top last. Used by cycle detection and the breadcrumb
// popup. At depth 1 it returns just the root.
func (m DetailModel) DrillChain() []k8s.RefTarget {
	out := make([]k8s.RefTarget, 0, len(m.drillStack)+1)
	out = append(out, m.RootRef())
	for _, f := range m.drillStack {
		out = append(out, f.ref)
	}
	return out
}

// CurrentLevelItem returns the ResourceItem of the level currently
// displayed on the Relatives tab. At root (depth 1) the zero value is
// returned — the caller (AppModel) substitutes the table-selected item.
func (m DetailModel) CurrentLevelItem() k8s.ResourceItem {
	return m.currentLevelItem()
}

// CurrentLevelRef returns the (kind, ns, name) identity of the resource
// the user is currently viewing on the Relatives tab. At root it's the
// table-selected resource (same as RootRef); at deeper levels it's the
// drilled-into resource. Used by the "space — jump to this resource"
// flow so the caller doesn't have to assemble the ref from CurrentLevelKind
// + CurrentLevelItem manually.
func (m DetailModel) CurrentLevelRef() k8s.RefTarget {
	if len(m.drillStack) == 0 {
		return m.RootRef()
	}
	return m.drillStack[len(m.drillStack)-1].ref
}

// Depth returns the current Relatives-tab drill level. Level 1 = root
// (table-selected resource); level 2+ = drilled in. Always >= 1 when the
// panel has data.
func (m DetailModel) Depth() int { return 1 + len(m.drillStack) }

// currentLevelDetail returns the ResourceDetail for the level the user is
// currently viewing on the Relatives tab.
func (m DetailModel) currentLevelDetail() k8s.ResourceDetail {
	if len(m.drillStack) == 0 {
		return m.detail
	}
	return m.drillStack[len(m.drillStack)-1].detail
}

// currentLevelKind returns the ResourceType for the level the user is
// currently viewing on the Relatives tab. At level 1 this is m.resourceType;
// at deeper levels it's the drilled-into resource's kind.
func (m DetailModel) currentLevelKind() k8s.ResourceType {
	if len(m.drillStack) == 0 {
		return m.resourceType
	}
	return m.drillStack[len(m.drillStack)-1].ref.Type
}

// currentLevelItem returns the ResourceItem of the current level — used
// for Y key fallback when the cursor isn't on a drillable entry. Returns
// zero ResourceItem at level 1 (caller falls back to the table-selected
// item from AppModel).
func (m DetailModel) currentLevelItem() k8s.ResourceItem {
	if len(m.drillStack) == 0 {
		return k8s.ResourceItem{}
	}
	return m.drillStack[len(m.drillStack)-1].item
}

// rebuildRelativeEntries refreshes m.relativeEntries based on the current
// resource type + detail data, and re-clamps the cursor to the first
// selectable entry when out of bounds.
func (m *DetailModel) rebuildRelativeEntries() {
	if !m.hasData {
		m.relativeEntries = nil
		m.relativeCursor = -1
		return
	}
	kind := m.currentLevelKind()
	detail := m.currentLevelDetail()
	switch kind {
	case k8s.ResourcePods:
		m.relativeEntries = buildPodRelativeEntries(detail)
	case k8s.ResourceServices:
		m.relativeEntries = buildServiceRelativeEntries(detail)
	default:
		m.relativeEntries = buildGenericRelativeEntries(detail)
	}
	if m.relativeCursor < 0 || m.relativeCursor >= len(m.relativeEntries) ||
		(m.relativeCursor < len(m.relativeEntries) && !m.relativeEntries[m.relativeCursor].isSelectable()) {
		m.relativeCursor = firstSelectableCursor(m.relativeEntries)
	}
}

// SelectedRelativeRef returns the drill ref under the Relatives cursor, or
// nil if the cursor is on an info-only row (or the tab has no entries).
func (m DetailModel) SelectedRelativeRef() *k8s.RefTarget {
	if m.ActiveTabName() != "Relatives" {
		return nil
	}
	if m.relativeCursor < 0 || m.relativeCursor >= len(m.relativeEntries) {
		return nil
	}
	return m.relativeEntries[m.relativeCursor].ref
}

// buildInfoLines renders ResourceDetail.Fields as an aligned key/value list.
// Only the Contexts "Info" tab uses it (the read-only kubeconfig view). Labels
// are right-padded to a common width so values line up; long values are left
// to truncate at the panel edge (zoom the panel with z to see more).
func (m DetailModel) buildInfoLines() []string {
	if !m.hasData || len(m.detail.Fields) == 0 {
		return []string{"  " + m.theme.DetailValueStyle().Render("(no detail)")}
	}
	labelW := 0
	for _, f := range m.detail.Fields {
		if w := lipgloss.Width(f.Label); w > labelW {
			labelW = w
		}
	}
	labelStyle := m.theme.DetailLabelStyle()
	valueStyle := m.theme.DetailValueStyle()
	lines := make([]string, 0, len(m.detail.Fields))
	for _, f := range m.detail.Fields {
		key := labelStyle.Render(fmt.Sprintf("%-*s", labelW+1, f.Label+":"))
		lines = append(lines, "  "+key+" "+valueStyle.Render(f.Value))
	}
	return lines
}

func (m DetailModel) buildEventLines() []string {
	if !m.hasData || len(m.events) == 0 {
		return []string{"  " + m.theme.DetailValueStyle().Render("No events")}
	}

	// Compute column widths.
	typeW := len("TYPE")
	reasonW := len("REASON")
	objectW := len("OBJECT")
	messageW := len("MESSAGE")
	ageW := len("AGE")

	for _, e := range m.events {
		if len(e.Type) > typeW {
			typeW = len(e.Type)
		}
		if len(e.Reason) > reasonW {
			reasonW = len(e.Reason)
		}
		if len(e.Object) > objectW {
			objectW = len(e.Object)
		}
		if len(e.Message) > messageW {
			messageW = len(e.Message)
		}
		if len(e.Age) > ageW {
			ageW = len(e.Age)
		}
	}

	// Cap message width so MESSAGE column wraps within panel.
	maxMsgW := m.width - typeW - reasonW - objectW - ageW - 12 // 4 gaps of 2 + leading 2 + trailing 2
	if maxMsgW < 10 {
		maxMsgW = 10
	}
	if messageW > maxMsgW {
		messageW = maxMsgW
	}

	labelStyle := m.theme.DetailLabelStyle()
	valueStyle := m.theme.DetailValueStyle()
	if !m.focused {
		// Mirror the sidebar / table / history dim treatment so panel 3
		// recedes consistently when focus moves elsewhere.
		dim := m.theme.TableDimRowStyle()
		labelStyle = dim
		valueStyle = dim
	}

	// Indent for message continuation lines: leading 2 + typeW + 2 + reasonW + 2 + objectW + 2
	msgIndent := strings.Repeat(" ", 2+typeW+2+reasonW+2+objectW+2)

	formatRows := func(t, r, o, msg, age string) []string {
		msgLines := wrapPlain(msg, messageW)
		if len(msgLines) == 0 {
			msgLines = []string{""}
		}
		first := fmt.Sprintf("  %-*s  %-*s  %-*s  %-*s  %-*s",
			typeW, t, reasonW, r, objectW, o, messageW, msgLines[0], ageW, age)
		out := []string{first}
		for _, cont := range msgLines[1:] {
			out = append(out, msgIndent+cont)
		}
		return out
	}

	var lines []string
	for _, row := range formatRows("TYPE", "REASON", "OBJECT", "MESSAGE", "AGE") {
		lines = append(lines, labelStyle.Render(row))
	}

	for _, e := range m.events {
		for _, row := range formatRows(e.Type, e.Reason, e.Object, e.Message, e.Age) {
			lines = append(lines, valueStyle.Render(row))
		}
	}

	return lines
}

// buildConditionsLines renders the Conditions tab as a TYPE/STATUS/REASON/
// MESSAGE/AGE table, mirroring kubectl describe's Conditions section. Status
// "False" is highlighted to draw the eye to the failing condition that's
// usually the diagnostic answer.
func (m DetailModel) buildConditionsLines() []string {
	if !m.hasData || len(m.detail.Conditions) == 0 {
		return []string{"  " + m.theme.DetailValueStyle().Render("No conditions")}
	}

	typeW := len("TYPE")
	statusW := len("STATUS")
	reasonW := len("REASON")
	messageW := len("MESSAGE")
	ageW := len("AGE")

	for _, c := range m.detail.Conditions {
		if len(c.Type) > typeW {
			typeW = len(c.Type)
		}
		if len(c.Status) > statusW {
			statusW = len(c.Status)
		}
		if len(c.Reason) > reasonW {
			reasonW = len(c.Reason)
		}
		if len(c.Message) > messageW {
			messageW = len(c.Message)
		}
		if len(c.Age) > ageW {
			ageW = len(c.Age)
		}
	}

	maxMsgW := m.width - typeW - statusW - reasonW - ageW - 12
	if maxMsgW < 10 {
		maxMsgW = 10
	}
	if messageW > maxMsgW {
		messageW = maxMsgW
	}

	labelStyle := m.theme.DetailLabelStyle()
	valueStyle := m.theme.DetailValueStyle()
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Status.Error))
	if !m.focused {
		// Same dim treatment as Events / Logs — the False-row red collapses
		// too. focused = look at diagnostics in detail; unfocused = let
		// the panel recede. focus is one Tab away.
		dim := m.theme.TableDimRowStyle()
		labelStyle = dim
		valueStyle = dim
		errorStyle = dim
	}

	msgIndent := strings.Repeat(" ", 2+typeW+2+statusW+2+reasonW+2)

	formatRows := func(t, s, r, msg, age string) []string {
		msgLines := wrapPlain(msg, messageW)
		if len(msgLines) == 0 {
			msgLines = []string{""}
		}
		first := fmt.Sprintf("  %-*s  %-*s  %-*s  %-*s  %-*s",
			typeW, t, statusW, s, reasonW, r, messageW, msgLines[0], ageW, age)
		out := []string{first}
		for _, cont := range msgLines[1:] {
			out = append(out, msgIndent+cont)
		}
		return out
	}

	var lines []string
	for _, row := range formatRows("TYPE", "STATUS", "REASON", "MESSAGE", "AGE") {
		lines = append(lines, labelStyle.Render(row))
	}

	for _, c := range m.detail.Conditions {
		rendered := valueStyle
		if c.Status == "False" {
			// Failing condition is usually the diagnostic answer — highlight
			// with the same Error palette used for status badges. Collapses
			// to dim when the panel isn't focused (errorStyle == dim).
			rendered = errorStyle
		}
		for _, row := range formatRows(c.Type, c.Status, c.Reason, c.Message, c.Age) {
			lines = append(lines, rendered.Render(row))
		}
	}

	return lines
}

// buildHistoryLines renders the Helm History tab (Phase 2c). Cursor-aware
// (j/k moves historyCursor) and tags the current deployed revision with a
// "●" marker so the user can tell rollback target from current state at a
// glance. Inactive rows are dimmed; cursor row gets reverse video.
func (m DetailModel) buildHistoryLines() []string {
	if !m.hasData || len(m.detail.ReleaseHistory) == 0 {
		return []string{"  " + m.theme.DetailValueStyle().Render("(no history)")}
	}
	revs := m.detail.ReleaseHistory

	// Find the current revision — the latest entry whose status is
	// "deployed". Usually the last row, but mid-rollback / mid-upgrade
	// can leave a different rev as deployed.
	currentRev := -1
	for _, r := range revs {
		if r.Status == "deployed" {
			currentRev = r.Revision
		}
	}

	revW := len("REV")
	statusW := len("STATUS")
	dateW := len("DATE")
	chartW := len("CHART")
	descW := len("DESCRIPTION")
	for _, r := range revs {
		if rs := fmt.Sprintf("%d", r.Revision); len(rs) > revW {
			revW = len(rs)
		}
		if len(r.Status) > statusW {
			statusW = len(r.Status)
		}
		if d := k8s.FormatHelmHistoryDate(r.Updated); len(d) > dateW {
			dateW = len(d)
		}
		if len(r.Chart) > chartW {
			chartW = len(r.Chart)
		}
		if len(r.Description) > descW {
			descW = len(r.Description)
		}
	}

	// Marker column = 2 cells; 4 inter-column gaps of 2 cells; leading 2
	// + trailing 2 padding. Cap DESCRIPTION so the table fits.
	const markerW = 2
	maxDescW := m.width - markerW - revW - statusW - dateW - chartW - 12
	if maxDescW < 10 {
		maxDescW = 10
	}
	if descW > maxDescW {
		descW = maxDescW
	}

	labelStyle := m.theme.DetailLabelStyle()
	valueStyle := m.theme.DetailValueStyle()
	cursorStyle := m.theme.TableSelectedRowStyle()
	if !m.focused {
		cursorStyle = m.theme.TableUnfocusedSelectedRowStyle()
		// Mirror the sidebar/table treatment: non-cursor history rows
		// collapse to overlay0 grey when the panel is unfocused, so
		// the cursor revision's lavender chip is the only signal that
		// survives. Header label keeps its dim grey too (it's chrome,
		// not data).
		dim := m.theme.TableDimRowStyle()
		labelStyle = dim
		valueStyle = dim
	}

	formatRow := func(marker, rev, status, date, chart, desc string) string {
		return fmt.Sprintf("  %-*s%-*s  %-*s  %-*s  %-*s  %-*s",
			markerW, marker,
			revW, rev,
			statusW, status,
			dateW, date,
			chartW, chart,
			descW, desc)
	}

	var lines []string
	lines = append(lines, labelStyle.Render(formatRow("", "REV", "STATUS", "DATE", "CHART", "DESCRIPTION")))
	for i, r := range revs {
		marker := "  "
		if r.Revision == currentRev {
			marker = "● "
		}
		desc := r.Description
		if len(desc) > descW {
			desc = desc[:descW]
		}
		row := formatRow(marker, fmt.Sprintf("%d", r.Revision), r.Status, k8s.FormatHelmHistoryDate(r.Updated), r.Chart, desc)
		if i == m.historyCursor {
			lines = append(lines, cursorStyle.Render(row))
		} else {
			lines = append(lines, valueStyle.Render(row))
		}
	}
	return lines
}

// handleHistoryKey owns cursor navigation on the History tab. Space is NOT
// handled here — AppModel intercepts it to open the rollback confirm popup
// so the cross-cutting confirm + subprocess flow stays in one place.
// Returns (model, handled, cmd) so the caller's switch falls through when
// the key isn't ours.
func (m DetailModel) handleHistoryKey(msg tea.KeyMsg) (DetailModel, bool, tea.Cmd) {
	if msg.Type != tea.KeyRunes || len(msg.Runes) != 1 {
		return m, false, nil
	}
	if len(m.detail.ReleaseHistory) == 0 {
		return m, false, nil
	}
	switch msg.Runes[0] {
	case 'j':
		if m.historyCursor < 0 {
			m.historyCursor = 0
		} else if m.historyCursor < len(m.detail.ReleaseHistory)-1 {
			m.historyCursor++
		}
		m.buildContentLines()
		return m, true, nil
	case 'k':
		if m.historyCursor > 0 {
			m.historyCursor--
		}
		m.buildContentLines()
		return m, true, nil
	case 'g':
		m.historyCursor = 0
		m.buildContentLines()
		return m, true, nil
	case 'G':
		m.historyCursor = len(m.detail.ReleaseHistory) - 1
		m.buildContentLines()
		return m, true, nil
	}
	return m, false, nil
}

// SelectedHistoryRevision returns the ReleaseRevision under the History
// cursor, or nil when the cursor is off the table / on the current
// (deployed) row. AppModel uses this to decide whether Space triggers a
// rollback confirm or stays silent.
func (m DetailModel) SelectedHistoryRevision() *k8s.ReleaseRevision {
	if m.ActiveTabName() != "History" {
		return nil
	}
	if m.historyCursor < 0 || m.historyCursor >= len(m.detail.ReleaseHistory) {
		return nil
	}
	r := m.detail.ReleaseHistory[m.historyCursor]
	// Current row is no-op (can't roll back to where you already are).
	for _, rev := range m.detail.ReleaseHistory {
		if rev.Status == "deployed" && rev.Revision == r.Revision {
			return nil
		}
	}
	return &r
}

// wrapPlain wraps plain (no ANSI) text to the given width, breaking at word
// boundaries when possible. Returns one string per output line. The returned
// lines do not include any indentation — callers prepend continuation indent.
// sanitizeLogText collapses carriage-return-based progress redraws to
// their final visible state before the text enters the log buffer.
//
// Container output for `apt update`, `npm install`, `pip install`,
// `docker pull` and similar progress-bar UX uses `\r` (or `\r` plus
// ANSI erase escapes) to rewrite the same terminal line in place.
// bufio.Scanner's default SplitFunc only breaks on `\n`, so those
// intra-line `\r`s survive into scanner.Text() and are then written
// through lipgloss to the real terminal. When a raw `\r` reaches the
// terminal it moves the cursor to column 0 of the *terminal* row —
// not the panel's inner column 0 — and subsequent bytes overwrite
// the panel border. The Logs tab visibly bleeds past its left edge.
//
// The pragmatic fix: split on `\r` and keep the last non-empty
// segment. That matches what a real terminal would end up showing
// after all the in-place rewrites resolve (the final progress-bar
// frame, `100%` line, etc.). Empty segments (trailing `\r`, `\r\r`
// runs) are skipped so a `foo\r` line still renders `foo` instead
// of "".
//
// After the `\r` collapse the text is stripped of ALL terminal control
// bytes before it enters the buffer. Container logs routinely carry more
// than color: cursor moves (`\x1b[G`), line/screen erase (`\x1b[K`,
// `\x1b[2J`), cursor-home, and bare C0 controls (backspace `\x08`,
// form-feed `\x0c`, bell). wrapPlain slices by byte and the panel is
// composed with a fixed width, so any escape that reaches the real
// terminal moves the cursor OUTSIDE the panel or clears the screen — the
// Logs tab then "explodes" over the whole frame. `ansi.Strip` removes
// ESC-based escape sequences but, being an ANSI parser, KEEPS bare C0/C1
// control chars (they are Execute actions), so a second pass drops those
// too (tab → space to avoid width surprises). In-log color is stripped
// as a consequence; kbu still colors its own per-container/pod prefix,
// matching how k9s / Lens render streaming logs.
func sanitizeLogText(s string) string {
	if strings.ContainsRune(s, '\r') {
		parts := strings.Split(s, "\r")
		s = ""
		for i := len(parts) - 1; i >= 0; i-- {
			if parts[i] != "" {
				s = parts[i]
				break
			}
		}
	}
	s = ansi.Strip(s)
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t':
			return ' '
		case r < 0x20, r >= 0x7f && r < 0xa0:
			return -1
		default:
			return r
		}
	}, s)
}

func wrapPlain(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	if len(text) <= width {
		return []string{text}
	}
	var out []string
	rest := text
	for len(rest) > width {
		cut := width
		// If the boundary char is a space, break exactly at width — keeps
		// "hello world" intact when width == 11.
		if rest[width] != ' ' {
			if idx := strings.LastIndex(rest[:width], " "); idx > 0 {
				cut = idx
			}
		}
		out = append(out, strings.TrimRight(rest[:cut], " "))
		rest = strings.TrimLeft(rest[cut:], " ")
	}
	if rest != "" {
		out = append(out, rest)
	}
	return out
}

// containerLogPalette is the set of foreground colors assigned to container
// log prefixes. 8 entries chosen for visual distinguishability on dark
// terminal backgrounds (Catppuccin-aligned).
var containerLogPalette = []lipgloss.Color{
	"#f38ba8", // red
	"#fab387", // peach
	"#f9e2af", // yellow
	"#a6e3a1", // green
	"#94e2d5", // teal
	"#89b4fa", // blue
	"#cba6f7", // mauve
	"#f5c2e7", // pink
}

// containerLogColor returns a stable per-container color via a tiny FNV-ish
// hash. Same container name always maps to the same palette entry across the
// session, so users can visually associate a color with a container.
func containerLogColor(name string) lipgloss.Color {
	return fnvPaletteColor(name)
}

// podLogColor mirrors containerLogColor for the pod dimension in aggregate
// log streams. Same palette so the two colors blend visually but are derived
// from different identifiers — two pods running the same container name get
// distinct pod-color stripes.
func podLogColor(name string) lipgloss.Color {
	return fnvPaletteColor(name)
}

func fnvPaletteColor(name string) lipgloss.Color {
	h := uint32(2166136261)
	for _, b := range []byte(name) {
		h = (h ^ uint32(b)) * 16777619
	}
	return containerLogPalette[int(h)%len(containerLogPalette)]
}

// podHashTag truncates a pod name to its trailing identifier, the random
// suffix that K8s appends to ReplicaSet pods (`nginx-7f9c4d-abc12` → `abc12`).
// Falls back to last 6 chars when the name has no dash.
func podHashTag(name string) string {
	const want = 5
	if idx := strings.LastIndex(name, "-"); idx >= 0 && idx < len(name)-1 {
		tail := name[idx+1:]
		if len(tail) > want {
			return tail[len(tail)-want:]
		}
		return tail
	}
	if len(name) > want {
		return name[len(name)-want:]
	}
	return name
}

func (m DetailModel) buildLogLines() []string {
	if !supportsLogs(m.resourceType) {
		return []string{"  " + m.theme.DetailValueStyle().Render("Logs not available for this resource type")}
	}
	if len(m.logLines) == 0 {
		return []string{"  " + m.theme.DetailValueStyle().Render("Waiting for logs...")}
	}
	var lines []string
	// Logs is the only panel-3 tab that does NOT dim on unfocus.
	// Streaming content is information actively arriving; dimming it
	// would hide updates that the user is glancing for from the corner
	// of the eye. The other panel-3 tabs (Events / Conditions /
	// Relatives / History) all dim via TableDimRowStyle because their
	// content is static — focus-back-and-read works fine for them.
	// Documented exception to the unfocus-dim convention, matching how
	// Lens / k9s treat streaming logs.
	sepDim := lipgloss.NewStyle().Foreground(lipgloss.Color("#7f849c"))
	for _, ll := range m.logLines {
		// Build plain + styled prefixes side by side. Plain is for wrap-width
		// math (avoid counting ANSI escapes); styled is what we actually emit.
		//
		// Aggregate prefix uses `<container>@<pod-hash> │ <text>` —
		// container-first because that's what users care about during
		// debugging ("which container is this from?"); the `@<pod-hash>`
		// disambiguates between pods running the same container. `│`
		// separates the prefix block from the log line itself.
		var plainPrefix, styledPrefix string
		if ll.pod != "" {
			tag := podHashTag(ll.pod)
			podStyle := lipgloss.NewStyle().Foreground(podLogColor(ll.pod)).Bold(true)
			ctrStyle := lipgloss.NewStyle().Foreground(containerLogColor(ll.container)).Bold(true)
			plainPrefix = "  " + ll.container + "@" + tag + " │ "
			styledPrefix = "  " + ctrStyle.Render(ll.container) + sepDim.Render("@") + podStyle.Render(tag) + " │ "
		} else {
			ctrStyle := lipgloss.NewStyle().Foreground(containerLogColor(ll.container)).Bold(true)
			plainPrefix = "  " + ll.container + " │ "
			styledPrefix = "  " + ctrStyle.Render(ll.container) + " │ "
		}
		// Budget the text column count off the prefix's VISUAL width, not
		// its byte length (`│` is 3 bytes / 1 col; a stray negative budget
		// from byte-counting sends wrapPlain down its width<=0 path, which
		// returns the line UNWRAPPED — a second overflow route). Floor at 1
		// so a prefix wider than the panel still wraps instead of emitting
		// a full-length line.
		prefixCols := lipgloss.Width(plainPrefix)
		textW := m.width - prefixCols
		if textW < 1 {
			textW = 1
		}
		wrapped := wrapPlain(ll.text, textW)
		if len(wrapped) == 0 {
			wrapped = []string{""}
		}
		lines = append(lines, styledPrefix+wrapped[0])
		if len(wrapped) > 1 {
			// Continuation needs same total visual width up to the rightmost
			// " │ " (3 cols) so text columns align under the first line.
			contIndent := strings.Repeat(" ", prefixCols-3) + " │ "
			for _, w := range wrapped[1:] {
				lines = append(lines, contIndent+w)
			}
		}
	}
	// Final guard: no log line may exceed the panel width. wrapPlain keeps
	// the text within budget, but an over-long prefix (or a wide char at the
	// wrap boundary) can still push a line past m.width, and one over-width
	// line shatters the fixed-width panel composition in app.go. ansi.Truncate
	// is escape-aware, so it clamps without splitting the styled prefix.
	for i, l := range lines {
		if lipgloss.Width(l) > m.width {
			lines[i] = ansi.Truncate(l, m.width, "")
		}
	}
	return lines
}

// isAggregateLogsKind reports whether a workload kind funnels its Pods
// through k8s.PodsForWorkload into a single aggregate Logs stream. Pods
// themselves don't qualify — they take the single-pod streaming path in
// app.go's dispatch switches. Mirrors supportsLogs minus Pods.
func isAggregateLogsKind(rt k8s.ResourceType) bool {
	return rt != k8s.ResourcePods && supportsLogs(rt)
}

// supportsLogs reports whether a resource type has a Logs tab in its detail
// panel. Pods stream single-pod; all other workload kinds stream aggregate
// from their managed Pods via k8s.PodsForWorkload (Deployment → current
// ReplicaSet; StatefulSet / DaemonSet / Job → selector; CronJob → all
// retained Jobs' Pods). The set MUST stay in sync with the workload-kind
// cases in SetResourceType (tab construction) and the dispatch switches
// in app.go (ResourceDetailMsg / RowSelectedMsg handlers).
func supportsLogs(rt k8s.ResourceType) bool {
	switch rt {
	case k8s.ResourcePods,
		k8s.ResourceDeployments,
		k8s.ResourceStatefulSets,
		k8s.ResourceDaemonSets,
		k8s.ResourceJobs,
		k8s.ResourceCronJobs:
		return true
	}
	return false
}

// sortedKeys returns the keys of a map sorted alphabetically.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
