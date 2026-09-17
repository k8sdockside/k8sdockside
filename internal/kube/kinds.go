package kube

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GatewayGroup is the Gateway API's group. Ingress is deprecated but still in
// wide use, so both are offered; these kinds are not a replacement for the
// Network group, they sit beside it.
const GatewayGroup = "gateway.networking.k8s.io"

// AdmissionGroup holds the webhook configurations and the newer admission
// policies alike. The policy kinds arrived long after the webhooks and are
// still reaching general availability, so a cluster may serve the webhooks and
// not the policies; both resolve through the same optional-kind path.
const AdmissionGroup = "admissionregistration.k8s.io"

// RBACGroup holds the roles and the bindings that grant them.
const RBACGroup = "rbac.authorization.k8s.io"

// The built-in groups that hold several of the kinds below. Unlike the Gateway
// API these ship with Kubernetes itself, but the newer kinds in them are alpha
// or beta and a cluster serves them only when they are switched on.
const (
	CertificatesGroup = "certificates.k8s.io"
	DRAGroup          = "resource.k8s.io"
	FlowControlGroup  = "flowcontrol.apiserver.k8s.io"
	LifecycleGroup    = "lifecycle.k8s.io"
	SchedulingGroup   = "scheduling.k8s.io"
	StorageGroup      = "storage.k8s.io"
)

// Gateway API and CRD kinds, continuing the contract in resources.go: these
// strings are what the frontend catalogue sends to BuildTable and Subscribe.
const (
	KindGatewayClasses     = "gatewayclasses"
	KindGateways           = "gateways"
	KindListenerSets       = "listenersets"
	KindHTTPRoutes         = "httproutes"
	KindGRPCRoutes         = "grpcroutes"
	KindTLSRoutes          = "tlsroutes"
	KindTCPRoutes          = "tcproutes"
	KindUDPRoutes          = "udproutes"
	KindBackendTLSPolicies = "backendtlspolicies"
	KindReferenceGrants    = "referencegrants"
	KindCRDs               = "customresourcedefinitions"
)

// CustomPrefix marks a kind that names a custom resource rather than one of the
// kinds compiled in above: "crd:<plural>.<group>", e.g.
// "crd:certificates.cert-manager.io".
//
// It is a prefixed string rather than a structured field because a tab's kind
// is persisted (appconfig.TabRef.Kind) and travels through the frontend's tab
// machinery untouched; making it a string keeps every one of those layers
// unchanged. Both sides parse it in exactly one place: here, and labelFor() in
// the frontend catalogue.
const CustomPrefix = "crd:"

// CustomKind builds the kind string for a custom resource.
func CustomKind(plural, group string) string {
	return CustomPrefix + plural + "." + group
}

// ParseCustomKind splits a "crd:" kind back into its plural and group. A group
// is required: every CRD has one, since custom resources may not live in the
// core group.
func ParseCustomKind(kind string) (plural, group string, ok bool) {
	rest, found := strings.CutPrefix(kind, CustomPrefix)
	if !found {
		return "", "", false
	}
	plural, group, found = strings.Cut(rest, ".")
	if !found || plural == "" || group == "" {
		return "", "", false
	}
	return plural, group, true
}

