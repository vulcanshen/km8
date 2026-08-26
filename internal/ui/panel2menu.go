package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/vulcanshen/kbu/internal/k8s"
	"github.com/vulcanshen/kbu/internal/theme"
)

// Panel2MenuPopupModel is the per-row context menu opened by Space on a
// panel 2 resource row (everything that isn't a Helm Release — that has
// its own doc menu). Items are resource-aware:
//
//   - YAML(Y) — always available, view the resource manifest
//   - Edit(E) — if not helm-managed (Rule A read-only block)
//   - Shell(S) — if the resource type has containers
//   - Delete(D) — if not helm-managed (Rule A)
//
// Menu items display the hotkey in parens so the user can either cursor
// + Enter, or hit the letter directly. After commit (either path) the
// popup closes — menu is an ephemeral launcher, not a persistent context.
// Mirrors HelmDocMenuPopupModel's shape; chose a separate type because
// the items are dynamic per-row and the action msg is different.
type Panel2MenuPopupModel struct {
	animator PopupAnimator
	items    []panel2MenuItem
	cursor   int
	screenW  int
	theme    *theme.Theme

	// Captured at Open. A watcher tick mid-popup must not be allowed to
	// shift the action target underneath the user.
	resource    k8s.ResourceType
	item        k8s.ResourceItem
	helmManaged bool

	// Title override — set by OpenForContainer so the popup header reads
	// "container/<name>" instead of the default "pod/<name>". Empty string
	// falls back to the default rendering.
	titleOverride string

	layer       int
	borderColor lipgloss.Color
}

type panel2MenuItem struct {
	label string // "YAML" / "Edit" / "Shell" / "Delete" (rendered + "(K)")
	key   string // hotkey trigger letter "Y" / "E" / "S" / "D"
	hint  string // short description shown next to the label, helmdocmenu-style

	// separator marks a non-selectable visual divider. Other fields
	// are ignored. j/k/g/G + mouse skip past it, Enter never commits
	// it, direct hotkeys don't match it. Used to split row-targeted
	// actions from list-level / panel-level entries.
	separator bool

	// header marks a non-selectable region label rendered in dim
	// grey above the items it introduces. Same skip rules as
	// separator. Used when the menu mixes operation kinds and
	// wants to label each region (e.g. "item operation" /
	// "panel operation").
	header bool
}

// Panel2MenuActionMsg is emitted when the user commits a menu item (cursor
// + Enter or direct hotkey letter). AppModel maps the Action key to the
// same code path as the direct keypress on the underlying panel 2 row.
type Panel2MenuActionMsg struct {
	Action   string // "Y" / "E" / "S" / "D"
	Resource k8s.ResourceType
	Item     k8s.ResourceItem
}

func NewPanel2MenuPopupModel(t *theme.Theme) Panel2MenuPopupModel {
	bc := theme.PopupLayerColor(1)
	return Panel2MenuPopupModel{
		theme:       t,
		animator:    NewPopupAnimator("panel2menu", bc),
		borderColor: bc,
		layer:       1,
	}
}

// SetLayer stamps nesting depth + derives border / animator color.
func (m *Panel2MenuPopupModel) SetLayer(layer int) {
	m.layer = layer
	m.borderColor = theme.PopupLayerColor(layer)
	m.animator.Color = m.borderColor
}

// Open shows the menu for the given row. Items are computed at Open so
// the same cursor item produces a stable menu (no rebuild on rerender).
// canPop=true appends the "Esc ↖" back entry — used when the table is
// inside a drill chain (e.g. user pressed Enter on a Deployment row and
// is now viewing its Pods, so Esc pops back to the Deployment list).
// compare carries the compare-mode flags so the per-row menu can surface
// "Lock to compare" / "Compare to this resource" / "Exit compare mode"
// in the right combinations.
func (m *Panel2MenuPopupModel) Open(rt k8s.ResourceType, item k8s.ResourceItem, canPop bool, compare panel2CompareCtx) tea.Cmd {
	m.resource = rt
	m.item = item
	m.helmManaged = k8s.IsHelmManaged(item)
	m.items = buildPanel2MenuItems(rt, item, m.helmManaged, compare)
	if canPop {
		// Esc is also a menu-only entry (no single-letter hotkey) so
		// it joins the top group alongside Enter / Compare / Lock —
		// prepended rather than appended.
		m.items = append([]panel2MenuItem{{
			label: "Esc " + drillUpIcon,
			key:   "Esc",
			hint:  "back to parent list",
		}}, m.items...)
	}
	m.cursor = m.firstSelectable()
	m.titleOverride = ""
	return m.animator.Open()
}

