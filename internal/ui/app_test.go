package ui

import (
	"os"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hinshun/vt10x"

	"github.com/vulcanshen/kbu/internal/config"
	"github.com/vulcanshen/kbu/internal/k8s"
	"github.com/vulcanshen/kbu/internal/theme"
)

// unsetEnvForTest clears `key` for the test's lifetime, restoring the
// previous state (set-to-original OR unset) on cleanup. Use this for
// the precedence tests that need to PROVE a var is absent — t.Setenv
// only handles the "set to X" case, not the "guaranteed-unset" case.
//
// Previously these tests used `orig := os.Getenv(key); defer
// os.Setenv(key, orig)`, which polluted a runner where `key` started
// unset (orig = "" → defer sets key=""). The pollution only surfaces
// for code paths that distinguish unset from empty (os.LookupEnv),
// so the build kept passing — but the trap was real.
func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()
	orig, had := os.LookupEnv(key)
	os.Unsetenv(key)
	t.Cleanup(func() {
		if had {
			os.Setenv(key, orig)
		} else {
			os.Unsetenv(key)
		}
	})
}

// isolateAltermEnv unsets every env var that buildShellTerminalCmd reads
// so a test can prove its own env value drives the result. Covers both
// tiers: the current $KBU__ALTERM_* names and the v2.0 legacy $KM8__ALTERM_*
// fallbacks.
func isolateAltermEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"KBU__ALTERM_SHELL", "KBU__ALTERM_LOGIN_SHELL",
		"KM8__ALTERM_SHELL", "KM8__ALTERM_LOGIN_SHELL",
	} {
		unsetEnvForTest(t, k)
	}
}

func TestBuildShellTerminalCmd_UsesShellEnv(t *testing.T) {
	isolateAltermEnv(t)
	t.Setenv("SHELL", "/usr/bin/fish")

	cmd := buildShellTerminalCmd("", false)
	if cmd == nil {
		t.Fatal("buildShellTerminalCmd returned nil")
	}
	if cmd.Args[0] != "/usr/bin/fish" {
		t.Errorf("expected /usr/bin/fish, got %q", cmd.Args[0])
	}
	if len(cmd.Args) != 1 {
		t.Errorf("expected no extra args (non-login interactive), got %v", cmd.Args)
	}
}

func TestBuildShellTerminalCmd_FallbackWhenShellUnset(t *testing.T) {
	isolateAltermEnv(t)
	unsetEnvForTest(t, "SHELL")

	cmd := buildShellTerminalCmd("", false)
	if cmd.Args[0] != "/bin/sh" {
		t.Errorf("expected /bin/sh fallback, got %q", cmd.Args[0])
	}
}

func TestBuildShellTerminalCmd_ConfigOverridesShellEnv(t *testing.T) {
	// alterm_shell config wins over $SHELL — the user wants fish inside
	// alterm but their host shell is still zsh.
	isolateAltermEnv(t)
	t.Setenv("SHELL", "/bin/zsh")

	cmd := buildShellTerminalCmd("/opt/homebrew/bin/fish", false)
	if cmd.Args[0] != "/opt/homebrew/bin/fish" {
		t.Errorf("expected config shell to win over $SHELL, got %q", cmd.Args[0])
	}
}

func TestBuildShellTerminalCmd_KBUAltermShellOverridesEverything(t *testing.T) {
	// $KBU__ALTERM_SHELL is the top of the precedence stack: it beats
	// both the alterm_shell config AND $SHELL. Use case: one-shot
	// `KBU__ALTERM_SHELL=/bin/bash kbu` to test a different shell without
	// touching the persisted config.
	isolateAltermEnv(t)
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("KBU__ALTERM_SHELL", "/bin/bash")

	cmd := buildShellTerminalCmd("/opt/homebrew/bin/fish", false)
	if cmd.Args[0] != "/bin/bash" {
		t.Errorf("expected $KBU__ALTERM_SHELL to win over config and $SHELL, got %q", cmd.Args[0])
	}
}

func TestBuildShellTerminalCmd_LegacyKM8AltermShellFallback(t *testing.T) {
	// v2.0 rename: $KM8__ALTERM_SHELL still honored when $KBU__ALTERM_SHELL
	// isn't set. Fallback ordering keeps existing shell rc / launchctl
	// plists working through the transition; EnvDeprecations surfaces the
	// nudge to rename.
	isolateAltermEnv(t)
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("KM8__ALTERM_SHELL", "/bin/bash")

	cmd := buildShellTerminalCmd("/opt/homebrew/bin/fish", false)
	if cmd.Args[0] != "/bin/bash" {
		t.Errorf("expected legacy $KM8__ALTERM_SHELL to win when new not set, got %q", cmd.Args[0])
	}
}

func TestBuildShellTerminalCmd_KBUWinsOverLegacyKM8(t *testing.T) {
	// Both set: KBU__ALTERM_SHELL wins, KM8__ALTERM_SHELL silently ignored
	// as the migration target.
	isolateAltermEnv(t)
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("KM8__ALTERM_SHELL", "/bin/dash")
	t.Setenv("KBU__ALTERM_SHELL", "/bin/bash")

	cmd := buildShellTerminalCmd("", false)
	if cmd.Args[0] != "/bin/bash" {
		t.Errorf("expected KBU__ to win over KM8__, got %q", cmd.Args[0])
	}
}

func TestBuildShellTerminalCmd_TrimsWhitespaceFromEnv(t *testing.T) {
	// Leading / trailing whitespace from a copy-pasted env value or a
	// sourced .env file would previously slip past the empty-check and
	// reach exec.Command verbatim → LookPath ENOENT, Alterm refuses to
	// open with no obvious reason. TrimSpace closes the trap.
	isolateAltermEnv(t)
	t.Setenv("KBU__ALTERM_SHELL", "  /opt/homebrew/bin/fish\t")

	cmd := buildShellTerminalCmd("", false)
	if cmd.Args[0] != "/opt/homebrew/bin/fish" {
		t.Errorf("expected whitespace stripped from $KBU__ALTERM_SHELL, got %q", cmd.Args[0])
	}
}

func TestBuildShellTerminalCmd_LoginConfigAppendsDashEll(t *testing.T) {
	// alterm_login_shell: true should spawn the shell with `-l`,
	// matching what the v1.7.1 baseline did unconditionally.
	isolateAltermEnv(t)
	t.Setenv("SHELL", "/bin/zsh")

	cmd := buildShellTerminalCmd("", true)
	if len(cmd.Args) != 2 || cmd.Args[1] != "-l" {
		t.Errorf("expected [shell -l] args, got %v", cmd.Args)
	}
}

func TestBuildShellTerminalCmd_LoginEnvOverridesConfig(t *testing.T) {
	// $KBU__ALTERM_LOGIN_SHELL=true forces login even when config says false.
	// Use case: ad-hoc rescue when launched from a non-login parent.
	isolateAltermEnv(t)
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("KBU__ALTERM_LOGIN_SHELL", "true")

	cmd := buildShellTerminalCmd("", false)
	if len(cmd.Args) != 2 || cmd.Args[1] != "-l" {
		t.Errorf("expected $KBU__ALTERM_LOGIN_SHELL=true to force login mode, got %v", cmd.Args)
	}
}

func TestBuildShellTerminalCmd_LoginEnvFalseOverridesConfig(t *testing.T) {
	// $KBU__ALTERM_LOGIN_SHELL="false" DISABLES login mode even when
	// config opted in.
	isolateAltermEnv(t)
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("KBU__ALTERM_LOGIN_SHELL", "false")

	cmd := buildShellTerminalCmd("", true)
	if len(cmd.Args) != 1 {
		t.Errorf("expected $KBU__ALTERM_LOGIN_SHELL=false to override cfgLogin=true, got %v", cmd.Args)
	}
}

