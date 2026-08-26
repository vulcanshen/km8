package k8s

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	sigsyaml "sigs.k8s.io/yaml"
)

// MarshalItemYAML returns a YAML representation of a resource item for the
// detail panel. ManagedFields are stripped for readability (matching kubectl's
// default behavior since 1.21). Returns an empty string when the item's Raw
// payload cannot be marshaled — callers should fall back to structured detail.
func MarshalItemYAML(item ResourceItem) string {
	if item.Raw == nil {
		return ""
	}
	// Contexts are not k8s objects — render a redacted, kubeconfig-shaped
	// view (allowlist; credentials never included) instead of marshaling.
	if ci, ok := item.Raw.(ContextInfo); ok {
		return contextYAML(ci)
	}
	obj, ok := item.Raw.(runtime.Object)
	if !ok {
		return ""
	}
	copied := obj.DeepCopyObject()
	// Restore apiVersion/kind on typed objects. client-go strips TypeMeta when
	// caching from list/watch because it is redundant at runtime; for YAML we
	// want it back. Unstructured objects (CRDs) keep their own GVK.
	if _, isUnstructured := copied.(*unstructured.Unstructured); !isUnstructured {
		if gvks, _, err := scheme.Scheme.ObjectKinds(copied); err == nil && len(gvks) > 0 {
			copied.GetObjectKind().SetGroupVersionKind(gvks[0])
		}
	}
	if accessor, err := meta.Accessor(copied); err == nil {
		accessor.SetManagedFields(nil)
	}
	out, err := sigsyaml.Marshal(copied)
	if err != nil {
		return ""
	}
	return string(out)
}

// MarshalItemYAMLForCompare returns the YAML representation of a resource
// item suitable for a diff against another instance of the same kind.
// Strips fields that are intrinsically per-instance noise:
//
//   - .status entirely — Kubernetes reconciles config → status, so any
//     legitimate config drift will surface on the next reconcile and
//     status drift between two reconciled resources is just churn.
//   - .metadata.managedFields  — server-side apply bookkeeping; the
//     same value would never line up between two instances.
//   - .metadata.resourceVersion, .uid, .creationTimestamp,
//     .generation — bookkeeping that increments / is unique per
//     instance and writes noise into every diff.
//
// Caller-controlled choice (not stripped here): namespace + name —
// those are the IDENTITY of each instance; surfacing them in the diff
// lets the user verify which side is which.
func MarshalItemYAMLForCompare(item ResourceItem) string {
	if item.Raw == nil {
		return ""
	}
	obj, ok := item.Raw.(runtime.Object)
	if !ok {
		return ""
	}
	copied := obj.DeepCopyObject()
	if _, isUnstructured := copied.(*unstructured.Unstructured); !isUnstructured {
		if gvks, _, err := scheme.Scheme.ObjectKinds(copied); err == nil && len(gvks) > 0 {
			copied.GetObjectKind().SetGroupVersionKind(gvks[0])
		}
	}
	if accessor, err := meta.Accessor(copied); err == nil {
		accessor.SetManagedFields(nil)
		accessor.SetResourceVersion("")
		accessor.SetUID("")
		accessor.SetGeneration(0)
		accessor.SetCreationTimestamp(metav1.Time{})
	}
	out, err := sigsyaml.Marshal(copied)
	if err != nil {
		return ""
	}
	// Strip the entire `status:` block via a YAML round-trip through a
	// generic map — the typed-object accessor doesn't expose status as
	// a single field across all kinds, but every K8s resource serializes
	// to YAML with a top-level `status:` key (or none, if not populated).
	return stripStatusBlock(string(out))
}

// stripStatusBlock removes the top-level `status:` mapping from a YAML
// document. Operates on the YAML text — round-tripping through a typed
// map would lose comment / ordering signals and require parsing every
// kind. Instead: find a line beginning with "status:" at column 0, drop
// it and every following indented continuation, stop at the next column-
// 0 key. Idempotent and safe on docs without a status block.
func stripStatusBlock(yamlText string) string {
	lines := strings.Split(yamlText, "\n")
	out := make([]string, 0, len(lines))
	skipping := false
	for _, ln := range lines {
		if skipping {
			if ln == "" {
				continue
			}
			// Continue skipping as long as the line is indented (part of
			// the status block) or blank.
			if ln[0] == ' ' || ln[0] == '\t' {
				continue
			}
			// Column-0 line — status block is over.
			skipping = false
		}
		if strings.HasPrefix(ln, "status:") {
			skipping = true
			continue
		}
		out = append(out, ln)
	}
	return strings.Join(out, "\n")
}

// MarshalContainerYAML extracts a single container's spec and status from the
// parent Pod (wrapped in item.Raw) and returns them as a YAML document.
// Returns empty string when item is not a Pod or the container is not found.
func MarshalContainerYAML(item ResourceItem, containerName string) string {
	if item.Raw == nil || containerName == "" {
		return ""
	}
	pod, ok := item.Raw.(*corev1.Pod)
	if !ok {
		return ""
	}
	type view struct {
		Spec   *corev1.Container       `json:"spec,omitempty"`
		Status *corev1.ContainerStatus `json:"status,omitempty"`
	}
	var v view
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name == containerName {
			v.Spec = &pod.Spec.Containers[i]
			break
		}
	}
	if v.Spec == nil {
		for i := range pod.Spec.InitContainers {
			if pod.Spec.InitContainers[i].Name == containerName {
				v.Spec = &pod.Spec.InitContainers[i]
				break
			}
		}
	}
	for i := range pod.Status.ContainerStatuses {
		if pod.Status.ContainerStatuses[i].Name == containerName {
			v.Status = &pod.Status.ContainerStatuses[i]
			break
		}
	}
	if v.Status == nil {
		for i := range pod.Status.InitContainerStatuses {
			if pod.Status.InitContainerStatuses[i].Name == containerName {
				v.Status = &pod.Status.InitContainerStatuses[i]
				break
			}
		}
	}
	if v.Spec == nil && v.Status == nil {
		return ""
	}
	out, err := sigsyaml.Marshal(v)
	if err != nil {
		return ""
	}
	return string(out)
}