// OpenForContainer is the variant used when the user has drilled into a Pod
// and pressed Space on a container row. Surfaces only the Shell action —
// containers aren't standalone API objects, so YAML / Edit / Delete don't
// apply. AppModel.execShell() reads m.drillDownContainers[cursor] to resolve
// which container actually gets the exec, so the Item carried here is the
// parent pod for record-keeping only.
func (m *Panel2MenuPopupModel) OpenForContainer(podName, namespace, containerName string) tea.Cmd {
	m.resource = k8s.ResourcePods
	m.item = k8s.ResourceItem{Name: podName, Namespace: namespace}
	m.helmManaged = false
	m.titleOverride = " container/" + containerName
	m.items = []panel2MenuItem{
		{label: "Shell", key: "S", hint: "kubectl exec -it"},
	}
	m.cursor = 0
	return m.animator.Open()
}

func (m *Panel2MenuPopupModel) Close() tea.Cmd     { return m.animator.Close() }
func (m *Panel2MenuPopupModel) SetSize(w, _ int)   { m.screenW = w }
func (m Panel2MenuPopupModel) IsActive() bool      { return m.animator.IsActive() }
func (m Panel2MenuPopupModel) IsInteractive() bool { return m.animator.IsInteractive() }

func (m *Panel2MenuPopupModel) HandleTick(msg AnimTickMsg) tea.Cmd {
	if msg.Target != m.animator.Target {
		return nil
	}
	return m.animator.Tick()
}

func (m Panel2MenuPopupModel) Update(msg tea.Msg) (Panel2MenuPopupModel, tea.Cmd) {
	if !m.animator.IsInteractive() {
		return m, nil
	}
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "j", "down":
		m.cursor = m.nextSelectable(m.cursor)
		return m, nil
	case "k", "up":
		m.cursor = m.prevSelectable(m.cursor)
		return m, nil
	case "g":
		m.cursor = m.firstSelectable()
		return m, nil
	case "G":
		m.cursor = m.lastSelectable()
		return m, nil
	case "enter":
		if m.cursor < 0 || m.cursor >= len(m.items) {
			return m, nil
		}
		if m.items[m.cursor].separator || m.items[m.cursor].header {
			return m, nil
		}
		return m, m.commit(m.items[m.cursor].key)
	case "Y", "E", "S", "D", "C", "alt+S":
		// Direct hotkey trigger — must match an item actually present
		// in the menu (Rule A removed Edit/Delete for helm-managed
		// rows; a hotkey shortcut shouldn't bypass that gate). C is
		// the contextual Compare hotkey — its menu entry varies
		// (Mark as anchor vs Show diff) but both bind to the same
		// letter so the muscle memory is "C = compare action for the
		// current row". Alt+Shift+S is the panel-2 Sort entry — bare
		// S is Shell so the modifier carves out a separate hotkey.
		// Unknown hotkey falls through to no-op so users don't
		// accidentally close the popup with a stray press.
		key := keyMsg.String()
		for _, it := range m.items {
			if it.key == key {
				return m, m.commit(key)
			}
		}
		return m, nil
	case "esc", " ":
		return m, m.animator.Close()
	}
	return m, nil
}

// HandleMouse routes a click against the rendered menu. Left-click
// on an item is identical to keyboard Enter on that item (commits
// the row's action + closes the popup). Right-click on the popup
// closes it (mirror of Esc). Outside-popup clicks are no-ops so an
// accidental click below the menu doesn't dismiss it.
func (m Panel2MenuPopupModel) HandleMouse(msg tea.MouseMsg, screenW, screenH int) (Panel2MenuPopupModel, tea.Cmd) {
	if !m.animator.IsInteractive() || msg.Action != tea.MouseActionPress {
		return m, nil
	}
	row := popupRowAt(m.renderFullPopup(), msg, screenW, screenH, 2, len(m.items))
	if row < 0 {
		return m, nil
	}
	switch msg.Button {
	case tea.MouseButtonLeft:
		if m.items[row].separator || m.items[row].header {
			return m, nil
		}
		m.cursor = row
		return m, m.commit(m.items[row].key)
	case tea.MouseButtonRight:
		return m, m.animator.Close()
	}
	return m, nil
}