func TestBuildShellTerminalCmd_LegacyKM8LoginFallback(t *testing.T) {
	// v2.0 rename fallback: $KM8__ALTERM_LOGIN_SHELL still read when
	// $KBU__ALTERM_LOGIN_SHELL isn't set.
	isolateAltermEnv(t)
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("KM8__ALTERM_LOGIN_SHELL", "true")

	cmd := buildShellTerminalCmd("", false)
	if len(cmd.Args) != 2 || cmd.Args[1] != "-l" {
		t.Errorf("expected legacy $KM8__ALTERM_LOGIN_SHELL=true to force login mode, got %v", cmd.Args)
	}
}

func TestTerminalTitle_PrefixAndSuffix(t *testing.T) {
	title := terminalTitle()
	if !strings.Contains(title, "Alterm") {
		t.Errorf("title must contain 'Alterm' marker, got %q", title)
	}
	// We deliberately pass mDNS suffixes through (`.local`, `.home`, ...)
	// because the user wants the raw hostname. No suffix assertion.
}

func TestDeleteConfirmSurface_Namespace(t *testing.T) {
	// Namespace deletes cascade — the confirm popup MUST explicitly
	// call out that ALL resources inside will be removed. A generic
	// "delete resource?" prompt would understate the blast radius and
	// invite a fat-finger disaster. Detail line matches kubectl's
	// cluster-scoped call (no -n flag).
	item := k8s.ResourceItem{Name: "test-ns"}
	msg, detail := deleteConfirmSurface(k8s.ResourceNamespaces, item)
	if !strings.Contains(msg, "namespace") {
		t.Errorf("message must mention namespace, got %q", msg)
	}
	if !strings.Contains(msg, "test-ns") {
		t.Errorf("message must include the target name for eyeball verification, got %q", msg)
	}
	if !strings.Contains(msg, "ALL resources") {
		t.Errorf("message must warn about cascading destruction, got %q", msg)
	}
	if detail != "kubectl delete namespace test-ns" {
		t.Errorf("detail line for ns must match cluster-scoped kubectl call, got %q", detail)
	}
}

func TestDeleteConfirmSurface_NamespacedResource(t *testing.T) {
	// Non-Namespace kinds keep the shared warning text + include the
	// -n <ns> flag so the popup detail mirrors deleteResource's actual
	// kubectl invocation.
	item := k8s.ResourceItem{Name: "web-abc123", Namespace: "default"}
	msg, detail := deleteConfirmSurface(k8s.ResourcePods, item)
	if strings.Contains(msg, "namespace") {
		t.Errorf("non-ns delete must not falsely warn about ns cascade, got %q", msg)
	}
	if detail != "kubectl delete pod web-abc123 -n default" {
		t.Errorf("detail must include -n flag for namespaced kinds, got %q", detail)
	}
}

func TestDeleteConfirmSurface_ClusterScopedNonNamespace(t *testing.T) {
	// ClusterRoles / CRDs / PVs / other cluster-scoped kinds: shared
	// warning text (no Namespace-cascade language) + no -n flag.
	item := k8s.ResourceItem{Name: "admin"} // Namespace empty
	msg, detail := deleteConfirmSurface(k8s.ResourceClusterRoles, item)
	if strings.Contains(msg, "ALL resources") {
		t.Errorf("non-ns cluster-scoped delete must not carry the ns cascade warning, got %q", msg)
	}
	if strings.Contains(detail, "-n") {
		t.Errorf("cluster-scoped detail must NOT include -n flag, got %q", detail)
	}
	if detail != "kubectl delete clusterrole admin" {
		t.Errorf("expected 'kubectl delete clusterrole admin', got %q", detail)
	}
}

func TestResourceAllowsDelete_NamespaceOptIn(t *testing.T) {
	// Namespaces was previously blocked from delete entirely; opening
	// the door requires the stronger confirm surface (see the tests
	// above). Guard against a regression re-adding Namespaces to the
	// block list — that would silently hide the ns delete flow.
	if !resourceAllowsDelete(k8s.ResourceNamespaces) {
		t.Error("Namespaces delete must be allowed (gated by strong confirm, not blocked outright)")
	}
	// Events / Nodes stay blocked — sanity check the rest of the
	// allow-list wasn't affected.
	if resourceAllowsDelete(k8s.ResourceEvents) {
		t.Error("Events delete must remain blocked (system-generated records)")
	}
	if resourceAllowsDelete(k8s.ResourceNodes) {
		t.Error("Nodes delete must remain blocked (admin infra action)")
	}
}

func TestBuildSessionState_PopulatesFromCursor(t *testing.T) {
	items := []k8s.ResourceItem{
		{Name: "svc-a", Namespace: "ns-a"},
		{Name: "svc-b", Namespace: "ns-b"},
	}
	s := buildSessionState("orbstack", k8s.SelectedNamespaces("kube-system"), k8s.ResourceServices, items, 1)
	if s.Context != "orbstack" {
		t.Errorf("Context = %q, want orbstack", s.Context)
	}
	if len(s.Namespaces) != 1 || s.Namespaces[0] != "kube-system" {
		t.Errorf("Namespaces = %v, want [kube-system]", s.Namespaces)
	}
	if s.Kind != k8s.ResourceServices.KubectlName() {
		t.Errorf("Kind = %q, want %q", s.Kind, k8s.ResourceServices.KubectlName())
	}
	if s.ObjectName != "svc-b" || s.ObjectNamespace != "ns-b" {
		t.Errorf("Object = %s/%s, want ns-b/svc-b", s.ObjectNamespace, s.ObjectName)
	}
}

func TestBuildSessionState_CursorOutOfRangeLeavesObjectEmpty(t *testing.T) {
	items := []k8s.ResourceItem{{Name: "svc-a", Namespace: "ns-a"}}

	cases := []struct {
		name   string
		cursor int
	}{
		{"negative cursor", -1},
		{"empty items", 0}, // items has 1, cursor 0 IS in range — flipped below
		{"cursor past end", 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			its := items
			if tc.name == "empty items" {
				its = nil
			}
			s := buildSessionState("ctx", k8s.AllNamespaces(), k8s.ResourcePods, its, tc.cursor)
			if s.ObjectName != "" || s.ObjectNamespace != "" {
				t.Errorf("expected empty object, got %s/%s", s.ObjectNamespace, s.ObjectName)
			}
			// Rest of state still populated — quit with no row selected is a
			// legitimate case (empty namespace, fresh cluster).
			if s.Context != "ctx" || s.Kind != k8s.ResourcePods.KubectlName() {
				t.Errorf("non-object fields missing: %+v", *s)
			}
		})
	}
}

// appWithItems builds a minimal AppModel for currentItemUID + ResourceDetailMsg
// testing. NewAppModel needs a real k8s.Client; for these tests we only need
// the items list, table cursor, and a detail model — wire those by hand.
func appWithItems(items []k8s.ResourceItem, cursor int) AppModel {
	th := theme.DefaultTheme()
	tbl := NewTableModel(th)
	rows := make([][]string, len(items))
	for i, it := range items {
		rows[i] = it.Row
	}
	tbl.SetRows(rows)
	tbl.cursor = cursor
	d := NewDetailModel(th)
	d.SetResourceType(k8s.ResourcePods)
	return AppModel{
		items:  items,
		table:  tbl,
		detail: d,
		theme:  th,
		// cfg is non-nil to match production NewAppModel — every other
		// m.cfg reader in app.go nil-guards, but the ctrl+t handler
		// (line ~1539) reads m.cfg.AltermShell/AltermLoginShell raw.
		// A test that dispatches Ctrl+T against an appWithItems-built
		// model would NPE without this; matching the production
		// invariant is cleaner than gating the hot-path handler.
		cfg:             &config.Config{},
		shellPty:        NewPtyView("ptyview_shell"),
		txPty:           NewPtyView("ptyview_tx"),
		toast:           NewToastModel(th),
		breadcrumbPopup: NewBreadcrumbPopupModel(th),
	}
}

