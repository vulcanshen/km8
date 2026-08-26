package ui

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/vulcanshen/kbu/internal/k8s"
	"github.com/vulcanshen/kbu/internal/theme"
)

func newTestTable() TableModel {
	t := theme.DefaultTheme()
	m := NewTableModel(t)
	m.SetFocused(true)
	return m
}

func sampleRows(n int) [][]string {
	rows := make([][]string, n)
	for i := 0; i < n; i++ {
		rows[i] = []string{
			fmt.Sprintf("pod-%d", i),
			"1/1",
			"Running",
			"0",
			"5m",
			"node-1",
		}
	}
	return rows
}

// keyMsg is declared in sidebar_test.go (same package)

func TestTableModel_CopyableContent_CursorRowTabSep(t *testing.T) {
	// v1.7.9 focus-content semantics: y copies ONLY the cursor row,
	// tab-separated raw values (no header, no padding). Enables
	// direct `pbpaste | awk -F$'\t' '{print $1}'` in the shell.
	m := newTestTable()
	m.SetRows(sampleRows(3))
	m.cursor = 1 // "pod-1" row
	got := m.CopyableContent()

	if got == "" {
		t.Fatal("expected non-empty content for a row cursor")
	}
	if strings.Contains(got, "\n") {
		t.Errorf("cursor-row output must be a single line, got:\n%s", got)
	}
	if strings.Contains(got, "Name") {
		t.Errorf("must not include header (focus = cursor row only), got:\n%s", got)
	}
	if !strings.Contains(got, "pod-1") {
		t.Errorf("expected the cursor row's name in output, got:\n%s", got)
	}
	for _, wrong := range []string{"pod-0", "pod-2"} {
		if strings.Contains(got, wrong) {
			t.Errorf("must not include non-cursor rows, but %q present in:\n%s", wrong, got)
		}
	}
	// Tab-separated: at least one tab must be present between cells.
	if !strings.Contains(got, "\t") {
		t.Errorf("expected tab-separated cells, got:\n%s", got)
	}
}

func TestTableModel_CopyableContent_EmptyWhenNoRows(t *testing.T) {
	m := newTestTable()
	if got := m.CopyableContent(); got != "" {
		t.Errorf("expected empty when no rows, got %q", got)
	}
}

func TestTableModel_CopyableContent_EmptyWhenCursorOutOfRange(t *testing.T) {
	m := newTestTable()
	m.SetRows(sampleRows(2))
	m.cursor = 99 // past end
	if got := m.CopyableContent(); got != "" {
		t.Errorf("out-of-range cursor: expected empty, got %q", got)
	}
}

func TestTableModel_InitialState(t *testing.T) {
	m := newTestTable()

	// Should start with Pods columns
	podCols := ColumnsForResource(k8s.ResourcePods)
	if len(m.columns) != len(podCols) {
		t.Fatalf("expected %d columns for Pods, got %d", len(podCols), len(m.columns))
	}
	for i, col := range m.columns {
		if col.Title != podCols[i].Title {
			t.Errorf("column %d: expected title %q, got %q", i, podCols[i].Title, col.Title)
		}
	}

	// Empty rows
	if len(m.rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(m.rows))
	}

	// Cursor at 0
	if m.cursor != 0 {
		t.Errorf("expected cursor at 0, got %d", m.cursor)
	}

	// Resource type is Pods
	if m.resourceType != k8s.ResourcePods {
		t.Errorf("expected resource type Pods, got %v", m.resourceType)
	}
}

func TestTableModel_NavigateDown(t *testing.T) {
	m := newTestTable()
	m.SetRows(sampleRows(5))

	m, _ = m.Update(keyMsg('j'))
	if m.cursor != 1 {
		t.Errorf("expected cursor at 1 after j, got %d", m.cursor)
	}

	m, _ = m.Update(keyMsg('j'))
	if m.cursor != 2 {
		t.Errorf("expected cursor at 2 after second j, got %d", m.cursor)
	}
}

func TestTableModel_NavigateUp(t *testing.T) {
	m := newTestTable()
	m.SetRows(sampleRows(5))
	m.cursor = 2

	m, _ = m.Update(keyMsg('k'))
	if m.cursor != 1 {
		t.Errorf("expected cursor at 1 after k, got %d", m.cursor)
	}
}