// firstSelectable / lastSelectable / nextSelectable / prevSelectable
// mirror the listpicker helpers: cursor navigation skips separator
// items, cycles within the selectable subset. Same shape so the two
// menus stay aligned visually and behaviourally.
func (m Panel2MenuPopupModel) firstSelectable() int {
	for i, it := range m.items {
		if !it.separator && !it.header {
			return i
		}
	}
	return 0
}

func (m Panel2MenuPopupModel) lastSelectable() int {
	for i := len(m.items) - 1; i >= 0; i-- {
		if !m.items[i].separator && !m.items[i].header {
			return i
		}
	}
	if len(m.items) == 0 {
		return 0
	}
	return len(m.items) - 1
}

func (m Panel2MenuPopupModel) nextSelectable(from int) int {
	n := len(m.items)
	if n == 0 {
		return from
	}
	for step := 1; step <= n; step++ {
		idx := (from + step) % n
		if !m.items[idx].separator && !m.items[idx].header {
			return idx
		}
	}
	return from
}

func (m Panel2MenuPopupModel) prevSelectable(from int) int {
	n := len(m.items)
	if n == 0 {
		return from
	}
	for step := 1; step <= n; step++ {
		idx := (from - step + n) % n
		if !m.items[idx].separator && !m.items[idx].header {
			return idx
		}
	}
	return from
}

// commit emits the action msg WITHOUT closing the menu. The menu is
// a source popup per §1.8 popup-convention — when the action's target
// is itself a popup (confirm / yaml / pty / diff / sort picker), the
// menu must stay open underneath so Esc on the target returns the
// user here. The app's Panel2MenuActionMsg handler closes the menu
// explicitly for actions whose target is a panel-state transition
// (Enter drill-down) rather than a popup.
func (m *Panel2MenuPopupModel) commit(key string) tea.Cmd {
	resource := m.resource
	item := m.item
	return func() tea.Msg {
		return Panel2MenuActionMsg{
			Action:   key,
			Resource: resource,
			Item:     item,
		}
	}
}

func (m Panel2MenuPopupModel) View() string { return "" }

func (m Panel2MenuPopupModel) RenderPopup() string {
	return m.animator.RenderFrame(m.renderFullPopup())
}

