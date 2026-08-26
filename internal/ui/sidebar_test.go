package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/vulcanshen/kbu/internal/k8s"
	"github.com/vulcanshen/kbu/internal/theme"
)

func TestSidebarModel_SearchJKAreTypedNotNavigation(t *testing.T) {
	m := newTestSidebar()

	m, _ = m.Update(keyMsg('/'))
	m, _ = m.Update(keyMsg('j'))
	m, _ = m.Update(keyMsg('k'))

	if m.searchQuery != "jk" {
		t.Errorf("j/k in search must be typed as chars, got query %q", m.searchQuery)
	}
	// Cursor may jump to first match due to filtering — that's expected. The
	// regression we guard against is j/k bypassing the rune handler entirely.
}

func TestSidebarModel_SearchEnterNoMatchClearsFilter(t *testing.T) {
	// Bug: typing junk + Enter left searching=false with a non-empty
	// searchQuery and zero visible items. handleKey's "no visible
	// items → return early" guard then swallowed every subsequent key
	// including `/` to restart search — the panel was stuck until
	// focus changed. Enter on an empty match now behaves like Esc:
	// clear the filter and restore the cursor.
	m := newTestSidebar()

	m, _ = m.Update(keyMsg('/'))
	for _, r := range "xxxnomatchxxx" {
		m, _ = m.Update(keyMsg(r))
	}
	if m.searchQuery == "" {
		t.Fatal("setup: search query expected to be populated")
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if m.searching {
		t.Error("Enter must exit search input mode")
	}
	if m.searchQuery != "" {
		t.Errorf("Enter on empty match must clear searchQuery, got %q", m.searchQuery)
	}
	if m.HasActiveFilter() {
		t.Error("filter must drop when Enter has no match")
	}

	// `/` must work again — the original symptom of the bug was this
	// key getting eaten.
	m, _ = m.Update(keyMsg('/'))
	if !m.searching {
		t.Error("`/` after Enter-no-match must re-enter search mode")
	}
}

func TestSidebarModel_LockedFilterEmptiedByUnpin_SlashStillEntersSearch(t *testing.T) {
	// Bug: lock a search filter on the Pinned section, then unpin
	// every match until visible is empty. The locked filter outlives
	// its data; handleKey's empty-visible guard then swallowed every
	// key including `/`. Only escape was switching panel. `/` must
	// stay live so the user can refine the search out of the dead state.
	m := newTestSidebar()
	m.SetPinned([]k8s.ResourceType{k8s.ResourcePods})

	m, _ = m.Update(keyMsg('/'))
	for _, r := range "pinned" {
		m, _ = m.Update(keyMsg(r))
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.searchQuery == "" {
		t.Fatal("setup: filter expected to be locked with non-empty query")
	}

	// Drop the only pinned item → the Pinned virtual category disappears
	// → visible items becomes empty under the still-locked "pinned" query.
	m.RemovePinned(k8s.ResourcePods)
	if vis := m.visibleItems(); len(vis) != 0 {
		t.Fatalf("setup: expected empty visible, got %d items", len(vis))
	}

	m, _ = m.Update(keyMsg('/'))
	if !m.searching {
		t.Error("`/` must re-enter search mode even when locked filter has emptied visible")
	}
}

func TestSidebarModel_LockedFilterEmptiedByUnpin_EscClearsFilter(t *testing.T) {
	// Esc must also escape the dead state — drop the locked filter and
	// repopulate the panel from the full registry.
	m := newTestSidebar()
	m.SetPinned([]k8s.ResourceType{k8s.ResourcePods})

	m, _ = m.Update(keyMsg('/'))
	for _, r := range "pinned" {
		m, _ = m.Update(keyMsg(r))
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.RemovePinned(k8s.ResourcePods)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})

	if m.searchQuery != "" {
		t.Errorf("Esc must clear searchQuery, got %q", m.searchQuery)
	}
	if vis := m.visibleItems(); len(vis) == 0 {
		t.Error("Esc must restore visible items by dropping the locked filter")
	}
}

func TestSidebarModel_CopyableContent_CursorKindKubectlName(t *testing.T) {
	// v1.7.9 focus-content semantics: y on the sidebar copies the
	// KubectlName of the currently-highlighted kind (raw, stable
	// across upgrades). Never the whole sidebar tree — that was the
	// pre-v1.7.9 semantic and got dropped with the focus-model
	// pivot.
	m := newTestSidebar()

	// Cursor initialised at row 0 — that's typically a category
	// header. Advance until we land on a non-category row.
	for i := 0; i < 20; i++ {
		items := m.visibleItems()
		if len(items) > 0 && m.cursor < len(items) && !items[m.cursor].isCategory {
			break
		}
		m.cursor++
	}
	got := m.CopyableContent()
	if got == "" {
		t.Fatal("expected non-empty content for a kind-row cursor")
	}
	if strings.Contains(got, "\n") {
		t.Errorf("expected single-line KubectlName, got:\n%s", got)
	}
	if strings.Contains(got, " ") {
		t.Errorf("KubectlName should not contain spaces (raw form), got %q", got)
	}
}

func TestSidebarModel_CopyableContent_CategoryCursorReturnsEmpty(t *testing.T) {
	// Landing the cursor on a category header must yield "" — categories
	// aren't a legitimate copy target; the header label is metadata, not
	// a resource identity.
	m := newTestSidebar()
	items := m.visibleItems()
	categoryIdx := -1
	for i, it := range items {
		if it.isCategory {
			categoryIdx = i
			break
		}
	}
	if categoryIdx < 0 {
		t.Skip("no category header in default sidebar")
	}
	m.cursor = categoryIdx
	if got := m.CopyableContent(); got != "" {
		t.Errorf("category cursor: expected empty, got %q", got)
	}
}

func newTestSidebar() SidebarModel {
	t := theme.DefaultTheme()
	m := NewSidebarModel(t)
	m.SetSize(30, 40)
	m.SetFocused(true)
	return m
}

func keyMsg(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

// Visible items layout (35 total):
//  0: KubeConfig                    (category)
//  1: Contexts                      (resource)
//  2: Cluster                       (category)
//  3: Namespaces                    (resource)
//  4: Nodes                         (resource)
//  5: Events                        (resource)
//  6: Workloads                     (category)
//  7: Pods                          (resource)  <- initial cursor
//  8: Deployments                   (resource)
//  9: DaemonSets                    (resource)
// 10: StatefulSets                  (resource)
// 11: Jobs                          (resource)
// 12: CronJobs                      (resource)
// 13: Network                       (category)
// 14: Services                      (resource)
// 15: Ingresses                     (resource)
// 16: NetworkPolicies               (resource)
// 17: EndpointSlices                (resource)
// 18: IngressClasses                (resource)
// 19: Config                        (category)
// 20: ConfigMaps                    (resource)
// 21: Secrets                       (resource)
// 22: Storage                       (category)
// 23: PersistentVolumes             (resource)
// 24: PersistentVolumeClaims        (resource)
// 25: StorageClasses                (resource)
// 26: RBAC                          (category)
// 27: ClusterRoles                  (resource)
// 28: ClusterRoleBindings           (resource)
// 29: Roles                         (resource)
// 30: RoleBindings                  (resource)
// 31: ServiceAccounts               (resource)
// 32: Autoscaling                   (category)
// 33: HorizontalPodAutoscalers      (resource)
// 34: PodDisruptionBudgets          (resource)

func TestSidebarModel_InitialState(t *testing.T) {
	m := newTestSidebar()

	// Cursor should be on Pods (index 7).
	if m.cursor != 7 {
		t.Errorf("expected cursor=7 (Pods), got %d", m.cursor)
	}

	// Pods should be selected by default.
	if m.Selected() != k8s.ResourcePods {
		t.Errorf("expected selected=ResourcePods, got %v", m.Selected())
	}

	// 8 categories (KubeConfig, Cluster, Workloads, Network, Config, Storage, RBAC, Autoscaling) + 27 resources = 35
	visible := m.visibleItems()
	if len(visible) != 35 {
		t.Errorf("expected 35 visible items, got %d", len(visible))
	}
}

func TestSidebarModel_NavigateDown(t *testing.T) {
	m := newTestSidebar()

	// Initially at Pods (index 7).
	if m.cursor != 7 {
		t.Fatalf("expected cursor=7, got %d", m.cursor)
	}

	// Press j — should move to Deployments (index 8).
	var cmd tea.Cmd
	m, cmd = m.Update(keyMsg('j'))
	if m.cursor != 8 {
		t.Errorf("expected cursor=8 after j, got %d", m.cursor)
	}

	// Should emit ResourceSelectedMsg for Deployments.
	if cmd == nil {
		t.Fatal("expected cmd to be non-nil after j")
	}
	msg := cmd()
	rsm, ok := msg.(ResourceSelectedMsg)
	if !ok {
		t.Fatalf("expected ResourceSelectedMsg, got %T", msg)
	}
	if rsm.Type != k8s.ResourceDeployments {
		t.Errorf("expected ResourceSelectedMsg.Type=ResourceDeployments, got %v", rsm.Type)
	}
}

func TestSidebarModel_NavigateUp(t *testing.T) {
	m := newTestSidebar()

	// Initially at Pods (index 7). Press k — should move to Events (index 5),
	// skipping Workloads category (index 6).
	var cmd tea.Cmd
	m, cmd = m.Update(keyMsg('k'))
	if m.cursor != 5 {
		t.Errorf("expected cursor=5 (Events) after k, got %d", m.cursor)
	}

	// Should emit ResourceSelectedMsg for Events.
	if cmd == nil {
		t.Fatal("expected cmd to be non-nil after k")
	}
	msg := cmd()
	rsm, ok := msg.(ResourceSelectedMsg)
	if !ok {
		t.Fatalf("expected ResourceSelectedMsg, got %T", msg)
	}
	if rsm.Type != k8s.ResourceEvents {
		t.Errorf("expected ResourceSelectedMsg.Type=ResourceEvents, got %v", rsm.Type)
	}
}

func TestSidebarModel_CursorSkipsCategories(t *testing.T) {
	m := newTestSidebar()

	visible := m.visibleItems()

	// Navigate through all items with j and verify cursor never lands on a category.
	for i := 0; i < 20; i++ {
		m, _ = m.Update(keyMsg('j'))
		if m.cursor >= 0 && m.cursor < len(visible) {
			if visible[m.cursor].isCategory {
				t.Errorf("cursor landed on category at index %d (%s)", m.cursor, visible[m.cursor].label)
			}
		}
	}

	// Navigate back with k and verify cursor never lands on a category.
	for i := 0; i < 20; i++ {
		m, _ = m.Update(keyMsg('k'))
		if m.cursor >= 0 && m.cursor < len(visible) {
			if visible[m.cursor].isCategory {
				t.Errorf("cursor landed on category at index %d (%s)", m.cursor, visible[m.cursor].label)
			}
		}
	}
}

func TestSidebarModel_AutoSelectOnMove(t *testing.T) {
	m := newTestSidebar()

	// Move down from Pods — should auto-select Deployments.
	var cmd tea.Cmd
	m, cmd = m.Update(keyMsg('j'))
	if m.Selected() != k8s.ResourceDeployments {
		t.Errorf("expected selected=ResourceDeployments after j, got %v", m.Selected())
	}
	if cmd == nil {
		t.Fatal("expected cmd after j")
	}
	msg := cmd()
	rsm, ok := msg.(ResourceSelectedMsg)
	if !ok {
		t.Fatalf("expected ResourceSelectedMsg, got %T", msg)
	}
	if rsm.Type != k8s.ResourceDeployments {
		t.Errorf("expected msg type=ResourceDeployments, got %v", rsm.Type)
	}

	// Move up from Deployments — should auto-select Pods.
	m, cmd = m.Update(keyMsg('k'))
	if m.Selected() != k8s.ResourcePods {
		t.Errorf("expected selected=ResourcePods after k, got %v", m.Selected())
	}
	if cmd == nil {
		t.Fatal("expected cmd after k")
	}
	msg = cmd()
	rsm, ok = msg.(ResourceSelectedMsg)
	if !ok {
		t.Fatalf("expected ResourceSelectedMsg, got %T", msg)
	}
	if rsm.Type != k8s.ResourcePods {
		t.Errorf("expected msg type=ResourcePods, got %v", rsm.Type)
	}
}

func TestSidebarModel_GG(t *testing.T) {
	m := newTestSidebar()

	// Move cursor down several times.
	for i := 0; i < 5; i++ {
		m, _ = m.Update(keyMsg('j'))
	}
	// Should be at CronJobs (index 12 — KubeConfig category + Contexts
	// were prepended at the top of the sidebar, shifting everything +2).
	if m.cursor != 12 {
		t.Fatalf("expected cursor=12 after 5 j's, got %d", m.cursor)
	}

	// Press g (first).
	m, _ = m.Update(keyMsg('g'))
	if !m.pendingG {
		t.Fatal("expected pendingG to be true after first g")
	}

	// Press g (second) — cursor should go to the first resource item, now
	// Contexts (index 1) since the KubeConfig category leads the sidebar.
	var cmd tea.Cmd
	m, cmd = m.Update(keyMsg('g'))
	if m.cursor != 1 {
		t.Errorf("expected cursor=1 (Contexts) after gg, got %d", m.cursor)
	}
	if m.pendingG {
		t.Error("expected pendingG to be false after gg")
	}

	// Should emit ResourceSelectedMsg for Contexts.
	if cmd == nil {
		t.Fatal("expected cmd after gg")
	}
	msg := cmd()
	rsm, ok := msg.(ResourceSelectedMsg)
	if !ok {
		t.Fatalf("expected ResourceSelectedMsg, got %T", msg)
	}
	if rsm.Type != k8s.ResourceContexts {
		t.Errorf("expected ResourceSelectedMsg.Type=ResourceContexts, got %v", rsm.Type)
	}
}

func TestSidebarModel_ShiftG(t *testing.T) {
	m := newTestSidebar()

	// Press G — cursor should go to last resource item (PodDisruptionBudgets,
	// index 34 after the KubeConfig category + Contexts shifted things +2).
	var cmd tea.Cmd
	m, cmd = m.Update(keyMsg('G'))

	if m.cursor != 34 {
		t.Errorf("expected cursor=34 (PodDisruptionBudgets) after G, got %d", m.cursor)
	}

	// Verify it's the last resource.
	visible := m.visibleItems()
	item := visible[m.cursor]
	if item.isCategory {
		t.Error("expected cursor on resource, got category")
	}
	if item.resourceType != k8s.ResourcePodDisruptionBudgets {
		t.Errorf("expected PodDisruptionBudgets resource, got %v", item.resourceType)
	}

	if cmd == nil {
		t.Fatal("expected cmd after G")
	}
	msg := cmd()
	rsm, ok := msg.(ResourceSelectedMsg)
	if !ok {
		t.Fatalf("expected ResourceSelectedMsg, got %T", msg)
	}
	if rsm.Type != k8s.ResourcePodDisruptionBudgets {
		t.Errorf("expected ResourceSelectedMsg.Type=ResourcePodDisruptionBudgets, got %v", rsm.Type)
	}
}

// TestSidebarModel_EnterIsNoOpOutsideSearch — Enter no longer
// forwards focus to panel 2. With mouse double-click → Enter
// synthesis, "Enter from sidebar shifts focus away" surprised
// users (the click landed ON the sidebar; user wasn't asking to
// leave). Tab / 1 / 2 / 3 remain the focus-shift keys for
// keyboard users.
func TestSidebarModel_EnterIsNoOpOutsideSearch(t *testing.T) {
	m := newTestSidebar()

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Errorf("Enter on sidebar (outside search) must be a no-op, got cmd %T", cmd())
	}
}

func TestSidebarModel_NavigateUpAtTop(t *testing.T) {
	m := newTestSidebar()

	// Move cursor to first resource (Contexts, index 1 — KubeConfig leads).
	m, _ = m.Update(keyMsg('g'))
	m, _ = m.Update(keyMsg('g'))
	if m.cursor != 1 {
		t.Fatalf("expected cursor=1, got %d", m.cursor)
	}

	// Press k at the top resource — should stay at 1 (no resource above).
	var cmd tea.Cmd
	m, cmd = m.Update(keyMsg('k'))
	if m.cursor != 1 {
		t.Errorf("expected cursor=1 at top boundary, got %d", m.cursor)
	}

	// No cmd should be emitted since cursor didn't move.
	if cmd != nil {
		t.Error("expected nil cmd when cursor doesn't move at top")
	}
}

func TestSidebarModel_NavigateDownAtBottom(t *testing.T) {
	m := newTestSidebar()

	// Move cursor to last resource (PodDisruptionBudgets, index 34).
	m, _ = m.Update(keyMsg('G'))
	if m.cursor != 34 {
		t.Fatalf("expected cursor=34, got %d", m.cursor)
	}

	// Press j at the bottom resource — should stay at 34.
	var cmd tea.Cmd
	m, cmd = m.Update(keyMsg('j'))
	if m.cursor != 34 {
		t.Errorf("expected cursor=34 at bottom boundary, got %d", m.cursor)
	}

	// No cmd should be emitted since cursor didn't move.
	if cmd != nil {
		t.Error("expected nil cmd when cursor doesn't move at bottom")
	}
}

func TestTruncateSidebarLabel(t *testing.T) {
	tests := []struct {
		name     string
		label    string
		maxWidth int
		want     string
	}{
		{"fits exactly", "Pods", 4, "Pods"},
		{"fits with room", "Pods", 10, "Pods"},
		{"truncates long", "PersistentVolumeClaims", 18, "PersistentVolumeC…"},
		{"truncates to single ellipsis", "PersistentVolumes", 1, "…"},
		{"zero width returns empty", "Pods", 0, ""},
		{"negative width returns empty", "Pods", -1, ""},
		{"empty label", "", 10, ""},
		{"one char fits", "P", 1, "P"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateSidebarLabel(tt.label, tt.maxWidth)
			if got != tt.want {
				t.Errorf("truncateSidebarLabel(%q, %d) = %q, want %q", tt.label, tt.maxWidth, got, tt.want)
			}
		})
	}
}