func TestAppModel_CurrentItemUID(t *testing.T) {
	items := []k8s.ResourceItem{
		{Name: "a", UID: "uid-a", Row: []string{"a"}},
		{Name: "b", UID: "uid-b", Row: []string{"b"}},
		{Name: "c", UID: "uid-c", Row: []string{"c"}},
	}

	cases := []struct {
		name    string
		items   []k8s.ResourceItem
		cursor  int
		wantUID string
	}{
		{"first row", items, 0, "uid-a"},
		{"middle row", items, 1, "uid-b"},
		{"last row", items, 2, "uid-c"},
		{"empty list", nil, 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := appWithItems(tc.items, tc.cursor)
			if got := m.currentItemUID(); got != tc.wantUID {
				t.Errorf("currentItemUID() = %q, want %q", got, tc.wantUID)
			}
		})
	}
}

// TestAppModel_ResourceDetailMsg_DropsStale verifies the UID guard rejects
// a fetch result whose ItemUID doesn't match the currently selected row.
// Without this, a slow EnrichRelatives (e.g. ClusterRole's two cluster-wide
// List calls) finishing after the user moved on would overwrite the
// freshly displayed detail.
func TestAppModel_ResourceDetailMsg_DropsStale(t *testing.T) {
	items := []k8s.ResourceItem{
		{Name: "a", UID: "uid-a", Row: []string{"a"}},
		{Name: "b", UID: "uid-b", Row: []string{"b"}},
	}
	m := appWithItems(items, 1) // cursor on "b"

	// Stale: msg for "a" while cursor is on "b" — must be dropped.
	stale := ResourceDetailMsg{
		ItemUID: "uid-a",
		Detail:  k8s.ResourceDetail{Name: "a", Kind: "Pod"},
	}
	updated, _ := m.Update(stale)
	got := updated.(AppModel)
	if got.detail.hasData {
		t.Errorf("stale msg must not populate detail")
	}

	// Fresh: msg for the currently selected row — applies.
	fresh := ResourceDetailMsg{
		ItemUID: "uid-b",
		Detail:  k8s.ResourceDetail{Name: "b", Kind: "Pod"},
	}
	updated2, _ := got.Update(fresh)
	got2 := updated2.(AppModel)
	if !got2.detail.hasData {
		t.Errorf("matching msg must populate detail")
	}
}

// TestAppModel_RowSelectedMsg_DebouncesViaSeq pins the rowSwitchTickMsg
// debounce: every RowSelectedMsg bumps m.rowSeq + schedules a tick. A
// stale tick (seq < current m.rowSeq) MUST drop early — without that
// guard, rapid j/k mashing would fire one fetchResourceDetail + one
// logStreamer.Start per row instead of just for the row the user
// settled on, hammering the API server and orphaning N-1 short-lived
// stream goroutines. ConfigMaps is used so the default case in the
// tick handler skips the log-stream Start (which would NPE on the
// nil-clientset LogStreamer); Stop in the immediate-dispatch path is
// safe with a nil clientset.
func TestAppModel_RowSelectedMsg_DebouncesViaSeq(t *testing.T) {
	items := []k8s.ResourceItem{
		{Name: "a", UID: "uid-a", Row: []string{"a"}},
		{Name: "b", UID: "uid-b", Row: []string{"b"}},
		{Name: "c", UID: "uid-c", Row: []string{"c"}},
	}
	m := appWithItems(items, 0)
	m.currentResource = k8s.ResourceConfigMaps
	m.logStreamer = k8s.NewLogStreamer(nil)

	for _, idx := range []int{0, 1, 2} {
		next, cmd := m.Update(RowSelectedMsg{Index: idx})
		m = next.(AppModel)
		if cmd == nil {
			t.Fatalf("RowSelectedMsg{Index:%d}: expected tea.Cmd (tick scheduled), got nil", idx)
		}
	}
	if m.rowSeq != 3 {
		t.Errorf("rowSeq = %d, want 3 (one bump per RowSelectedMsg)", m.rowSeq)
	}

	// Stale tick — seq=1 against m.rowSeq=3 — must drop. fetchResourceDetail
	// would NPE on the nil k8sClient, so reaching it is the test failure.
	staleTick := rowSwitchTickMsg{
		seq:  1,
		kind: k8s.ResourceConfigMaps,
		item: items[0],
	}
	next, cmd := m.Update(staleTick)
	m = next.(AppModel)
	if cmd != nil {
		t.Errorf("stale rowSwitchTickMsg must return nil Cmd, got %v", cmd)
	}
}

// TestAppModel_LinkPushMsg_CycleBlocked verifies that drilling into a
// resource already on the chain is blocked with a toast — prevents
// infinite-loop navigation like Pod -> ConfigMap -> Pod (same pod).
func TestAppModel_LinkPushMsg_CycleBlocked(t *testing.T) {
	items := []k8s.ResourceItem{
		{Name: "nginx-x", UID: "uid-pod", Namespace: "default", Row: []string{"nginx-x"}},
	}
	m := appWithItems(items, 0)
	m.currentResource = k8s.ResourcePods
	// Seed root detail so RootRef returns a meaningful ref.
	m.detail.SetDetail(k8s.ResourceDetail{Name: "nginx-x", Namespace: "default", Kind: "Pod"}, nil)

	// Drilling into the SAME root pod is a cycle.
	cycleMsg := RelativePushMsg{Ref: k8s.RefTarget{Type: k8s.ResourcePods, Name: "nginx-x", Namespace: "default"}}
	updated, cmd := m.Update(cycleMsg)
	got := updated.(AppModel)
	if got.detail.Depth() != 1 {
		t.Errorf("cycle should not push a frame, depth=%d", got.detail.Depth())
	}
	if cmd == nil {
		t.Fatal("cycle should return a toast Cmd")
	}
	// Execute the Cmd to verify it's the toast (toast.Show returns a Cmd).
	if msg := cmd(); msg == nil {
		t.Errorf("cycle toast Cmd returned nil")
	}
}

// TestAppModel_linkDrillFetchedMsg_PushesFrame verifies that a successful
// drill fetch lands on detail.drillStack and depth increases.
func TestAppModel_linkDrillFetchedMsg_PushesFrame(t *testing.T) {
	items := []k8s.ResourceItem{
		{Name: "nginx-x", UID: "uid-pod", Namespace: "default", Row: []string{"nginx-x"}},
	}
	m := appWithItems(items, 0)
	m.detail.SetDetail(k8s.ResourceDetail{Name: "nginx-x", Namespace: "default", Kind: "Pod"}, nil)

	msg := relativeDrillFetchedMsg{
		ref:       k8s.RefTarget{Type: k8s.ResourceDeployments, Name: "nginx", Namespace: "default"},
		sourceUID: "uid-pod",
		item:      k8s.ResourceItem{Name: "nginx", Namespace: "default", UID: "uid-dep"},
		detail:    k8s.ResourceDetail{Name: "nginx", Namespace: "default", Kind: "Deployment"},
	}
	updated, _ := m.Update(msg)
	got := updated.(AppModel)
	if got.detail.Depth() != 2 {
		t.Errorf("expected depth 2 after push, got %d", got.detail.Depth())
	}
}