// builtinKinds maps the kinds the UI offers by name onto their API group and
// kind. Versions are deliberately absent: the REST mapper picks whichever
// version the cluster in front of us actually serves, which is what lets one
// entry cover both v1 and v1beta1 of the Gateway API.
var builtinKinds = map[string]schema.GroupKind{
	KindPods:        {Kind: "Pod"},
	KindNamespaces:  {Kind: "Namespace"},
	KindNodes:       {Kind: "Node"},
	KindServices:    {Kind: "Service"},
	KindConfigMaps:  {Kind: "ConfigMap"},
	KindSecrets:     {Kind: "Secret"},
	KindPVCs:        {Kind: "PersistentVolumeClaim"},
	KindEvents:      {Kind: "Event"},
	KindDeployments: {Group: "apps", Kind: "Deployment"},
	KindStatefulSet: {Group: "apps", Kind: "StatefulSet"},
	KindDaemonSets:  {Group: "apps", Kind: "DaemonSet"},
	KindJobs:        {Group: "batch", Kind: "Job"},
	KindCronJobs:    {Group: "batch", Kind: "CronJob"},
	KindIngresses:   {Group: "networking.k8s.io", Kind: "Ingress"},

	KindReplicaSets:            {Group: "apps", Kind: "ReplicaSet"},
	KindReplicationControllers: {Kind: "ReplicationController"},
	KindHPAs:                   {Group: "autoscaling", Kind: "HorizontalPodAutoscaler"},

	KindResourceQuotas: {Kind: "ResourceQuota"},
	KindLimitRanges:    {Kind: "LimitRange"},
	KindLeases:         {Group: "coordination.k8s.io", Kind: "Lease"},

	KindPDBs:            {Group: "policy", Kind: "PodDisruptionBudget"},
	KindPriorityClasses: {Group: "scheduling.k8s.io", Kind: "PriorityClass"},
	KindRuntimeClasses:  {Group: "node.k8s.io", Kind: "RuntimeClass"},

	KindMutatingWebhooks:                  {Group: AdmissionGroup, Kind: "MutatingWebhookConfiguration"},
	KindValidatingWebhooks:                {Group: AdmissionGroup, Kind: "ValidatingWebhookConfiguration"},
	KindMutatingAdmissionPolicies:         {Group: AdmissionGroup, Kind: "MutatingAdmissionPolicy"},
	KindMutatingAdmissionPolicyBindings:   {Group: AdmissionGroup, Kind: "MutatingAdmissionPolicyBinding"},
	KindValidatingAdmissionPolicies:       {Group: AdmissionGroup, Kind: "ValidatingAdmissionPolicy"},
	KindValidatingAdmissionPolicyBindings: {Group: AdmissionGroup, Kind: "ValidatingAdmissionPolicyBinding"},

	KindEndpointSlices:  {Group: "discovery.k8s.io", Kind: "EndpointSlice"},
	KindEndpoints:       {Kind: "Endpoints"},
	KindIngressClasses:  {Group: "networking.k8s.io", Kind: "IngressClass"},
	KindNetworkPolicies: {Group: "networking.k8s.io", Kind: "NetworkPolicy"},

	KindStorageClasses: {Group: "storage.k8s.io", Kind: "StorageClass"},
	KindPVs:            {Kind: "PersistentVolume"},

	KindServiceAccounts:     {Kind: "ServiceAccount"},
	KindRoles:               {Group: RBACGroup, Kind: "Role"},
	KindRoleBindings:        {Group: RBACGroup, Kind: "RoleBinding"},
	KindClusterRoles:        {Group: RBACGroup, Kind: "ClusterRole"},
	KindClusterRoleBindings: {Group: RBACGroup, Kind: "ClusterRoleBinding"},

	KindGatewayClasses: {Group: GatewayGroup, Kind: "GatewayClass"},
	KindGateways:       {Group: GatewayGroup, Kind: "Gateway"},
	// Only the standard-channel ListenerSet. Before v1.5 it was experimental,
	// as XListenerSet in gateway.networking.x-k8s.io -- a different group and
	// kind, which the definitions tree still reaches.
	KindListenerSets: {Group: GatewayGroup, Kind: "ListenerSet"},
	KindHTTPRoutes:   {Group: GatewayGroup, Kind: "HTTPRoute"},
	KindGRPCRoutes:   {Group: GatewayGroup, Kind: "GRPCRoute"},
	// The TLS, TCP and UDP routes spent years at v1alpha2 in the experimental
	// channel before reaching v1; the mapper serves whichever is installed.
	KindTLSRoutes:          {Group: GatewayGroup, Kind: "TLSRoute"},
	KindTCPRoutes:          {Group: GatewayGroup, Kind: "TCPRoute"},
	KindUDPRoutes:          {Group: GatewayGroup, Kind: "UDPRoute"},
	KindBackendTLSPolicies: {Group: GatewayGroup, Kind: "BackendTLSPolicy"},
	KindReferenceGrants:    {Group: GatewayGroup, Kind: "ReferenceGrant"},

	KindCRDs: {Group: "apiextensions.k8s.io", Kind: "CustomResourceDefinition"},

	KindControllerRevisions: {Group: "apps", Kind: "ControllerRevision"},
	KindPodTemplates:        {Kind: "PodTemplate"},
	KindLeaseCandidates:     {Group: "coordination.k8s.io", Kind: "LeaseCandidate"},

	KindServiceCIDRs: {Group: "networking.k8s.io", Kind: "ServiceCIDR"},
	KindIPAddresses:  {Group: "networking.k8s.io", Kind: "IPAddress"},

	KindVolumeAttachments:       {Group: StorageGroup, Kind: "VolumeAttachment"},
	KindVolumeAttributesClasses: {Group: StorageGroup, Kind: "VolumeAttributesClass"},
	KindCSIDrivers:              {Group: StorageGroup, Kind: "CSIDriver"},
	KindCSINodes:                {Group: StorageGroup, Kind: "CSINode"},
	KindCSIStorageCapacities:    {Group: StorageGroup, Kind: "CSIStorageCapacity"},

	KindCSRs:                   {Group: CertificatesGroup, Kind: "CertificateSigningRequest"},
	KindClusterTrustBundles:    {Group: CertificatesGroup, Kind: "ClusterTrustBundle"},
	KindPodCertificateRequests: {Group: CertificatesGroup, Kind: "PodCertificateRequest"},

	KindWorkloads:          {Group: SchedulingGroup, Kind: "Workload"},
	KindPodGroups:          {Group: SchedulingGroup, Kind: "PodGroup"},
	KindCompositePodGroups: {Group: SchedulingGroup, Kind: "CompositePodGroup"},
	// Not policy/v1's Eviction, which is a pod subresource and cannot be
	// listed: this is the object the eviction-request controller keeps per
	// target, in a group of its own.
	KindEvictionRequests: {Group: LifecycleGroup, Kind: "EvictionRequest"},
	KindEvictions:        {Group: LifecycleGroup, Kind: "Eviction"},

	KindDeviceClasses:              {Group: DRAGroup, Kind: "DeviceClass"},
	KindResourceClaims:             {Group: DRAGroup, Kind: "ResourceClaim"},
	KindResourceClaimTemplates:     {Group: DRAGroup, Kind: "ResourceClaimTemplate"},
	KindResourceSlices:             {Group: DRAGroup, Kind: "ResourceSlice"},
	KindDeviceTaintRules:           {Group: DRAGroup, Kind: "DeviceTaintRule"},
	KindResourcePoolStatusRequests: {Group: DRAGroup, Kind: "ResourcePoolStatusRequest"},

	// APIService is served by the aggregator inside every API server, though
	// its types live outside k8s.io/api -- which is no matter here, since
	// everything travels unstructured.
	KindAPIServices:                 {Group: "apiregistration.k8s.io", Kind: "APIService"},
	KindFlowSchemas:                 {Group: FlowControlGroup, Kind: "FlowSchema"},
	KindPriorityLevelConfigurations: {Group: FlowControlGroup, Kind: "PriorityLevelConfiguration"},
	KindStorageVersions:             {Group: "internal.apiserver.k8s.io", Kind: "StorageVersion"},
	KindStorageVersionMigrations:    {Group: "storagemigration.k8s.io", Kind: "StorageVersionMigration"},
}