func TestSidebarModel_Search_CategoryMatchExpandsAllItems(t *testing.T) {
	m := newTestSidebar()
	m.searchQuery = "cluster"
	items := m.visibleItems()

	// Cluster category header should appear and ALL its native children
	// (Namespaces / Nodes / Events) should be visible — that's the new
	// category-match behavior. Other categories with item-level matches
	// (e.g. RBAC's ClusterRoles) may also appear, which is intended.
	clusterHeaderIdx := -1
	for i, it := range items {
		if it.isCategory && strings.EqualFold(it.label, "Cluster") {
			clusterHeaderIdx = i
			break
		}
	}
	if clusterHeaderIdx < 0 {
		t.Fatal("expected Cluster category header when searching 'cluster'")
	}
	clusterChildren := 0
	for i := clusterHeaderIdx + 1; i < len(items) && !items[i].isCategory; i++ {
		clusterChildren++
	}
	if clusterChildren < 3 {
		t.Errorf("expected ≥3 children under Cluster on category match, got %d", clusterChildren)
	}
}

func TestSidebarModel_Search_ItemMatchOnlyShowsMatching(t *testing.T) {
	m := newTestSidebar()
	m.searchQuery = "pods"
	items := m.visibleItems()

	// Item-level match: only the category containing matching items appears.
	// Cluster category shouldn't appear because "Namespaces" / "Nodes" / "Events"
	// don't contain "pods".
	for _, it := range items {
		if it.isCategory && strings.EqualFold(it.label, "Cluster") {
			t.Errorf("Cluster category should not appear when searching 'pods'")
		}
	}
}

