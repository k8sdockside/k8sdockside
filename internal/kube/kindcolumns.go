package kube

import (
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// The columns of the kinds that fill in the rest of what Kubernetes serves:
// the storage and certificate plumbing, dynamic resource allocation, gang
// scheduling, the eviction API and the API server's own configuration.
//
// They follow `kubectl get` wherever it has a printer for the kind, because
// that is the table the reader already knows; where it has none, they show the
// few fields that tell one object from the next.

// ---- workloads -------------------------------------------------------------

var controllerRevisionColumns = []column{
	nameColumn,
	{Name: "Controller", From: controllerOf},
	{Name: "Revision", Path: ".revision"},
	ageColumn,
}

var podTemplateColumns = []column{
	nameColumn,
	{Name: "Containers", From: func(u *unstructured.Unstructured) Cell {
		return plain(joinStrings(fieldOfEach(nestedSlice(u, "template", "spec", "containers"), "name"), 3))
	}},
	{Name: "Images", From: func(u *unstructured.Unstructured) Cell {
		return muted(joinStrings(fieldOfEach(nestedSlice(u, "template", "spec", "containers"), "image"), 2))
	}},
	{Name: "Pod Labels", From: func(u *unstructured.Unstructured) Cell {
		labels, _, _ := unstructured.NestedStringMap(u.Object, "template", "metadata", "labels")
		return muted(joinMap(labels))
	}},
	ageColumn,
}

// ---- cluster ---------------------------------------------------------------

var leaseCandidateColumns = []column{
	nameColumn,
	{Name: "Lease", Path: ".spec.leaseName"},
	{Name: "Binary Version", Path: ".spec.binaryVersion"},
	{Name: "Emulation Version", Path: ".spec.emulationVersion"},
	{Name: "Strategy", Path: ".spec.strategy"},
	{Name: "Renewed", From: func(u *unstructured.Unstructured) Cell {
		return timeCell(parseTime(nestedString(u, "spec", "renewTime")))
	}},
	ageColumn,
}

// ---- network ---------------------------------------------------------------

var serviceCIDRColumns = []column{
	nameColumn,
	{Name: "CIDRs", From: func(u *unstructured.Unstructured) Cell {
		return plain(joinAny(nestedSlice(u, "spec", "cidrs"), 3))
	}},
	{Name: "Ready", From: conditionCell("Ready", "status", "conditions")},
	ageColumn,
}

var ipAddressColumns = []column{
	nameColumn,
	// What holds the address, which is almost always a Service.
	{Name: "Parent", From: func(u *unstructured.Unstructured) Cell {
		name := nestedString(u, "spec", "parentRef", "name")
		if name == "" {
			return muted("<none>")
		}
		if ns := nestedString(u, "spec", "parentRef", "namespace"); ns != "" {
			name = ns + "/" + name
		}
		return plain(nestedString(u, "spec", "parentRef", "resource") + "/" + name)
	}},
	ageColumn,
}

// ---- storage ---------------------------------------------------------------

var volumeAttachmentColumns = []column{
	nameColumn,
	{Name: "Attacher", Path: ".spec.attacher"},
	{Name: "Volume", From: func(u *unstructured.Unstructured) Cell {
		if pv := nestedString(u, "spec", "source", "persistentVolumeName"); pv != "" {
			return plain(pv)
		}
		// An inline volume is part of a pod's spec and has no name of its own.
		return muted("<inline>")
	}},
	{Name: "Node", Path: ".spec.nodeName"},
	{Name: "Attached", From: func(u *unstructured.Unstructured) Cell {
		attached, _, _ := unstructured.NestedBool(u.Object, "status", "attached")
		switch {
		case attached:
			return toned("true", "ok")
		case nestedString(u, "status", "attachError", "message") != "":
			return toned("false", "error")
		default:
			return toned("false", "warn")
		}
	}},
	ageColumn,
}

var volumeAttributesClassColumns = []column{
	nameColumn,
	{Name: "Driver", Path: ".driverName"},
	{Name: "Parameters", From: func(u *unstructured.Unstructured) Cell {
		params, _, _ := unstructured.NestedStringMap(u.Object, "parameters")
		return muted(joinMap(params))
	}},
	ageColumn,
}

var csiDriverColumns = []column{
	nameColumn,
	{Name: "Attach Required", Path: ".spec.attachRequired"},
	{Name: "Pod Info On Mount", Path: ".spec.podInfoOnMount"},
	{Name: "Storage Capacity", Path: ".spec.storageCapacity"},
	{Name: "Token Requests", From: func(u *unstructured.Unstructured) Cell {
		return muted(joinStrings(fieldOfEach(nestedSlice(u, "spec", "tokenRequests"), "audience"), 2))
	}},
	{Name: "Requires Republish", Path: ".spec.requiresRepublish"},
	{Name: "Modes", From: func(u *unstructured.Unstructured) Cell {
		return muted(joinAny(nestedSlice(u, "spec", "volumeLifecycleModes"), 3))
	}},
	{Name: "FS Group Policy", Path: ".spec.fsGroupPolicy"},
	ageColumn,
}

var csiNodeColumns = []column{
	nameColumn,
	{Name: "Drivers", From: func(u *unstructured.Unstructured) Cell {
		return plain(joinStrings(fieldOfEach(nestedSlice(u, "spec", "drivers"), "name"), 3))
	}},
	ageColumn,
}

var csiStorageCapacityColumns = []column{
	nameColumn,
	{Name: "Storage Class", Path: ".storageClassName"},
	{Name: "Capacity", From: func(u *unstructured.Unstructured) Cell {
		return quantityCell(nestedString(u, "capacity"))
	}},
	{Name: "Max Volume Size", From: func(u *unstructured.Unstructured) Cell {
		return quantityCell(nestedString(u, "maximumVolumeSize"))
	}},
	ageColumn,
}

// ---- certificates ----------------------------------------------------------

var csrColumns = []column{
	nameColumn,
	{Name: "Signer", Path: ".spec.signerName"},
	{Name: "Requestor", Path: ".spec.username"},
	{Name: "Requested Duration", From: func(u *unstructured.Unstructured) Cell {
		seconds := nestedInt(u, "spec", "expirationSeconds")
		if seconds <= 0 {
			return muted("<none>")
		}
		return durationCell(time.Duration(seconds) * time.Second)
	}},
	ageColumn,
	{Name: "Condition", From: csrCondition},
}

var clusterTrustBundleColumns = []column{
	nameColumn,
	{Name: "Signer", From: func(u *unstructured.Unstructured) Cell {
		if signer := nestedString(u, "spec", "signerName"); signer != "" {
			return plain(signer)
		}
		return muted("<none>")
	}},
	// A bundle is PEM, and how many roots it carries is the one thing about
	// it that fits in a cell.
	{Name: "Certificates", From: func(u *unstructured.Unstructured) Cell {
		return number(strings.Count(nestedString(u, "spec", "trustBundle"), "-----BEGIN CERTIFICATE-----"))
	}},
	ageColumn,
}

var podCertificateRequestColumns = []column{
	nameColumn,
	{Name: "Signer", Path: ".spec.signerName"},
	{Name: "Pod", Path: ".spec.podName"},
	{Name: "Service Account", Path: ".spec.serviceAccountName"},
	{Name: "Node", Path: ".spec.nodeName"},
	ageColumn,
	{Name: "Status", From: conditionStates(toned("Pending", "warn"),
		conditionState{"Issued", "Issued", "ok"},
		conditionState{"Denied", "Denied", "error"},
		conditionState{"Failed", "Failed", "error"},
	)},
}

// csrCondition is kubectl's CONDITION column for a signing request: whether it
// has been approved or denied, and whether a certificate has been issued. A
// request nobody has answered yet is Pending, which is the one that asks
// somebody to do something.
func csrCondition(u *unstructured.Unstructured) Cell {
	var tags []Tag
	for _, raw := range nestedSlice(u, "status", "conditions") {
		c := asMap(raw)
		// A signing request's conditions are True unless said otherwise.
		if mapString(c, "status") == "False" {
			continue
		}
		switch t := mapString(c, "type"); t {
		case "Approved":
			tags = append(tags, Tag{Text: t, Tone: "ok"})
		case "Denied", "Failed":
			tags = append(tags, Tag{Text: t, Tone: "error"})
		}
	}
	if nestedString(u, "status", "certificate") != "" {
		tags = append(tags, Tag{Text: "Issued", Tone: "ok"})
	}
	if len(tags) == 0 {
		return toned("Pending", "warn")
	}
	return tagged(tags)
}

// csrPending reports whether nobody has approved or denied a request yet,
// which is when the panel offers to.
func csrPending(u *unstructured.Unstructured) bool {
	return csrCondition(u).Text == "Pending"
}

// ---- scheduling ------------------------------------------------------------

var workloadAPIColumns = []column{
	nameColumn,
	{Name: "Controller", From: func(u *unstructured.Unstructured) Cell {
		name := nestedString(u, "spec", "controllerRef", "name")
		if name == "" {
			return muted("<none>")
		}
		return plain(nestedString(u, "spec", "controllerRef", "kind") + "/" + name)
	}},
	{Name: "Pod Groups", From: func(u *unstructured.Unstructured) Cell {
		return plain(joinStrings(fieldOfEach(nestedSlice(u, "spec", "podGroupTemplates"), "name"), 3))
	}},
	ageColumn,
}

var podGroupColumns = []column{
	nameColumn,
	{Name: "Workload", Path: ".spec.workloadRef.workloadName"},
	{Name: "Template", Path: ".spec.workloadRef.templateName"},
	{Name: "Policy", From: schedulingPolicy("minCount")},
	{Name: "Priority Class", Path: ".spec.priorityClassName"},
	ageColumn,
	{Name: "Status", From: conditionStates(toned("Pending", "warn"),
		conditionState{"PodGroupInitiallyScheduled", "Scheduled", "ok"},
	)},
}

var compositePodGroupColumns = []column{
	nameColumn,
	{Name: "Workload", Path: ".spec.workloadRef.workloadName"},
	{Name: "Parent", Path: ".spec.parentCompositePodGroupName"},
	{Name: "Policy", From: schedulingPolicy("minGroupCount")},
	{Name: "Priority Class", Path: ".spec.priorityClassName"},
	ageColumn,
}

// schedulingPolicy reads a pod group's policy: "Gang (min 4)" or "Basic". The
// minimum lives under a different name for a composite group, which counts
// groups rather than pods.
func schedulingPolicy(minimum string) func(*unstructured.Unstructured) Cell {
	return func(u *unstructured.Unstructured) Cell {
		if gang := asMap(nestedMap(u, "spec", "schedulingPolicy", "gang")); gang != nil {
			return plain("Gang (min " + mapNumber(gang, minimum) + ")")
		}
		if asMap(nestedMap(u, "spec", "schedulingPolicy", "basic")) != nil {
			return plain("Basic")
		}
		return muted("<none>")
	}
}

var evictionRequestColumns = []column{
	nameColumn,
	{Name: "Pod", Path: ".spec.target.pod.name"},
	{Name: "Requester", Path: ".spec.requester"},
	{Name: "Intent", From: func(u *unstructured.Unstructured) Cell {
		intent := nestedString(u, "spec", "intent")
		if intent == "Withdrawn" {
			return muted(intent)
		}
		return toned(intent, "warn")
	}},
	ageColumn,
}

var evictionColumns = []column{
	nameColumn,
	{Name: "Pod", Path: ".spec.target.pod.name"},
	{Name: "Requesters", From: func(u *unstructured.Unstructured) Cell {
		return plain(joinStrings(fieldOfEach(nestedSlice(u, "status", "requesters"), "name"), 2))
	}},
	// Whoever the eviction is waiting on right now.
	{Name: "Responder", From: func(u *unstructured.Unstructured) Cell {
		for _, raw := range nestedSlice(u, "status", "targetResponders") {
			r := asMap(raw)
			if mapString(r, "state") == "Active" {
				return plain(mapString(r, "name"))
			}
		}
		return muted("<none>")
	}},
	ageColumn,
	{Name: "Status", From: conditionStates(toned("In progress", "warn"),
		conditionState{"TargetEvicted", "Evicted", "ok"},
		conditionState{"Failed", "Failed", "error"},
	)},
}

// ---- dynamic resource allocation ------------------------------------------

var deviceClassColumns = []column{
	nameColumn,
	{Name: "Extended Resource", Path: ".spec.extendedResourceName"},
	{Name: "Selectors", From: func(u *unstructured.Unstructured) Cell {
		return number(len(nestedSlice(u, "spec", "selectors")))
	}},
	ageColumn,
}

var resourceClaimColumns = []column{
	nameColumn,
	{Name: "Device Classes", From: func(u *unstructured.Unstructured) Cell {
		return plain(joinStrings(requestedClasses(nestedSlice(u, "spec", "devices", "requests")), 3))
	}},
	{Name: "Reserved For", From: func(u *unstructured.Unstructured) Cell {
		var names []string
		for _, raw := range nestedSlice(u, "status", "reservedFor") {
			r := asMap(raw)
			names = append(names, mapString(r, "resource")+"/"+mapString(r, "name"))
		}
		return muted(joinStrings(names, 2))
	}},
	ageColumn,
	{Name: "State", From: claimState},
}

var resourceClaimTemplateColumns = []column{
	nameColumn,
	{Name: "Device Classes", From: func(u *unstructured.Unstructured) Cell {
		return plain(joinStrings(requestedClasses(nestedSlice(u, "spec", "spec", "devices", "requests")), 3))
	}},
	ageColumn,
}

var resourceSliceColumns = []column{
	nameColumn,
	{Name: "Node", From: func(u *unstructured.Unstructured) Cell {
		if node := nestedString(u, "spec", "nodeName"); node != "" {
			return plain(node)
		}
		all, _, _ := unstructured.NestedBool(u.Object, "spec", "allNodes")
		switch {
		case all:
			return muted("all nodes")
		case len(asMap(nestedMap(u, "spec", "nodeSelector"))) > 0:
			return muted("by selector")
		default:
			return muted("per device")
		}
	}},
	{Name: "Driver", Path: ".spec.driver"},
	{Name: "Pool", Path: ".spec.pool.name"},
	{Name: "Devices", From: func(u *unstructured.Unstructured) Cell {
		return number(len(nestedSlice(u, "spec", "devices")))
	}},
	ageColumn,
}

var deviceTaintRuleColumns = []column{
	nameColumn,
	{Name: "Taint", From: func(u *unstructured.Unstructured) Cell {
		key := nestedString(u, "spec", "taint", "key")
		if key == "" {
			return muted("<none>")
		}
		if value := nestedString(u, "spec", "taint", "value"); value != "" {
			key += "=" + value
		}
		return toned(key+":"+nestedString(u, "spec", "taint", "effect"), "warn")
	}},
	{Name: "Devices", From: func(u *unstructured.Unstructured) Cell {
		var parts []string
		for _, field := range []string{"deviceClassName", "driver", "pool", "device"} {
			if v := nestedString(u, "spec", "deviceSelector", field); v != "" {
				parts = append(parts, field+"="+v)
			}
		}
		if len(parts) == 0 {
			return muted("all")
		}
		return plain(strings.Join(parts, ", "))
	}},
	{Name: "Evicting", From: conditionStates(muted("False"),
		conditionState{"EvictionInProgress", "True", "warn"},
	)},
	ageColumn,
}

var resourcePoolStatusRequestColumns = []column{
	nameColumn,
	{Name: "Driver", Path: ".spec.driver"},
	{Name: "Pool", From: func(u *unstructured.Unstructured) Cell {
		if pool := nestedString(u, "spec", "poolName"); pool != "" {
			return plain(pool)
		}
		return muted("all")
	}},
	{Name: "Pools", Path: ".status.poolCount"},
	ageColumn,
	{Name: "Status", From: conditionStates(toned("Pending", "warn"),
		conditionState{"Complete", "Complete", "ok"},
		conditionState{"Failed", "Failed", "error"},
	)},
}

// requestedClasses lists the device classes a claim's requests name, both the
// exact ones and each alternative of a first-available one.
func requestedClasses(requests []any) []string {
	var out []string
	for _, raw := range requests {
		r := asMap(raw)
		if class := mapString(asMap(r["exactly"]), "deviceClassName"); class != "" {
			out = append(out, class)
		}
		for _, alt := range asSlice(r["firstAvailable"]) {
			if class := mapString(asMap(alt), "deviceClassName"); class != "" {
				out = append(out, class)
			}
		}
	}
	return out
}

// claimState is kubectl's STATE column for a claim: pending until devices are
// allocated to it, reserved once a pod is using them.
func claimState(u *unstructured.Unstructured) Cell {
	if u.GetDeletionTimestamp() != nil {
		return toned("Deleting", "warn")
	}
	if len(asMap(nestedMap(u, "status", "allocation"))) == 0 {
		return toned("Pending", "warn")
	}
	tags := []Tag{{Text: "Allocated", Tone: "ok"}}
	if len(nestedSlice(u, "status", "reservedFor")) > 0 {
		tags = append(tags, Tag{Text: "Reserved", Tone: "ok"})
	}
	return tagged(tags)
}

// ---- API server ------------------------------------------------------------

var apiServiceColumns = []column{
	nameColumn,
	{Name: "Service", From: func(u *unstructured.Unstructured) Cell {
		name := nestedString(u, "spec", "service", "name")
		if name == "" {
			// Served by the API server itself rather than by an extension.
			return muted("Local")
		}
		return plain(nestedString(u, "spec", "service", "namespace") + "/" + name)
	}},
	{Name: "Available", From: apiServiceAvailable},
	ageColumn,
}

// apiServiceAvailable reads as kubectl does: True, or False with the reason,
// since an unavailable aggregated API is the classic cause of discovery
// errors across every client of the cluster, and the reason says why.
func apiServiceAvailable(u *unstructured.Unstructured) Cell {
	for _, raw := range nestedSlice(u, "status", "conditions") {
		c := asMap(raw)
		if mapString(c, "type") != "Available" {
			continue
		}
		switch mapString(c, "status") {
		case "True":
			return toned("True", "ok")
		case "False":
			text := "False"
			if reason := mapString(c, "reason"); reason != "" {
				text += " (" + reason + ")"
			}
			return toned(text, "error")
		}
	}
	return muted("Unknown")
}

var flowSchemaColumns = []column{
	nameColumn,
	{Name: "Priority Level", Path: ".spec.priorityLevelConfiguration.name"},
	{Name: "Precedence", Path: ".spec.matchingPrecedence"},
	{Name: "Distinguisher", From: func(u *unstructured.Unstructured) Cell {
		if method := nestedString(u, "spec", "distinguisherMethod", "type"); method != "" {
			return plain(method)
		}
		return muted("<none>")
	}},
	ageColumn,
	// A schema pointing at a priority level that does not exist.
	{Name: "Missing Level", From: conditionStates(muted("False"),
		conditionState{"Dangling", "True", "warn"},
	)},
}

var priorityLevelColumns = []column{
	nameColumn,
	{Name: "Type", Path: ".spec.type"},
	{Name: "Shares", From: func(u *unstructured.Unstructured) Cell {
		if shares := evalPath(u, ".spec.limited.nominalConcurrencyShares"); shares != "" {
			return plain(shares)
		}
		return plain(evalPath(u, ".spec.exempt.nominalConcurrencyShares"))
	}},
	{Name: "Queues", Path: ".spec.limited.limitResponse.queuing.queues"},
	{Name: "Hand Size", Path: ".spec.limited.limitResponse.queuing.handSize"},
	{Name: "Queue Length", Path: ".spec.limited.limitResponse.queuing.queueLengthLimit"},
	ageColumn,
}

var storageVersionColumns = []column{
	nameColumn,
	{Name: "Encoding Version", Path: ".status.commonEncodingVersion"},
	{Name: "API Servers", From: func(u *unstructured.Unstructured) Cell {
		return number(len(nestedSlice(u, "status", "storageVersions")))
	}},
	{Name: "Agreed", From: conditionCell("AllEncodingVersionsEqual", "status", "conditions")},
	ageColumn,
}

var storageVersionMigrationColumns = []column{
	nameColumn,
	{Name: "Resource", From: func(u *unstructured.Unstructured) Cell {
		resource := nestedString(u, "spec", "resource", "resource")
		if group := nestedString(u, "spec", "resource", "group"); group != "" {
			resource += "." + group
		}
		return plain(resource)
	}},
	ageColumn,
	{Name: "Status", From: conditionStates(toned("Pending", "warn"),
		conditionState{"Failed", "Failed", "error"},
		conditionState{"Succeeded", "Succeeded", "ok"},
		conditionState{"Running", "Running", "warn"},
	)},
}

// ---- shared ----------------------------------------------------------------

// controllerOf names what manages an object, as kubectl's CONTROLLER column
// does: the owner marked as controller, or the first owner when none is.
func controllerOf(u *unstructured.Unstructured) Cell {
	refs := u.GetOwnerReferences()
	for _, ref := range refs {
		if ref.Controller != nil && *ref.Controller {
			return plain(ref.Kind + "/" + ref.Name)
		}
	}
	if len(refs) > 0 {
		return plain(refs[0].Kind + "/" + refs[0].Name)
	}
	return muted("<none>")
}

// fieldOfEach reads one string field from every entry of a list, skipping the
// entries that do not have it.
func fieldOfEach(items []any, field string) []string {
	var out []string
	for _, raw := range items {
		if v := mapString(asMap(raw), field); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// conditionState is one reading of a condition: when it is True, the cell
// says text in tone.
type conditionState struct {
	condition, text, tone string
}

// conditionStates renders the first of several conditions that holds, in the
// order given, or otherwise when none does. It is for the kinds whose status
// is a set of mutually exclusive outcomes -- Issued or Denied, Succeeded or
// Failed -- rather than a single Ready.
func conditionStates(otherwise Cell, states ...conditionState) func(*unstructured.Unstructured) Cell {
	return func(u *unstructured.Unstructured) Cell {
		for _, s := range states {
			if conditionStatus(u, s.condition, "status", "conditions") == "True" {
				return toned(s.text, s.tone)
			}
		}
		return otherwise
	}
}