// TestAppModel_linkDrillFetchedMsg_StaleDrop verifies that drill results
// whose sourceUID doesn't match the currently selected table row get
// dropped (user moved on while the fetch was in flight).
func TestAppModel_linkDrillFetchedMsg_StaleDrop(t *testing.T) {
	items := []k8s.ResourceItem{
		{Name: "nginx-x", UID: "uid-pod", Namespace: "default", Row: []string{"nginx-x"}},
	}
	m := appWithItems(items, 0)
	m.detail.SetDetail(k8s.ResourceDetail{Name: "nginx-x", Namespace: "default", Kind: "Pod"}, nil)

	// sourceUID points at a different pod the user has since left.
	msg := relativeDrillFetchedMsg{
		ref:       k8s.RefTarget{Type: k8s.ResourceDeployments, Name: "nginx"},
		sourceUID: "uid-stale",
		item:      k8s.ResourceItem{Name: "nginx", UID: "uid-dep"},
		detail:    k8s.ResourceDetail{Name: "nginx", Kind: "Deployment"},
	}
	updated, _ := m.Update(msg)
	if updated.(AppModel).detail.Depth() != 1 {
		t.Errorf("stale drill result must not push, depth=%d", updated.(AppModel).detail.Depth())
	}
}

// TestAppModel_ResourceDetailMsg_DropsWhenNoSelection verifies that
// detail-fetch results arriving after the items list was cleared
// (namespace/context change) get dropped rather than populating the
// freshly cleared panel.
func TestAppModel_ResourceDetailMsg_DropsWhenNoSelection(t *testing.T) {
	m := appWithItems(nil, 0)
	msg := ResourceDetailMsg{
		ItemUID: "uid-a",
		Detail:  k8s.ResourceDetail{Name: "a", Kind: "Pod"},
	}
	updated, _ := m.Update(msg)
	if updated.(AppModel).detail.hasData {
		t.Errorf("msg with no selection must not populate detail")
	}
}

// TestAppModel_SwitchToResourceMsg_RoutesSidebarAndPending verifies the
// confirmed Relatives-tab jump: sidebar selection updates synchronously,
// pendingTableSelect captures the target ref for the next ResourceDataMsg,
// and the cmd returned re-emits ResourceSelectedMsg so the watcher restarts.
func TestAppModel_SwitchToResourceMsg_RoutesSidebarAndPending(t *testing.T) {
	m := appWithItems(nil, 0)
	m.sidebar = NewSidebarModel(theme.DefaultTheme())
	target := k8s.RefTarget{Type: k8s.ResourceServices, Name: "svc-a", Namespace: "ns-a"}

	updated, cmd := m.Update(SwitchToResourceMsg{Ref: target})
	got := updated.(AppModel)

	if got.sidebar.Selected() != k8s.ResourceServices {
		t.Errorf("sidebar.Selected() = %v, want ResourceServices", got.sidebar.Selected())
	}
	if got.pendingTableSelect == nil || *got.pendingTableSelect != target {
		t.Errorf("pendingTableSelect = %+v, want %+v", got.pendingTableSelect, target)
	}
	if cmd == nil {
		t.Fatal("expected cmd to re-emit ResourceSelectedMsg, got nil")
	}
	rsm, ok := cmd().(ResourceSelectedMsg)
	if !ok {
		t.Fatalf("cmd output type = %T, want ResourceSelectedMsg", cmd())
	}
	if rsm.Type != k8s.ResourceServices {
		t.Errorf("ResourceSelectedMsg.Type = %v, want ResourceServices", rsm.Type)
	}
}

// TestAppModel_SwitchToResourceMsg_ClearsSearchFilters guards against a
// stale search filter hiding the freshly switched-to resource. Before
// the fix, an active sidebar filter like "svc" could survive the switch
// and leave the new resource type invisible in panel 1. Detail-panel
// search was removed in the post-v1.5 panel-3 simplification, so only
// the sidebar still has search state to seed/assert.
func TestAppModel_SwitchToResourceMsg_ClearsSearchFilters(t *testing.T) {
	m := appWithItems(nil, 0)
	m.sidebar = NewSidebarModel(theme.DefaultTheme())
	m.sidebar.searchQuery = "stale-sidebar-filter"
	m.sidebar.searching = true

	target := k8s.RefTarget{Type: k8s.ResourceServices, Name: "svc-a", Namespace: "ns-a"}
	updated, _ := m.Update(SwitchToResourceMsg{Ref: target})
	got := updated.(AppModel)

	if got.sidebar.searchQuery != "" || got.sidebar.searching {
		t.Errorf("sidebar search must be cleared, got query=%q searching=%v",
			got.sidebar.searchQuery, got.sidebar.searching)
	}
	// Table search clears via the chained ResourceSelectedMsg handler —
	// not directly here. Covered by table's own ResourceSelectedMsg tests.
}

// TestAppModel_HonorPendingTableSelect_FindsRow verifies the cursor-snap
// hook used by the Relatives-tab space hotkey: when items for the matching
// kind arrive, cursor jumps to the row whose name+namespace matches and
// pending clears.
func TestAppModel_HonorPendingTableSelect_FindsRow(t *testing.T) {
	m := appWithItems(nil, 0)
	target := k8s.RefTarget{Type: k8s.ResourceServices, Name: "svc-b", Namespace: "ns-a"}
	m.pendingTableSelect = &target

	items := []k8s.ResourceItem{
		{Name: "svc-a", Namespace: "ns-a", UID: "u-a", Row: []string{"svc-a"}},
		{Name: "svc-b", Namespace: "ns-a", UID: "u-b", Row: []string{"svc-b"}},
		{Name: "svc-c", Namespace: "ns-a", UID: "u-c", Row: []string{"svc-c"}},
	}
	rows := make([][]string, len(items))
	for i, it := range items {
		rows[i] = it.Row
	}
	m.table.SetRows(rows)
	m.honorPendingTableSelect(k8s.ResourceServices, items)

	if m.table.SelectedRow() != 1 {
		t.Errorf("table cursor = %d, want 1 (svc-b)", m.table.SelectedRow())
	}
	if m.pendingTableSelect != nil {
		t.Errorf("pendingTableSelect should clear after honoring, got %+v", m.pendingTableSelect)
	}
}

// TestAppModel_HonorPendingTableSelect_MissingTargetClears verifies that
// when the requested resource isn't in the result set (different namespace
// scope, drifted away), cursor stays put and pending clears (so we don't
// keep hunting on every subsequent watcher tick).
func TestAppModel_HonorPendingTableSelect_MissingTargetClears(t *testing.T) {
	m := appWithItems(nil, 0)
	missing := k8s.RefTarget{Type: k8s.ResourceServices, Name: "not-here", Namespace: "ns-x"}
	m.pendingTableSelect = &missing

	items := []k8s.ResourceItem{
		{Name: "svc-a", Namespace: "ns-a", UID: "u-a", Row: []string{"svc-a"}},
	}
	m.table.SetRows([][]string{{"svc-a"}})
	m.honorPendingTableSelect(k8s.ResourceServices, items)

	if m.table.SelectedRow() != 0 {
		t.Errorf("missing target: cursor should stay at 0, got %d", m.table.SelectedRow())
	}
	if m.pendingTableSelect != nil {
		t.Errorf("pendingTableSelect should clear even on miss, got %+v", m.pendingTableSelect)
	}
}

// TestAppModel_HonorPendingTableSelect_KindMismatchSkips verifies that
// data arriving for an unrelated kind doesn't consume the pending
// pointer — it must survive for the correct ResourceDataMsg.
func TestAppModel_HonorPendingTableSelect_KindMismatchSkips(t *testing.T) {
	m := appWithItems(nil, 0)
	target := k8s.RefTarget{Type: k8s.ResourceServices, Name: "svc-b", Namespace: "ns-a"}
	m.pendingTableSelect = &target

	m.honorPendingTableSelect(k8s.ResourcePods, []k8s.ResourceItem{})

	if m.pendingTableSelect == nil {
		t.Error("pendingTableSelect must survive a kind-mismatched data arrival")
	}
}

// ── dual-slot PTY coexistence ──────────────────────────────────────────────