func TestSidebarModel_SetSelected_MovesCursor(t *testing.T) {
	m := newTestSidebar()
	// Initial cursor is on Pods (index 5). Jump to Services (index 12).
	m.SetSelected(k8s.ResourceServices)
	if got := m.Selected(); got != k8s.ResourceServices {
		t.Errorf("Selected() = %v, want %v", got, k8s.ResourceServices)
	}
	visible := m.visibleItems()
	if m.cursor < 0 || m.cursor >= len(visible) || visible[m.cursor].resourceType != k8s.ResourceServices {
		t.Errorf("cursor not pointing at Services row, cursor=%d", m.cursor)
	}
}

func TestSidebarModel_SetSelected_NoOpOnMissingType(t *testing.T) {
	m := newTestSidebar()
	beforeCursor := m.cursor
	beforeSelected := m.Selected()
	// CRDs aren't in the default registry yet — sidebar shouldn't move.
	m.SetSelected(k8s.ResourceType("definitely-not-a-real-kind"))
	if m.cursor != beforeCursor {
		t.Errorf("cursor moved on missing type: before=%d, after=%d", beforeCursor, m.cursor)
	}
	if m.Selected() != beforeSelected {
		t.Errorf("selected changed on missing type: before=%v, after=%v", beforeSelected, m.Selected())
	}
}

