package ui

import (
	"strings"
	"testing"

	"github.com/vulcanshen/kbu/internal/k8s"
)

func TestMarkCurrentContextRow(t *testing.T) {
	rows := [][]string{
		{"ctx-a", "", "cluster-a", "user-a", "ns-a"},
		{"ctx-b", "", "cluster-b", "user-b", "default"},
	}
	markCurrentContextRow(rows, "ctx-b")
	if rows[1][0] != "ctx-b *" {
		t.Errorf("expected current context marked, got %q", rows[1][0])
	}
	if rows[0][0] != "ctx-a" {
		t.Errorf("non-current row must be untouched, got %q", rows[0][0])
	}
}

func TestMarkCurrentContextRow_NoMatchIsNoOp(t *testing.T) {
	rows := [][]string{{"ctx-a", "", "cluster-a"}}
	markCurrentContextRow(rows, "does-not-exist")
	markCurrentContextRow(rows, "") // empty current → no-op
	if rows[0][0] != "ctx-a" {
		t.Errorf("no-match must leave rows untouched, got %q", rows[0][0])
	}
}

func TestContextsAreReadOnly(t *testing.T) {
	if resourceAllowsEdit(k8s.ResourceContexts) {
		t.Error("Contexts must not allow edit (read-only kubeconfig view)")
	}
	if resourceAllowsDelete(k8s.ResourceContexts) {
		t.Error("Contexts must not allow delete (read-only kubeconfig view)")
	}
}

// Now that the YAML popup opens for a context (its YAML is non-empty), the
// popup's own E must stay gated so read-only kinds can't reach kubectl edit
// through the popup.
func TestYamlPopup_EditGatedForContexts(t *testing.T) {
	m := newTestYamlPopup()
	item := k8s.ResourceItem{Name: "ctx-a"}
	m.Open("# context ctx-a\ncluster:\n  server: https://a\n", k8s.ResourceContexts, item, "test-ctx")
	m.animator.Finalize()
	if _, cmd := m.Update(keyMsg('E')); cmd != nil {
		t.Error("E in the YAML popup must be a no-op for read-only Contexts")
	}
}

func TestDetailModel_ContextsInfoTab(t *testing.T) {
	m := newTestDetail()
	m.SetResourceType(k8s.ResourceContexts)
	if len(m.tabs) != 1 || m.tabs[0] != "Info" {
		t.Fatalf("expected tabs=[Info] for Contexts, got %v", m.tabs)
	}
	// Feed a context detail and confirm the Info tab renders the fields.
	detail := k8s.ResourceDetail{
		Name: "ctx-a",
		Kind: "Context",
		Fields: []k8s.DetailField{
			{Label: "Context", Value: "ctx-a"},
			{Label: "Cluster", Value: "cluster-a"},
			{Label: "User", Value: "user-a"},
			{Label: "Namespace", Value: "ns-a"},
		},
	}
	m.SetDetail(detail, nil)
	body := strings.Join(m.contentLines, "\n")
	for _, want := range []string{"Context", "ctx-a", "Cluster", "cluster-a", "User", "user-a", "Namespace", "ns-a"} {
		if !strings.Contains(body, want) {
			t.Errorf("Info tab body missing %q; body=\n%s", want, body)
		}
	}
}
