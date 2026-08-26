package k8s

// ClusterInfo holds metadata about the currently connected Kubernetes cluster.
type ClusterInfo struct {
	ContextName string
	ClusterName string
	ServerURL   string
	Namespace   string
}

// ResourceType identifies a Kubernetes resource kind supported by kbu.
type ResourceType string

const (
	ResourceNamespaces          ResourceType = "namespaces"
	ResourceNodes               ResourceType = "nodes"
	ResourcePods                ResourceType = "pods"
	ResourceDeployments         ResourceType = "deployments"
	ResourceDaemonSets          ResourceType = "daemonsets"
	ResourceStatefulSets        ResourceType = "statefulsets"
	ResourceJobs                ResourceType = "jobs"
	ResourceCronJobs            ResourceType = "cronjobs"
	ResourceServices            ResourceType = "services"
	ResourceIngresses           ResourceType = "ingresses"
	ResourceConfigMaps          ResourceType = "configmaps"
	ResourceSecrets             ResourceType = "secrets"
	ResourceEvents              ResourceType = "events"
	ResourceClusterRoles        ResourceType = "clusterroles"
	ResourceClusterRoleBindings ResourceType = "clusterrolebindings"
	ResourceRoles               ResourceType = "roles"
	ResourceRoleBindings        ResourceType = "rolebindings"
	ResourceServiceAccounts     ResourceType = "serviceaccounts"

	ResourcePersistentVolumes      ResourceType = "persistentvolumes"
	ResourcePersistentVolumeClaims ResourceType = "persistentvolumeclaims"
	ResourceStorageClasses         ResourceType = "storageclasses"

	ResourceHorizontalPodAutoscalers ResourceType = "horizontalpodautoscalers"
	ResourcePodDisruptionBudgets     ResourceType = "poddisruptionbudgets"

	ResourceNetworkPolicies ResourceType = "networkpolicies"
	ResourceEndpointSlices  ResourceType = "endpointslices"
	ResourceIngressClasses  ResourceType = "ingressclasses"

	// ResourceReleases is the kbu 27th ResourceType — Helm releases. Treated as
	// a normal ResourceType so existing graph/drill machinery is reused, but the
	// fetcher goes through `helm` CLI rather than client-go. Registered at
	// runtime only when `helm` is found on PATH (see helm.go).
	ResourceReleases ResourceType = "releases"

	// ResourceContexts is a read-only view over the local kubeconfig's
	// contexts — it is NOT a cluster object. Its fetcher reads the kubeconfig
	// file rather than calling the API server, and it exposes no write ops
	// (edit / delete are gated off; switching context stays in the C picker).
	// Lives in its own "KubeConfig" sidebar category (see builtins.go).
	ResourceContexts ResourceType = "contexts"
)

// String returns the human-readable name of the resource type.
func (r ResourceType) String() string {
	if def := DefaultRegistry.Get(r); def != nil {
		return def.DisplayName
	}
	return string(r)
}

// ResourceItem holds a single resource's table row and metadata.
type ResourceItem struct {
	Row       []string
	Name      string
	Namespace string
	UID       string
	Raw       interface{}
}

// ResourceDetail holds structured detail for a resource.
//
// YAML, if non-empty, is the canonical serialized form of the resource — the
// detail panel renders it instead of the structured Fields/Containers when
// available. Structured fields are still used for synthetic detail views
// (e.g. container drill-down) that have no native YAML.
//
// PodRelatives / ServiceRelatives are legacy typed payloads for the two kinds with
// rich domain-specific structure. Every other kind populates the generic
// Relatives slice instead — the UI dispatcher reads from the right field.
type ResourceDetail struct {
	Name             string
	Namespace        string
	Kind             string
	UID              string
	CreatedAt        string
	Labels           map[string]string
	Annotations      map[string]string
	Fields           []DetailField
	Containers       []ContainerInfo
	YAML             string
	PodRelatives     *PodRelativesData
	ServiceRelatives *ServiceRelativesData
	Relatives        []RelativeSection

	// ReleaseHistory carries the `helm history` rows for a Helm Release.
	// Populated by enrichReleaseHistory (Phase 2c). Nil for every other kind.
	ReleaseHistory []ReleaseRevision

	// Conditions carries the resource's .status.conditions for display on the
	// Conditions tab. Populated by ExtractConditions; nil for kinds without
	// conditions (ConfigMap, Secret, Service, ServiceAccount, ...). The
	// Conditions tab is hidden when this slice is empty.
	Conditions []ConditionItem
}

