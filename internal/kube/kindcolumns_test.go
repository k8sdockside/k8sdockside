package kube

import (
	"testing"
)

// The kinds that fill in the rest of what Kubernetes serves. As in
// kinds_test.go, what is pinned is the columns that are computed rather than
// read straight off a field.

func TestEveryKindKubernetesServesIsOffered(t *testing.T) {
	// The persisted, listable kinds of k8s.io/api v0.37, by the group and kind
	// the cluster serves them as. ComponentStatus is left out on purpose: it
	// is deprecated and cannot be watched, and a live table is a watch.
	want := []struct{ group, kind string }{
		{"", "Pod"}, {"", "PodTemplate"}, {"", "ReplicationController"}, {"", "Service"},
		{"", "Endpoints"}, {"", "Node"}, {"", "Event"}, {"", "LimitRange"},
		{"", "ResourceQuota"}, {"", "Namespace"}, {"", "Secret"}, {"", "ServiceAccount"},
		{"", "PersistentVolume"}, {"", "PersistentVolumeClaim"}, {"", "ConfigMap"},
		{AdmissionGroup, "ValidatingWebhookConfiguration"}, {AdmissionGroup, "MutatingWebhookConfiguration"},
		{AdmissionGroup, "ValidatingAdmissionPolicy"}, {AdmissionGroup, "ValidatingAdmissionPolicyBinding"},
		{AdmissionGroup, "MutatingAdmissionPolicy"}, {AdmissionGroup, "MutatingAdmissionPolicyBinding"},
		{"internal.apiserver.k8s.io", "StorageVersion"},
		{"apiregistration.k8s.io", "APIService"},
		{"apiextensions.k8s.io", "CustomResourceDefinition"},
		{"apps", "Deployment"}, {"apps", "StatefulSet"}, {"apps", "DaemonSet"}, {"apps", "ReplicaSet"},
		{"apps", "ControllerRevision"},
		{"autoscaling", "HorizontalPodAutoscaler"},
		{"batch", "Job"}, {"batch", "CronJob"},
		{CertificatesGroup, "CertificateSigningRequest"}, {CertificatesGroup, "ClusterTrustBundle"},
		{CertificatesGroup, "PodCertificateRequest"},
		{"coordination.k8s.io", "Lease"}, {"coordination.k8s.io", "LeaseCandidate"},
		{"discovery.k8s.io", "EndpointSlice"},
		{FlowControlGroup, "FlowSchema"}, {FlowControlGroup, "PriorityLevelConfiguration"},
		{LifecycleGroup, "EvictionRequest"}, {LifecycleGroup, "Eviction"},
		{"networking.k8s.io", "Ingress"}, {"networking.k8s.io", "IngressClass"},
		{"networking.k8s.io", "NetworkPolicy"}, {"networking.k8s.io", "IPAddress"},
		{"networking.k8s.io", "ServiceCIDR"},
		{"node.k8s.io", "RuntimeClass"},
		{"policy", "PodDisruptionBudget"},
		{RBACGroup, "Role"}, {RBACGroup, "RoleBinding"}, {RBACGroup, "ClusterRole"}, {RBACGroup, "ClusterRoleBinding"},
		{DRAGroup, "DeviceClass"}, {DRAGroup, "DeviceTaintRule"}, {DRAGroup, "ResourceClaim"},
		{DRAGroup, "ResourceClaimTemplate"}, {DRAGroup, "ResourceSlice"}, {DRAGroup, "ResourcePoolStatusRequest"},
		{SchedulingGroup, "PriorityClass"}, {SchedulingGroup, "Workload"}, {SchedulingGroup, "PodGroup"},
		{SchedulingGroup, "CompositePodGroup"},
		{StorageGroup, "StorageClass"}, {StorageGroup, "VolumeAttachment"}, {StorageGroup, "CSINode"},
		{StorageGroup, "CSIDriver"}, {StorageGroup, "CSIStorageCapacity"}, {StorageGroup, "VolumeAttributesClass"},
		{"storagemigration.k8s.io", "StorageVersionMigration"},
	}
	for _, w := range want {
		found := false
		for _, gk := range builtinKinds {
			if gk.Group == w.group && gk.Kind == w.kind {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s/%s is served by Kubernetes but has no view", w.group, w.kind)
		}
	}
}

// A built-in kind's name is its plural, which kindForResource relies on to
// turn an IPAddress's parent or a claim's consumer into something to open.
func TestBuiltinKindsAreNamedAfterTheirPlural(t *testing.T) {
	for kind, gk := range builtinKinds {
		if got := kindForResource(gk.Group, kind); got != kind {
			t.Errorf("kindForResource(%q, %q) = %q", gk.Group, kind, got)
		}
	}
	if got := kindForResource("example.com", "widgets"); got != "crd:widgets.example.com" {
		t.Errorf("a resource the app does not know = %q, want the crd: form", got)
	}
	if got := kindForResource("", "bindings"); got != "" {
		t.Errorf("a core resource the app does not know = %q, want nothing to open", got)
	}
}

func TestCertificateSigningRequestsReadAsKubectlDoes(t *testing.T) {
	pending := project(t, KindCSRs, false, map[string]any{
		"metadata": map[string]any{"name": "csr-1"},
		"spec": map[string]any{
			"signerName":        "kubernetes.io/kubelet-serving",
			"username":          "system:node:worker-1",
			"expirationSeconds": int64(86400),
		},
	})
	want(t, pending, "Signer", "kubernetes.io/kubelet-serving")
	want(t, pending, "Requestor", "system:node:worker-1")
	want(t, pending, "Requested Duration", "24h")
	want(t, pending, "Condition", "Pending")

	issued := project(t, KindCSRs, false, map[string]any{
		"metadata": map[string]any{"name": "csr-2"},
		"spec":     map[string]any{"signerName": "kubernetes.io/kube-apiserver-client"},
		"status": map[string]any{
			"conditions":  []any{map[string]any{"type": "Approved", "status": "True"}},
			"certificate": "LS0tLS1CRUdJTg==",
		},
	})
	want(t, issued, "Condition", "Approved,Issued")
	want(t, issued, "Requested Duration", "<none>")
}

func TestCSRPendingOnlyUntilAnswered(t *testing.T) {
	csr := obj(map[string]any{"kind": "CertificateSigningRequest", "metadata": map[string]any{"name": "c"}})
	if !csrPending(csr) {
		t.Error("an unanswered request is not pending")
	}
	csr.Object["status"] = map[string]any{"conditions": []any{map[string]any{"type": "Denied", "status": "True"}}}
	if csrPending(csr) {
		t.Error("a denied request still reads as pending")
	}
}

func TestResourceClaimsSayWhetherTheyAreAllocated(t *testing.T) {
	pending := project(t, KindResourceClaims, true, map[string]any{
		"metadata": map[string]any{"name": "gpu", "namespace": "ml"},
		"spec": map[string]any{"devices": map[string]any{"requests": []any{
			map[string]any{"name": "gpu", "exactly": map[string]any{"deviceClassName": "gpu.nvidia.com"}},
			map[string]any{"name": "alt", "firstAvailable": []any{
				map[string]any{"name": "big", "deviceClassName": "gpu.example.com"},
			}},
		}}},
	})
	want(t, pending, "Device Classes", "gpu.nvidia.com, gpu.example.com")
	want(t, pending, "State", "Pending")

	reserved := project(t, KindResourceClaims, true, map[string]any{
		"metadata": map[string]any{"name": "gpu", "namespace": "ml"},
		"status": map[string]any{
			"allocation":  map[string]any{"devices": map[string]any{"results": []any{}}},
			"reservedFor": []any{map[string]any{"resource": "pods", "name": "trainer-0"}},
		},
	})
	want(t, reserved, "State", "Allocated,Reserved")
	want(t, reserved, "Reserved For", "pods/trainer-0")
}

func TestResourceSlicesSayWhichNodesTheyServe(t *testing.T) {
	onNode := project(t, KindResourceSlices, false, map[string]any{
		"metadata": map[string]any{"name": "worker-1-gpu"},
		"spec": map[string]any{
			"driver":   "gpu.nvidia.com",
			"nodeName": "worker-1",
			"pool":     map[string]any{"name": "worker-1"},
			"devices":  []any{map[string]any{"name": "gpu-0"}, map[string]any{"name": "gpu-1"}},
		},
	})
	want(t, onNode, "Node", "worker-1")
	want(t, onNode, "Pool", "worker-1")
	want(t, onNode, "Devices", "2")

	everywhere := project(t, KindResourceSlices, false, map[string]any{
		"metadata": map[string]any{"name": "network"},
		"spec":     map[string]any{"driver": "net.example.com", "allNodes": true},
	})
	want(t, everywhere, "Node", "all nodes")
}

func TestPodGroupsNameTheirPolicy(t *testing.T) {
	gang := project(t, KindPodGroups, true, map[string]any{
		"metadata": map[string]any{"name": "train-0", "namespace": "ml"},
		"spec": map[string]any{
			"workloadRef":      map[string]any{"workloadName": "train", "templateName": "workers"},
			"schedulingPolicy": map[string]any{"gang": map[string]any{"minCount": int64(4)}},
		},
		"status": map[string]any{"conditions": []any{
			map[string]any{"type": "PodGroupInitiallyScheduled", "status": "True"},
		}},
	})
	want(t, gang, "Workload", "train")
	want(t, gang, "Policy", "Gang (min 4)")
	want(t, gang, "Status", "Scheduled")

	basic := project(t, KindPodGroups, true, map[string]any{
		"metadata": map[string]any{"name": "web-0", "namespace": "ml"},
		"spec":     map[string]any{"schedulingPolicy": map[string]any{"basic": map[string]any{}}},
	})
	want(t, basic, "Policy", "Basic")
	want(t, basic, "Status", "Pending")
}

func TestEvictionsSayWhoTheyAreWaitingOn(t *testing.T) {
	cells := project(t, KindEvictions, true, map[string]any{
		"metadata": map[string]any{"name": "web-0", "namespace": "prod"},
		"spec":     map[string]any{"target": map[string]any{"pod": map[string]any{"name": "web-0"}}},
		"status": map[string]any{
			"requesters": []any{map[string]any{"name": "cluster-autoscaler"}},
			"targetResponders": []any{
				map[string]any{"name": "backup.example.com", "state": "Completed"},
				map[string]any{"name": "imperative-eviction.k8s.io/evictor", "state": "Active"},
			},
		},
	})
	want(t, cells, "Pod", "web-0")
	want(t, cells, "Requesters", "cluster-autoscaler")
	want(t, cells, "Responder", "imperative-eviction.k8s.io/evictor")
	want(t, cells, "Status", "In progress")
}

func TestAPIServicesSayWhyTheyAreUnavailable(t *testing.T) {
	down := project(t, KindAPIServices, false, map[string]any{
		"metadata": map[string]any{"name": "v1beta1.metrics.k8s.io"},
		"spec":     map[string]any{"service": map[string]any{"namespace": "kube-system", "name": "metrics-server"}},
		"status": map[string]any{"conditions": []any{
			map[string]any{"type": "Available", "status": "False", "reason": "MissingEndpoints"},
		}},
	})
	want(t, down, "Service", "kube-system/metrics-server")
	want(t, down, "Available", "False (MissingEndpoints)")

	local := project(t, KindAPIServices, false, map[string]any{
		"metadata": map[string]any{"name": "v1.apps"},
		"status":   map[string]any{"conditions": []any{map[string]any{"type": "Available", "status": "True"}}},
	})
	want(t, local, "Service", "Local")
	want(t, local, "Available", "True")
}

func TestPriorityLevelsReadEitherShape(t *testing.T) {
	limited := project(t, KindPriorityLevelConfigurations, false, map[string]any{
		"metadata": map[string]any{"name": "workload-low"},
		"spec": map[string]any{
			"type": "Limited",
			"limited": map[string]any{
				"nominalConcurrencyShares": int64(100),
				"limitResponse": map[string]any{"type": "Queue", "queuing": map[string]any{
					"queues": int64(128), "handSize": int64(6), "queueLengthLimit": int64(50),
				}},
			},
		},
	})
	want(t, limited, "Shares", "100")
	want(t, limited, "Queues", "128")
	want(t, limited, "Hand Size", "6")

	exempt := project(t, KindPriorityLevelConfigurations, false, map[string]any{
		"metadata": map[string]any{"name": "exempt"},
		"spec":     map[string]any{"type": "Exempt", "exempt": map[string]any{"nominalConcurrencyShares": int64(0)}},
	})
	want(t, exempt, "Shares", "0")
	want(t, exempt, "Queues", "")
}

func TestControllerRevisionsNameTheirController(t *testing.T) {
	controller := true
	u := obj(map[string]any{
		"metadata": map[string]any{"name": "web-5d8f", "namespace": "prod"},
		"revision": int64(3),
	})
	u.SetOwnerReferences(nil)
	cells := project(t, KindControllerRevisions, true, u.Object)
	want(t, cells, "Controller", "<none>")

	u.Object["metadata"].(map[string]any)["ownerReferences"] = []any{map[string]any{
		"apiVersion": "apps/v1", "kind": "StatefulSet", "name": "web", "uid": "1", "controller": controller,
	}}
	cells = project(t, KindControllerRevisions, true, u.Object)
	want(t, cells, "Controller", "StatefulSet/web")
	want(t, cells, "Revision", "3")
}

func TestStorageKindsReadTheirOddlyPlacedFields(t *testing.T) {
	// CSIStorageCapacity and VolumeAttributesClass keep their fields at the
	// top level rather than under spec.
	capacity := project(t, KindCSIStorageCapacities, true, map[string]any{
		"metadata":         map[string]any{"name": "csisc-1", "namespace": "kube-system"},
		"storageClassName": "fast",
		"capacity":         "100Gi",
	})
	want(t, capacity, "Storage Class", "fast")
	want(t, capacity, "Capacity", "100Gi")

	class := project(t, KindVolumeAttributesClasses, false, map[string]any{
		"metadata":   map[string]any{"name": "gold"},
		"driverName": "ebs.csi.aws.com",
		"parameters": map[string]any{"iops": "3000"},
	})
	want(t, class, "Driver", "ebs.csi.aws.com")
	want(t, class, "Parameters", "iops=3000")

	attachment := project(t, KindVolumeAttachments, false, map[string]any{
		"metadata": map[string]any{"name": "csi-abc"},
		"spec": map[string]any{
			"attacher": "ebs.csi.aws.com",
			"nodeName": "worker-1",
			"source":   map[string]any{"persistentVolumeName": "pvc-1"},
		},
		"status": map[string]any{"attached": true},
	})
	want(t, attachment, "Volume", "pvc-1")
	want(t, attachment, "Attached", "true")
}

func TestTheNewClusterScopedKindsHaveNoNamespaceColumn(t *testing.T) {
	// withNamespace is only asked to add one for a namespaced kind, so this
	// pins the list of new kinds that are not -- it is what the cluster says,
	// and the tables are built from the mapper's answer, but a test with the
	// wrong idea about scope would be testing the wrong layout.
	for _, kind := range []string{
		KindServiceCIDRs, KindIPAddresses, KindVolumeAttachments, KindVolumeAttributesClasses,
		KindCSIDrivers, KindCSINodes, KindCSRs, KindClusterTrustBundles, KindDeviceClasses,
		KindResourceSlices, KindDeviceTaintRules, KindResourcePoolStatusRequests, KindAPIServices,
		KindFlowSchemas, KindPriorityLevelConfigurations, KindStorageVersions, KindStorageVersionMigrations,
	} {
		for _, c := range withNamespace(builtinColumns[kind], false) {
			if c.Name == "Namespace" {
				t.Errorf("%s has a Namespace column but is cluster-scoped", kind)
			}
		}
	}
}