// buildPanel2MenuItems builds the per-row menu. Three layers of gating:
//
//  1. Rule A — helm-managed resources are read-only — drops Edit and Delete.
//  2. resourceAllowsEdit / resourceAllowsDelete — opinionated dev-workflow
//     gates that hide kubectl actions which technically work but have no
//     practical purpose for a scout tool (e.g. Events can't be edited
//     meaningfully; Node deletion is admin-scope, not kbu's audience).
//  3. resourceHasContainer — Shell only on kinds with containers (Pod).
//
// Layout (top → bottom):
//   1. Row-targeted hotkey actions: Y / E / S / D / C in that order
//   2. Row-targeted navigation: Enter (drill into cursor item's
//      children) — last entry of the row-level group because it's
//      a navigation gesture rather than a kubectl-style verb, and
//      the user already has Enter on the row directly
//   3. Separator (rendered as a purple horizontal rule)
//   4. Panel-level entries: Order — operates on the list view, not
//      the row, so visually demarcated by the separator above
//
// Exit compare mode is NOT a menu entry — Esc is the single canonical
// gesture for "back out of compare", and a hint in panel 2's
// bottom-left border surfaces that affordance whenever the anchor is
// set. Earlier "Exit compare mode" entry made the menu list "two
// ways to do the same thing".
//
// Multi-char keys ("Enter") skip bracketHotkey (no false "[E]nter"
// rendering) and bypass direct-hotkey dispatch (cursor+Enter only —
// direct Enter from elsewhere in the menu commits the cursor's own
// item, not the drill entry).
func buildPanel2MenuItems(rt k8s.ResourceType, item k8s.ResourceItem, helmManaged bool, compare panel2CompareCtx) []panel2MenuItem {
	canEdit := !helmManaged && resourceAllowsEdit(rt)
	canDelete := !helmManaged && resourceAllowsDelete(rt)

	var items []panel2MenuItem

	// Compare entry — single "C" key, label switches on state so the
	// user always sees what C would do on the current row:
	//   - not set + >1 selectable items → "Mark as Compare anchor"
	//     (the anchor IS the baseline; C marks current row as it)
	//   - set + cursor on a different row of the same kind →
	//     "Compare to anchor" (C opens the diff popup)
	//   - set + cursor sits on the anchor row itself →
	//     "Unmark Compare anchor" (C cancels the anchor, exiting
	//     compare mode — same effect as Esc, surfaced here so C is a
	//     toggle from any row of the same kind)
	//   - kind switched away from anchor's kind, or single-item list:
	//     entry hidden (acting on it would be a no-op)
	//
	// Slot rule: "Compare to anchor" is surfaced FIRST in the item
	// group when in compare mode + comparable cursor — it IS what the
	// user came to do (they set the anchor, opened the menu on a
	// candidate row; burying it after YAML/Edit/Shell/Delete makes
	// the user scan past 4 unrelated row-CRUD verbs every time). The
	// "Mark" and "Unmark" sub-cases stay in the post-Delete slot:
	// Mark is offered alongside other row actions (no anchor yet,
	// user might be doing anything), Unmark is a cleanup verb on the
	// anchor row itself.
	if compare.locked && compare.cursorComparable {
		items = append(items, panel2MenuItem{
			label: "Compare to anchor",
			key:   "C",
			hint:  "open the YAML diff popup",
		})
	}

	// ── hotkey group ──
	items = append(items, panel2MenuItem{label: "YAML", key: "Y", hint: "view resource manifest"})
	if canEdit {
		items = append(items, panel2MenuItem{label: "Edit", key: "E", hint: "kubectl edit"})
	}
	if resourceHasContainer(rt) {
		items = append(items, panel2MenuItem{label: "Shell", key: "S", hint: "kubectl exec -it"})
	}
	if canDelete {
		items = append(items, panel2MenuItem{label: "Delete", key: "D", hint: "kubectl delete"})
	}
	if compare.locked && compare.cursorOnAnchor {
		items = append(items, panel2MenuItem{
			label: "Unmark Compare anchor",
			key:   "C",
			hint:  "cancel anchor, exit compare mode",
		})
	} else if !compare.locked && compare.canLock {
		items = append(items, panel2MenuItem{
			label: "Mark as Compare anchor",
			key:   "C",
			hint:  "set this row as the diff baseline",
		})
	}

	// Enter (drill) — last entry in the row-targeted group. Acts on
	// the cursor's item (drills into ITS children), so it sits
	// ABOVE the separator with the other per-row actions.
	if rt.SupportsDrillDown() {
		target := panel2DrillLabel(rt, item)
		hint := "drill into " + target
		if target == "" {
			hint = "drill into children"
		}
		items = append(items, panel2MenuItem{
			label: "Enter " + drillDownIcon,
			key:   "Enter",
			hint:  hint,
		})
	}

	// Sort entry — list-level (not row-level) operation. Sits BELOW
	// a separator that visually demarcates "this acts on the panel,
	// not on the row." Opens the same column picker the panel-1
	// sidebar Space menu's Sort entry opens.
	//
	// Hotkey is Alt+Shift+S (rendered "[Alt-S]ort panel 2 list")
	// because bare S on panel 2 is already Shell — the modifier
	// carves out room for sort without breaking the existing Shell
	// muscle memory. Label is pre-composed with literal "[Alt-S]"
	// marker (same chord-bracket convention the bottom statusline
	// uses for [Alt-t]erm); bracketHotkey is bypassed for multi-key
	// chords (len(key) > 1) so the marker passes through unchanged.
	//
	// When Sort is present the menu has two operation regions, so
	// we prepend "item operation" + append "panel operation" labels
	// so each region reads as a labelled group. Without Sort the
	// menu stays flat (single region, no header to keep visually
	// quiet).
	if def := k8s.DefaultRegistry.Get(rt); def != nil && len(def.Columns) > 0 {
		items = append([]panel2MenuItem{{header: true, label: "item operation"}}, items...)
		items = append(items, panel2MenuItem{separator: true})
		items = append(items, panel2MenuItem{header: true, label: "panel operation"})
		items = append(items, panel2MenuItem{
			label: "[Alt-S]ort panel 2 list",
			key:   "alt+S",
			hint:  "open sort column picker",
		})
	}
	return items
}