// RelativeSection is one labeled group of link rows on the Relatives tab. Title is
// the header label (empty title renders no header row). Entries with a
// non-nil Ref are drillable; entries with Ref==nil are informational text.
type RelativeSection struct {
	Title   string
	Entries []RelativeRow
}

// RelativeRow is one row inside a RelativeSection. Display format is
// "Label  Value [→]" — the arrow appears when Ref is non-nil.
type RelativeRow struct {
	Label string
	Value string
	Ref   *RefTarget
}

// RefTarget identifies another Kubernetes resource that the Relatives tab can
// drill into. Type is the kbu ResourceType (Pod, Secret, ConfigMap, Node, ...);
// Namespace is empty for cluster-scoped kinds.
type RefTarget struct {
	Type      ResourceType
	Name      string
	Namespace string
}

// PodRelativesData is the structured "Relatives" content for a Pod.
//
// Owner is the immediate K8s owner reference (ReplicaSet / DaemonSet /
// StatefulSet / Job). Drilling further (RS → Deployment) is left to follow-up
// commits — for MVP we surface the one-hop owner only.
//
// Volumes lists the Pod's spec.volumes with their source kind and an optional
// drill ref (ConfigMap / Secret / PVC sources are drillable; emptyDir /
// hostPath / projected / downwardAPI are informational).
//
// Images carries the rendered image strings (e.g. "nginx:1.27.1") for
// informational display — there's no K8s resource to drill into for an image.
type PodRelativesData struct {
	Owner          *RefTarget
	Node           *RefTarget // cluster-scoped
	ServiceAccount *RefTarget
	Volumes        []VolumeRef
	Images         []string
	InitImages     []string
}

// VolumeRef describes a Pod volume's source. Ref is non-nil when the source
// is another K8s resource the user can drill into (ConfigMap, Secret, PVC).
type VolumeRef struct {
	Name string // volume name in spec.volumes
	Kind string // "configMap" / "secret" / "persistentVolumeClaim" / "emptyDir" / "hostPath" / "projected" / "downwardAPI" / "other"
	Ref  *RefTarget
}

// ServiceRelativesData is the navigable-refs payload for a Service detail.
// Pods is the workload selected by the Service's label selector — each one
// is a drillable RefTarget so the user can answer "which pods does this
// Service route to?" in one keystroke.
//
// Populated by EnrichRelatives at fetch time (it issues a CoreV1().Pods().List
// against the selector); the synchronous detailService can't do this
// because it has no clientset.
type ServiceRelativesData struct {
	Pods []RefTarget
}

// DetailField is a key-value pair for resource-specific detail.
type DetailField struct {
	Label string
	Value string
}

// ContainerInfo holds detail about a single container in a Pod.
type ContainerInfo struct {
	Name     string
	Image    string
	State    string
	Ready    bool
	Restarts int
	Ports    string
	Init     bool
}

// EventItem holds a single event for display.
type EventItem struct {
	Type    string
	Reason  string
	Object  string
	Message string
	Age     string
}

// ConditionItem holds one row of a resource's .status.conditions for display
// on the Conditions tab. Mirrors what `kubectl describe` shows in its
// Conditions section: type / status / reason / message + age since last
// transition. Populated by ExtractConditions for kinds that carry conditions
// (Pod, Deployment, StatefulSet, ...); nil for kinds without (ConfigMap,
// Secret, etc.) — the Conditions tab is hidden when this slice is empty.
type ConditionItem struct {
	Type    string // e.g. "Ready", "PodScheduled", "Available"
	Status  string // "True" | "False" | "Unknown"
	Reason  string // brief CamelCase reason
	Message string // human-readable message
	Age     string // since LastTransitionTime
}

// SupportsDrillDown returns true if this resource type can drill down to children.
func (r ResourceType) SupportsDrillDown() bool {
	if def := DefaultRegistry.Get(r); def != nil {
		return def.DrillDown != nil
	}
	return false
}

// KubectlName returns the kubectl resource name (e.g. "pod", "deployment").
func (r ResourceType) KubectlName() string {
	if def := DefaultRegistry.Get(r); def != nil {
		return def.KubectlName
	}
	return string(r)
}

// AllResourceTypes returns all supported resource types in display order.
func AllResourceTypes() []ResourceType {
	return DefaultRegistry.AllTypes()
}
