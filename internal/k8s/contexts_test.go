package k8s

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testKubeconfig has two contexts exercising the metadata + redaction paths:
//   - ctx-a: explicit namespace, CA data, a bearer TOKEN (must be redacted)
//   - ctx-b: no namespace (→ default), insecure TLS, exec auth
const testKubeconfig = `apiVersion: v1
kind: Config
current-context: ctx-b
contexts:
- name: ctx-b
  context:
    cluster: cluster-b
    user: user-b
- name: ctx-a
  context:
    cluster: cluster-a
    user: user-a
    namespace: ns-a
clusters:
- name: cluster-a
  cluster:
    server: https://a.example.com
    certificate-authority-data: Zm9v
- name: cluster-b
  cluster:
    server: https://b.example.com
    insecure-skip-tls-verify: true
users:
- name: user-a
  user:
    token: SECRET-TOKEN-VALUE
- name: user-b
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1beta1
      command: aws-iam-authenticator
      args: ["token", "-i", "mycluster"]
`

func writeTestKubeconfig(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(testKubeconfig), 0o600); err != nil {
		t.Fatalf("writing test kubeconfig: %v", err)
	}
	t.Setenv("KUBECONFIG", path)
}

func TestFetchContextItems_SortedAndFlattened(t *testing.T) {
	writeTestKubeconfig(t)

	items, err := fetchContextItems()
	if err != nil {
		t.Fatalf("fetchContextItems: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 contexts, got %d", len(items))
	}

	// Sorted by name: ctx-a before ctx-b (kubeconfig lists b first).
	if items[0].Name != "ctx-a" || items[1].Name != "ctx-b" {
		t.Fatalf("expected sorted [ctx-a, ctx-b], got [%s, %s]", items[0].Name, items[1].Name)
	}

	// Row now carries the server URL as the last column.
	wantA := []string{"ctx-a", "cluster-a", "user-a", "ns-a", "https://a.example.com"}
	if got := items[0].Row; !equalStrings(got, wantA) {
		t.Errorf("ctx-a row = %v, want %v", got, wantA)
	}
	// ctx-b: no namespace set → defaults to "default".
	wantB := []string{"ctx-b", "cluster-b", "user-b", "default", "https://b.example.com"}
	if got := items[1].Row; !equalStrings(got, wantB) {
		t.Errorf("ctx-b row = %v, want %v", got, wantB)
	}

	// Raw carries the flattened, non-secret ContextInfo.
	ci, ok := items[0].Raw.(ContextInfo)
	if !ok {
		t.Fatalf("ctx-a Raw is %T, want ContextInfo", items[0].Raw)
	}
	if ci.Server != "https://a.example.com" || !ci.HasCAData || ci.AuthMethod != "token" {
		t.Errorf("ctx-a ContextInfo = %+v", ci)
	}
	if items[0].UID != "context/ctx-a" {
		t.Errorf("ctx-a UID = %q, want context/ctx-a", items[0].UID)
	}

	cb := items[1].Raw.(ContextInfo)
	if !cb.Insecure || cb.AuthMethod != "exec" || cb.AuthDetail != "aws-iam-authenticator" {
		t.Errorf("ctx-b ContextInfo = %+v", cb)
	}
}

func TestDetailContext_RicherFields(t *testing.T) {
	writeTestKubeconfig(t)
	items, err := fetchContextItems()
	if err != nil {
		t.Fatalf("fetchContextItems: %v", err)
	}

	d := detailContext(items[0]) // ctx-a
	if d.Kind != "Context" || d.Name != "ctx-a" {
		t.Errorf("detail kind/name = %q/%q", d.Kind, d.Name)
	}
	got := map[string]string{}
	for _, f := range d.Fields {
		got[f.Label] = f.Value
	}
	// Server is now surfaced; TLS + Auth summarise the connection; the token
	// is described, never shown.
	checks := map[string]string{
		"Context":   "ctx-a",
		"Cluster":   "cluster-a",
		"Server":    "https://a.example.com",
		"TLS":       "verify (CA data embedded)",
		"User":      "user-a",
		"Auth":      "token (redacted)",
		"Namespace": "ns-a",
	}
	for label, want := range checks {
		if got[label] != want {
			t.Errorf("field %q = %q, want %q", label, got[label], want)
		}
	}
}

func TestContextYAML_RendersAndRedactsSecrets(t *testing.T) {
	writeTestKubeconfig(t)
	items, err := fetchContextItems()
	if err != nil {
		t.Fatalf("fetchContextItems: %v", err)
	}

	// The Y popup pulls YAML through MarshalItemYAML — it must be non-empty
	// for a context now (previously the popup wouldn't open).
	yaml := MarshalItemYAML(items[0]) // ctx-a (token auth)
	if yaml == "" {
		t.Fatal("expected non-empty YAML for a context item")
	}
	for _, want := range []string{"server: https://a.example.com", "certificate-authority-data: <redacted>", "token: <redacted>"} {
		if !strings.Contains(yaml, want) {
			t.Errorf("context YAML missing %q:\n%s", want, yaml)
		}
	}
	// CRITICAL: the actual token value must never appear.
	if strings.Contains(yaml, "SECRET-TOKEN-VALUE") {
		t.Fatalf("SECURITY: token value leaked into context YAML:\n%s", yaml)
	}

	// ctx-b: insecure + exec auth.
	yamlB := MarshalItemYAML(items[1])
	for _, want := range []string{"insecure-skip-tls-verify: true", "exec:", "command: aws-iam-authenticator"} {
		if !strings.Contains(yamlB, want) {
			t.Errorf("ctx-b YAML missing %q:\n%s", want, yamlB)
		}
	}
}

func TestContextsRegisteredUnderKubeConfig(t *testing.T) {
	def := DefaultRegistry.Get(ResourceContexts)
	if def == nil {
		t.Fatal("ResourceContexts not registered")
	}
	if def.Category != "KubeConfig" {
		t.Errorf("category = %q, want KubeConfig", def.Category)
	}
	if !def.ClusterScoped {
		t.Error("Contexts should be ClusterScoped (no leading Namespace column)")
	}
	if def.WatchStarter == nil {
		t.Error("Contexts needs a WatchStarter (nil panics the watcher)")
	}
	// KubeConfig sits at the very top of the sidebar (CategoryOrder -1).
	cats := DefaultRegistry.SidebarCategories()
	if len(cats) == 0 || cats[0].Label != "KubeConfig" {
		var first string
		if len(cats) > 0 {
			first = cats[0].Label
		}
		t.Errorf("expected KubeConfig first in sidebar, got %q", first)
	}
}

func equalStrings(a, b []string) bool {
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
