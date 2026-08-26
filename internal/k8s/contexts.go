package k8s

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

// ContextInfo is one kubeconfig context, flattened with the cluster + user it
// references. It is the ResourceItem.Raw payload for the read-only Contexts
// view — a purely local, client-side value with no cluster object behind it.
//
// SECURITY: only non-secret metadata is stored here. Tokens, passwords,
// client-key / client-cert data, auth-provider config values and exec env are
// NEVER copied off the kubeconfig into this struct — kbu displays context
// metadata only, never credential content (project rule §7). AuthDetail holds
// only non-secret specifics (exec command, auth-provider name, cert/token file
// path, or username).
type ContextInfo struct {
	Name      string
	Cluster   string
	User      string
	Namespace string
	Server    string

	// TLS / connection metadata (all non-secret).
	Insecure  bool
	CAFile    string // certificate-authority path (not the data)
	HasCAData bool   // certificate-authority-data present (rendered as <redacted>)
	TLSName   string // tls-server-name override
	ProxyURL  string

	// Auth summary (no secrets). AuthMethod is one of exec / auth-provider /
	// client-certificate / token / tokenFile / basic / (none). AuthDetail is
	// the method's non-secret specific, or "" when there isn't one.
	AuthMethod string
	AuthDetail string
}

// contextsPollInterval is how often the Contexts list is re-read while the
// user sits on it. The kubeconfig almost never changes mid-session, so this
// is deliberately slow — the poll only re-reads a local file (no API call).
const contextsPollInterval = 15 * time.Second

// fetchContextItems lists every context in the local kubeconfig, sorted by
// name. It reads the kubeconfig directly via the default loading rules (same
// resolution NewClient uses — honours $KUBECONFIG, falls back to
// ~/.kube/config), so it works even when the cluster is unreachable. The
// clientset / namespace args from the ResourceFetcher signature are unused:
// contexts are not a cluster resource.
//
// The "current" context is intentionally NOT marked here — which context is
// active is app runtime state (the C picker rebinds the client without
// writing the kubeconfig), so the UI layer stamps the marker from the live
// client instead (see markCurrentContextRow in app.go).
func fetchContextItems() ([]ResourceItem, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	raw, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules, &clientcmd.ConfigOverrides{},
	).RawConfig()
	if err != nil {
		return nil, fmt.Errorf("loading kubeconfig: %w", err)
	}

	names := make([]string, 0, len(raw.Contexts))
	for name := range raw.Contexts {
		names = append(names, name)
	}
	sort.Strings(names)

	items := make([]ResourceItem, 0, len(names))
	for _, name := range names {
		ci := contextInfoFrom(name, raw.Contexts[name], raw.Clusters, raw.AuthInfos)
		items = append(items, ResourceItem{
			Name: name,
			UID:  "context/" + name, // synthetic, stable per context name
			Raw:  ci,
			Row:  []string{ci.Name, ci.Cluster, ci.User, ci.Namespace, ci.Server},
		})
	}
	return items, nil
}

// contextInfoFrom flattens a kubeconfig context and the cluster + authinfo it
// points at into a ContextInfo, capturing non-secret metadata only.
func contextInfoFrom(name string, c *api.Context, clusters map[string]*api.Cluster, auths map[string]*api.AuthInfo) ContextInfo {
	ci := ContextInfo{Name: name}
	if c != nil {
		ci.Cluster = c.Cluster
		ci.User = c.AuthInfo
		ci.Namespace = c.Namespace
	}
	if ci.Namespace == "" {
		ci.Namespace = "default"
	}
	if cl := clusters[ci.Cluster]; cl != nil {
		ci.Server = cl.Server
		ci.Insecure = cl.InsecureSkipTLSVerify
		ci.CAFile = cl.CertificateAuthority
		ci.HasCAData = len(cl.CertificateAuthorityData) > 0
		ci.TLSName = cl.TLSServerName
		ci.ProxyURL = cl.ProxyURL
	}
	ci.AuthMethod, ci.AuthDetail = authSummary(auths[ci.User])
	return ci
}

// authSummary classifies an authinfo into a method + a NON-SECRET detail.
// Never returns a token / password / key — only the method and a path/name.
func authSummary(a *api.AuthInfo) (method, detail string) {
	if a == nil {
		return "(none)", ""
	}
	switch {
	case a.Exec != nil:
		return "exec", a.Exec.Command
	case a.AuthProvider != nil:
		return "auth-provider", a.AuthProvider.Name
	case len(a.ClientCertificateData) > 0 || a.ClientCertificate != "":
		return "client-certificate", a.ClientCertificate // path, or "" when embedded
	case a.Token != "":
		return "token", "" // value redacted
	case a.TokenFile != "":
		return "tokenFile", a.TokenFile
	case a.Username != "":
		return "basic", a.Username
	}
	return "(none)", ""
}