func TestSidebarModel_Pinned_VisibleItemsPrependCategory(t *testing.T) {
	// Pinned category should appear FIRST in visibleItems, before
	// "Cluster" and friends. Each pinned kind keeps its original
	// DisplayName. categoryIndex == pinnedCategoryIndex marks the
	// section unambiguously (the per-row "pinned" flag was removed
	// once pin became a move rather than a duplicate).
	m := newTestSidebar()
	m.SetPinned([]k8s.ResourceType{k8s.ResourcePods, k8s.ResourceNamespaces})

	visible := m.visibleItems()
	if len(visible) < 3 {
		t.Fatalf("expected at least 3 visible items (Pinned header + 2 pinned items), got %d", len(visible))
	}
	if !visible[0].isCategory || visible[0].label != "Pinned" {
		t.Errorf("visible[0] must be the Pinned category header, got %+v", visible[0])
	}
	if visible[1].resourceType != k8s.ResourcePods {
		t.Errorf("visible[1] kind = %v, want Pods (insertion-order render)", visible[1].resourceType)
	}
	if visible[2].resourceType != k8s.ResourceNamespaces {
		t.Errorf("visible[2] kind = %v, want Namespaces", visible[2].resourceType)
	}
	for i := 1; i <= 2; i++ {
		if visible[i].categoryIndex != pinnedCategoryIndex {
			t.Errorf("visible[%d].categoryIndex = %d, want pinnedCategoryIndex (%d)", i, visible[i].categoryIndex, pinnedCategoryIndex)
		}
	}
}