// fakeAlivePtyView mirrors hookedPtyView from ptyview_test.go — a PtyView
// that LOOKS post-Start (IsAlive/IsActive true) without spawning a real
// subprocess.
func fakeAlivePtyView(kind PtyKind, hidden bool) *PtyView {
	p := NewPtyView("ptyview_test")
	p.active = true
	p.hidden = hidden
	p.kind = kind
	p.term = vt10x.New(vt10x.WithSize(80, 24))
	p.mu = &sync.Mutex{}
	p.pendingLine = &strings.Builder{}
	return p
}

func TestAppModel_PtyExitMsg_EditKindClearsEditingFlag(t *testing.T) {
	m := appWithItems(nil, 0)
	m.editing = true
	updated, _ := m.Update(PtyExitMsg{Kind: PtyKindEdit, ExitCode: 0})
	if updated.(AppModel).editing {
		t.Error("PtyExitMsg{Kind:Edit} must clear editing flag")
	}
}

func TestAppModel_PtyExitMsg_ShellKindLeavesEditingFlag(t *testing.T) {
	// Alterm exit should NOT touch editing — dual-slot independence.
	m := appWithItems(nil, 0)
	m.editing = true
	updated, _ := m.Update(PtyExitMsg{Kind: PtyKindShell, ExitCode: 0})
	if !updated.(AppModel).editing {
		t.Error("PtyExitMsg{Kind:Shell} must NOT clear editing flag")
	}
}

func TestAppModel_PtyExitMsg_ExecKindLeavesEditingFlag(t *testing.T) {
	// Same independence for the exec case.
	m := appWithItems(nil, 0)
	m.editing = true
	updated, _ := m.Update(PtyExitMsg{Kind: PtyKindExec, ExitCode: 0})
	if !updated.(AppModel).editing {
		t.Error("PtyExitMsg{Kind:Exec} must NOT clear editing flag")
	}
}

func TestAppModel_DualSlot_AltermHidden_AllowsExecSpawn(t *testing.T) {
	// The bug fix: with shellPty alive + hidden, startShellExecMsg must
	// NOT be blocked. Old single-slot design blocked any new PTY when
	// shell was alive even if hidden — confusing UX.
	m := appWithItems(nil, 0)
	m.shellPty = fakeAlivePtyView(PtyKindShell, true)
	// Sanity: shell is alive + hidden, tx is fresh
	if !m.shellPty.IsAlive() || !m.shellPty.IsHidden() {
		t.Fatal("setup: shellPty must be alive + hidden")
	}
	if m.txPty.IsAlive() {
		t.Fatal("setup: txPty must be fresh (not alive)")
	}
	// The guard at startShellExecMsg checks m.txPty.IsAlive() — it must
	// be false right now, so the spawn is allowed to proceed (we don't
	// drive it through to actual Start, which would fork a process).
	if m.txPty.IsAlive() {
		t.Error("guard precondition broken: txPty.IsAlive should be false")
	}
}

func TestAppModel_DualSlot_TxAlive_BlocksAnotherExec(t *testing.T) {
	// Counterpart: when a transient PTY IS alive, another exec must be
	// blocked. The guard fires only on txPty, not shellPty.
	m := appWithItems(nil, 0)
	m.txPty = fakeAlivePtyView(PtyKindExec, false)
	if !m.txPty.IsAlive() {
		t.Fatal("setup: txPty must be alive")
	}
	// Send a new startShellExecMsg — handler should return a toast cmd
	// (not nil, since it returns the toast.Show cmd).
	_, cmd := m.Update(startShellExecMsg{
		podName: "x", namespace: "y", container: "z", contextName: "c",
	})
	if cmd == nil {
		t.Error("startShellExecMsg with active txPty must return a toast cmd, got nil")
	}
}

// TestStartEditMsg_PtyAlwaysLayer1 pins the §1.10 layer contract:
// PTY is a context-shift target that closes the popup tree underneath.
// UX-wise the user sees a single popup on screen when the PTY is
// visible, regardless of which chain launched it. Layer must be 1
// (lavenphire25) even though startEditMsg fires from inside
// panel2Menu → confirm — popupDepth() at that point still counts both
// (closeAll is a staged Cmd, not yet applied).
func TestStartEditMsg_PtyAlwaysLayer1(t *testing.T) {
	m := appWithItems(nil, 0)
	th := theme.DefaultTheme()
	m.panel2Menu = NewPanel2MenuPopupModel(th)
	m.confirm = NewConfirmModel(th)
	m.appLog = NewAppLogModel(th)

	// Mock the panel2Menu → confirm → Edit launch chain: both popups
	// fully open and alive when the handler fires.
	_ = m.panel2Menu.Open(k8s.ResourcePods, k8s.ResourceItem{Name: "nginx"}, false, panel2CompareCtx{})
	m.panel2Menu.animator.Finalize()
	_ = m.confirm.Show(ConfirmEdit, "Edit?", "kubectl edit pod/nginx", nil)
	m.confirm.animator.Finalize()
	if m.popupDepth() < 2 {
		t.Fatalf("setup: popupDepth must be ≥ 2 with panel2Menu + confirm open, got %d", m.popupDepth())
	}

	updated, _ := m.Update(startEditMsg{
		resource:    k8s.ResourcePods,
		item:        k8s.ResourceItem{Name: "nginx", Namespace: "default"},
		contextName: "ctx",
	})
	app := updated.(AppModel)

	if got := app.txPty.layer; got != 1 {
		t.Errorf("§1.10 PTY context-shift must always be layer 1, got %d "+
			"(popupDepth-based stamp would have produced %d)", got, m.popupDepth()+1)
	}
}

// TestStartShellExecMsg_PtyAlwaysLayer1 pins the same invariant for
// the kubectl exec path. Same launch chain as Edit, same contract.
func TestStartShellExecMsg_PtyAlwaysLayer1(t *testing.T) {
	m := appWithItems(nil, 0)
	th := theme.DefaultTheme()
	m.panel2Menu = NewPanel2MenuPopupModel(th)
	m.confirm = NewConfirmModel(th)
	m.appLog = NewAppLogModel(th)

	_ = m.panel2Menu.Open(k8s.ResourcePods, k8s.ResourceItem{Name: "nginx"}, false, panel2CompareCtx{})
	m.panel2Menu.animator.Finalize()
	_ = m.confirm.Show(ConfirmShellExec, "Shell?", "kubectl exec", nil)
	m.confirm.animator.Finalize()

	updated, _ := m.Update(startShellExecMsg{
		podName: "nginx", namespace: "default", container: "main", contextName: "ctx",
	})
	app := updated.(AppModel)

	if got := app.txPty.layer; got != 1 {
		t.Errorf("§1.10 PTY context-shift must always be layer 1, got %d", got)
	}
}