func TestTableModel_NavigateDownAtBottom(t *testing.T) {
	m := newTestTable()
	m.SetRows(sampleRows(5))
	m.cursor = 4 // last row

	m, _ = m.Update(keyMsg('j'))
	if m.cursor != 4 {
		t.Errorf("expected cursor to stay at 4, got %d", m.cursor)
	}
}

func TestTableModel_NavigateUpAtTop(t *testing.T) {
	m := newTestTable()
	m.SetRows(sampleRows(5))
	m.cursor = 0

	m, _ = m.Update(keyMsg('k'))
	if m.cursor != 0 {
		t.Errorf("expected cursor to stay at 0, got %d", m.cursor)
	}
}

func TestTableModel_GG(t *testing.T) {
	m := newTestTable()
	m.SetRows(sampleRows(10))
	m.cursor = 5

	// First g: sets pendingG
	m, _ = m.Update(keyMsg('g'))
	if !m.pendingG {
		t.Fatal("expected pendingG to be true after first g")
	}
	if m.cursor != 5 {
		t.Errorf("expected cursor to remain at 5 after first g, got %d", m.cursor)
	}

	// Second g: cursor goes to 0
	m, _ = m.Update(keyMsg('g'))
	if m.cursor != 0 {
		t.Errorf("expected cursor at 0 after gg, got %d", m.cursor)
	}
	if m.pendingG {
		t.Error("expected pendingG to be false after gg")
	}
}

func TestTableModel_ShiftG(t *testing.T) {
	m := newTestTable()
	m.SetRows(sampleRows(10))
	m.cursor = 0

	m, _ = m.Update(keyMsg('G'))
	if m.cursor != 9 {
		t.Errorf("expected cursor at 9 (last row), got %d", m.cursor)
	}
}

func TestTableModel_ResourceSelectedMsg(t *testing.T) {
	m := newTestTable()
	m.SetRows(sampleRows(5))
	m.cursor = 3

	msg := ResourceSelectedMsg{Type: k8s.ResourceDeployments}
	m, _ = m.Update(msg)

	deployCols := ColumnsForResource(k8s.ResourceDeployments)
	if len(m.columns) != len(deployCols) {
		t.Fatalf("expected %d columns for Deployments, got %d", len(deployCols), len(m.columns))
	}
	for i, col := range m.columns {
		if col.Title != deployCols[i].Title {
			t.Errorf("column %d: expected %q, got %q", i, deployCols[i].Title, col.Title)
		}
	}
	if len(m.rows) != 0 {
		t.Errorf("expected rows to be cleared, got %d", len(m.rows))
	}
	if m.cursor != 0 {
		t.Errorf("expected cursor reset to 0, got %d", m.cursor)
	}
	if m.resourceType != k8s.ResourceDeployments {
		t.Errorf("expected resource type Deployments, got %v", m.resourceType)
	}
}

func TestTableModel_SetRows(t *testing.T) {
	m := newTestTable()
	rows := sampleRows(3)
	m.SetRows(rows)

	if len(m.rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(m.rows))
	}
	if m.rows[0][0] != "pod-0" {
		t.Errorf("expected first row name %q, got %q", "pod-0", m.rows[0][0])
	}
	if m.rows[2][0] != "pod-2" {
		t.Errorf("expected last row name %q, got %q", "pod-2", m.rows[2][0])
	}
}

func TestTableModel_ScrollOffset(t *testing.T) {
	m := newTestTable()
	m.SetSize(80, 5) // 5 lines total, 4 visible rows (height - 1 for header)
	m.SetRows(sampleRows(20))

	// Navigate down past visible area
	for i := 0; i < 6; i++ {
		m, _ = m.Update(keyMsg('j'))
	}

	if m.cursor != 6 {
		t.Errorf("expected cursor at 6, got %d", m.cursor)
	}

	// scrollOffset should have adjusted so cursor is visible
	visible := m.visibleRows()
	if m.cursor >= m.scrollOffset+visible {
		t.Errorf("cursor %d should be visible with scrollOffset %d and visible %d",
			m.cursor, m.scrollOffset, visible)
	}
	if m.scrollOffset <= 0 {
		t.Errorf("expected scrollOffset > 0 after scrolling down, got %d", m.scrollOffset)
	}
}