// mappingForKind resolves a kind named by the UI to the resource to watch.
//
// Custom resources take the other branch: they are named by plural and group
// rather than by kind, because that is what a CRD's own identity is, so they
// resolve through the resource side of the mapper.
func (c *clusterClient) mappingForKind(kind string) (*meta.RESTMapping, error) {
	if plural, group, ok := ParseCustomKind(kind); ok {
		gvr := schema.GroupVersionResource{Group: group, Resource: plural}
		gvk, err := c.mapper.KindFor(gvr)
		if err != nil && c.refreshDiscovery() {
			gvk, err = c.mapper.KindFor(gvr)
		}
		if err != nil {
			return nil, notServed(plural+"."+group, group, err)
		}
		m, err := c.mappingFor(gvk.GroupKind())
		if err != nil {
			return nil, notServed(plural+"."+group, group, err)
		}
		return m, nil
	}

	gk, ok := builtinKinds[kind]
	if !ok {
		return nil, fmt.Errorf("unknown resource kind: %s", kind)
	}
	m, err := c.mappingFor(gk)
	if err != nil {
		return nil, notServed(kind, gk.Group, err)
	}
	return m, nil
}

// notServed explains a kind the cluster has no mapping for.
//
// The Gateway API and every CRD are optional, so this is an ordinary state
// rather than a fault, and the message has to say what would fix it: naming the
// API group turns "no matches for kind GRPCRoute" into something the reader can
// act on.
func notServed(kind, group string, err error) error {
	if !meta.IsNoMatchError(err) {
		return fmt.Errorf("looking up %s: %w", kind, err)
	}
	if group == "" {
		return fmt.Errorf("this cluster does not serve %s", kind)
	}
	return fmt.Errorf("this cluster does not serve %s -- the %s API is not installed", kind, group)
}

// ErrNotServed reports whether an error came from a kind this cluster does not
// serve, which the UI treats as an empty view rather than a failure.
func ErrNotServed(err error) bool {
	return err != nil && strings.Contains(err.Error(), "does not serve")
}
