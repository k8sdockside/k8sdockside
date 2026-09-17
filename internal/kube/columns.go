package kube

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// column is one column of a live table: a header, plus how to get its value out
// of an object.
//
// Path is a JSONPath expression, the same form a CRD's additionalPrinterColumns
// uses -- which is the point: custom resources describe their own columns that
// way, so serving built-in kinds through the same mechanism means there is one
// renderer rather than two. From is for the columns JSONPath genuinely cannot
// express, where the value is computed rather than read: a pod's ready count is
// a tally across its containers, not a field.
type column struct {
	Name string
	Path string
	From func(*unstructured.Unstructured) Cell
}

// value renders one column for one object.
func (c column) value(u *unstructured.Unstructured) Cell {
	if c.From != nil {
		return c.From(u)
	}
	return plain(evalPath(u, c.Path))
}

// nameColumn and namespaceColumn open every table, matching how the tables have
// always been laid out and what Row carries separately for the describe panel.
var nameColumn = column{Name: "Name", From: func(u *unstructured.Unstructured) Cell { return plain(u.GetName()) }}
var namespaceColumn = column{Name: "Namespace", From: func(u *unstructured.Unstructured) Cell { return muted(u.GetNamespace()) }}
var ageColumn = column{Name: "Age", From: func(u *unstructured.Unstructured) Cell {
	return timeCell(u.GetCreationTimestamp().Time)
}}

// defaultOrder names the column a kind is arranged by before the frontend ever
// sees it. Everything absent from here is ordered by namespace then name.
//
// Events are the case that motivates it: a list of what has just gone wrong is
// close to useless in alphabetical order, and the reader wants the most recent
// line first every time the table refreshes, not only on the first paint.
var defaultOrder = map[string]string{
	KindEvents: "Last Seen",
}

// buildLiveTable projects cached objects into the table the UI renders.
func buildLiveTable(kind string, cols []column, namespaced bool, items []*unstructured.Unstructured) Table {
	headers := make([]string, len(cols))
	for i, c := range cols {
		headers[i] = c.Name
	}

	rows := make([]Row, 0, len(items))
	for _, u := range items {
		cells := make([]Cell, len(cols))
		for i, c := range cols {
			cells[i] = c.value(u)
		}
		rows = append(rows, Row{
			ID:        rowID(kind, u.GetNamespace(), u.GetName()),
			Name:      u.GetName(),
			Namespace: u.GetNamespace(),
			Cells:     cells,
		})
	}

	orderRows(kind, headers, rows)

	return Table{Kind: kind, Columns: headers, Rows: rows, Namespaced: namespaced}
}

// columnsFor returns the columns for a kind. Custom resources are asked what
// their columns are -- a CRD carries additionalPrinterColumns precisely so that
// clients do not have to know anything about the kind in advance -- and every
// other kind uses the set compiled in below.
func (c *clusterClient) columnsFor(kind string, namespaced bool) ([]column, error) {
	if _, _, ok := ParseCustomKind(kind); ok {
		return c.customColumns(kind, namespaced)
	}
	cols, ok := builtinColumns[kind]
	if !ok {
		return nil, fmt.Errorf("no columns defined for %s", kind)
	}
	return withNamespace(cols, namespaced), nil
}

// withNamespace inserts the Namespace column after Name for namespaced kinds.
func withNamespace(cols []column, namespaced bool) []column {
	if !namespaced {
		return cols
	}
	out := make([]column, 0, len(cols)+1)
	out = append(out, cols[0], namespaceColumn)
	return append(out, cols[1:]...)
}

// ---- value helpers ---------------------------------------------------------

// All four tolerate a nil object and answer with the zero value. That is not
// defensive padding: a detail view reads several objects that may or may not
// exist -- a stopped VM has no instance, a pending one no pod -- and the
// alternative is a nil check at every one of a hundred field reads.

func nestedString(u *unstructured.Unstructured, fields ...string) string {
	if u == nil {
		return ""
	}
	s, _, _ := unstructured.NestedString(u.Object, fields...)
	return s
}

func nestedInt(u *unstructured.Unstructured, fields ...string) int64 {
	if u == nil {
		return 0
	}
	n, _, _ := unstructured.NestedInt64(u.Object, fields...)
	return n
}