// panel2CompareCtx carries the compare-mode flags into menu construction.
// Held as a struct (rather than 3 bool args) so the call site reads more
// purposefully — "the menu cares about compare context", not "three
// random bool flags".
//
//   - locked:           AppModel.inCompareMode()
//   - canLock:          len(panel-2 items) > 1 (single-item lists hide
//     the Lock entry — locking with no alternative
//     target is a dead end)
//   - cursorComparable: cursor row is a different UID from the locked
//     row AND same resource type. False when cursor
//     is on the locked row itself, or when locked
//     item is from a different (now-switched) kind.
//   - cursorOnAnchor:   cursor sits on the anchor row itself. Distinct
//     from cursorComparable=false because the
//     kind-mismatch case is also "not comparable" but
//     shouldn't surface the Unmark entry — Unmark only
//     makes sense when the user is looking at the
//     anchor.
type panel2CompareCtx struct {
	locked           bool
	canLock          bool
	cursorComparable bool
	cursorOnAnchor   bool
}

// panel2DrillLabel returns the human-readable plural for what Enter drills
// into. Pod → "containers" (special-cased: containers aren't a K8s API
// resource, just a Pod sub-component). Other kinds resolve via registry's
// ChildTypeFor + KubectlName + "s".
func panel2DrillLabel(rt k8s.ResourceType, item k8s.ResourceItem) string {
	if rt == k8s.ResourcePods {
		return "containers"
	}
	def := k8s.DefaultRegistry.Get(rt)
	if def == nil || def.DrillDown == nil {
		return ""
	}
	childType := def.DrillDown.ChildTypeFor(item)
	if childType == "" {
		return ""
	}
	return childType.KubectlName() + "s"
}

// resourceAllowsEdit returns false for kinds where `kubectl edit` is
// technically allowed but has no dev-workflow value. Currently only
// Events — they're system-generated immutable records.
func resourceAllowsEdit(rt k8s.ResourceType) bool {
	// Contexts are a read-only kubeconfig view, not a cluster object —
	// no editable surface.
	return rt != k8s.ResourceEvents && rt != k8s.ResourceContexts
}

// resourceAllowsDelete returns false for kinds where `kubectl delete` is
// blocked by kbu's scout-tool stance. Events (no point — they're
// system-generated immutable records), Nodes (admin infra action, not
// kbu's audience).
//
// Namespaces is intentionally NOT blocked here — the cascading destruction
// of every resource in the namespace makes it the single most dangerous
// delete kbu exposes, but blocking it entirely also makes kbu useless
// for "kubectl delete ns test-XYZ" cleanup which IS a normal dev-workflow
// action. The tradeoff resolves through the confirm popup: the delete-
// triggering paths (d hotkey + Space menu Delete) build a stronger
// warning message for the Namespace kind via deleteConfirmSurface —
// "will remove ALL resources in it" — so the user cannot fat-finger
// past a generic "delete resource?" prompt when the target is a ns.
func resourceAllowsDelete(rt k8s.ResourceType) bool {
	switch rt {
	case k8s.ResourceEvents, k8s.ResourceNodes, k8s.ResourceContexts:
		return false
	}
	return true
}

// bracketHotkey wraps the hotkey letter inside the label with square
// brackets (vim-help convention). "YAML" + "Y" → "[Y]AML",
// "Unpin Pods" + "P" → "Un[P]in Pods". The bracketed letter is rendered
// in the key's casing (always uppercase since hotkeys are case-
// sensitive uppercase) — without this, "Unpin"'s lowercase p inside
// the label would render "Un[p]in" while the actual hotkey is Shift+P,
// which reads as a key-mismatch hint to users. Falls back to the
// unmodified label when the hotkey isn't a substring (case-insensitive
// match) or when the key is multi-character (e.g. "Enter" — bracketing
// would be misleading since Enter isn't a single-letter shortcut),
// preserving label readability over hint correctness.
func bracketHotkey(label, key string) string {
	if label == "" || key == "" || len(key) > 1 {
		return label
	}
	upperLabel := strings.ToUpper(label)
	upperKey := strings.ToUpper(key)
	idx := strings.Index(upperLabel, upperKey)
	if idx < 0 {
		return label
	}
	return label[:idx] + "[" + upperKey + "]" + label[idx+1:]
}

// resourceHasContainer returns true for kinds where `kubectl exec` is
// directly meaningful on the row. Currently only Pod — Deployment / STS /
// DS / Job / CronJob require a pod-selection step that execShell doesn't
// yet support. Users wanting a shell into a Deployment's pod drill in
// (Enter on the row) to the pod list, then S there.
func resourceHasContainer(rt k8s.ResourceType) bool {
	return rt == k8s.ResourcePods
}

