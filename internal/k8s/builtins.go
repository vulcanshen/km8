package k8s

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
)

// fetchPodsForPVCDrillDown is a thin wrapper so the DrillDown ChildFetcher
// signature matches without exposing the helper.
func fetchPodsForPVCDrillDown(ctx context.Context, cs kubernetes.Interface, item ResourceItem) ([]ResourceItem, error) {
	return fetchPodsForPVC(ctx, cs, item)
}

func fetchHPATargetDrillDown(ctx context.Context, cs kubernetes.Interface, item ResourceItem) ([]ResourceItem, error) {
	return fetchHPATarget(ctx, cs, item)
}

func fetchPodsForPDBDrillDown(ctx context.Context, cs kubernetes.Interface, item ResourceItem) ([]ResourceItem, error) {
	return fetchPodsForPDB(ctx, cs, item)
}

// drillDownBySelector returns a ChildFetcher that extracts the label selector
// from the parent resource (Deployment, DaemonSet, StatefulSet) and fetches
// matching pods.
func drillDownBySelector(kind string) ChildFetcher {
	return func(ctx context.Context, cs kubernetes.Interface, item ResourceItem) ([]ResourceItem, error) {
		var selector string
		switch kind {
		case "Deployment":
			dep, _ := item.Raw.(*appsv1.Deployment)
			sel, _ := metav1.LabelSelectorAsSelector(dep.Spec.Selector)
			selector = sel.String()
		case "DaemonSet":
			ds, _ := item.Raw.(*appsv1.DaemonSet)
			sel, _ := metav1.LabelSelectorAsSelector(ds.Spec.Selector)
			selector = sel.String()
		case "StatefulSet":
			ss, _ := item.Raw.(*appsv1.StatefulSet)
			sel, _ := metav1.LabelSelectorAsSelector(ss.Spec.Selector)
			selector = sel.String()
		}
		return fetchPodsWithSelector(ctx, cs, item.Namespace, selector)
	}
}

func fetchPodsForJobDrillDown(ctx context.Context, cs kubernetes.Interface, item ResourceItem) ([]ResourceItem, error) {
	return fetchPodsForJob(ctx, cs, item)
}

func fetchJobsForCronJobDrillDown(ctx context.Context, cs kubernetes.Interface, item ResourceItem) ([]ResourceItem, error) {
	return fetchJobsForCronJob(ctx, cs, item)
}