func nestedSlice(u *unstructured.Unstructured, fields ...string) []any {
	if u == nil {
		return nil
	}
	s, _, _ := unstructured.NestedSlice(u.Object, fields...)
	return s
}

// nestedMap reads a nested object. Returned as `any` so it composes with asMap,
// which is how every caller here uses it.
func nestedMap(u *unstructured.Unstructured, fields ...string) any {
	if u == nil {
		return nil
	}
	m, _, _ := unstructured.NestedMap(u.Object, fields...)
	return m
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func mapString(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

// mapInt reads a whole number from an unstructured map. The dynamic client
// decodes JSON numbers to int64, but an object that has been through a JSON
// round trip can hold float64, so both are read; anything else -- including a
// missing key -- counts as absent, which for a printer column's priority is
// the same as saying zero.
func mapInt(m map[string]any, key string) int64 {
	switch n := m[key].(type) {
	case int64:
		return n
	case float64:
		return int64(n)
	}
	return 0
}

// ageOf renders how long ago an object was created, in the same shape the rest
// of the app uses.
func ageOf(u *unstructured.Unstructured) string {
	t := u.GetCreationTimestamp()
	if t.IsZero() {
		return ""
	}
	return age(int(time.Since(t.Time).Minutes()))
}

// conditionStatus reports the status of one condition, the shape shared by
// almost every modern API including the Gateway API.
func conditionStatus(u *unstructured.Unstructured, want string, fields ...string) string {
	for _, raw := range nestedSlice(u, fields...) {
		c := asMap(raw)
		if mapString(c, "type") == want {
			return mapString(c, "status")
		}
	}
	return ""
}

// conditionCell renders a True/False condition with the matching tone.
func conditionCell(want string, fields ...string) func(*unstructured.Unstructured) Cell {
	return func(u *unstructured.Unstructured) Cell {
		switch conditionStatus(u, want, fields...) {
		case "True":
			return toned("True", "ok")
		case "False":
			return toned("False", "error")
		default:
			return muted("Unknown")
		}
	}
}

// ---- built-in column sets --------------------------------------------------

var builtinColumns = map[string][]column{
	KindPods: {
		nameColumn,
		{Name: "Ready", From: podReady},
		// Beside the ratio rather than instead of it: the rectangles say which
		// container is unhappy, the ratio says how many at a glance, and the
		// two are read for different reasons.
		{Name: "Containers", From: podContainers},
		{Name: "Status", From: podStatus},
		{Name: "Restarts", From: podRestarts},
		{Name: "Node", Path: ".spec.nodeName"},
		{Name: "QoS", Path: ".status.qosClass"},
		ageColumn,
	},
	KindDeployments: workloadColumns,
	KindStatefulSet: workloadColumns,
	KindDaemonSets: {
		nameColumn,
		{Name: "Desired", Path: ".status.desiredNumberScheduled"},
		{Name: "Current", Path: ".status.currentNumberScheduled"},
		{Name: "Ready", From: func(u *unstructured.Unstructured) Cell {
			return ratio(nestedInt(u, "status", "numberReady"), nestedInt(u, "status", "desiredNumberScheduled"))
		}},
		{Name: "Node Selector", From: func(u *unstructured.Unstructured) Cell {
			sel, _, _ := unstructured.NestedStringMap(u.Object, "spec", "template", "spec", "nodeSelector")
			return muted(joinMap(sel))
		}},
		ageColumn,
	},
	KindJobs: {
		nameColumn,
		{Name: "Completions", From: func(u *unstructured.Unstructured) Cell {
			want := nestedInt(u, "spec", "completions")
			if want == 0 {
				want = 1
			}
			return ratio(nestedInt(u, "status", "succeeded"), want)
		}},
		{Name: "Duration", From: jobDuration},
		ageColumn,
		{Name: "Condition", From: jobCondition},
	},
	KindCronJobs: {
		nameColumn,
		{Name: "Schedule", Path: ".spec.schedule"},
		{Name: "Suspend", From: func(u *unstructured.Unstructured) Cell {
			suspended, _, _ := unstructured.NestedBool(u.Object, "spec", "suspend")
			if suspended {
				return toned("True", "warn")
			}
			return toned("False", "ok")
		}},
		{Name: "Active", From: func(u *unstructured.Unstructured) Cell {
			return number(len(nestedSlice(u, "status", "active")))
		}},
		{Name: "Last Schedule", From: func(u *unstructured.Unstructured) Cell {
			return timeCell(parseTime(nestedString(u, "status", "lastScheduleTime")))
		}},
		ageColumn,
	},
	KindServices: {
		nameColumn,
		{Name: "Type", Path: ".spec.type"},
		{Name: "Cluster IP", Path: ".spec.clusterIP"},
		{Name: "Ports", From: servicePorts},
		{Name: "External IP", From: serviceExternalIP},
		ageColumn,
	},
	KindIngresses: {
		nameColumn,
		{Name: "Class", Path: ".spec.ingressClassName"},
		{Name: "Hosts", From: ingressHosts},
		{Name: "Address", From: loadBalancerAddress},
		ageColumn,
	},
	KindConfigMaps: {
		nameColumn,
		{Name: "Keys", From: func(u *unstructured.Unstructured) Cell {
			data, _, _ := unstructured.NestedMap(u.Object, "data")
			binary, _, _ := unstructured.NestedMap(u.Object, "binaryData")
			return number(len(data) + len(binary))
		}},
		ageColumn,
	},
	KindSecrets: {
		nameColumn,
		{Name: "Type", Path: ".type"},
		{Name: "Keys", From: func(u *unstructured.Unstructured) Cell {
			data, _, _ := unstructured.NestedMap(u.Object, "data")
			return number(len(data))
		}},
		ageColumn,
	},
	KindPVCs: {
		nameColumn,
		{Name: "Storage Class", Path: ".spec.storageClassName"},
		{Name: "Size", From: func(u *unstructured.Unstructured) Cell {
			return quantityCell(nestedString(u, "status", "capacity", "storage"))
		}},
		{Name: "Volume", Path: ".spec.volumeName"},
		ageColumn,
		{Name: "Status", From: func(u *unstructured.Unstructured) Cell {
			return status(nestedString(u, "status", "phase"))
		}},
	},
	KindNodes: {
		nameColumn,
		{Name: "Roles", From: nodeRoles},
		{Name: "Version", Path: ".status.nodeInfo.kubeletVersion"},
		// The rest of what `kubectl get nodes -o wide` prints, in its order:
		// where the node is reached, then what it runs, straight off nodeInfo.
		{Name: "Internal IP", From: nodeAddress("InternalIP")},
		{Name: "External IP", From: nodeAddress("ExternalIP")},
		{Name: "OS Image", Path: ".status.nodeInfo.osImage"},
		{Name: "Kernel Version", Path: ".status.nodeInfo.kernelVersion"},
		{Name: "Container Runtime", Path: ".status.nodeInfo.containerRuntimeVersion"},
		// Capacity is the hardware; allocatable is what the scheduler may
		// actually hand out, once the kubelet has kept back what it needs. The
		// gap between them is invisible with only one on screen, and it is the
		// difference between a node looking full and being full.
		//
		// Neither is usage: what a node is really burning, and what has been
		// requested on it, need the metrics API and every pod in the cluster
		// respectively. Both are in the node's detail panel, which can afford
		// the extra reads that a live table refreshing on every node event
		// cannot.
		{Name: "CPU", Path: ".status.capacity.cpu"},
		{Name: "CPU alloc", Path: ".status.allocatable.cpu"},
		{Name: "Memory", Path: ".status.capacity.memory"},
		{Name: "Mem alloc", Path: ".status.allocatable.memory"},
		{Name: "Taints", From: func(u *unstructured.Unstructured) Cell {
			return number(len(nestedSlice(u, "spec", "taints")))
		}},
		ageColumn,
		{Name: "Conditions", From: nodeCondition},
	},
	KindNamespaces: {
		nameColumn,
		{Name: "Labels", From: func(u *unstructured.Unstructured) Cell { return muted(joinMap(u.GetLabels())) }},
		ageColumn,
		{Name: "Status", From: func(u *unstructured.Unstructured) Cell {
			return status(nestedString(u, "status", "phase"))
		}},
	},
	KindEvents: {
		{Name: "Type", From: func(u *unstructured.Unstructured) Cell { return status(nestedString(u, "type")) }},
		{Name: "Message", From: func(u *unstructured.Unstructured) Cell { return plain(nestedString(u, "message")) }},
		{Name: "Reason", From: func(u *unstructured.Unstructured) Cell { return muted(nestedString(u, "reason")) }},
		{Name: "Involved Object", From: func(u *unstructured.Unstructured) Cell {
			return muted(nestedString(u, "involvedObject", "kind") + "/" + nestedString(u, "involvedObject", "name"))
		}},
		{Name: "Last Seen", From: func(u *unstructured.Unstructured) Cell {
			return timeCell(parseTime(lastSeen(u)))
		}},
	},

	// ---- Gateway API -------------------------------------------------------
	KindGatewayClasses: {
		nameColumn,
		{Name: "Controller", Path: ".spec.controllerName"},
		{Name: "Accepted", From: conditionCell("Accepted", "status", "conditions")},
		ageColumn,
	},
	KindGateways: {
		nameColumn,
		{Name: "Class", Path: ".spec.gatewayClassName"},
		{Name: "Address", From: gatewayAddress},
		{Name: "Programmed", From: conditionCell("Programmed", "status", "conditions")},
		ageColumn,
	},
	KindListenerSets: {
		nameColumn,
		{Name: "Parent", From: listenerSetParent},
		{Name: "Listeners", From: listenerSetListeners},
		{Name: "Accepted", From: conditionCell("Accepted", "status", "conditions")},
		{Name: "Programmed", From: conditionCell("Programmed", "status", "conditions")},
		ageColumn,
	},
	KindHTTPRoutes: {
		nameColumn,
		{Name: "Hostnames", From: httpRouteHostnames},
		{Name: "Parents", From: routeParents},
		ageColumn,
	},
	KindGRPCRoutes: routeColumns,
	KindTLSRoutes:  routeColumns,
	KindTCPRoutes:  l4RouteColumns,
	KindUDPRoutes:  l4RouteColumns,
	KindBackendTLSPolicies: {
		nameColumn,
		{Name: "Targets", From: policyTargets},
		{Name: "Hostname", Path: ".spec.validation.hostname"},
		{Name: "Accepted", From: policyAccepted},
		ageColumn,
	},
	KindReferenceGrants: {nameColumn, {Name: "From", From: referenceGrantFrom}, ageColumn},

	KindCRDs: {
		nameColumn,
		{Name: "Group", Path: ".spec.group"},
		{Name: "Version", From: crdVersions},
		{Name: "Scope", Path: ".spec.scope"},
		{Name: "Kind", Path: ".spec.names.kind"},
		ageColumn,
	},

	// ---- Workloads ---------------------------------------------------------
	KindReplicaSets:            replicaColumns,
	KindReplicationControllers: replicaColumns,
	KindHPAs: {
		nameColumn,
		// Which workload this scales is the first thing worth knowing; an HPA
		// row without it is just a name and three numbers.
		{Name: "Reference", From: func(u *unstructured.Unstructured) Cell {
			kind := nestedString(u, "spec", "scaleTargetRef", "kind")
			name := nestedString(u, "spec", "scaleTargetRef", "name")
			if kind == "" && name == "" {
				return muted("")
			}
			return plain(kind + "/" + name)
		}},
		{Name: "Min", Path: ".spec.minReplicas"},
		{Name: "Max", Path: ".spec.maxReplicas"},
		{Name: "Replicas", Path: ".status.currentReplicas"},
		ageColumn,
	},

	// ---- Namespace configuration and coordination --------------------------
	KindResourceQuotas: {
		nameColumn,
		// Summarised rather than counted: "4 constraints" tells the reader
		// nothing, and how close used is to hard is the whole question.
		{Name: "Hard", From: quantityMap("hard")},
		{Name: "Used", From: quantityMap("used")},
		ageColumn,
	},
	KindLimitRanges: {
		nameColumn,
		{Name: "Limit Types", From: func(u *unstructured.Unstructured) Cell {
			var types []string
			for _, item := range nestedSlice(u, "spec", "limits") {
				if t := mapString(asMap(item), "type"); t != "" {
					types = append(types, t)
				}
			}
			return plain(strings.Join(types, ", "))
		}},
		ageColumn,
	},
	KindLeases: {
		nameColumn,
		{Name: "Holder", Path: ".spec.holderIdentity"},
		ageColumn,
	},

	// ---- Scheduling and availability ---------------------------------------
	KindPDBs: {
		nameColumn,
		{Name: "Min Available", Path: ".spec.minAvailable"},
		{Name: "Max Unavailable", Path: ".spec.maxUnavailable"},
		{Name: "Allowed Disruptions", Path: ".status.disruptionsAllowed"},
		ageColumn,
	},
	KindPriorityClasses: {
		nameColumn,
		{Name: "Value", Path: ".value"},
		{Name: "Global Default", Path: ".globalDefault"},
		{Name: "Preemption", Path: ".preemptionPolicy"},
		ageColumn,
	},
	KindRuntimeClasses: {
		nameColumn,
		{Name: "Handler", Path: ".handler"},
		ageColumn,
	},

	// ---- Admission ---------------------------------------------------------
	KindMutatingWebhooks:   webhookColumns,
	KindValidatingWebhooks: webhookColumns,
	KindMutatingAdmissionPolicies: {
		nameColumn,
		{Name: "Failure Policy", Path: ".spec.failurePolicy"},
		{Name: "Reinvocation", Path: ".spec.reinvocationPolicy"},
		ageColumn,
	},
	KindValidatingAdmissionPolicies: {
		nameColumn,
		{Name: "Failure Policy", Path: ".spec.failurePolicy"},
		{Name: "Validations", From: func(u *unstructured.Unstructured) Cell {
			return number(len(nestedSlice(u, "spec", "validations")))
		}},
		ageColumn,
	},
	KindMutatingAdmissionPolicyBindings:   policyBindingColumns,
	KindValidatingAdmissionPolicyBindings: policyBindingColumns,

	// ---- Networking --------------------------------------------------------
	KindEndpointSlices: {
		nameColumn,
		{Name: "Address Type", Path: ".addressType"},
		{Name: "Ports", From: func(u *unstructured.Unstructured) Cell {
			var ports []string
			for _, p := range nestedSlice(u, "ports") {
				if n := mapNumber(asMap(p), "port"); n != "" {
					ports = append(ports, n)
				}
			}
			return plain(strings.Join(ports, ", "))
		}},
		// How many endpoints are actually taking traffic, which is the question
		// a slice is usually opened to answer.
		{Name: "Ready", From: func(u *unstructured.Unstructured) Cell {
			items := nestedSlice(u, "endpoints")
			var ready int64
			for _, e := range items {
				conds, _ := asMap(e)["conditions"].(map[string]any)
				if isReady, ok := conds["ready"].(bool); ok && isReady {
					ready++
				}
			}
			return ratio(ready, int64(len(items)))
		}},
		ageColumn,
	},
	KindEndpoints: {
		nameColumn,
		{Name: "Endpoints", From: endpointAddresses},
		ageColumn,
	},
	KindIngressClasses: {
		nameColumn,
		{Name: "Controller", Path: ".spec.controller"},
		{Name: "Default", From: annotationFlag("ingressclass.kubernetes.io/is-default-class")},
		ageColumn,
	},
	KindNetworkPolicies: {
		nameColumn,
		{Name: "Pod Selector", From: func(u *unstructured.Unstructured) Cell {
			labels, _, _ := unstructured.NestedStringMap(u.Object, "spec", "podSelector", "matchLabels")
			if len(labels) == 0 {
				// An empty selector is not nothing: it is every pod in the
				// namespace, which is the opposite of what blank would suggest.
				return muted("all pods")
			}
			return plain(joinMap(labels))
		}},
		{Name: "Types", From: func(u *unstructured.Unstructured) Cell {
			return plain(joinAny(nestedSlice(u, "spec", "policyTypes"), 3))
		}},
		ageColumn,
	},

	// ---- Cluster storage ---------------------------------------------------
	KindStorageClasses: {
		nameColumn,
		{Name: "Provisioner", Path: ".provisioner"},
		{Name: "Default", From: annotationFlag("storageclass.kubernetes.io/is-default-class")},
		{Name: "Reclaim", Path: ".reclaimPolicy"},
		{Name: "Binding", Path: ".volumeBindingMode"},
		ageColumn,
	},
	KindPVs: {
		nameColumn,
		{Name: "Capacity", From: func(u *unstructured.Unstructured) Cell {
			return quantityCell(nestedString(u, "spec", "capacity", "storage"))
		}},
		{Name: "Access", From: func(u *unstructured.Unstructured) Cell {
			return muted(joinAny(nestedSlice(u, "spec", "accessModes"), 3))
		}},
		{Name: "Reclaim", Path: ".spec.persistentVolumeReclaimPolicy"},
		{Name: "Claim", From: func(u *unstructured.Unstructured) Cell {
			ns := nestedString(u, "spec", "claimRef", "namespace")
			name := nestedString(u, "spec", "claimRef", "name")
			if name == "" {
				return muted("")
			}
			return plain(ns + "/" + name)
		}},
		{Name: "Class", Path: ".spec.storageClassName"},
		ageColumn,
		{Name: "Status", From: func(u *unstructured.Unstructured) Cell {
			return status(nestedString(u, "status", "phase"))
		}},
	},

	// ---- Access ------------------------------------------------------------
	KindServiceAccounts: {
		nameColumn,
		{Name: "Secrets", From: func(u *unstructured.Unstructured) Cell {
			return number(len(nestedSlice(u, "secrets")))
		}},
		ageColumn,
	},
	KindRoles:               roleColumns,
	KindClusterRoles:        roleColumns,
	KindRoleBindings:        bindingColumns,
	KindClusterRoleBindings: bindingColumns,

	// ---- The rest of what Kubernetes serves: see kindcolumns.go ------------
	KindControllerRevisions: controllerRevisionColumns,
	KindPodTemplates:        podTemplateColumns,
	KindLeaseCandidates:     leaseCandidateColumns,

	KindServiceCIDRs: serviceCIDRColumns,
	KindIPAddresses:  ipAddressColumns,

	KindVolumeAttachments:       volumeAttachmentColumns,
	KindVolumeAttributesClasses: volumeAttributesClassColumns,
	KindCSIDrivers:              csiDriverColumns,
	KindCSINodes:                csiNodeColumns,
	KindCSIStorageCapacities:    csiStorageCapacityColumns,

	KindCSRs:                   csrColumns,
	KindClusterTrustBundles:    clusterTrustBundleColumns,
	KindPodCertificateRequests: podCertificateRequestColumns,

	KindWorkloads:          workloadAPIColumns,
	KindPodGroups:          podGroupColumns,
	KindCompositePodGroups: compositePodGroupColumns,
	KindEvictionRequests:   evictionRequestColumns,
	KindEvictions:          evictionColumns,

	KindDeviceClasses:              deviceClassColumns,
	KindResourceClaims:             resourceClaimColumns,
	KindResourceClaimTemplates:     resourceClaimTemplateColumns,
	KindResourceSlices:             resourceSliceColumns,
	KindDeviceTaintRules:           deviceTaintRuleColumns,
	KindResourcePoolStatusRequests: resourcePoolStatusRequestColumns,

	KindAPIServices:                 apiServiceColumns,
	KindFlowSchemas:                 flowSchemaColumns,
	KindPriorityLevelConfigurations: priorityLevelColumns,
	KindStorageVersions:             storageVersionColumns,
	KindStorageVersionMigrations:    storageVersionMigrationColumns,
}

// roleColumns is shared by Roles and ClusterRoles, which differ only in scope.
// The rule count stands in for the permissions themselves: a rule is a list of
// verbs over a list of resources and does not fit a table cell, so the number
// says how much there is to read and the describe panel shows it.
var roleColumns = []column{
	nameColumn,
	{Name: "Rules", From: func(u *unstructured.Unstructured) Cell {
		return number(len(nestedSlice(u, "rules")))
	}},
	ageColumn,
}

// bindingColumns is shared by RoleBindings and ClusterRoleBindings.
var bindingColumns = []column{
	nameColumn,
	{Name: "Role", From: func(u *unstructured.Unstructured) Cell {
		kind := nestedString(u, "roleRef", "kind")
		name := nestedString(u, "roleRef", "name")
		if name == "" {
			return muted("")
		}
		return plain(kind + "/" + name)
	}},
	{Name: "Subjects", From: func(u *unstructured.Unstructured) Cell {
		var subjects []string
		for _, s := range nestedSlice(u, "subjects") {
			m := asMap(s)
			if name := mapString(m, "name"); name != "" {
				subjects = append(subjects, mapString(m, "kind")+"/"+name)
			}
		}
		return plain(strings.Join(subjects, ", "))
	}},
	ageColumn,
}

// endpointAddresses lists the addresses behind a (deprecated) Endpoints object.
func endpointAddresses(u *unstructured.Unstructured) Cell {
	var addresses []string
	for _, subset := range nestedSlice(u, "subsets") {
		for _, a := range nestedSlice(&unstructured.Unstructured{Object: asMap(subset)}, "addresses") {
			if ip := mapString(asMap(a), "ip"); ip != "" {
				addresses = append(addresses, ip)
			}
		}
	}
	if len(addresses) == 0 {
		return muted("")
	}
	if len(addresses) > 4 {
		return plain(strings.Join(addresses[:4], ", ") + fmt.Sprintf(" +%d more", len(addresses)-4))
	}
	return plain(strings.Join(addresses, ", "))
}

// annotationFlag reads one of the "is-default-class" annotations, which is how
// both IngressClass and StorageClass mark the default rather than by a field.
func annotationFlag(key string) func(*unstructured.Unstructured) Cell {
	return func(u *unstructured.Unstructured) Cell {
		if u.GetAnnotations()[key] == "true" {
			return toned("true", "ok")
		}
		return muted("false")
	}
}

// mapNumber reads a numeric field as text, whatever shape the JSON gave it.
func mapNumber(m map[string]any, key string) string {
	switch v := m[key].(type) {
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatInt(int64(v), 10)
	case string:
		return v
	}
	return ""
}

// replicaColumns is shared by ReplicaSets and ReplicationControllers, which
// report the same counts under the same field names and carry the same pod
// template.
var replicaColumns = []column{
	nameColumn,
	{Name: "Desired", Path: ".spec.replicas"},
	{Name: "Current", Path: ".status.replicas"},
	{Name: "Ready", From: func(u *unstructured.Unstructured) Cell {
		return ratio(nestedInt(u, "status", "readyReplicas"), nestedInt(u, "spec", "replicas"))
	}},
	{Name: "Image", From: firstImage},
	ageColumn,
}

// webhookColumns is shared by the mutating and validating webhook
// configurations, which differ only in when they run.
var webhookColumns = []column{
	nameColumn,
	{Name: "Webhooks", From: func(u *unstructured.Unstructured) Cell {
		return number(len(nestedSlice(u, "webhooks")))
	}},
	ageColumn,
}

// policyBindingColumns is shared by the mutating and validating policy
// bindings, whose job is entirely to name a policy and who it applies to.
var policyBindingColumns = []column{
	nameColumn,
	{Name: "Policy", Path: ".spec.policyName"},
	ageColumn,
}

// quantityMap renders one of a ResourceQuota's resource maps as "cpu=10,
// memory=20Gi", sorted so the two columns line up row to row.
func quantityMap(field string) func(*unstructured.Unstructured) Cell {
	return func(u *unstructured.Unstructured) Cell {
		values, _, _ := unstructured.NestedStringMap(u.Object, "status", field)
		return muted(joinMap(values))
	}
}

// workloadColumns is shared by Deployments and StatefulSets, which report the
// same things under the same field names.
var workloadColumns = []column{
	nameColumn,
	{Name: "Pods", From: func(u *unstructured.Unstructured) Cell {
		return ratio(nestedInt(u, "status", "readyReplicas"), nestedInt(u, "spec", "replicas"))
	}},
	{Name: "Image", From: firstImage},
	ageColumn,
	{Name: "Condition", From: workloadCondition},
}

// routeColumns is shared by GRPCRoute and TLSRoute, which match on a hostname
// but are not something a browser can open. HTTPRoute has the same columns
// with the hostnames as links.
var routeColumns = []column{
	nameColumn,
	{Name: "Hostnames", From: func(u *unstructured.Unstructured) Cell {
		return plain(joinAny(nestedSlice(u, "spec", "hostnames"), 3))
	}},
	{Name: "Parents", From: routeParents},
	ageColumn,
}

// l4RouteColumns is shared by TCPRoute and UDPRoute. With no hostname to match
// on, a listener hands them everything, so where it goes is what tells one
// route from the next.
var l4RouteColumns = []column{
	nameColumn,
	{Name: "Parents", From: routeParents},
	{Name: "Backends", From: routeBackends},
	ageColumn,
}

// crdResource is where CustomResourceDefinitions themselves live.
var crdResource = schema.GroupVersionResource{
	Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions",
}

// customColumns asks a CRD what its own table looks like.
//
// This is the whole reason custom resources need no special-casing anywhere
// else: a CRD carries additionalPrinterColumns -- a header and a JSONPath for
// each -- precisely so a client can render a kind it has never heard of. It is
// where `kubectl get` gets its columns too, so the tables match what the user
// sees on the command line: the priority-0 columns of a plain `kubectl get`,
// without the extras that only `-o wide` asks for.
func (c *clusterClient) customColumns(kind string, namespaced bool) ([]column, error) {
	plural, group, ok := ParseCustomKind(kind)
	if !ok {
		return nil, fmt.Errorf("not a custom resource kind: %s", kind)
	}

	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	crd, err := c.dynamic.Resource(crdResource).Get(ctx, plural+"."+group, metav1.GetOptions{})
	if err != nil {
		// Without the CRD we still know every object's name, namespace and age,
		// so fall back to those rather than failing the tab outright: a user
		// without RBAC on CRDs can often still list the resources themselves.
		return withNamespace([]column{nameColumn, ageColumn}, namespaced), nil
	}

	return withNamespace(crdColumns(crd), namespaced), nil
}

// crdColumns turns what a definition declares into the columns of its table,
// between the Name and Age every kind gets.
func crdColumns(crd *unstructured.Unstructured) []column {
	cols := []column{nameColumn}
	for _, raw := range pickCRDVersion(crd) {
		spec := asMap(raw)
		name, path := mapString(spec, "name"), mapString(spec, "jsonPath")
		if name == "" || path == "" {
			continue
		}
		// A printer column's priority says which view it belongs to: 0 is the
		// standard table, anything higher is held back for `kubectl get -o
		// wide`. A tab is the standard table, so the wide-only columns are
		// dropped rather than left to crowd it -- an operator that declares a
		// dozen of them would otherwise make the tab unreadable.
		if mapInt(spec, "priority") != 0 {
			continue
		}
		// A CRD is free to define an Age column of its own; ours goes last, so
		// dropping the duplicate keeps the table from carrying two.
		if strings.EqualFold(name, "Age") {
			continue
		}
		cols = append(cols, column{Name: name, Path: path})
	}
	return append(cols, ageColumn)
}

// pickCRDVersion returns the printer columns of the version the cluster stores,
// falling back to the first served version. A CRD may serve several versions
// with different columns, and the storage version is the one whose shape the
// other views (describe, in particular) will report.
func pickCRDVersion(crd *unstructured.Unstructured) []any {
	versions := nestedSlice(crd, "spec", "versions")
	var first []any
	for _, raw := range versions {
		v := asMap(raw)
		if served, _ := v["served"].(bool); !served {
			continue
		}
		cols, _ := v["additionalPrinterColumns"].([]any)
		if stored, _ := v["storage"].(bool); stored {
			return cols
		}
		if first == nil {
			first = cols
		}
	}
	return first
}

// orderRows arranges a kind's rows: by its declared column where it has one,
// otherwise by namespace and name.
func orderRows(kind string, headers []string, rows []Row) {
	at := -1
	if want, ok := defaultOrder[kind]; ok {
		at = slices.Index(headers, want)
	}

	if at == -1 {
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].Namespace != rows[j].Namespace {
				return rows[i].Namespace < rows[j].Namespace
			}
			return rows[i].Name < rows[j].Name
		})
		return
	}

	sort.SliceStable(rows, func(i, j int) bool {
		return sortKeyAt(rows[i], at) < sortKeyAt(rows[j], at)
	})
}

// sortKeyAt is what a cell sorts by: its Sort value where it has one, its text
// otherwise. Numeric keys are compared as numbers, which is why they are padded
// here rather than at every call site that builds one.
func sortKeyAt(row Row, at int) string {
	if at >= len(row.Cells) {
		return ""
	}
	cell := row.Cells[at]
	if cell.Sort == "" {
		return cell.Text
	}
	if n, err := strconv.ParseInt(cell.Sort, 10, 64); err == nil {
		return fmt.Sprintf("%019d", n)
	}
	return cell.Sort
}
