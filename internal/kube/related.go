package kube

// What the detail panel draws above the report: an object's conditions, the
// objects it names or is owned by, and the pods it selects.
//
// Every one of these is in the YAML already, but as a string in a field -- a
// claim name, a node name, an owner's UID -- and reading one means leaving the
// panel to go and find it. Laid out here each is a link that opens the object
// it names in the same panel.

import (
	"context"
	"slices"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Reference is one object another one names.
type Reference struct {
	// Role is how the object is related: "Owner", "Node", "Config map".
	Role string `json:"role"`
	// Kind is the kind as the app opens it, or empty for one it cannot open --
	// a core kind it has no view for.
	Kind      string `json:"kind"`
	APIKind   string `json:"apiKind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// Condition is one entry of an object's status.conditions, toned by what its
// status means for that particular condition.
type Condition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
	Age     string `json:"age"`
	Tone    string `json:"tone"`
}

// RelatedPod is one pod an object selects, as the pods table would show it.
type RelatedPod struct {
	Namespace  string `json:"namespace"`
	Name       string `json:"name"`
	Status     Cell   `json:"status"`
	Ready      Cell   `json:"ready"`
	Restarts   Cell   `json:"restarts"`
	Node       string `json:"node"`
	Age        string `json:"age"`
	Containers []Pill `json:"containers"`
}

// ObjectLinks is everything the panel's summary sections show.
type ObjectLinks struct {
	Conditions []Condition `json:"conditions"`
	References []Reference `json:"references"`
	// Pods is nil for a kind that selects none, and empty for one that could
	// and selects nothing right now -- which the panel says, since a Service
	// with no pods behind it is the thing someone opened it to find out.
	Pods []RelatedPod `json:"pods"`
	// PodsTotal is how many pods matched, of which Pods holds the first few.
	PodsTotal int `json:"podsTotal"`
	// Selector is the label selector the pods were found by.
	Selector string `json:"selector"`
}

// maxRelatedPods bounds the pod list. A DaemonSet on a large cluster selects a
// pod per node, and the panel is a summary rather than the pods table.
const maxRelatedPods = 50

// negativeConditions are the condition types whose True is the bad news:
// a node under pressure, a deployment that failed to create a replica set.
var negativeConditions = map[string]bool{
	"MemoryPressure":     true,
	"DiskPressure":       true,
	"PIDPressure":        true,
	"NetworkUnavailable": true,
	"ReplicaFailure":     true,
	"Failed":             true,
	"FailureTarget":      true,
	"Denied":             true,
	"Dangling":           true,
	"Stalled":            true,
	"Degraded":           true,
	"DisruptionTarget":   true,
	"EvictionInProgress": true,
	"KernelDeadlock":     true,
	"ReadonlyFilesystem": true,
	"Terminating":        true,
	"Suspended":          true,
}

// conditionMeaning says what a condition's status means. True is good unless the
// condition names a problem; False is a warning rather than an error, because
// "not yet Complete" is not a failure.
func conditionMeaning(kind, status string) string {
	bad := negativeConditions[kind]
	switch status {
	case "True":
		if bad {
			return "error"
		}
		return "ok"
	case "False":
		if bad {
			return "ok"
		}
		return "warn"
	default:
		return "info"
	}
}

// conditionsOf reads status.conditions in the order the object lists them.
func conditionsOf(u *unstructured.Unstructured) []Condition {
	out := []Condition{}
	for _, raw := range nestedSlice(u, "status", "conditions") {
		c := asMap(raw)
		kind := mapString(c, "type")
		if kind == "" {
			continue
		}
		// Different kinds stamp different times; the transition is the one
		// that says how long things have been this way.
		when := ""
		for _, field := range []string{"lastTransitionTime", "lastUpdateTime", "lastHeartbeatTime", "lastProbeTime"} {
			if ts := mapString(c, field); ts != "" {
				when = since(ts)
				break
			}
		}
		status := mapString(c, "status")
		if status == "" {
			// A signing request's conditions leave it out, and mean True.
			status = "True"
		}
		out = append(out, Condition{
			Type:    kind,
			Status:  status,
			Reason:  mapString(c, "reason"),
			Message: mapString(c, "message"),
			Age:     when,
			Tone:    conditionMeaning(kind, status),
		})
	}
	return out
}

// refList collects references without repeating one: a pod naming the same
// config map as a volume and as an environment source lists it once.
type refList struct {
	out  []Reference
	seen map[string]bool
}

func (r *refList) add(role, kind, apiKind, namespace, name string) {
	if name == "" {
		return
	}
	key := role + "|" + kind + "|" + apiKind + "|" + namespace + "|" + name
	if r.seen == nil {
		r.seen = map[string]bool{}
	}
	if r.seen[key] {
		return
	}
	r.seen[key] = true
	r.out = append(r.out, Reference{Role: role, Kind: kind, APIKind: apiKind, Namespace: namespace, Name: name})
}

// kindForResource names a resource the way the app opens it. A built-in kind's
// name is its plural, which is what makes this a lookup rather than a table.
func kindForResource(group, resource string) string {
	if gk, ok := builtinKinds[resource]; ok && gk.Group == group {
		return resource
	}
	if group == "" {
		return ""
	}
	return CustomKind(resource, group)
}

// kindFor names an API kind the way the app opens it, asking the cluster for
// the plural of one the app does not know.
func (c *clusterClient) kindFor(apiVersion, apiKind string) string {
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return ""
	}
	gk := schema.GroupKind{Group: gv.Group, Kind: apiKind}
	if kind, ok := builtinByGroupKind[gk]; ok {
		return kind
	}
	if gk.Group == "" {
		return ""
	}
	mapping, err := c.mapper.RESTMapping(gk)
	if err != nil {
		return ""
	}
	return CustomKind(mapping.Resource.Resource, gk.Group)
}

// podTemplateRefs lists what a pod spec names: its node, identity, volumes and
// the config maps and secrets it reads.
func podTemplateRefs(r *refList, spec map[string]any, namespace string) {
	if spec == nil {
		return
	}
	r.add("Node", KindNodes, "Node", "", mapString(spec, "nodeName"))
	r.add("Service account", KindServiceAccounts, "ServiceAccount", namespace, mapString(spec, "serviceAccountName"))
	r.add("Priority class", KindPriorityClasses, "PriorityClass", "", mapString(spec, "priorityClassName"))
	r.add("Runtime class", KindRuntimeClasses, "RuntimeClass", "", mapString(spec, "runtimeClassName"))

	for _, raw := range asSlice(spec["volumes"]) {
		v := asMap(raw)
		r.add("Volume claim", KindPVCs, "PersistentVolumeClaim", namespace, mapString(asMap(v["persistentVolumeClaim"]), "claimName"))
		r.add("Config map", KindConfigMaps, "ConfigMap", namespace, mapString(asMap(v["configMap"]), "name"))
		r.add("Secret", KindSecrets, "Secret", namespace, mapString(asMap(v["secret"]), "secretName"))
		for _, src := range asSlice(asMap(v["projected"])["sources"]) {
			s := asMap(src)
			r.add("Config map", KindConfigMaps, "ConfigMap", namespace, mapString(asMap(s["configMap"]), "name"))
			r.add("Secret", KindSecrets, "Secret", namespace, mapString(asMap(s["secret"]), "name"))
		}
	}
	for _, raw := range asSlice(spec["imagePullSecrets"]) {
		r.add("Pull secret", KindSecrets, "Secret", namespace, mapString(asMap(raw), "name"))
	}
	for _, raw := range asSlice(spec["resourceClaims"]) {
		claim := asMap(raw)
		r.add("Resource claim", KindResourceClaims, "ResourceClaim", namespace, mapString(claim, "resourceClaimName"))
		r.add("Claim template", KindResourceClaimTemplates, "ResourceClaimTemplate", namespace, mapString(claim, "resourceClaimTemplateName"))
	}

	var containers []any
	containers = append(containers, asSlice(spec["initContainers"])...)
	containers = append(containers, asSlice(spec["containers"])...)
	for _, raw := range containers {
		ctr := asMap(raw)
		for _, src := range asSlice(ctr["envFrom"]) {
			s := asMap(src)
			r.add("Config map", KindConfigMaps, "ConfigMap", namespace, mapString(asMap(s["configMapRef"]), "name"))
			r.add("Secret", KindSecrets, "Secret", namespace, mapString(asMap(s["secretRef"]), "name"))
		}
		for _, env := range asSlice(ctr["env"]) {
			from := asMap(asMap(env)["valueFrom"])
			r.add("Config map", KindConfigMaps, "ConfigMap", namespace, mapString(asMap(from["configMapKeyRef"]), "name"))
			r.add("Secret", KindSecrets, "Secret", namespace, mapString(asMap(from["secretKeyRef"]), "name"))
		}
	}
}

// referencesOf lists what an object names. Owners come first, since "what made
// this" is the question asked most often of anything a controller created.
func (c *clusterClient) referencesOf(kind string, u *unstructured.Unstructured) []Reference {
	r := &refList{}
	ns := u.GetNamespace()

	for _, owner := range u.GetOwnerReferences() {
		role := "Owner"
		if owner.Controller != nil && *owner.Controller {
			role = "Controlled by"
		}
		r.add(role, c.kindFor(owner.APIVersion, owner.Kind), owner.Kind, ns, owner.Name)
	}

	switch kind {
	case KindPods:
		podTemplateRefs(r, asMap(nestedMap(u, "spec")), ns)
		for _, raw := range nestedSlice(u, "status", "resourceClaimStatuses") {
			r.add("Resource claim", KindResourceClaims, "ResourceClaim", ns, mapString(asMap(raw), "resourceClaimName"))
		}

	case KindDeployments, KindStatefulSet, KindDaemonSets, KindReplicaSets, KindReplicationControllers, KindJobs:
		podTemplateRefs(r, asMap(nestedMap(u, "spec", "template", "spec")), ns)
		r.add("Governing service", KindServices, "Service", ns, nestedString(u, "spec", "serviceName"))
		for _, raw := range nestedSlice(u, "spec", "volumeClaimTemplates") {
			// Each pod gets its own claim, named after the template and the pod.
			r.add("Claim template", "", "PersistentVolumeClaim", ns, mapString(asMap(asMap(raw)["metadata"]), "name"))
		}

	case KindCronJobs:
		for _, raw := range nestedSlice(u, "status", "active") {
			r.add("Active job", KindJobs, "Job", ns, mapString(asMap(raw), "name"))
		}
		podTemplateRefs(r, asMap(nestedMap(u, "spec", "jobTemplate", "spec", "template", "spec")), ns)

	case KindHPAs:
		target := asMap(nestedMap(u, "spec", "scaleTargetRef"))
		r.add("Scales", c.kindFor(mapString(target, "apiVersion"), mapString(target, "kind")), mapString(target, "kind"), ns, mapString(target, "name"))

	case KindIngresses:
		r.add("Class", KindIngressClasses, "IngressClass", "", nestedString(u, "spec", "ingressClassName"))
		r.add("Backend", KindServices, "Service", ns, nestedString(u, "spec", "defaultBackend", "service", "name"))
		for _, rule := range nestedSlice(u, "spec", "rules") {
			for _, path := range asSlice(asMap(asMap(rule)["http"])["paths"]) {
				backend := asMap(asMap(path)["backend"])
				r.add("Backend", KindServices, "Service", ns, mapString(asMap(backend["service"]), "name"))
			}
		}
		for _, tls := range nestedSlice(u, "spec", "tls") {
			r.add("TLS secret", KindSecrets, "Secret", ns, mapString(asMap(tls), "secretName"))
		}

	case KindPVCs:
		r.add("Volume", KindPVs, "PersistentVolume", "", nestedString(u, "spec", "volumeName"))
		r.add("Storage class", KindStorageClasses, "StorageClass", "", nestedString(u, "spec", "storageClassName"))
		r.add("Attributes class", KindVolumeAttributesClasses, "VolumeAttributesClass", "", nestedString(u, "spec", "volumeAttributesClassName"))

	case KindPVs:
		r.add("Claim", KindPVCs, "PersistentVolumeClaim", nestedString(u, "spec", "claimRef", "namespace"), nestedString(u, "spec", "claimRef", "name"))
		r.add("Storage class", KindStorageClasses, "StorageClass", "", nestedString(u, "spec", "storageClassName"))
		r.add("Attributes class", KindVolumeAttributesClasses, "VolumeAttributesClass", "", nestedString(u, "spec", "volumeAttributesClassName"))

	case KindRoleBindings, KindClusterRoleBindings:
		roleKind := nestedString(u, "roleRef", "kind")
		roleNS := ""
		target := KindClusterRoles
		if roleKind == "Role" {
			target, roleNS = KindRoles, ns
		}
		r.add("Role", target, roleKind, roleNS, nestedString(u, "roleRef", "name"))
		for _, raw := range nestedSlice(u, "subjects") {
			s := asMap(raw)
			if mapString(s, "kind") == "ServiceAccount" {
				r.add("Subject", KindServiceAccounts, "ServiceAccount", mapString(s, "namespace"), mapString(s, "name"))
			} else {
				r.add("Subject", "", mapString(s, "kind"), "", mapString(s, "name"))
			}
		}

	case KindServiceAccounts:
		for _, raw := range nestedSlice(u, "secrets") {
			r.add("Secret", KindSecrets, "Secret", ns, mapString(asMap(raw), "name"))
		}
		for _, raw := range nestedSlice(u, "imagePullSecrets") {
			r.add("Pull secret", KindSecrets, "Secret", ns, mapString(asMap(raw), "name"))
		}

	case KindEndpointSlices:
		r.add("Service", KindServices, "Service", ns, u.GetLabels()["kubernetes.io/service-name"])
		for _, raw := range nestedSlice(u, "endpoints") {
			e := asMap(raw)
			target := asMap(e["targetRef"])
			if mapString(target, "kind") == "Pod" {
				r.add("Endpoint", KindPods, "Pod", mapString(target, "namespace"), mapString(target, "name"))
			}
			r.add("Node", KindNodes, "Node", "", mapString(e, "nodeName"))
		}

	case KindEndpoints:
		r.add("Service", KindServices, "Service", ns, u.GetName())

	case KindServices:
		// The slices a Service's controller keeps are named after it, and
		// labelled with its name rather than owned in any way worth reading.
		r.add("Endpoints", KindEndpoints, "Endpoints", ns, u.GetName())

	case KindGateways:
		r.add("Class", KindGatewayClasses, "GatewayClass", "", nestedString(u, "spec", "gatewayClassName"))
		for _, raw := range nestedSlice(u, "spec", "listeners") {
			for _, cert := range asSlice(asMap(asMap(raw)["tls"])["certificateRefs"]) {
				ref := asMap(cert)
				certNS := mapString(ref, "namespace")
				if certNS == "" {
					certNS = ns
				}
				if kind := mapString(ref, "kind"); kind == "" || kind == "Secret" {
					r.add("Certificate", KindSecrets, "Secret", certNS, mapString(ref, "name"))
				}
			}
		}

	case KindHTTPRoutes, KindGRPCRoutes, KindTLSRoutes, KindTCPRoutes, KindUDPRoutes, KindListenerSets:
		parents := nestedSlice(u, "spec", "parentRefs")
		if kind == KindListenerSets {
			parents = []any{nestedMap(u, "spec", "parentRef")}
		}
		for _, raw := range parents {
			p := asMap(raw)
			kindName := mapString(p, "kind")
			target := KindGateways
			switch kindName {
			case "ListenerSet":
				target = KindListenerSets
			case "":
				kindName = "Gateway"
			}
			parentNS := mapString(p, "namespace")
			if parentNS == "" {
				parentNS = ns
			}
			r.add("Parent", target, kindName, parentNS, mapString(p, "name"))
		}
		for _, rule := range nestedSlice(u, "spec", "rules") {
			for _, raw := range asSlice(asMap(rule)["backendRefs"]) {
				b := asMap(raw)
				if kind := mapString(b, "kind"); kind != "" && kind != "Service" {
					continue
				}
				backendNS := mapString(b, "namespace")
				if backendNS == "" {
					backendNS = ns
				}
				r.add("Backend", KindServices, "Service", backendNS, mapString(b, "name"))
			}
		}

	case KindMutatingWebhooks, KindValidatingWebhooks:
		for _, raw := range nestedSlice(u, "webhooks") {
			svc := asMap(asMap(asMap(raw)["clientConfig"])["service"])
			r.add("Service", KindServices, "Service", mapString(svc, "namespace"), mapString(svc, "name"))
		}

	case KindMutatingAdmissionPolicyBindings:
		r.add("Policy", KindMutatingAdmissionPolicies, "MutatingAdmissionPolicy", "", nestedString(u, "spec", "policyName"))

	case KindValidatingAdmissionPolicyBindings:
		r.add("Policy", KindValidatingAdmissionPolicies, "ValidatingAdmissionPolicy", "", nestedString(u, "spec", "policyName"))

	case KindAPIServices:
		r.add("Service", KindServices, "Service", nestedString(u, "spec", "service", "namespace"), nestedString(u, "spec", "service", "name"))

	case KindFlowSchemas:
		r.add("Priority level", KindPriorityLevelConfigurations, "PriorityLevelConfiguration", "", nestedString(u, "spec", "priorityLevelConfiguration", "name"))

	case KindVolumeAttachments:
		r.add("Volume", KindPVs, "PersistentVolume", "", nestedString(u, "spec", "source", "persistentVolumeName"))
		r.add("Node", KindNodes, "Node", "", nestedString(u, "spec", "nodeName"))

	case KindCSINodes:
		r.add("Node", KindNodes, "Node", "", u.GetName())
		for _, raw := range nestedSlice(u, "spec", "drivers") {
			r.add("Driver", KindCSIDrivers, "CSIDriver", "", mapString(asMap(raw), "name"))
		}

	case KindCSIStorageCapacities:
		r.add("Storage class", KindStorageClasses, "StorageClass", "", nestedString(u, "storageClassName"))

	case KindStorageClasses:
		r.add("Driver", KindCSIDrivers, "CSIDriver", "", nestedString(u, "provisioner"))

	case KindResourceClaims:
		for _, class := range requestedClasses(nestedSlice(u, "spec", "devices", "requests")) {
			r.add("Device class", KindDeviceClasses, "DeviceClass", "", class)
		}
		for _, raw := range nestedSlice(u, "status", "reservedFor") {
			res := asMap(raw)
			r.add("Reserved for", kindForResource(mapString(res, "apiGroup"), mapString(res, "resource")), "", ns, mapString(res, "name"))
		}

	case KindResourceClaimTemplates:
		for _, class := range requestedClasses(nestedSlice(u, "spec", "spec", "devices", "requests")) {
			r.add("Device class", KindDeviceClasses, "DeviceClass", "", class)
		}

	case KindResourceSlices:
		r.add("Node", KindNodes, "Node", "", nestedString(u, "spec", "nodeName"))

	case KindPodGroups, KindCompositePodGroups:
		r.add("Workload", KindWorkloads, "Workload", ns, nestedString(u, "spec", "workloadRef", "workloadName"))
		r.add("Parent group", KindCompositePodGroups, "CompositePodGroup", ns, nestedString(u, "spec", "parentCompositePodGroupName"))
		r.add("Priority class", KindPriorityClasses, "PriorityClass", "", nestedString(u, "spec", "priorityClassName"))

	case KindWorkloads:
		ctrl := asMap(nestedMap(u, "spec", "controllerRef"))
		group := mapString(ctrl, "apiGroup")
		apiVersion := group + "/v1"
		if group == "" {
			apiVersion = "v1"
		}
		r.add("Controller", c.kindFor(apiVersion, mapString(ctrl, "kind")), mapString(ctrl, "kind"), ns, mapString(ctrl, "name"))

	case KindEvictionRequests, KindEvictions:
		r.add("Pod", KindPods, "Pod", ns, nestedString(u, "spec", "target", "pod", "name"))

	case KindPodCertificateRequests:
		r.add("Pod", KindPods, "Pod", ns, nestedString(u, "spec", "podName"))
		r.add("Service account", KindServiceAccounts, "ServiceAccount", ns, nestedString(u, "spec", "serviceAccountName"))
		r.add("Node", KindNodes, "Node", "", nestedString(u, "spec", "nodeName"))

	case KindIPAddresses:
		parent := asMap(nestedMap(u, "spec", "parentRef"))
		r.add("Parent", kindForResource(mapString(parent, "group"), mapString(parent, "resource")), "", mapString(parent, "namespace"), mapString(parent, "name"))

	case KindLeaseCandidates:
		r.add("Lease", KindLeases, "Lease", ns, nestedString(u, "spec", "leaseName"))

	case KindPDBs, KindNetworkPolicies:
		// Their pods are listed below; there is nothing else to name.
	}

	if r.out == nil {
		return []Reference{}
	}
	return r.out
}

// podSelectorOf reads the selector a kind finds its pods by, or reports that
// the kind selects none. Services and ReplicationControllers write theirs as a
// plain map; everything else as a label selector, which selectorFor reads.
func podSelectorOf(kind string, u *unstructured.Unstructured) (string, bool) {
	switch kind {
	case KindServices, KindReplicationControllers:
		labels, _, _ := unstructured.NestedStringMap(u.Object, "spec", "selector")
		if len(labels) == 0 {
			return "", false
		}
		parts := make([]string, 0, len(labels))
		for k, v := range labels {
			parts = append(parts, k+"="+v)
		}
		sort.Strings(parts)
		return strings.Join(parts, ","), true

	case KindDeployments, KindStatefulSet, KindDaemonSets, KindReplicaSets, KindJobs, KindPDBs:
		selector, err := selectorFor(u)
		return selector, err == nil

	case KindNetworkPolicies:
		// An empty pod selector is every pod in the namespace, which is a
		// real answer here rather than a missing one.
		raw, _, _ := unstructured.NestedMap(u.Object, "spec", "podSelector")
		probe := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{"selector": raw}}}
		selector, err := selectorFor(probe)
		if err != nil {
			return "", true
		}
		return selector, true
	}
	return "", false
}

// relatedPod projects one pod the way its row in the pods table reads.
func relatedPod(pod *unstructured.Unstructured) RelatedPod {
	return RelatedPod{
		Namespace:  pod.GetNamespace(),
		Name:       pod.GetName(),
		Status:     podStatus(pod),
		Ready:      podReady(pod),
		Restarts:   podRestarts(pod),
		Node:       nestedString(pod, "spec", "nodeName"),
		Age:        ageOf(pod),
		Containers: podContainers(pod).Pills,
	}
}

// ObjectLinks reads what the panel's summary sections show for one object.
func (w *Watcher) ObjectLinks(kc Context, kind, namespace, name string) (ObjectLinks, error) {
	out := ObjectLinks{Conditions: []Condition{}, References: []Reference{}}
	err := w.withClient(kc, func(c *clusterClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()

		u, _, err := c.get(ctx, kind, namespace, name)
		if err != nil {
			return err
		}
		out.Conditions = conditionsOf(u)
		out.References = c.referencesOf(kind, u)

		selector, selects := podSelectorOf(kind, u)
		if !selects {
			return nil
		}
		out.Selector = selector
		out.Pods = []RelatedPod{}
		// A pod listing that fails -- no RBAC on pods, most likely -- costs
		// the section rather than the whole summary.
		pods, err := c.podsMatching(ctx, u.GetNamespace(), selector)
		if err != nil {
			out.Pods = nil
			return nil
		}
		out.PodsTotal = len(pods)
		for i := range pods {
			if i == maxRelatedPods {
				break
			}
			out.Pods = append(out.Pods, relatedPod(pods[i]))
		}
		return nil
	})
	return out, err
}

// podsMatching lists the pods in a namespace a selector matches, by name.
func (c *clusterClient) podsMatching(ctx context.Context, namespace, selector string) ([]*unstructured.Unstructured, error) {
	mapping, err := c.mappingForKind(KindPods)
	if err != nil {
		return nil, err
	}
	list, err := resourceFor(c.dynamic, mapping, namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, err
	}
	out := make([]*unstructured.Unstructured, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, &list.Items[i])
	}
	slices.SortFunc(out, func(a, b *unstructured.Unstructured) int { return strings.Compare(a.GetName(), b.GetName()) })
	return out, nil
}