// TestAppModel_ExitDrillDownFromContainers_RowsStayHelmAligned guards the
// v1.5.5-era regression where exiting the container drill-down (Pod →
// containers → back) repopulated the table with raw item.Row instead of
// augmentRowsWithHelm. ColumnsForResource(Pods) always reserves index 1
// for the helm marker, so raw rows shifted Status one column left and
// the Pod-Status semantic-color path — which colored by the column
// whose Title=="Status" — looked at the wrong cell, killing the
// Running green until the user switched resources. The coloring
// path was removed in v1.7.x (all kinds render plain) but the
// column-alignment regression this test guards against is still
// real for any future per-column treatment, so the test stays.
func TestAppModel_ExitDrillDownFromContainers_RowsStayHelmAligned(t *testing.T) {
	// Real Pod rows are 7 cells (Name/Ready/Status/Restarts/Age/IP/Node)
	// per the Pod ResourceDefinition's Columns slice. Match that shape
	// exactly so column index 3 (Status, post-helm-augment) lines up
	// with the "Running" / "Pending" cell.
	items := []k8s.ResourceItem{
		{Name: "nginx-a", UID: "uid-a", Row: []string{"nginx-a", "1/1", "Running", "0", "5m", "10.244.0.5", "node-1"}},
		{Name: "nginx-b", UID: "uid-b", Row: []string{"nginx-b", "0/1", "Pending", "0", "1m", "<none>", "node-2"}},
	}
	m := appWithItems(items, 0)
	m.currentResource = k8s.ResourcePods
	m.statusLine = NewStatusLineModel(m.theme)
	m.logStreamer = k8s.NewLogStreamer(nil)

	// Simulate being in container view: drillDownPod set, columns swapped
	// to containerColumns (no Status column). This is the state we exit
	// from.
	pod := items[0]
	m.drillDownPod = &pod
	m.table.SetColumns(containerColumns())
	m.table.SetRows(containerRows(nil))

	_ = m.exitDrillDown() // returned Cmd is a fetch closure — never executed

	wantRows := augmentRowsWithHelm(items, k8s.ResourcePods)
	if len(m.table.rows) != len(wantRows) {
		t.Fatalf("row count = %d, want %d", len(m.table.rows), len(wantRows))
	}
	for i := range wantRows {
		if len(m.table.rows[i]) != len(wantRows[i]) {
			t.Errorf("row[%d] width = %d, want %d (raw item.Row leaked back into the table — Status column is now mis-aligned)", i, len(m.table.rows[i]), len(wantRows[i]))
		}
	}
	// Belt-and-braces: the cell under the Status column must read
	// "Running" / "Pending", not the Name. Any future per-column
	// treatment would also depend on this alignment.
	statusIdx := -1
	for i, col := range m.table.columns {
		if col.Title == "Status" {
			statusIdx = i
			break
		}
	}
	if statusIdx < 0 {
		t.Fatal("Pod columns have no Status column — test setup broken")
	}
	if m.table.rows[0][statusIdx] != "Running" {
		t.Errorf("row[0] Status cell = %q, want %q (column/row mis-alignment after exitDrillDown)", m.table.rows[0][statusIdx], "Running")
	}
}

// ── compare-mode state machine ────────────────────────────────────────────

func TestAppModel_Compare_LockLifecycle(t *testing.T) {
	// setCompareLock → inCompareMode flips on, table.lockedRow tracks the
	// item index. clearCompareLock zeroes both.
	items := []k8s.ResourceItem{
		{Name: "a", UID: "uid-a", Row: []string{"a"}},
		{Name: "b", UID: "uid-b", Row: []string{"b"}},
	}
	m := appWithItems(items, 0)
	m.currentResource = k8s.ResourcePods

	if m.inCompareMode() {
		t.Fatal("compare mode should be off initially")
	}
	m.setCompareLock(items[1], k8s.ResourcePods) // lock the second item
	if !m.inCompareMode() {
		t.Fatal("compare mode should turn on after setCompareLock")
	}
	if m.compareLock.uid != "uid-b" {
		t.Errorf("compareLock UID = %q, want uid-b", m.compareLock.uid)
	}
	if m.table.lockedRow != 1 {
		t.Errorf("table.lockedRow = %d, want 1 (item index)", m.table.lockedRow)
	}
	m.clearCompareLock()
	if m.inCompareMode() {
		t.Error("compare mode should be off after clearCompareLock")
	}
	if m.table.lockedRow != -1 {
		t.Errorf("table.lockedRow = %d, want -1 after clear", m.table.lockedRow)
	}
}

func TestAppModel_Compare_DropLockIfMissing(t *testing.T) {
	// Watcher delivers an items slice no longer containing the locked
	// UID → compare mode auto-exits and a toast Cmd is returned. The
	// status-bar marker would otherwise hang around pointing at a row
	// that no longer exists.
	items := []k8s.ResourceItem{
		{Name: "a", UID: "uid-a", Row: []string{"a"}},
		{Name: "b", UID: "uid-b", Row: []string{"b"}},
	}
	m := appWithItems(items, 0)
	m.currentResource = k8s.ResourcePods
	m.appLog = NewAppLogModel(m.theme)
	m.setCompareLock(items[1], k8s.ResourcePods)
	if !m.inCompareMode() {
		t.Fatal("setup: lock should be active")
	}

	// Simulate watcher delete: items now only has uid-a.
	newItems := []k8s.ResourceItem{items[0]}
	cmd := m.dropCompareLockIfMissing(newItems)
	if m.inCompareMode() {
		t.Error("compare mode should auto-exit when locked UID disappears")
	}
	if cmd == nil {
		t.Error("expected a toast Cmd when lock dropped, got nil")
	}
}

func TestAppModel_Compare_DropLockIfMissing_NoOpWhenPresent(t *testing.T) {
	// Locked UID still in items → no drop, no toast.
	items := []k8s.ResourceItem{
		{Name: "a", UID: "uid-a", Row: []string{"a"}},
		{Name: "b", UID: "uid-b", Row: []string{"b"}},
	}
	m := appWithItems(items, 0)
	m.currentResource = k8s.ResourcePods
	m.setCompareLock(items[0], k8s.ResourcePods)
	if cmd := m.dropCompareLockIfMissing(items); cmd != nil {
		t.Error("dropCompareLockIfMissing must be a no-op when UID still present")
	}
	if !m.inCompareMode() {
		t.Error("lock should remain active when UID still present")
	}
}

func TestAppModel_Compare_ExitOnLeavePanel2(t *testing.T) {
	// Focus moving from TablePanel to any other panel ends compare mode.
	items := []k8s.ResourceItem{
		{Name: "a", UID: "uid-a", Row: []string{"a"}},
		{Name: "b", UID: "uid-b", Row: []string{"b"}},
	}
	m := appWithItems(items, 0)
	m.currentResource = k8s.ResourcePods
	m.activePanel = TablePanel
	m.setCompareLock(items[0], k8s.ResourcePods)

	m.exitCompareOnLeave(TablePanel, SidebarPanel)
	if m.inCompareMode() {
		t.Error("compare mode must end when focus leaves panel 2 for sidebar")
	}
}

func TestAppModel_Compare_NoExitWhenLeavingOtherPanel(t *testing.T) {
	// Lock is panel-2-bound — leaving sidebar / detail does NOT touch
	// the lock (those transitions don't even happen while locked since
	// the lock would already be gone, but the guard is worth covering).
	items := []k8s.ResourceItem{{Name: "a", UID: "uid-a", Row: []string{"a"}}}
	m := appWithItems(items, 0)
	m.compareLock = &compareLockedRef{uid: "uid-a", resourceType: k8s.ResourcePods, name: "a"}

	m.exitCompareOnLeave(SidebarPanel, DetailPanel)
	if !m.inCompareMode() {
		t.Error("sidebar→detail transition must NOT clear compare lock")
	}
}