func (m Panel2MenuPopupModel) renderFullPopup() string {
	bc := m.borderColor
	bStyle := lipgloss.NewStyle().Foreground(bc)
	tStyle := lipgloss.NewStyle().Foreground(bc).Bold(true)
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7f849c"))
	cursorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#1e1e2e")).Background(bc).Bold(true)

	// Title icon: helm glyph when cursor row is helm-managed (so the
	// popup signals its provenance the same way the row's helm column
	// does), else the default popup glyph.
	icon := ""
	if m.helmManaged {
		icon = k8s.HelmIcon()
	}
	title := icon + " " + m.resource.KubectlName() + "/" + m.item.Name
	if m.titleOverride != "" {
		title = m.titleOverride
	}
	hint := " j/k: move  Space: close "

	// Width: pick widest of title / bottom hint / rows; clamp to 85% screen.
	// Row shape: "   [K]rest   hint " — constant 2-space gutter, no leading
	// arrow; cursor row uses reverse-highlight alone (matches listpicker
	// + helmdocmenu after their unification). Hotkey gets vim-help-style
	// bracketing; no separate color since [] already marks the key.
	maxInnerW := 60
	if m.screenW > 0 {
		maxInnerW = m.screenW * 85 / 100
		if maxInnerW < 40 {
			maxInnerW = 40
		}
	}
	innerW := lipgloss.Width(title) + 4
	if w := lipgloss.Width(hint) + 4; w > innerW {
		innerW = w
	}
	// Calc must match render's gap formula (max(2, 16-labelW)) — using a
	// fixed gap=4 here used to under-count innerW and cause hint text to
	// punch through the right border.
	for _, it := range m.items {
		labelDisplay := bracketHotkey(it.label, it.key)
		labelW := lipgloss.Width(labelDisplay)
		gap := max(2, 16-labelW)
		w := 1 + 2 + labelW + gap + lipgloss.Width(it.hint) + 1
		if w > innerW {
			innerW = w
		}
	}
	if innerW > maxInnerW {
		innerW = maxInnerW
	}

	// Constant 2-space gutter on every row — cursor row is differentiated
	// by reverse-highlight alone (no leading arrow), so non-cursor rows
	// align cleanly without a phantom marker column.
	const gutter = "  "
	headerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7f849c"))
	var rows []string
	for i, it := range m.items {
		if it.header {
			// Dim grey region label, indented by the gutter so it
			// reads as a section heading above the items below.
			rows = append(rows, " "+gutter+headerStyle.Render(it.label))
			continue
		}
		if it.separator {
			// Same purple horizontal rule the hintpopup uses to
			// split its action region from the cheatsheet below —
			// keeps every kbu popup's internal divider visually
			// consistent.
			rows = append(rows, bStyle.Render(strings.Repeat("─", innerW)))
			continue
		}
		isCursor := i == m.cursor
		labelDisplay := bracketHotkey(it.label, it.key)
		labelW := lipgloss.Width(labelDisplay)
		gap := strings.Repeat(" ", max(2, 16-labelW))
		bodyPlain := " " + gutter + labelDisplay + gap + it.hint
		padW := innerW - 1 - lipgloss.Width(bodyPlain)
		if padW < 0 {
			padW = 0
		}
		pad := strings.Repeat(" ", padW)
		if isCursor {
			rows = append(rows, cursorStyle.Render(bodyPlain+pad))
			continue
		}
		// Non-cursor: label keeps default color, hint dimmed.
		rows = append(rows,
			" "+gutter+labelDisplay+gap+hintStyle.Render(it.hint)+pad)
	}

	dashesAfter := innerW - 1 - lipgloss.Width(title)
	if dashesAfter < 0 {
		dashesAfter = 0
	}

	var b strings.Builder
	b.WriteString(bStyle.Render("╭─") + tStyle.Render(title) + bStyle.Render(strings.Repeat("─", dashesAfter)+"╮") + "\n")
	left := bStyle.Render("│")
	right := bStyle.Render("│")
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
	bottomDashes := innerW - lipgloss.Width(hint) - 1
	if bottomDashes < 0 {
		bottomDashes = 0
	}
	b.WriteString(bStyle.Render("╰─") + tStyle.Render(hint) + bStyle.Render(strings.Repeat("─", bottomDashes)+"╯"))
	return b.String()
}