func TestTableModel_EnterSearch(t *testing.T) {
	m := newTestTable()
	m.SetRows(sampleRows(5))

	// Press / to enter search mode.
	m, _ = m.Update(keyMsg('/'))
	if !m.searching {
		t.Fatal("expected searching=true after /")
	}
	if m.searchQuery != "" {
		t.Errorf("expected empty searchQuery, got %q", m.searchQuery)
	}

	// All rows should still be visible (empty query).
	if len(m.rows) != 5 {
		t.Errorf("expected 5 rows with empty query, got %d", len(m.rows))
	}
}

func TestTableModel_SearchFilter(t *testing.T) {
	m := newTestTable()
	rows := [][]string{
		{"nginx-pod", "1/1", "Running", "0", "5m", "node-1"},
		{"redis-pod", "1/1", "Running", "0", "3m", "node-2"},
		{"postgres-db", "1/1", "Running", "0", "10m", "node-1"},
		{"nginx-svc", "1/1", "Pending", "0", "2m", "node-3"},
	}
	m.SetRows(rows)

	// Enter search mode.
	m, _ = m.Update(keyMsg('/'))
	if !m.searching {
		t.Fatal("expected searching=true after /")
	}

	// Type "nginx" — should filter to 2 rows.
	for _, r := range "nginx" {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if m.searchQuery != "nginx" {
		t.Errorf("expected searchQuery='nginx', got %q", m.searchQuery)
	}
	if len(m.rows) != 2 {
		t.Fatalf("expected 2 filtered rows for 'nginx', got %d", len(m.rows))
	}
	if m.rows[0][0] != "nginx-pod" {
		t.Errorf("expected first filtered row 'nginx-pod', got %q", m.rows[0][0])
	}
	if m.rows[1][0] != "nginx-svc" {
		t.Errorf("expected second filtered row 'nginx-svc', got %q", m.rows[1][0])
	}
}

func TestTableModel_SearchFilterCaseInsensitive(t *testing.T) {
	m := newTestTable()
	rows := [][]string{
		{"Nginx-Pod", "1/1", "Running", "0", "5m", "node-1"},
		{"redis-pod", "1/1", "Running", "0", "3m", "node-2"},
	}
	m.SetRows(rows)

	// Enter search mode and type "nginx" (lowercase).
	m, _ = m.Update(keyMsg('/'))
	for _, r := range "nginx" {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	// Should match "Nginx-Pod" case-insensitively.
	if len(m.rows) != 1 {
		t.Fatalf("expected 1 filtered row for case-insensitive 'nginx', got %d", len(m.rows))
	}
	if m.rows[0][0] != "Nginx-Pod" {
		t.Errorf("expected filtered row 'Nginx-Pod', got %q", m.rows[0][0])
	}
}

func TestTableModel_SearchClearOnEsc(t *testing.T) {
	m := newTestTable()
	rows := [][]string{
		{"nginx-pod", "1/1", "Running", "0", "5m", "node-1"},
		{"redis-pod", "1/1", "Running", "0", "3m", "node-2"},
		{"postgres-db", "1/1", "Running", "0", "10m", "node-1"},
	}
	m.SetRows(rows)

	// Enter search, type "nginx", then press Esc.
	m, _ = m.Update(keyMsg('/'))
	for _, r := range "nginx" {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if len(m.rows) != 1 {
		t.Fatalf("expected 1 row for 'nginx', got %d", len(m.rows))
	}

	// Press Esc — should clear filter and exit search mode.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if m.searching {
		t.Error("expected searching=false after Esc")
	}
	if m.searchQuery != "" {
		t.Errorf("expected empty searchQuery after Esc, got %q", m.searchQuery)
	}
	if len(m.rows) != 3 {
		t.Errorf("expected all 3 rows after Esc, got %d", len(m.rows))
	}
}

func TestTableModel_SearchConfirmOnEnter(t *testing.T) {
	m := newTestTable()
	rows := [][]string{
		{"nginx-pod", "1/1", "Running", "0", "5m", "node-1"},
		{"redis-pod", "1/1", "Running", "0", "3m", "node-2"},
		{"postgres-db", "1/1", "Running", "0", "10m", "node-1"},
	}
	m.SetRows(rows)

	// Enter search, type "redis", then press Enter.
	m, _ = m.Update(keyMsg('/'))
	for _, r := range "redis" {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if len(m.rows) != 1 {
		t.Fatalf("expected 1 row for 'redis', got %d", len(m.rows))
	}

	// Press Enter — should exit search mode but keep filter.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.searching {
		t.Error("expected searching=false after Enter")
	}
	if m.searchQuery != "redis" {
		t.Errorf("expected searchQuery='redis' after Enter, got %q", m.searchQuery)
	}
	if len(m.rows) != 1 {
		t.Errorf("expected filter to remain active with 1 row, got %d", len(m.rows))
	}
}

func TestTableModel_SearchOriginalIndex(t *testing.T) {
	m := newTestTable()
	rows := [][]string{
		{"nginx-pod", "1/1", "Running", "0", "5m", "node-1"},    // original index 0
		{"redis-pod", "1/1", "Running", "0", "3m", "node-2"},    // original index 1
		{"postgres-db", "1/1", "Running", "0", "10m", "node-1"}, // original index 2
		{"nginx-svc", "1/1", "Pending", "0", "2m", "node-3"},    // original index 3
	}
	m.SetRows(rows)

	// Enter search, type "nginx".
	m, _ = m.Update(keyMsg('/'))
	for _, r := range "nginx" {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	// 2 rows should be visible: nginx-pod (original 0) and nginx-svc (original 3).
	if len(m.rows) != 2 {
		t.Fatalf("expected 2 filtered rows, got %d", len(m.rows))
	}

	// OriginalIndex for display index 0 should be 0.
	if m.OriginalIndex(0) != 0 {
		t.Errorf("expected OriginalIndex(0)=0, got %d", m.OriginalIndex(0))
	}

	// OriginalIndex for display index 1 should be 3.
	if m.OriginalIndex(1) != 3 {
		t.Errorf("expected OriginalIndex(1)=3, got %d", m.OriginalIndex(1))
	}

	// SelectedRow should return the original index.
	m.cursor = 1
	if m.SelectedRow() != 3 {
		t.Errorf("expected SelectedRow()=3 when cursor=1, got %d", m.SelectedRow())
	}
}

func TestTableModel_SearchBackspace(t *testing.T) {
	m := newTestTable()
	rows := [][]string{
		{"nginx-pod", "1/1", "Running", "0", "5m", "node-1"},
		{"redis-pod", "1/1", "Running", "0", "3m", "node-2"},
	}
	m.SetRows(rows)

	// Enter search, type "nginx", then backspace one char.
	m, _ = m.Update(keyMsg('/'))
	for _, r := range "nginx" {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if len(m.rows) != 1 {
		t.Fatalf("expected 1 row for 'nginx', got %d", len(m.rows))
	}

	// Backspace — query becomes "ngin", still matches only "nginx-pod".
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if m.searchQuery != "ngin" {
		t.Errorf("expected searchQuery='ngin' after backspace, got %q", m.searchQuery)
	}
	if len(m.rows) != 1 {
		t.Errorf("expected 1 row for 'ngin', got %d", len(m.rows))
	}

	// Backspace 4 more times — query becomes empty, all rows visible.
	for i := 0; i < 4; i++ {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	if m.searchQuery != "" {
		t.Errorf("expected empty searchQuery after clearing, got %q", m.searchQuery)
	}
	if len(m.rows) != 2 {
		t.Errorf("expected 2 rows with empty query, got %d", len(m.rows))
	}
}

func TestTableModel_SearchNavigateWhileSearching(t *testing.T) {
	m := newTestTable()
	rows := [][]string{
		{"nginx-pod-1", "1/1", "Running", "0", "5m", "node-1"},
		{"nginx-pod-2", "1/1", "Running", "0", "3m", "node-2"},
		{"nginx-pod-3", "1/1", "Running", "0", "10m", "node-1"},
	}
	m.SetRows(rows)

	// Enter search, type "nginx".
	m, _ = m.Update(keyMsg('/'))
	for _, r := range "nginx" {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if m.cursor != 0 {
		t.Fatalf("expected cursor at 0, got %d", m.cursor)
	}

	// Navigate down with arrow key while searching.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.cursor != 1 {
		t.Errorf("expected cursor at 1 after down arrow while searching, got %d", m.cursor)
	}

	// Still in search mode.
	if !m.searching {
		t.Error("expected to still be in search mode after navigating")
	}
}

func TestTableModel_SearchJKAreTypedNotNavigation(t *testing.T) {
	m := newTestTable()
	m.SetRows(sampleRows(5))

	m, _ = m.Update(keyMsg('/'))
	m, _ = m.Update(keyMsg('j'))
	m, _ = m.Update(keyMsg('k'))

	if m.cursor != 0 {
		t.Errorf("j/k in search must not move cursor, got %d", m.cursor)
	}
	if m.searchQuery != "jk" {
		t.Errorf("j/k in search must be typed, got query %q", m.searchQuery)
	}
}

func TestTableModel_SetRowsPreservesFilter(t *testing.T) {
	m := newTestTable()
	rows := [][]string{
		{"nginx-pod", "1/1", "Running", "0", "5m", "node-1"},
		{"redis-pod", "1/1", "Running", "0", "3m", "node-2"},
	}
	m.SetRows(rows)

	// Enter search, type "nginx", confirm with Enter.
	m, _ = m.Update(keyMsg('/'))
	for _, r := range "nginx" {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Now set new rows (simulating a watch update).
	newRows := [][]string{
		{"nginx-pod", "1/1", "Running", "0", "6m", "node-1"},
		{"redis-pod", "1/1", "Running", "0", "4m", "node-2"},
		{"nginx-new", "1/1", "Running", "0", "1m", "node-3"},
	}
	m.SetRows(newRows)

	// Filter "nginx" should still be active, showing 2 of 3 rows.
	if m.searchQuery != "nginx" {
		t.Errorf("expected searchQuery='nginx' after SetRows, got %q", m.searchQuery)
	}
	if len(m.rows) != 2 {
		t.Errorf("expected 2 filtered rows after SetRows, got %d", len(m.rows))
	}
}

func TestTableModel_SearchColumnMatch(t *testing.T) {
	m := newTestTable()
	rows := [][]string{
		{"pod-1", "1/1", "Running", "0", "5m", "node-1"},
		{"pod-2", "0/1", "Pending", "0", "3m", "node-2"},
		{"pod-3", "1/1", "Error", "5", "10m", "node-1"},
	}
	m.SetRows(rows)

	// Search for "Pending" — should match on status column.
	m, _ = m.Update(keyMsg('/'))
	for _, r := range "Pending" {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	if len(m.rows) != 1 {
		t.Fatalf("expected 1 row matching 'Pending', got %d", len(m.rows))
	}
	if m.rows[0][0] != "pod-2" {
		t.Errorf("expected matching row 'pod-2', got %q", m.rows[0][0])
	}
}

// The Namespace column middle-truncates too (front + tail kept), same as
// Name — long team/env namespaces bury their identity signal at both ends.
func TestTableModel_RenderRow_NamespaceMiddleTruncates(t *testing.T) {
	m := newTestTable()
	m.columns = []Column{{Title: "Namespace", MinWidth: 8}}
	style := m.theme.TableRowStyle()

	line := m.renderRow([]int{8}, []string{"team-prod-environment"}, style, false)
	plain := strings.TrimRight(ansiStrip(line), " ")

	if !strings.Contains(plain, "…") {
		t.Fatalf("Namespace must truncate with an ellipsis, got %q", plain)
	}
	// Ellipsis in the MIDDLE (back kept), not at the end (tail-truncation).
	if strings.HasSuffix(plain, "…") {
		t.Errorf("Namespace must middle-truncate (back kept), got %q", plain)
	}
	if !strings.HasPrefix(plain, "t") {
		t.Errorf("front of the namespace must be kept, got %q", plain)
	}
}

func TestColumnsForResource(t *testing.T) {
	allTypes := k8s.AllResourceTypes()

	if len(allTypes) != 27 {
		t.Fatalf("expected 27 resource types, got %d", len(allTypes))
	}

	for _, rt := range allTypes {
		cols := ColumnsForResource(rt)
		if len(cols) == 0 {
			t.Errorf("ColumnsForResource(%v) returned empty columns", rt)
		}
		// The unlabeled helm-marker column sits right after Name — index 1
		// for cluster-scoped kinds, index 2 for namespaced ones (which
		// lead with the Namespace column). Helm Releases get no marker.
		helmIdx := 1
		if isNamespacedResource(rt) {
			helmIdx = 2
		}
		for i, col := range cols {
			if col.Title == "" && !(i == helmIdx && rt != k8s.ResourceReleases) {
				t.Errorf("ColumnsForResource(%v) column %d has empty title", rt, i)
			}
			if col.MinWidth <= 0 {
				t.Errorf("ColumnsForResource(%v) column %d %q has non-positive MinWidth %d",
					rt, i, col.Title, col.MinWidth)
			}
		}
	}
}

func TestTableModel_SetCursor_BasicAndOutOfRange(t *testing.T) {
	m := newTestTable()
	m.SetColumns(ColumnsForResource(k8s.ResourcePods))
	rows := sampleRows(5)
	m.SetRows(rows)

	m.SetCursor(3)
	if m.cursor != 3 {
		t.Errorf("after SetCursor(3), cursor=%d want 3", m.cursor)
	}

	// Out-of-range is silently ignored.
	m.SetCursor(99)
	if m.cursor != 3 {
		t.Errorf("after SetCursor(99) (out of range), cursor=%d want 3 unchanged", m.cursor)
	}
	m.SetCursor(-1)
	if m.cursor != 3 {
		t.Errorf("after SetCursor(-1), cursor=%d want 3 unchanged", m.cursor)
	}
}

func TestTableModel_SetCursor_MapsThroughFilter(t *testing.T) {
	m := newTestTable()
	m.SetColumns(ColumnsForResource(k8s.ResourcePods))
	rows := sampleRows(5) // rows are "pod-0".."pod-4" in column 1
	m.SetRows(rows)
	m.searchQuery = "pod-3"
	m.filterRows()
	if len(m.rows) != 1 {
		t.Fatalf("filter setup failed, visible rows = %d, want 1", len(m.rows))
	}
	// Original index 3 should map to filtered position 0.
	m.SetCursor(3)
	if m.cursor != 0 {
		t.Errorf("filtered SetCursor(3) -> cursor=%d, want 0", m.cursor)
	}
	if got := m.SelectedRow(); got != 3 {
		t.Errorf("SelectedRow() = %d, want 3 (original idx)", got)
	}
}

// TestTableModel_RenderRow_VisualWidthTruncation locks in the fix for the
// byte-vs-visual-width truncation bug. The Nerd Font helm glyph "" is
// 3 bytes / 1 cell wide; pre-fix, a 2-cell column ran val[:1] and produced
// an invalid UTF-8 byte (\xee), rendering as ◇ in terminals. Now the
// renderer uses ansi.Truncate + lipgloss.Width so any multi-byte cell
// content survives a narrow column intact.
func TestTableModel_RenderRow_VisualWidthTruncation(t *testing.T) {
	m := newTestTable()
	// Pin column 0 to Name so the middle-truncation branch (keyed on the
	// "Name" title) is exercised — panel-2 tables now lead with Namespace
	// for namespaced kinds, which would otherwise put Name at index 1.
	m.columns = []Column{{Title: "Name", MinWidth: 8}}
	style := m.theme.TableRowStyle()
	helmGlyph := "" // Nerd Font nf-dev-helm — 3 bytes, 1 cell

	cases := []struct {
		name       string
		val        string
		w          int
		wantInLine string // raw substring that must be present
	}{
		{"helm glyph fits in width 2", helmGlyph, 2, helmGlyph},
		{"helm glyph fits exact width 1", helmGlyph, 1, helmGlyph},
		// Name column (pinned above) uses middle-truncation so the
		// distinctive pod-hash tail stays visible.
		{"ascii truncates with mid ellipsis", "CrashLoopBackOff", 8, "Cras…Off"},
		{"ascii short pads to width", "abc", 6, "abc"},
		{"empty cell pads to width", "", 4, ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			line := m.renderRow([]int{c.w}, []string{c.val}, style, false)
			plain := ansiStrip(line)
			if !utf8.ValidString(plain) {
				t.Errorf("rendered cell is not valid UTF-8: %q", plain)
			}
			if c.wantInLine != "" && !strings.Contains(plain, c.wantInLine) {
				t.Errorf("rendered cell missing %q in %q", c.wantInLine, plain)
			}
		})
	}
}

func TestTruncateMiddle(t *testing.T) {
	cases := []struct {
		name string
		s    string
		w    int
		want string
	}{
		{"fits unchanged", "nginx", 10, "nginx"},
		{"exact width unchanged", "nginx", 5, "nginx"},
		{"kubectl-style pod name at 10", "nginx-6d8f7c-xyz", 10, "nginx…-xyz"},
		{"kubectl-style pod name at 8", "nginx-6d8f7c-xyz", 8, "ngin…xyz"},
		{"very tight w=3 splits 1+1+1", "abcdefghij", 3, "a…j"},
		{"w=2 leaves left char + ellipsis", "abcdefghij", 2, "a…"},
		{"w=1 collapses to ellipsis", "abcdefghij", 1, "…"},
		{"w=0 empties", "abcdefghij", 0, ""},
		{"empty input", "", 5, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := truncateMiddle(c.s, c.w)
			if got != c.want {
				t.Errorf("truncateMiddle(%q, %d) = %q, want %q", c.s, c.w, got, c.want)
			}
			if c.w > 0 {
				if vw := lipgloss.Width(got); vw > c.w {
					t.Errorf("truncateMiddle(%q, %d) width=%d exceeds w", c.s, c.w, vw)
				}
			}
		})
	}
}

func TestTableModel_EmptyStateRendersMessage(t *testing.T) {
	m := newTestTable()
	m.SetSize(80, 20)
	// resourceType defaults to Pods per newTestTable; leave rows empty.
	view := ansiStrip(m.View())
	if !strings.Contains(view, "No pods") {
		t.Errorf("empty table must render 'No pods' placeholder, got %q", view)
	}
}

func TestTableModel_EmptyStateWithSearchShowsQuery(t *testing.T) {
	m := newTestTable()
	m.SetSize(80, 20)
	m.searchQuery = "nginx"
	view := ansiStrip(m.View())
	if !strings.Contains(view, "matching") || !strings.Contains(view, "nginx") {
		t.Errorf("empty table with search must reference the query, got %q", view)
	}
}

// ansiStrip removes ANSI escape sequences so substring checks aren't
// brittle to style codes. Minimal implementation — just enough for the
// tests in this file.
func ansiStrip(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if r == 0x1b {
			inEsc = true
			continue
		}
		if inEsc {
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// TestStatusCellColor_HealthyReturnsEmpty pins the "color is signal" rule:
// healthy / normal states must return "" so the renderer falls back to the
// row's base foreground. Anything that emits color here is a regression
// toward decorating instead of signaling.
func TestStatusCellColor_HealthyReturnsEmpty(t *testing.T) {
	th := theme.DefaultTheme()
	cases := []struct {
		kind     k8s.ResourceType
		colTitle string
		raw      string
	}{
		// Pod healthy
		{k8s.ResourcePods, "Status", "Running"},
		{k8s.ResourcePods, "Status", "Succeeded"},
		{k8s.ResourcePods, "Status", "Completed"},
		// Node healthy
		{k8s.ResourceNodes, "Status", "Ready"},
		// Namespace healthy
		{k8s.ResourceNamespaces, "Status", "Active"},
		// PVC healthy
		{k8s.ResourcePersistentVolumeClaims, "Status", "Bound"},
		// PV healthy
		{k8s.ResourcePersistentVolumes, "Status", "Available"},
		{k8s.ResourcePersistentVolumes, "Status", "Bound"},
		// Helm healthy
		{k8s.ResourceReleases, "Status", "deployed"},
		{k8s.ResourceReleases, "Status", "superseded"},
		// Event normal
		{k8s.ResourceEvents, "Type", "Normal"},
		// Ready column never colored — replicas-mismatch isn't signal
		// worth painting (Status surfaces the same condition when
		// it's actually wrong).
		{k8s.ResourcePods, "Ready", "1/1"},
		{k8s.ResourcePods, "Ready", "0/1"},
		{k8s.ResourceDeployments, "Ready", "1/3"},
		// Empty cell
		{k8s.ResourcePods, "Status", ""},
		{k8s.ResourcePods, "Status", "   "},
		// Non-status column
		{k8s.ResourcePods, "Name", "anything"},
		{k8s.ResourcePods, "Restarts", "0"},
		// Type column on non-Events kind ignored
		{k8s.ResourcePods, "Type", "Warning"},
	}
	for _, c := range cases {
		t.Run(string(c.kind)+"/"+c.colTitle+"/"+c.raw, func(t *testing.T) {
			if got := statusCellColor(c.kind, c.colTitle, c.raw, th, false); got != "" {
				t.Errorf("statusCellColor(%v, %q, %q) = %q, want \"\" (healthy/unknown must not emit color)",
					c.kind, c.colTitle, c.raw, got)
			}
		})
	}
}

// TestStatusCellColor_YellowPending pins the abnormal-yellow set. Pending
// is yellow with no time threshold — a healthy Pod briefly Pending still
// flashes yellow, and that's fine: the steady state is healthy and quiet,
// so the brief yellow is noise the user can ignore.
func TestStatusCellColor_YellowPending(t *testing.T) {
	th := theme.DefaultTheme()
	wantYellow := th.Status.Pending
	cases := []struct {
		kind     k8s.ResourceType
		colTitle string
		raw      string
	}{
		// Pod transitional
		{k8s.ResourcePods, "Status", "Pending"},
		{k8s.ResourcePods, "Status", "ContainerCreating"},
		{k8s.ResourcePods, "Status", "PodInitializing"},
		{k8s.ResourcePods, "Status", "Terminating"},
		// Node degraded
		{k8s.ResourceNodes, "Status", "SchedulingDisabled"},
		// Namespace
		{k8s.ResourceNamespaces, "Status", "Terminating"},
		// PV transitional
		{k8s.ResourcePersistentVolumes, "Status", "Released"},
		// Helm transitional
		{k8s.ResourceReleases, "Status", "pending-install"},
		{k8s.ResourceReleases, "Status", "pending-upgrade"},
		{k8s.ResourceReleases, "Status", "uninstalling"},
	}
	for _, c := range cases {
		t.Run(string(c.kind)+"/"+c.colTitle+"/"+c.raw, func(t *testing.T) {
			if got := statusCellColor(c.kind, c.colTitle, c.raw, th, false); got != wantYellow {
				t.Errorf("statusCellColor(%v, %q, %q) = %q, want %q",
					c.kind, c.colTitle, c.raw, got, wantYellow)
			}
		})
	}
}

// TestStatusCellColor_RedFailure pins the abnormal-red set. Anything that
// turns red here should be a state where the user genuinely needs to act —
// not a transient hiccup the controller will resolve in seconds.
func TestStatusCellColor_RedFailure(t *testing.T) {
	th := theme.DefaultTheme()
	wantRed := th.Status.Error
	cases := []struct {
		kind     k8s.ResourceType
		colTitle string
		raw      string
	}{
		{k8s.ResourcePods, "Status", "CrashLoopBackOff"},
		{k8s.ResourcePods, "Status", "Error"},
		{k8s.ResourcePods, "Status", "ImagePullBackOff"},
		{k8s.ResourcePods, "Status", "ErrImagePull"},
		{k8s.ResourcePods, "Status", "Failed"},
		{k8s.ResourcePods, "Status", "Evicted"},
		{k8s.ResourcePods, "Status", "OOMKilled"},
		{k8s.ResourcePods, "Status", "Init:CrashLoopBackOff"}, // Init:* falls through to red
		{k8s.ResourceNodes, "Status", "NotReady"},
		{k8s.ResourcePersistentVolumeClaims, "Status", "Lost"},
		{k8s.ResourcePersistentVolumes, "Status", "Failed"},
		{k8s.ResourceReleases, "Status", "failed"},
		{k8s.ResourceEvents, "Type", "Warning"},
	}
	for _, c := range cases {
		t.Run(string(c.kind)+"/"+c.colTitle+"/"+c.raw, func(t *testing.T) {
			if got := statusCellColor(c.kind, c.colTitle, c.raw, th, false); got != wantRed {
				t.Errorf("statusCellColor(%v, %q, %q) = %q, want %q",
					c.kind, c.colTitle, c.raw, got, wantRed)
			}
		})
	}
}

// TestStatusCellColor_OnLightBgUsesLatte verifies the cursor / locked-row
// reverse-video bg gets the darker Latte variant — the default Mocha
// pastel (#f9e2af / #f38ba8) washes out on a light bg and would read as
// "barely visible status text".
func TestStatusCellColor_OnLightBgUsesLatte(t *testing.T) {
	th := theme.DefaultTheme()
	wantYellowDark := "#df8e1d"
	wantRedDark := "#d20f39"
	if got := statusCellColor(k8s.ResourcePods, "Status", "Pending", th, true); got != wantYellowDark {
		t.Errorf("onLightBg Pending = %q, want %q", got, wantYellowDark)
	}
	if got := statusCellColor(k8s.ResourcePods, "Status", "CrashLoopBackOff", th, true); got != wantRedDark {
		t.Errorf("onLightBg CrashLoopBackOff = %q, want %q", got, wantRedDark)
	}
}