func TestAppModel_Compare_CtxForMenu(t *testing.T) {
	items := []k8s.ResourceItem{
		{Name: "a", UID: "uid-a", Row: []string{"a"}},
		{Name: "b", UID: "uid-b", Row: []string{"b"}},
	}
	cases := []struct {
		name           string
		setup          func(*AppModel)
		cursor         k8s.ResourceItem
		wantLocked     bool
		wantCanLock    bool
		wantComparable bool
		wantOnAnchor   bool
	}{
		{
			name:        "no lock, multiple items",
			setup:       func(_ *AppModel) {},
			cursor:      items[0],
			wantCanLock: true,
		},
		{
			name: "no lock, single item — cannot lock",
			setup: func(m *AppModel) {
				m.items = items[:1]
			},
			cursor:      items[0],
			wantCanLock: false,
		},
		{
			name: "locked, cursor on other row — comparable",
			setup: func(m *AppModel) {
				m.setCompareLock(items[0], k8s.ResourcePods)
			},
			cursor:         items[1],
			wantLocked:     true,
			wantCanLock:    true,
			wantComparable: true,
		},
		{
			name: "locked, cursor on locked row — onAnchor, not comparable",
			setup: func(m *AppModel) {
				m.setCompareLock(items[0], k8s.ResourcePods)
			},
			cursor:         items[0],
			wantLocked:     true,
			wantCanLock:    true,
			wantComparable: false,
			wantOnAnchor:   true,
		},
		{
			name: "locked, kind switched — neither comparable nor onAnchor",
			setup: func(m *AppModel) {
				m.setCompareLock(items[0], k8s.ResourcePods)
				m.currentResource = k8s.ResourceServices
			},
			cursor:         items[1],
			wantLocked:     true,
			wantCanLock:    true,
			wantComparable: false,
			wantOnAnchor:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := appWithItems(items, 0)
			m.currentResource = k8s.ResourcePods
			tc.setup(&m)
			got := m.compareCtxForMenu(tc.cursor)
			if got.locked != tc.wantLocked {
				t.Errorf("locked = %v, want %v", got.locked, tc.wantLocked)
			}
			if got.canLock != tc.wantCanLock {
				t.Errorf("canLock = %v, want %v", got.canLock, tc.wantCanLock)
			}
			if got.cursorComparable != tc.wantComparable {
				t.Errorf("cursorComparable = %v, want %v", got.cursorComparable, tc.wantComparable)
			}
			if got.cursorOnAnchor != tc.wantOnAnchor {
				t.Errorf("cursorOnAnchor = %v, want %v", got.cursorOnAnchor, tc.wantOnAnchor)
			}
		})
	}
}

func TestAppModel_CompareHotkeyDispatch_TogglesOnAnchorRow(t *testing.T) {
	// C on the anchor row itself = cancel anchor (exit compare mode).
	// Mirrors the menu's "Unmark Compare anchor" entry — same letter
	// from either surface flips the lock back off.
	items := []k8s.ResourceItem{
		{Name: "a", UID: "uid-a", Row: []string{"a"}},
		{Name: "b", UID: "uid-b", Row: []string{"b"}},
	}
	m := appWithItems(items, 0)
	m.currentResource = k8s.ResourcePods
	m.appLog = NewAppLogModel(m.theme)

	// Press C on row 0 → marks it as anchor.
	if cmd := m.compareHotkeyDispatch(k8s.ResourcePods, items[0]); cmd != nil {
		t.Errorf("mark anchor must not return a cmd, got %T", cmd)
	}
	if !m.inCompareMode() {
		t.Fatal("mark anchor should turn compare mode on")
	}
	if m.compareLock.uid != "uid-a" {
		t.Errorf("compareLock UID = %q, want uid-a", m.compareLock.uid)
	}

	// Press C on the SAME row → cancel anchor, exit compare mode.
	if cmd := m.compareHotkeyDispatch(k8s.ResourcePods, items[0]); cmd != nil {
		t.Errorf("unmark anchor must not return a cmd, got %T", cmd)
	}
	if m.inCompareMode() {
		t.Error("C on anchor row must cancel anchor (exit compare mode)")
	}
	if m.table.lockedRow != -1 {
		t.Errorf("table.lockedRow = %d, want -1 after cancel", m.table.lockedRow)
	}
}

// TestPanel2MenuC_MarkAnchor_ClosesMenu pins the panel-2 menu close
// behavior on "Mark as Compare anchor". The action is a pure state
// mutation (no target popup), so the menu must close so the user can
// see the resulting lavender anchor-row highlight on panel 2.
// Anti-regression for: "啟動 mark anchor 後 space menu 應該直接關閉".
func TestPanel2MenuC_MarkAnchor_ClosesMenu(t *testing.T) {
	items := []k8s.ResourceItem{
		{Name: "a", UID: "uid-a", Row: []string{"a"}},
		{Name: "b", UID: "uid-b", Row: []string{"b"}},
	}
	m := appWithItems(items, 0)
	m.currentResource = k8s.ResourcePods
	th := theme.DefaultTheme()
	m.appLog = NewAppLogModel(th)
	m.panel2Menu = NewPanel2MenuPopupModel(th)
	m.comparePopup = NewCompareYamlPopupModel(th)

	// Open the menu for row 0 — Mark-anchor branch.
	_ = m.panel2Menu.Open(k8s.ResourcePods, items[0], false, panel2CompareCtx{canLock: true})
	m.panel2Menu.animator.Finalize()
	if !m.panel2Menu.IsActive() {
		t.Fatal("setup: panel2Menu must be active before commit")
	}

	// Commit "C" via Panel2MenuActionMsg — same path the menu uses
	// when the user picks Enter on the Mark entry.
	updated, cmd := m.Update(Panel2MenuActionMsg{Action: "C", Resource: k8s.ResourcePods, Item: items[0]})
	app := updated.(AppModel)

	if !app.inCompareMode() {
		t.Error("mark anchor must set compare mode")
	}
	if cmd == nil {
		t.Fatal("Mark anchor commit must return a Cmd (the menu close)")
	}
	// Drain the close animation so we can assert the steady state.
	app.panel2Menu.animator.Finalize()
	if app.panel2Menu.IsActive() {
		t.Error("Mark anchor must close panel2 menu — state mutation surface, not §1.8 target launch")
	}
}

// TestPanel2MenuC_UnmarkAnchor_ClosesMenu pins the same close behavior
// for the unmark branch. Same shape — state mutation, no target popup,
// menu must close so the user sees the cleared lock immediately.
func TestPanel2MenuC_UnmarkAnchor_ClosesMenu(t *testing.T) {
	items := []k8s.ResourceItem{
		{Name: "a", UID: "uid-a", Row: []string{"a"}},
	}
	m := appWithItems(items, 0)
	m.currentResource = k8s.ResourcePods
	th := theme.DefaultTheme()
	m.appLog = NewAppLogModel(th)
	m.panel2Menu = NewPanel2MenuPopupModel(th)
	m.comparePopup = NewCompareYamlPopupModel(th)

	// Pre-mark so the dispatch hits the unmark branch.
	m.setCompareLock(items[0], k8s.ResourcePods)
	if !m.inCompareMode() {
		t.Fatal("setup: anchor must be set")
	}
	_ = m.panel2Menu.Open(k8s.ResourcePods, items[0], false, panel2CompareCtx{locked: true, cursorOnAnchor: true})
	m.panel2Menu.animator.Finalize()

	updated, cmd := m.Update(Panel2MenuActionMsg{Action: "C", Resource: k8s.ResourcePods, Item: items[0]})
	app := updated.(AppModel)

	if app.inCompareMode() {
		t.Error("unmark anchor must exit compare mode")
	}
	if cmd == nil {
		t.Fatal("Unmark anchor commit must return a Cmd (the menu close)")
	}
	app.panel2Menu.animator.Finalize()
	if app.panel2Menu.IsActive() {
		t.Error("Unmark anchor must close panel2 menu")
	}
}

// TestPanel2MenuC_OpenDiff_KeepsMenuOpen pins the §1.8 contract for
// the OTHER C branch — Compare-to-anchor opens the diff popup, which
// is a target launch. Menu must STAY underneath so Esc on the diff
// returns the user to the menu.
func TestPanel2MenuC_OpenDiff_KeepsMenuOpen(t *testing.T) {
	items := []k8s.ResourceItem{
		{Name: "a", UID: "uid-a", Namespace: "ns", Row: []string{"a"}},
		{Name: "b", UID: "uid-b", Namespace: "ns", Row: []string{"b"}},
	}
	// Both items need raw objects so MarshalItemYAMLForCompare doesn't
	// blow up — but for the close-or-not assertion we don't need real
	// YAML output, just that the dispatch path completes without
	// closing the menu.
	for i := range items {
		items[i].Raw = nil // dispatch path tolerates nil Raw → marshal returns empty string
	}
	m := appWithItems(items, 1)
	m.currentResource = k8s.ResourcePods
	th := theme.DefaultTheme()
	m.appLog = NewAppLogModel(th)
	m.panel2Menu = NewPanel2MenuPopupModel(th)
	m.comparePopup = NewCompareYamlPopupModel(th)

	// Pre-mark row 0 as anchor; cursor C-press will be on row 1 (a
	// different row of the same kind → open-diff branch).
	m.setCompareLock(items[0], k8s.ResourcePods)
	_ = m.panel2Menu.Open(k8s.ResourcePods, items[1], false, panel2CompareCtx{
		locked: true, cursorComparable: true,
	})
	m.panel2Menu.animator.Finalize()

	updated, _ := m.Update(Panel2MenuActionMsg{Action: "C", Resource: k8s.ResourcePods, Item: items[1]})
	app := updated.(AppModel)

	if !app.panel2Menu.IsActive() {
		t.Error("§1.8: Compare-to-anchor opens comparePopup as target; menu must STAY open underneath")
	}
}