func init() {
	// -----------------------------------------------------------------------
	// KubeConfig (order -1, top of sidebar) — local kubeconfig, NOT cluster
	// data. Read-only: no edit / delete (switching context stays in the C
	// picker). Room to grow to Clusters / Users later.
	// -----------------------------------------------------------------------

	// Contexts
	DefaultRegistry.Register(&ResourceDefinition{
		Type:            ResourceContexts,
		DisplayName:     "Contexts",
		KubectlName:     "context",
		Category:        "KubeConfig",
		CategoryOrder:   -1,
		OrderInCategory: 0,
		ClusterScoped:   true,
		Columns: []Column{
			{Title: "Name", MinWidth: 20},
			{Title: "Cluster", MinWidth: 16},
			{Title: "User", MinWidth: 16},
			{Title: "Namespace", MinWidth: 12},
			{Title: "Server", MinWidth: 24},
		},
		Fetcher: func(_ context.Context, _ kubernetes.Interface, _ string) ([]ResourceItem, error) {
			return fetchContextItems()
		},
		Detailer:     detailContext,
		WatchStarter: contextsPollWatch,
	})

	// -----------------------------------------------------------------------
	// Cluster (order 0)
	// -----------------------------------------------------------------------

	// Namespaces
	DefaultRegistry.Register(&ResourceDefinition{
		Type:            ResourceNamespaces,
		DisplayName:     "Namespaces",
		KubectlName:     "namespace",
		Category:        "Cluster",
		CategoryOrder:   0,
		OrderInCategory: 0,
		ClusterScoped:   true,
		Columns: []Column{
			{Title: "Name", MinWidth: 20},
			{Title: "Status", MinWidth: 10},
			{Title: "Age", MinWidth: 8},
		},
		Fetcher: func(ctx context.Context, cs kubernetes.Interface, _ string) ([]ResourceItem, error) {
			return fetchNamespaces(ctx, cs)
		},
		Detailer: detailNamespace,
		WatchStarter: func(ctx context.Context, cs kubernetes.Interface, _ string) (watch.Interface, error) {
			return cs.CoreV1().Namespaces().Watch(ctx, metav1.ListOptions{})
		},
	})

	// Nodes
	DefaultRegistry.Register(&ResourceDefinition{
		Type:            ResourceNodes,
		DisplayName:     "Nodes",
		KubectlName:     "node",
		Category:        "Cluster",
		CategoryOrder:   0,
		OrderInCategory: 1,
		ClusterScoped:   true,
		Columns: []Column{
			{Title: "Name", MinWidth: 20},
			{Title: "Status", MinWidth: 10},
			{Title: "Roles", MinWidth: 12},
			{Title: "Version", MinWidth: 10},
			{Title: "Age", MinWidth: 8},
		},
		Fetcher: func(ctx context.Context, cs kubernetes.Interface, _ string) ([]ResourceItem, error) {
			return fetchNodes(ctx, cs)
		},
		Detailer: detailNode,
		WatchStarter: func(ctx context.Context, cs kubernetes.Interface, _ string) (watch.Interface, error) {
			return cs.CoreV1().Nodes().Watch(ctx, metav1.ListOptions{})
		},
	})

	// Events
	DefaultRegistry.Register(&ResourceDefinition{
		Type:            ResourceEvents,
		DisplayName:     "Events",
		KubectlName:     "event",
		Category:        "Cluster",
		CategoryOrder:   0,
		OrderInCategory: 2,
		Columns: []Column{
			{Title: "Type", MinWidth: 8},
			{Title: "Reason", MinWidth: 15},
			{Title: "Object", MinWidth: 20},
			{Title: "Message", MinWidth: 30},
			{Title: "Age", MinWidth: 8},
		},
		Fetcher:  fetchEvents,
		Detailer: detailEvent,
		WatchStarter: func(ctx context.Context, cs kubernetes.Interface, ns string) (watch.Interface, error) {
			return cs.CoreV1().Events(ns).Watch(ctx, metav1.ListOptions{})
		},
	})

	// -----------------------------------------------------------------------
	// Workloads (order 1)
	// -----------------------------------------------------------------------

	// Pods
	DefaultRegistry.Register(&ResourceDefinition{
		Type:            ResourcePods,
		DisplayName:     "Pods",
		KubectlName:     "pod",
		Category:        "Workloads",
		CategoryOrder:   1,
		OrderInCategory: 0,
		Columns: []Column{
			{Title: "Name", MinWidth: 20},
			{Title: "Ready", MinWidth: 7},
			{Title: "Status", MinWidth: 10},
			{Title: "Restarts", MinWidth: 10},
			{Title: "Age", MinWidth: 8},
			{Title: "IP", MinWidth: 14},
			{Title: "Node", MinWidth: 15},
		},
		Fetcher:  fetchPods,
		Detailer: detailPod,
		WatchStarter: func(ctx context.Context, cs kubernetes.Interface, ns string) (watch.Interface, error) {
			return cs.CoreV1().Pods(ns).Watch(ctx, metav1.ListOptions{})
		},
		DrillDown: &DrillDownConfig{
			ChildTypeFor:  StaticChildType(ResourcePods),
			FetchChildren: nil, // Pod→Container is special-cased in UI
		},
		HasLogs: true,
	})

	// Deployments
	DefaultRegistry.Register(&ResourceDefinition{
		Type:            ResourceDeployments,
		DisplayName:     "Deployments",
		KubectlName:     "deployment",
		Category:        "Workloads",
		CategoryOrder:   1,
		OrderInCategory: 1,
		Columns: []Column{
			{Title: "Name", MinWidth: 20},
			{Title: "Ready", MinWidth: 7},
			{Title: "Up-to-date", MinWidth: 12},
			{Title: "Available", MinWidth: 10},
			{Title: "Age", MinWidth: 8},
		},
		Fetcher:  fetchDeployments,
		Detailer: detailDeployment,
		WatchStarter: func(ctx context.Context, cs kubernetes.Interface, ns string) (watch.Interface, error) {
			return cs.AppsV1().Deployments(ns).Watch(ctx, metav1.ListOptions{})
		},
		DrillDown: &DrillDownConfig{
			ChildTypeFor:  StaticChildType(ResourcePods),
			FetchChildren: drillDownBySelector("Deployment"),
		},
	})

	// DaemonSets
	DefaultRegistry.Register(&ResourceDefinition{
		Type:            ResourceDaemonSets,
		DisplayName:     "DaemonSets",
		KubectlName:     "daemonset",
		Category:        "Workloads",
		CategoryOrder:   1,
		OrderInCategory: 2,
		Columns: []Column{
			{Title: "Name", MinWidth: 20},
			{Title: "Desired", MinWidth: 8},
			{Title: "Current", MinWidth: 8},
			{Title: "Ready", MinWidth: 7},
			{Title: "Age", MinWidth: 8},
		},
		Fetcher:  fetchDaemonSets,
		Detailer: detailDaemonSet,
		WatchStarter: func(ctx context.Context, cs kubernetes.Interface, ns string) (watch.Interface, error) {
			return cs.AppsV1().DaemonSets(ns).Watch(ctx, metav1.ListOptions{})
		},
		DrillDown: &DrillDownConfig{
			ChildTypeFor:  StaticChildType(ResourcePods),
			FetchChildren: drillDownBySelector("DaemonSet"),
		},
	})

	// StatefulSets
	DefaultRegistry.Register(&ResourceDefinition{
		Type:            ResourceStatefulSets,
		DisplayName:     "StatefulSets",
		KubectlName:     "statefulset",
		Category:        "Workloads",
		CategoryOrder:   1,
		OrderInCategory: 3,
		Columns: []Column{
			{Title: "Name", MinWidth: 20},
			{Title: "Ready", MinWidth: 7},
			{Title: "Age", MinWidth: 8},
		},
		Fetcher:  fetchStatefulSets,
		Detailer: detailStatefulSet,
		WatchStarter: func(ctx context.Context, cs kubernetes.Interface, ns string) (watch.Interface, error) {
			return cs.AppsV1().StatefulSets(ns).Watch(ctx, metav1.ListOptions{})
		},
		DrillDown: &DrillDownConfig{
			ChildTypeFor:  StaticChildType(ResourcePods),
			FetchChildren: drillDownBySelector("StatefulSet"),
		},
	})

	// Jobs
	DefaultRegistry.Register(&ResourceDefinition{
		Type:            ResourceJobs,
		DisplayName:     "Jobs",
		KubectlName:     "job",
		Category:        "Workloads",
		CategoryOrder:   1,
		OrderInCategory: 4,
		Columns: []Column{
			{Title: "Name", MinWidth: 20},
			{Title: "Completions", MinWidth: 12},
			{Title: "Duration", MinWidth: 10},
			{Title: "Age", MinWidth: 8},
		},
		Fetcher:  fetchJobs,
		Detailer: detailJob,
		WatchStarter: func(ctx context.Context, cs kubernetes.Interface, ns string) (watch.Interface, error) {
			return cs.BatchV1().Jobs(ns).Watch(ctx, metav1.ListOptions{})
		},
		DrillDown: &DrillDownConfig{
			ChildTypeFor:  StaticChildType(ResourcePods),
			FetchChildren: fetchPodsForJobDrillDown,
		},
	})

	// CronJobs
	DefaultRegistry.Register(&ResourceDefinition{
		Type:            ResourceCronJobs,
		DisplayName:     "CronJobs",
		KubectlName:     "cronjob",
		Category:        "Workloads",
		CategoryOrder:   1,
		OrderInCategory: 5,
		Columns: []Column{
			{Title: "Name", MinWidth: 20},
			{Title: "Schedule", MinWidth: 15},
			{Title: "Suspend", MinWidth: 8},
			{Title: "Active", MinWidth: 7},
			{Title: "Last Schedule", MinWidth: 15},
		},
		Fetcher:  fetchCronJobs,
		Detailer: detailCronJob,
		WatchStarter: func(ctx context.Context, cs kubernetes.Interface, ns string) (watch.Interface, error) {
			return cs.BatchV1().CronJobs(ns).Watch(ctx, metav1.ListOptions{})
		},
		DrillDown: &DrillDownConfig{
			ChildTypeFor:  StaticChildType(ResourceJobs),
			FetchChildren: fetchJobsForCronJobDrillDown,
		},
	})

	// -----------------------------------------------------------------------
	// Network (order 2)
	// -----------------------------------------------------------------------

	// Services
	DefaultRegistry.Register(&ResourceDefinition{
		Type:            ResourceServices,
		DisplayName:     "Services",
		KubectlName:     "service",
		Category:        "Network",
		CategoryOrder:   2,
		OrderInCategory: 0,
		Columns: []Column{
			{Title: "Name", MinWidth: 20},
			{Title: "Type", MinWidth: 12},
			{Title: "Cluster-IP", MinWidth: 15},
			{Title: "External-IP", MinWidth: 15},
			{Title: "Ports", MinWidth: 15},
			{Title: "Age", MinWidth: 8},
		},
		Fetcher:  fetchServices,
		Detailer: detailService,
		WatchStarter: func(ctx context.Context, cs kubernetes.Interface, ns string) (watch.Interface, error) {
			return cs.CoreV1().Services(ns).Watch(ctx, metav1.ListOptions{})
		},
	})

	// Ingresses
	DefaultRegistry.Register(&ResourceDefinition{
		Type:            ResourceIngresses,
		DisplayName:     "Ingresses",
		KubectlName:     "ingress",
		Category:        "Network",
		CategoryOrder:   2,
		OrderInCategory: 1,
		Columns: []Column{
			{Title: "Name", MinWidth: 20},
			{Title: "Class", MinWidth: 10},
			{Title: "Hosts", MinWidth: 20},
			{Title: "Address", MinWidth: 15},
			{Title: "Ports", MinWidth: 10},
			{Title: "Age", MinWidth: 8},
		},
		Fetcher:  fetchIngresses,
		Detailer: detailIngress,
		WatchStarter: func(ctx context.Context, cs kubernetes.Interface, ns string) (watch.Interface, error) {
			return cs.NetworkingV1().Ingresses(ns).Watch(ctx, metav1.ListOptions{})
		},
	})

	// NetworkPolicies (namespaced)
	DefaultRegistry.Register(&ResourceDefinition{
		Type:            ResourceNetworkPolicies,
		DisplayName:     "NetworkPolicies",
		KubectlName:     "networkpolicy",
		Category:        "Network",
		CategoryOrder:   2,
		OrderInCategory: 2,
		Columns: []Column{
			{Title: "Name", MinWidth: 20},
			{Title: "Pod-Selector", MinWidth: 20},
			{Title: "Types", MinWidth: 16},
			{Title: "Age", MinWidth: 8},
		},
		Fetcher:  fetchNetworkPolicies,
		Detailer: detailNetworkPolicy,
		WatchStarter: func(ctx context.Context, cs kubernetes.Interface, ns string) (watch.Interface, error) {
			return cs.NetworkingV1().NetworkPolicies(ns).Watch(ctx, metav1.ListOptions{})
		},
	})

	// EndpointSlices (namespaced)
	DefaultRegistry.Register(&ResourceDefinition{
		Type:            ResourceEndpointSlices,
		DisplayName:     "EndpointSlices",
		KubectlName:     "endpointslice",
		Category:        "Network",
		CategoryOrder:   2,
		OrderInCategory: 3,
		Columns: []Column{
			{Title: "Name", MinWidth: 20},
			{Title: "AddressType", MinWidth: 12},
			{Title: "Ports", MinWidth: 12},
			{Title: "Endpoints", MinWidth: 20},
			{Title: "Age", MinWidth: 8},
		},
		Fetcher:  fetchEndpointSlices,
		Detailer: detailEndpointSlice,
		WatchStarter: func(ctx context.Context, cs kubernetes.Interface, ns string) (watch.Interface, error) {
			return cs.DiscoveryV1().EndpointSlices(ns).Watch(ctx, metav1.ListOptions{})
		},
	})

	// IngressClasses (cluster-scoped)
	DefaultRegistry.Register(&ResourceDefinition{
		Type:            ResourceIngressClasses,
		DisplayName:     "IngressClasses",
		KubectlName:     "ingressclass",
		Category:        "Network",
		CategoryOrder:   2,
		OrderInCategory: 4,
		ClusterScoped:   true,
		Columns: []Column{
			{Title: "Name", MinWidth: 20},
			{Title: "Controller", MinWidth: 25},
			{Title: "Parameters", MinWidth: 20},
			{Title: "Age", MinWidth: 8},
		},
		Fetcher: func(ctx context.Context, cs kubernetes.Interface, _ string) ([]ResourceItem, error) {
			return fetchIngressClasses(ctx, cs)
		},
		Detailer: detailIngressClass,
		WatchStarter: func(ctx context.Context, cs kubernetes.Interface, _ string) (watch.Interface, error) {
			return cs.NetworkingV1().IngressClasses().Watch(ctx, metav1.ListOptions{})
		},
	})

	// -----------------------------------------------------------------------
	// Config (order 3)
	// -----------------------------------------------------------------------

	// ConfigMaps
	DefaultRegistry.Register(&ResourceDefinition{
		Type:            ResourceConfigMaps,
		DisplayName:     "ConfigMaps",
		KubectlName:     "configmap",
		Category:        "Config",
		CategoryOrder:   3,
		OrderInCategory: 0,
		Columns: []Column{
			{Title: "Name", MinWidth: 20},
			{Title: "Namespace", MinWidth: 15},
			{Title: "Data", MinWidth: 6},
			{Title: "Age", MinWidth: 8},
		},
		Fetcher:  fetchConfigMaps,
		Detailer: detailConfigMap,
		WatchStarter: func(ctx context.Context, cs kubernetes.Interface, ns string) (watch.Interface, error) {
			return cs.CoreV1().ConfigMaps(ns).Watch(ctx, metav1.ListOptions{})
		},
	})

	// Secrets
	DefaultRegistry.Register(&ResourceDefinition{
		Type:            ResourceSecrets,
		DisplayName:     "Secrets",
		KubectlName:     "secret",
		Category:        "Config",
		CategoryOrder:   3,
		OrderInCategory: 1,
		Columns: []Column{
			{Title: "Name", MinWidth: 20},
			{Title: "Type", MinWidth: 20},
			{Title: "Data", MinWidth: 6},
			{Title: "Age", MinWidth: 8},
		},
		Fetcher:  fetchSecrets,
		Detailer: detailSecret,
		WatchStarter: func(ctx context.Context, cs kubernetes.Interface, ns string) (watch.Interface, error) {
			return cs.CoreV1().Secrets(ns).Watch(ctx, metav1.ListOptions{})
		},
	})

	// -----------------------------------------------------------------------
	// Storage (order 4)
	// -----------------------------------------------------------------------

	// PersistentVolumes (cluster-scoped)
	DefaultRegistry.Register(&ResourceDefinition{
		Type:            ResourcePersistentVolumes,
		DisplayName:     "PersistentVolumes",
		KubectlName:     "persistentvolume",
		Category:        "Storage",
		CategoryOrder:   4,
		OrderInCategory: 0,
		ClusterScoped:   true,
		Columns: []Column{
			{Title: "Name", MinWidth: 20},
			{Title: "Capacity", MinWidth: 10},
			{Title: "Access", MinWidth: 8},
			{Title: "Status", MinWidth: 10},
			{Title: "Claim", MinWidth: 20},
			{Title: "StorageClass", MinWidth: 15},
			{Title: "Age", MinWidth: 8},
		},
		Fetcher: func(ctx context.Context, cs kubernetes.Interface, _ string) ([]ResourceItem, error) {
			return fetchPersistentVolumes(ctx, cs)
		},
		Detailer: detailPersistentVolume,
		WatchStarter: func(ctx context.Context, cs kubernetes.Interface, _ string) (watch.Interface, error) {
			return cs.CoreV1().PersistentVolumes().Watch(ctx, metav1.ListOptions{})
		},
	})

	// PersistentVolumeClaims (namespaced) — drill-down to Pods that mount it
	DefaultRegistry.Register(&ResourceDefinition{
		Type:            ResourcePersistentVolumeClaims,
		DisplayName:     "PersistentVolumeClaims",
		KubectlName:     "persistentvolumeclaim",
		Category:        "Storage",
		CategoryOrder:   4,
		OrderInCategory: 1,
		Columns: []Column{
			{Title: "Name", MinWidth: 20},
			{Title: "Status", MinWidth: 10},
			{Title: "Volume", MinWidth: 20},
			{Title: "Capacity", MinWidth: 10},
			{Title: "Access", MinWidth: 8},
			{Title: "StorageClass", MinWidth: 15},
			{Title: "Age", MinWidth: 8},
		},
		Fetcher:  fetchPersistentVolumeClaims,
		Detailer: detailPersistentVolumeClaim,
		WatchStarter: func(ctx context.Context, cs kubernetes.Interface, ns string) (watch.Interface, error) {
			return cs.CoreV1().PersistentVolumeClaims(ns).Watch(ctx, metav1.ListOptions{})
		},
		DrillDown: &DrillDownConfig{
			ChildTypeFor:  StaticChildType(ResourcePods),
			FetchChildren: fetchPodsForPVCDrillDown,
		},
	})

	// StorageClasses (cluster-scoped)
	DefaultRegistry.Register(&ResourceDefinition{
		Type:            ResourceStorageClasses,
		DisplayName:     "StorageClasses",
		KubectlName:     "storageclass",
		Category:        "Storage",
		CategoryOrder:   4,
		OrderInCategory: 2,
		ClusterScoped:   true,
		Columns: []Column{
			{Title: "Name", MinWidth: 20},
			{Title: "Provisioner", MinWidth: 20},
			{Title: "ReclaimPolicy", MinWidth: 12},
			{Title: "VolumeBinding", MinWidth: 18},
			{Title: "Age", MinWidth: 8},
		},
		Fetcher: func(ctx context.Context, cs kubernetes.Interface, _ string) ([]ResourceItem, error) {
			return fetchStorageClasses(ctx, cs)
		},
		Detailer: detailStorageClass,
		WatchStarter: func(ctx context.Context, cs kubernetes.Interface, _ string) (watch.Interface, error) {
			return cs.StorageV1().StorageClasses().Watch(ctx, metav1.ListOptions{})
		},
	})

	// -----------------------------------------------------------------------
	// RBAC (order 5)
	// -----------------------------------------------------------------------

	// ClusterRoles
	DefaultRegistry.Register(&ResourceDefinition{
		Type:            ResourceClusterRoles,
		DisplayName:     "ClusterRoles",
		KubectlName:     "clusterrole",
		Category:        "RBAC",
		CategoryOrder:   5,
		OrderInCategory: 0,
		ClusterScoped:   true,
		Columns: []Column{
			{Title: "Name", MinWidth: 20},
			{Title: "Age", MinWidth: 8},
		},
		Fetcher: func(ctx context.Context, cs kubernetes.Interface, _ string) ([]ResourceItem, error) {
			return fetchClusterRoles(ctx, cs)
		},
		Detailer: detailClusterRole,
		WatchStarter: func(ctx context.Context, cs kubernetes.Interface, _ string) (watch.Interface, error) {
			return cs.RbacV1().ClusterRoles().Watch(ctx, metav1.ListOptions{})
		},
	})

	// ClusterRoleBindings
	DefaultRegistry.Register(&ResourceDefinition{
		Type:            ResourceClusterRoleBindings,
		DisplayName:     "ClusterRoleBindings",
		KubectlName:     "clusterrolebinding",
		Category:        "RBAC",
		CategoryOrder:   5,
		OrderInCategory: 1,
		ClusterScoped:   true,
		Columns: []Column{
			{Title: "Name", MinWidth: 20},
			{Title: "Role", MinWidth: 20},
			{Title: "Age", MinWidth: 8},
		},
		Fetcher: func(ctx context.Context, cs kubernetes.Interface, _ string) ([]ResourceItem, error) {
			return fetchClusterRoleBindings(ctx, cs)
		},
		Detailer: detailClusterRoleBinding,
		WatchStarter: func(ctx context.Context, cs kubernetes.Interface, _ string) (watch.Interface, error) {
			return cs.RbacV1().ClusterRoleBindings().Watch(ctx, metav1.ListOptions{})
		},
	})

	// Roles
	DefaultRegistry.Register(&ResourceDefinition{
		Type:            ResourceRoles,
		DisplayName:     "Roles",
		KubectlName:     "role",
		Category:        "RBAC",
		CategoryOrder:   5,
		OrderInCategory: 2,
		Columns: []Column{
			{Title: "Name", MinWidth: 20},
			{Title: "Namespace", MinWidth: 15},
			{Title: "Age", MinWidth: 8},
		},
		Fetcher:  fetchRoles,
		Detailer: detailRole,
		WatchStarter: func(ctx context.Context, cs kubernetes.Interface, ns string) (watch.Interface, error) {
			return cs.RbacV1().Roles(ns).Watch(ctx, metav1.ListOptions{})
		},
	})

	// RoleBindings
	DefaultRegistry.Register(&ResourceDefinition{
		Type:            ResourceRoleBindings,
		DisplayName:     "RoleBindings",
		KubectlName:     "rolebinding",
		Category:        "RBAC",
		CategoryOrder:   5,
		OrderInCategory: 3,
		Columns: []Column{
			{Title: "Name", MinWidth: 20},
			{Title: "Namespace", MinWidth: 15},
			{Title: "Role", MinWidth: 20},
			{Title: "Age", MinWidth: 8},
		},
		Fetcher:  fetchRoleBindings,
		Detailer: detailRoleBinding,
		WatchStarter: func(ctx context.Context, cs kubernetes.Interface, ns string) (watch.Interface, error) {
			return cs.RbacV1().RoleBindings(ns).Watch(ctx, metav1.ListOptions{})
		},
	})

	// ServiceAccounts
	DefaultRegistry.Register(&ResourceDefinition{
		Type:            ResourceServiceAccounts,
		DisplayName:     "ServiceAccounts",
		KubectlName:     "serviceaccount",
		Category:        "RBAC",
		CategoryOrder:   5,
		OrderInCategory: 4,
		Columns: []Column{
			{Title: "Name", MinWidth: 20},
			{Title: "Secrets", MinWidth: 8},
			{Title: "Age", MinWidth: 8},
		},
		Fetcher:  fetchServiceAccounts,
		Detailer: detailServiceAccount,
		WatchStarter: func(ctx context.Context, cs kubernetes.Interface, ns string) (watch.Interface, error) {
			return cs.CoreV1().ServiceAccounts(ns).Watch(ctx, metav1.ListOptions{})
		},
	})

	// -----------------------------------------------------------------------
	// Autoscaling (order 6)
	// -----------------------------------------------------------------------

	// HorizontalPodAutoscalers — drill-down to target workload's pods
	DefaultRegistry.Register(&ResourceDefinition{
		Type:            ResourceHorizontalPodAutoscalers,
		DisplayName:     "HorizontalPodAutoscalers",
		KubectlName:     "horizontalpodautoscaler",
		Category:        "Autoscaling",
		CategoryOrder:   6,
		OrderInCategory: 0,
		Columns: []Column{
			{Title: "Name", MinWidth: 20},
			{Title: "Reference", MinWidth: 20},
			{Title: "Min", MinWidth: 5},
			{Title: "Max", MinWidth: 5},
			{Title: "Replicas", MinWidth: 8},
			{Title: "Targets", MinWidth: 12},
			{Title: "Age", MinWidth: 8},
		},
		Fetcher:  fetchHorizontalPodAutoscalers,
		Detailer: detailHorizontalPodAutoscaler,
		WatchStarter: func(ctx context.Context, cs kubernetes.Interface, ns string) (watch.Interface, error) {
			return cs.AutoscalingV2().HorizontalPodAutoscalers(ns).Watch(ctx, metav1.ListOptions{})
		},
		DrillDown: &DrillDownConfig{
			ChildTypeFor:  hpaTargetChildType,
			FetchChildren: fetchHPATargetDrillDown,
		},
	})

	// PodDisruptionBudgets — drill-down to selector-matched pods
	DefaultRegistry.Register(&ResourceDefinition{
		Type:            ResourcePodDisruptionBudgets,
		DisplayName:     "PodDisruptionBudgets",
		KubectlName:     "poddisruptionbudget",
		Category:        "Autoscaling",
		CategoryOrder:   6,
		OrderInCategory: 1,
		Columns: []Column{
			{Title: "Name", MinWidth: 20},
			{Title: "Min Available", MinWidth: 12},
			{Title: "Max Unavailable", MinWidth: 14},
			{Title: "Allowed", MinWidth: 8},
			{Title: "Age", MinWidth: 8},
		},
		Fetcher:  fetchPodDisruptionBudgets,
		Detailer: detailPodDisruptionBudget,
		WatchStarter: func(ctx context.Context, cs kubernetes.Interface, ns string) (watch.Interface, error) {
			return cs.PolicyV1().PodDisruptionBudgets(ns).Watch(ctx, metav1.ListOptions{})
		},
		DrillDown: &DrillDownConfig{
			ChildTypeFor:  StaticChildType(ResourcePods),
			FetchChildren: fetchPodsForPDBDrillDown,
		},
	})
}