// tlsDisplay describes the cluster's TLS verification posture in one line.
func (ci ContextInfo) tlsDisplay() string {
	switch {
	case ci.Insecure:
		return "insecure-skip-tls-verify"
	case ci.HasCAData:
		return "verify (CA data embedded)"
	case ci.CAFile != "":
		return "verify (CA: " + ci.CAFile + ")"
	}
	return "verify (system roots)"
}

// authDisplay is the human-readable auth line for the detail panel.
func (ci ContextInfo) authDisplay() string {
	if ci.AuthDetail != "" {
		return ci.AuthMethod + " (" + ci.AuthDetail + ")"
	}
	if ci.AuthMethod == "token" {
		return "token (redacted)"
	}
	return ci.AuthMethod
}

// detailContext builds the read-only detail for a context row from its
// flattened, non-secret ContextInfo. Raw is not a k8s object, so this
// populates ResourceDetail directly instead of going through baseDetail.
// Optional fields (proxy / TLS server name) appear only when set.
func detailContext(item ResourceItem) ResourceDetail {
	ci, _ := item.Raw.(ContextInfo)
	fields := []DetailField{
		{Label: "Context", Value: ci.Name},
		{Label: "Cluster", Value: ci.Cluster},
		{Label: "Server", Value: ci.Server},
		{Label: "TLS", Value: ci.tlsDisplay()},
		{Label: "User", Value: ci.User},
		{Label: "Auth", Value: ci.authDisplay()},
		{Label: "Namespace", Value: ci.Namespace},
	}
	if ci.TLSName != "" {
		fields = append(fields, DetailField{Label: "TLS Server Name", Value: ci.TLSName})
	}
	if ci.ProxyURL != "" {
		fields = append(fields, DetailField{Label: "Proxy", Value: ci.ProxyURL})
	}
	return ResourceDetail{Name: ci.Name, Kind: "Context", Fields: fields}
}

// contextYAML renders a readable, kubeconfig-shaped view of a context for the
// Y popup. It is built by an ALLOWLIST — only fields explicitly written here
// appear, and every credential-bearing field is emitted as <redacted> — so no
// token / key / password can leak even if the kubeconfig carries one.
func contextYAML(ci ContextInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# context %q — credentials redacted by kbu\n", ci.Name)
	b.WriteString("context:\n")
	fmt.Fprintf(&b, "  cluster: %s\n", ci.Cluster)
	fmt.Fprintf(&b, "  user: %s\n", ci.User)
	fmt.Fprintf(&b, "  namespace: %s\n", ci.Namespace)

	b.WriteString("cluster:\n")
	fmt.Fprintf(&b, "  server: %s\n", ci.Server)
	if ci.TLSName != "" {
		fmt.Fprintf(&b, "  tls-server-name: %s\n", ci.TLSName)
	}
	if ci.Insecure {
		b.WriteString("  insecure-skip-tls-verify: true\n")
	}
	switch {
	case ci.HasCAData:
		b.WriteString("  certificate-authority-data: <redacted>\n")
	case ci.CAFile != "":
		fmt.Fprintf(&b, "  certificate-authority: %s\n", ci.CAFile)
	}
	if ci.ProxyURL != "" {
		fmt.Fprintf(&b, "  proxy-url: %s\n", ci.ProxyURL)
	}

	b.WriteString("user:\n")
	switch ci.AuthMethod {
	case "exec":
		b.WriteString("  exec:\n")
		fmt.Fprintf(&b, "    command: %s\n", ci.AuthDetail)
		b.WriteString("    # args / env redacted\n")
	case "auth-provider":
		b.WriteString("  auth-provider:\n")
		fmt.Fprintf(&b, "    name: %s\n", ci.AuthDetail)
		b.WriteString("    # config redacted\n")
	case "client-certificate":
		if ci.AuthDetail != "" {
			fmt.Fprintf(&b, "  client-certificate: %s\n", ci.AuthDetail)
		} else {
			b.WriteString("  client-certificate-data: <redacted>\n")
		}
		b.WriteString("  client-key-data: <redacted>\n")
	case "token":
		b.WriteString("  token: <redacted>\n")
	case "tokenFile":
		fmt.Fprintf(&b, "  tokenFile: %s\n", ci.AuthDetail)
	case "basic":
		fmt.Fprintf(&b, "  username: %s\n", ci.AuthDetail)
		b.WriteString("  password: <redacted>\n")
	default:
		b.WriteString("  # (no credentials configured)\n")
	}
	return b.String()
}

// contextsPollWatch satisfies the WatchStarter signature. The registry calls
// WatchStarter unconditionally (a nil one panics), and contexts have no real
// watch API, so — like Helm releases — it drives refreshes off a slow poll
// that re-reads the kubeconfig file. Reuses the helm poll-watch primitive.
func contextsPollWatch(ctx context.Context, _ kubernetes.Interface, _ string) (watch.Interface, error) {
	return newPollWatch(ctx, contextsPollInterval), nil
}