// silence unused warning if tea is not referenced elsewhere in tests
var _ tea.Msg = struct{}{}

func TestAppModel_CloseAllBlockingPopups_NilWhenIdle(t *testing.T) {
	// §1.10 — helper must return nil (not a no-op tea.Batch wrapping
	// nothing) when no blocking popup is open. Callers rely on this
	// to keep `tea.Batch(closeAll, mainCmd)` from carrying a payload
	// when there is nothing to close.
	th := theme.DefaultTheme()
	m := AppModel{
		theme:           th,
		panel2Menu:      NewPanel2MenuPopupModel(th),
		confirm:         NewConfirmModel(th),
		help:            NewHelpModel(th),
		appLog:          NewAppLogModel(th),
		hintPopup:       NewHintPopupModel(th),
		listPicker:      NewListPickerModel(th),
		settingsPopup:   NewSettingsPopupModel(th),
		yamlPopup:       NewYamlPopupModel(th),
		comparePopup:    NewCompareYamlPopupModel(th),
		breadcrumbPopup: NewBreadcrumbPopupModel(th),
		contextPicker:   NewContextPickerModel(th),
		namespacePicker: NewNamespacePickerModel(th),
		helmDocMenu:     NewHelmDocMenuPopupModel(th),
	}
	if cmd := m.closeAllBlockingPopups(); cmd != nil {
		t.Errorf("closeAllBlockingPopups must return nil when nothing is open, got %T", cmd)
	}
}

func TestAppModel_CloseAllBlockingPopups_ClosesActiveOnes(t *testing.T) {
	// §1.10 — every active blocking popup must enter its closing
	// animation when the helper fires. Mocks the post-commit state
	// where panel2Menu launched a confirm: both popups are active,
	// then a context-shift target (e.g. PTY) enters via its handler
	// and the helper takes both down.
	th := theme.DefaultTheme()
	m := AppModel{
		theme:           th,
		panel2Menu:      NewPanel2MenuPopupModel(th),
		confirm:         NewConfirmModel(th),
		help:            NewHelpModel(th),
		appLog:          NewAppLogModel(th),
		hintPopup:       NewHintPopupModel(th),
		listPicker:      NewListPickerModel(th),
		settingsPopup:   NewSettingsPopupModel(th),
		yamlPopup:       NewYamlPopupModel(th),
		comparePopup:    NewCompareYamlPopupModel(th),
		breadcrumbPopup: NewBreadcrumbPopupModel(th),
		contextPicker:   NewContextPickerModel(th),
		namespacePicker: NewNamespacePickerModel(th),
		helmDocMenu:     NewHelmDocMenuPopupModel(th),
	}
	// Open panel2Menu + confirm; drain animations to fully open.
	_ = m.panel2Menu.Open(k8s.ResourcePods, k8s.ResourceItem{Name: "nginx"}, false, panel2CompareCtx{})
	m.panel2Menu.animator.Finalize()
	_ = m.confirm.Show(ConfirmEdit, "Edit?", "kubectl edit pod/nginx", nil)
	m.confirm.animator.Finalize()
	if !m.panel2Menu.IsActive() || !m.confirm.IsActive() {
		t.Fatalf("setup: both popups must be active, got panel2Menu=%v confirm=%v",
			m.panel2Menu.IsActive(), m.confirm.IsActive())
	}

	cmd := m.closeAllBlockingPopups()
	if cmd == nil {
		t.Fatal("closeAllBlockingPopups must return a Cmd when popups are open")
	}
	// Helper triggers the close animation; finalize fast-forwards
	// past it so we can assert the post-animation steady state.
	m.panel2Menu.animator.Finalize()
	m.confirm.animator.Finalize()
	if m.panel2Menu.IsActive() {
		t.Error("§1.10: panel2Menu must be closed after helper + finalize")
	}
	if m.confirm.IsActive() {
		t.Error("§1.10: confirm must be closed after helper + finalize")
	}
}

func TestAppModel_EscOnPanel2WithCompareMode_ClearsLockAndPopsDrill(t *testing.T) {
	// Panel 2 Esc with compare mode active AND a drill chain in
	// place: ONE Esc must both release the compare lock AND pop one
	// drill level. Without this combined handling Esc would either
	// silently drop one of the actions or force the user to press
	// Esc twice — both confusing relative to every other Esc in kbu.
	items := []k8s.ResourceItem{
		{Name: "a", UID: "uid-a", Row: []string{"a"}},
		{Name: "b", UID: "uid-b", Row: []string{"b"}},
	}
	m := appWithItems(items, 0)
	m.currentResource = k8s.ResourcePods
	m.activePanel = TablePanel
	m.appLog = NewAppLogModel(m.theme)
	m.statusLine = NewStatusLineModel(m.theme)
	m.logStreamer = k8s.NewLogStreamer(nil)

	// Set up: in compare mode AND on a drill stack (e.g. drilled
	// from Deployment → Pods).
	m.setCompareLock(items[0], k8s.ResourcePods)
	m.drillDownStack = append(m.drillDownStack, drillDownEntry{parentType: k8s.ResourceDeployments, parentName: "nginx"})

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	got := updated.(AppModel)
	if got.inCompareMode() {
		t.Error("Esc must release compare lock when both lock + drill are active")
	}
	if len(got.drillDownStack) != 0 {
		t.Errorf("Esc must also pop the drill stack in the same press; got len=%d", len(got.drillDownStack))
	}
}

func TestAppModel_EscOnPanel2WithCompareMode_NoDrill_JustClearsLock(t *testing.T) {
	// Compare mode active, no drill: Esc only releases the lock.
	// Without drill stack to pop, the keypress does its compare-mode
	// work and otherwise no-ops.
	items := []k8s.ResourceItem{
		{Name: "a", UID: "uid-a", Row: []string{"a"}},
		{Name: "b", UID: "uid-b", Row: []string{"b"}},
	}
	m := appWithItems(items, 0)
	m.currentResource = k8s.ResourcePods
	m.activePanel = TablePanel
	m.appLog = NewAppLogModel(m.theme)
	m.setCompareLock(items[0], k8s.ResourcePods)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	got := updated.(AppModel)
	if got.inCompareMode() {
		t.Error("Esc must release compare lock")
	}
}

// `q` quits immediately — there is no confirm step. The teardown lives in the
// quitMsg handler, so the key handler's only job is emitting that msg.
func TestAppModel_QuitKeySkipsConfirm(t *testing.T) {
	m := appWithItems(nil, 0)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if got := updated.(AppModel); got.confirm.IsActive() {
		t.Error("q must not open a confirm popup")
	}
	if cmd == nil {
		t.Fatal("q must return a quit cmd")
	}
	if msg := cmd(); !isQuitMsg(msg) {
		t.Errorf("q must emit quitMsg, got %T", msg)
	}
}

func isQuitMsg(msg tea.Msg) bool {
	_, ok := msg.(quitMsg)
	return ok
}