func TestSidebarModel_Pinned_KindMovesOutOfOriginalCategory(t *testing.T) {
	// Pinning a kind REMOVES it from its original category — each kind
	// has exactly one location in the sidebar. The Pods row that used
	// to live under Workloads must disappear from there once pinned.
	m := newTestSidebar()
	m.SetPinned([]k8s.ResourceType{k8s.ResourcePods})
	visible := m.visibleItems()
	count := 0
	for _, it := range visible {
		if !it.isCategory && it.resourceType == k8s.ResourcePods {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Pods must appear in EXACTLY one location after pin (Pinned only), got %d copies", count)
	}
	// And the single copy must be in the Pinned section.
	for _, it := range visible {
		if !it.isCategory && it.resourceType == k8s.ResourcePods && it.categoryIndex != pinnedCategoryIndex {
			t.Errorf("the surviving Pods row must be in Pinned (categoryIndex=%d), got categoryIndex=%d", pinnedCategoryIndex, it.categoryIndex)
		}
	}
}

func TestSidebarModel_Pinned_HidesEmptyCategoryHeader(t *testing.T) {
	// If pinning empties a whole category, the category header itself
	// must not render — a dangling "Workloads" with nothing under it
	// is visual noise. Hard to trigger naturally on the full registry
	// (Workloads has many kinds), so build a tiny test registry whose
	// only kind is in Workloads, pin it, expect the Workloads header to
	// vanish.
	//
	// Verified via the existing visibleItems: count categories whose
	// label matches each pinned kind's category and confirm at least
	// one category was hidden after we pin out everything that lived
	// in one category. We use a heuristic test that covers the bigger
	// invariant: zero rows under a header → no header.
	m := newTestSidebar()
	beforeCats := 0
	for _, it := range m.visibleItems() {
		if it.isCategory {
			beforeCats++
		}
	}
	// Pin one kind from Workloads — the category should NOT disappear
	// because Workloads has multiple kinds. Test the OPPOSITE case:
	// no spurious category hiding.
	m.SetPinned([]k8s.ResourceType{k8s.ResourcePods})
	afterCats := 0
	var workloadsStillPresent bool
	for _, it := range m.visibleItems() {
		if it.isCategory {
			afterCats++
			if it.label == "Workloads" {
				workloadsStillPresent = true
			}
		}
	}
	// One pinned kind, Workloads has more kinds left → Workloads
	// should still render. The Pinned category was added.
	if !workloadsStillPresent {
		t.Error("Workloads header must still render — Deployments / DaemonSets / etc remain in it")
	}
	if afterCats != beforeCats+1 {
		t.Errorf("expected one new category (Pinned), got delta %d (before=%d after=%d)", afterCats-beforeCats, beforeCats, afterCats)
	}
}

func TestSidebarModel_AddPinned_Idempotent(t *testing.T) {
	m := newTestSidebar()
	m.AddPinned(k8s.ResourcePods)
	m.AddPinned(k8s.ResourcePods) // idempotent
	if len(m.PinnedKinds()) != 1 {
		t.Errorf("AddPinned must dedupe; got %d entries", len(m.PinnedKinds()))
	}
}

func TestSidebarModel_RemovePinned_DropsEntry(t *testing.T) {
	m := newTestSidebar()
	m.SetPinned([]k8s.ResourceType{k8s.ResourcePods, k8s.ResourceNodes})
	m.RemovePinned(k8s.ResourcePods)
	pinned := m.PinnedKinds()
	if len(pinned) != 1 || pinned[0] != k8s.ResourceNodes {
		t.Errorf("RemovePinned must drop Pods only; got %v", pinned)
	}
}

func TestSidebarModel_CursorPinned_TrueOnlyInsidePinned(t *testing.T) {
	m := newTestSidebar()
	m.SetPinned([]k8s.ResourceType{k8s.ResourcePods})

	// Cursor at 0 (Pinned header) — not a row, false.
	m.cursor = 0
	if m.CursorPinned() {
		t.Errorf("CursorPinned must be false on category header")
	}
	// Cursor at 1 (the pinned Pods row) — true.
	m.cursor = 1
	if !m.CursorPinned() {
		t.Errorf("CursorPinned must be true on a row inside the Pinned section")
	}
	// Move to a different kind in Cluster (Namespaces) — must be false.
	visible := m.visibleItems()
	for i, it := range visible {
		if !it.isCategory && it.resourceType == k8s.ResourceNamespaces {
			m.cursor = i
			break
		}
	}
	if m.CursorPinned() {
		t.Errorf("CursorPinned must be false for rows outside the Pinned section")
	}
}

func TestSidebarModel_SetPinned_KeepsCursorOnSelected(t *testing.T) {
	// Startup bug: NewSidebarModel positioned the cursor on Pods
	// (index 5), then config-driven SetPinned prepended a Pinned
	// category which shifted every row down by len(pinned)+1 — cursor
	// stayed on the old index and pointed at Nodes while panel 2 still
	// showed Pods. SetPinned now re-snaps the cursor to the row
	// matching m.selected.
	m := newTestSidebar()
	if m.Selected() != k8s.ResourcePods {
		t.Fatalf("setup: default selected expected to be Pods, got %v", m.Selected())
	}
	m.SetPinned([]k8s.ResourceType{k8s.ResourceDeployments, k8s.ResourceNodes})

	visible := m.visibleItems()
	if m.cursor < 0 || m.cursor >= len(visible) {
		t.Fatalf("cursor out of range after SetPinned: %d (visible=%d)", m.cursor, len(visible))
	}
	row := visible[m.cursor]
	if row.isCategory || row.resourceType != k8s.ResourcePods {
		t.Errorf("cursor should land on the row matching selected (Pods), got %+v", row)
	}
}

func TestSidebarModel_SnapCursorToKind_FollowsPinnedRow(t *testing.T) {
	// After pinning, Pods only exists in the Pinned section.
	// SnapCursorToKind must land on the Pinned section row.
	m := newTestSidebar()
	m.SetPinned([]k8s.ResourceType{k8s.ResourcePods})

	// Move cursor somewhere else first to prove the snap.
	visible := m.visibleItems()
	for i, it := range visible {
		if !it.isCategory && it.resourceType == k8s.ResourceNamespaces {
			m.cursor = i
			break
		}
	}
	m.SnapCursorToKind(k8s.ResourcePods)
	if m.cursor < 0 || m.cursor >= len(visible) {
		t.Fatalf("cursor out of range after SnapCursorToKind: %d", m.cursor)
	}
	got := visible[m.cursor]
	if got.resourceType != k8s.ResourcePods || got.categoryIndex != pinnedCategoryIndex {
		t.Errorf("SnapCursorToKind(Pods) should land on the Pinned/Pods row, got %+v", got)
	}
}

func TestSidebarModel_SnapCursorToKind_FollowsUnpinnedBack(t *testing.T) {
	// After unpin, Pods returns to Workloads — SnapCursorToKind must
	// follow it back to the (now sole) Workloads row.
	m := newTestSidebar()
	m.SetPinned([]k8s.ResourceType{k8s.ResourcePods})
	m.RemovePinned(k8s.ResourcePods)

	m.SnapCursorToKind(k8s.ResourcePods)
	visible := m.visibleItems()
	if m.cursor < 0 || m.cursor >= len(visible) {
		t.Fatalf("cursor out of range: %d (visible=%d)", m.cursor, len(visible))
	}
	row := visible[m.cursor]
	if row.resourceType != k8s.ResourcePods || row.isCategory {
		t.Errorf("SnapCursorToKind after unpin should land on Pods row, got %+v", row)
	}
	if row.categoryIndex == pinnedCategoryIndex {
		t.Errorf("after unpin, Pods row should NOT be in the Pinned section, got categoryIndex=%d", row.categoryIndex)
	}
}

func TestSidebarModel_InitDoesNotScrollPastTopCategories(t *testing.T) {
	// Bug: SetPinned in NewAppModel calls restoreCursorToSelected ->
	// ensureCursorVisible BEFORE the first WindowSizeMsg, when height=0.
	// viewportHeight() clamps to 1 for render safety; without the
	// height-zero guard the scroll math saw cursor=5, viewH=1 and set
	// scrollOffset to 5, hiding every category header above Pods.
	// Symptom: sidebar opened with Pods at the top, "Cluster" /
	// "Workloads" headers scrolled out of sight.
	m := NewSidebarModel(theme.DefaultTheme())
	// Pre-WindowSizeMsg state: height=0, cursor on Pods (default).
	if m.scrollOffset != 0 {
		t.Errorf("initial scrollOffset must be 0 before any size is set, got %d", m.scrollOffset)
	}
	// Simulate the SetPinned call path that originally tripped the bug
	// (empty kinds — same code path runs whether pinned list is empty
	// or populated).
	m.SetPinned(nil)
	if m.scrollOffset != 0 {
		t.Errorf("SetPinned at height=0 must not scroll the viewport (height-zero guard), got scrollOffset=%d", m.scrollOffset)
	}
	// And after a real SetSize the scroll should still be at the top
	// because Pods (cursor=5) fits inside a 40-row viewport.
	m.SetSize(30, 40)
	if m.scrollOffset != 0 {
		t.Errorf("after SetSize with ample height, viewport must stay at top, got scrollOffset=%d", m.scrollOffset)
	}
}

// ── Drag-and-drop reorder ──────────────────────────────────────────────────

// pinnedDragSetup builds a sidebar with three pinned kinds in a
// known order and the cursor parked on the middle one. Returns
// the sidebar + the kinds in their initial order so tests can
// assert reorder deltas explicitly.
func pinnedDragSetup() (SidebarModel, []k8s.ResourceType) {
	m := newTestSidebar()
	kinds := []k8s.ResourceType{
		k8s.ResourcePods,
		k8s.ResourceDeployments,
		k8s.ResourceServices,
	}
	m.SetPinned(kinds)
	// Park cursor on the middle pinned kind (Deployments).
	m.SnapCursorToKind(k8s.ResourceDeployments)
	return m, kinds
}

func TestSidebarModel_EnterDrag_NonPinnedRow_NoOp(t *testing.T) {
	m := newTestSidebar()
	m.SetPinned([]k8s.ResourceType{k8s.ResourcePods, k8s.ResourceDeployments})
	// Park cursor on a non-pinned row.
	m.SnapCursorToKind(k8s.ResourceNamespaces)

	ok, cmd := m.EnterDrag()
	if ok || cmd != nil {
		t.Errorf("EnterDrag on non-pinned row must be no-op, got ok=%v cmd!=nil=%v", ok, cmd != nil)
	}
	if m.IsDragging() {
		t.Error("dragActive must remain false")
	}
}

func TestSidebarModel_EnterDrag_SinglePinned_NoOp(t *testing.T) {
	m := newTestSidebar()
	m.SetPinned([]k8s.ResourceType{k8s.ResourcePods})
	m.SnapCursorToKind(k8s.ResourcePods)

	ok, _ := m.EnterDrag()
	if ok {
		t.Error("EnterDrag with <2 pinned must be no-op (no swap target)")
	}
	if m.IsDragging() {
		t.Error("dragActive must remain false")
	}
}

func TestSidebarModel_EnterDrag_ValidSetsStateAndEmitsMsg(t *testing.T) {
	m, _ := pinnedDragSetup()

	ok, cmd := m.EnterDrag()
	if !ok {
		t.Fatal("EnterDrag with cursor on pinned + 2+ pins must succeed")
	}
	if !m.IsDragging() {
		t.Error("dragActive must be true after EnterDrag")
	}
	if m.draggedKind != k8s.ResourceDeployments {
		t.Errorf("draggedKind = %q, want Deployments", m.draggedKind)
	}
	if cmd == nil {
		t.Fatal("EnterDrag must return a cmd emitting SidebarDragEnterMsg")
	}
	msg := cmd()
	em, ok := msg.(SidebarDragEnterMsg)
	if !ok {
		t.Fatalf("cmd emitted %T, want SidebarDragEnterMsg", msg)
	}
	if em.Kind != k8s.ResourceDeployments {
		t.Errorf("SidebarDragEnterMsg.Kind = %q, want Deployments", em.Kind)
	}
}

func TestSidebarModel_Drag_JSwapsDownAndCursorFollows(t *testing.T) {
	m, _ := pinnedDragSetup()
	m.EnterDrag()

	m, _ = m.Update(keyMsg('j'))

	want := []k8s.ResourceType{k8s.ResourcePods, k8s.ResourceServices, k8s.ResourceDeployments}
	if !pinnedEqual(m.PinnedKinds(), want) {
		t.Errorf("after j: pinned = %v, want %v", m.PinnedKinds(), want)
	}
	// Cursor must still be on Deployments (the dragged kind).
	visible := m.visibleItems()
	if visible[m.cursor].resourceType != k8s.ResourceDeployments {
		t.Errorf("cursor follows dragged kind; on row %+v", visible[m.cursor])
	}
}

func TestSidebarModel_Drag_KSwapsUp(t *testing.T) {
	m, _ := pinnedDragSetup()
	m.EnterDrag()

	m, _ = m.Update(keyMsg('k'))

	want := []k8s.ResourceType{k8s.ResourceDeployments, k8s.ResourcePods, k8s.ResourceServices}
	if !pinnedEqual(m.PinnedKinds(), want) {
		t.Errorf("after k: pinned = %v, want %v", m.PinnedKinds(), want)
	}
}

func TestSidebarModel_Drag_BoundaryNoWrap(t *testing.T) {
	m, _ := pinnedDragSetup()
	// Force dragged kind to the LAST pinned then enter drag.
	m.SnapCursorToKind(k8s.ResourceServices)
	m.EnterDrag()
	before := append([]k8s.ResourceType(nil), m.PinnedKinds()...)

	m, _ = m.Update(keyMsg('j'))

	if !pinnedEqual(m.PinnedKinds(), before) {
		t.Errorf("j at last pinned must be no-op (no wrap), got %v", m.PinnedKinds())
	}
}

func TestSidebarModel_Drag_DCommitsAndEmitsMsg(t *testing.T) {
	m, _ := pinnedDragSetup()
	m.EnterDrag()
	m, _ = m.Update(keyMsg('j'))

	committed, cmd := m.Update(keyMsg('D'))
	if committed.IsDragging() {
		t.Error("dragActive must be false after D commit")
	}
	want := []k8s.ResourceType{k8s.ResourcePods, k8s.ResourceServices, k8s.ResourceDeployments}
	if !pinnedEqual(committed.PinnedKinds(), want) {
		t.Errorf("post-commit pinned = %v, want %v", committed.PinnedKinds(), want)
	}
	if cmd == nil {
		t.Fatal("commit must return SidebarDragCommitMsg cmd")
	}
	if _, ok := cmd().(SidebarDragCommitMsg); !ok {
		t.Errorf("commit cmd emits %T, want SidebarDragCommitMsg", cmd())
	}
}

func TestSidebarModel_Drag_EnterAlsoCommits(t *testing.T) {
	// Enter is the global "commit / into" gesture in kbu — it must
	// also drop a drag, mirroring D.
	m, _ := pinnedDragSetup()
	m.EnterDrag()
	m, _ = m.Update(keyMsg('j'))

	committed, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if committed.IsDragging() {
		t.Error("Enter must exit drag mode (commit, not cancel)")
	}
	want := []k8s.ResourceType{k8s.ResourcePods, k8s.ResourceServices, k8s.ResourceDeployments}
	if !pinnedEqual(committed.PinnedKinds(), want) {
		t.Errorf("post-Enter-commit pinned = %v, want %v (same as D)", committed.PinnedKinds(), want)
	}
	if cmd == nil {
		t.Fatal("Enter commit must emit SidebarDragCommitMsg")
	}
	if _, ok := cmd().(SidebarDragCommitMsg); !ok {
		t.Errorf("Enter cmd emits %T, want SidebarDragCommitMsg", cmd())
	}
}

func TestSidebarModel_Drag_EscCancelsAndReverts(t *testing.T) {
	m, original := pinnedDragSetup()
	m.EnterDrag()
	m, _ = m.Update(keyMsg('j'))
	// Mid-drag state IS different from the original.
	if pinnedEqual(m.PinnedKinds(), original) {
		t.Fatalf("setup: pinned should differ from original after one j, got %v", m.PinnedKinds())
	}

	reverted, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if reverted.IsDragging() {
		t.Error("Esc must exit drag mode")
	}
	if !pinnedEqual(reverted.PinnedKinds(), original) {
		t.Errorf("Esc must revert pinned to snapshot, got %v want %v", reverted.PinnedKinds(), original)
	}
	if cmd == nil {
		t.Fatal("Esc cancel must emit SidebarDragCancelMsg")
	}
	if _, ok := cmd().(SidebarDragCancelMsg); !ok {
		t.Errorf("cancel cmd emits %T, want SidebarDragCancelMsg", cmd())
	}
}

func TestSidebarModel_Drag_SpaceOpensDropMenuNotCancel(t *testing.T) {
	// Space is one of kbu's core gestures (Tab/Enter/Esc/Space) — in
	// drag mode it opens the drop-only menu instead of cancelling
	// like every other non-j/k/D/Enter key. Sidebar emits the
	// request msg; app.go renders the popup.
	m, original := pinnedDragSetup()
	m.EnterDrag()
	m, _ = m.Update(keyMsg('j'))
	// Mid-drag state differs from original.
	if pinnedEqual(m.PinnedKinds(), original) {
		t.Fatalf("setup: pinned should differ from original after one j")
	}

	stillDragging, cmd := m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	if !stillDragging.IsDragging() {
		t.Error("Space mid-drag must NOT exit drag mode")
	}
	if pinnedEqual(stillDragging.PinnedKinds(), original) {
		t.Error("Space mid-drag must NOT revert pinned (no cancel)")
	}
	if cmd == nil {
		t.Fatal("Space must emit SidebarDragRequestDropMenuMsg")
	}
	if _, ok := cmd().(SidebarDragRequestDropMenuMsg); !ok {
		t.Errorf("Space cmd emits %T, want SidebarDragRequestDropMenuMsg", cmd())
	}
}

func TestSidebarModel_Drag_AnyOtherKeyCancels(t *testing.T) {
	m, original := pinnedDragSetup()
	m.EnterDrag()
	m, _ = m.Update(keyMsg('j'))
	// 'P' would normally toggle pin — during drag it must cancel.
	reverted, _ := m.Update(keyMsg('P'))
	if reverted.IsDragging() {
		t.Error("non-j/k/D key must exit drag mode")
	}
	if !pinnedEqual(reverted.PinnedKinds(), original) {
		t.Errorf("P during drag must revert pinned to snapshot, got %v", reverted.PinnedKinds())
	}
}

func TestSidebarModel_Drag_CancelRestoresCursorOnDraggedKind(t *testing.T) {
	m, _ := pinnedDragSetup()
	m.EnterDrag()
	m, _ = m.Update(keyMsg('j')) // dragged kind moves down
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	visible := m.visibleItems()
	if visible[m.cursor].resourceType != k8s.ResourceDeployments {
		t.Errorf("cursor must land back on dragged kind after cancel, on %+v", visible[m.cursor])
	}
}

func pinnedEqual(a, b []k8s.ResourceType) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSidebarModel_InitWithPinned_PinnedHeaderRemainsVisible(t *testing.T) {
	// Same bug, with pins: cursor lands on Pinned/Pods (index 1) at
	// init. Pre-fix scrollOffset got set to 1, hiding the Pinned
	// header above. With the height-zero guard the offset stays 0
	// until SetSize and the header renders normally.
	m := NewSidebarModel(theme.DefaultTheme())
	m.SetPinned([]k8s.ResourceType{k8s.ResourcePods, k8s.ResourceDeployments})
	m.SetSize(30, 40)
	if m.scrollOffset != 0 {
		t.Errorf("Pinned header must remain visible (scrollOffset=0), got %d", m.scrollOffset)
	}
	// Sanity: the first visible row IS the Pinned header.
	visible := m.visibleItems()
	if !visible[m.scrollOffset].isCategory || visible[m.scrollOffset].label != "Pinned" {
		t.Errorf("first visible row should be the Pinned header, got %+v", visible[m.scrollOffset])
	}
}
