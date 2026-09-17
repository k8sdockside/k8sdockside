// The Kubernetes primer: what a cluster is, what the objects in the sidebar
// are, and the words that come up everywhere. Written for someone who has a
// cluster in front of them and not much idea what a ReplicaSet is, and kept
// to what the app can show; each object links to the official page for the
// rest. The order of the objects follows the sidebar's sections.

import type { Page } from './types';

const K8S = 'https://kubernetes.io/docs';

export const KUBERNETES_PRIMER: Page = {
    title: 'Kubernetes primer',
    lede: 'What a cluster is, what the things in the sidebar are, and where to read more.',
    sections: [
        {
            id: 'what',
            label: 'What Kubernetes is',
            icon: 'info',
            lede: 'A system that keeps a set of machines running the containers you asked for.',
            blocks: [
                {
                    type: 'p',
                    text: 'Kubernetes runs containers across a group of machines. You tell it what you want running — three copies of this image, reachable on this port, with this much memory — and it works out where to put them, starts them, restarts them when they die, and moves them when a machine goes away. The thing you describe is the **desired state**; what is actually running is the **current state**; the whole system is a set of loops that try to make the second match the first.',
                },
                { type: 'h3', text: 'The parts' },
                {
                    type: 'terms',
                    terms: [
                        {
                            term: 'Cluster',
                            meaning: 'The whole thing: a control plane plus the machines that run your workloads. A kubeconfig context points at one cluster.',
                            href: `${K8S}/concepts/overview/components/`,
                        },
                        {
                            term: 'Control plane',
                            meaning: 'The brain. The **API server** is the front door every tool talks to, including this app. **etcd** stores everything. The **scheduler** decides which node each pod goes to. The **controller manager** runs the loops that keep current state matching desired state.',
                            href: `${K8S}/concepts/overview/components/#control-plane-components`,
                        },
                        {
                            term: 'Node',
                            meaning: 'A machine — real or virtual — that runs containers. Each runs a **kubelet**, which takes orders from the API server and reports back, and a container runtime that actually starts the containers.',
                            resource: 'nodes',
                            href: `${K8S}/concepts/architecture/nodes/`,
                        },
                        {
                            term: 'The API',
                            meaning: 'Everything in Kubernetes is an object with a kind, a name, a spec (what you want) and a status (what is). `kubectl` reads and writes those objects over HTTPS; so does this app. There is no other way in, which is what makes tools interchangeable.',
                            href: `${K8S}/concepts/overview/kubernetes-api/`,
                        },
                        {
                            term: 'kubeconfig and context',
                            meaning: 'A kubeconfig file holds clusters (an address and a certificate), users (how to authenticate), and contexts that pair one with the other under a name. Selecting a context is choosing which cluster to talk to as whom. This app reads your kubeconfig files and never writes them.',
                            href: `${K8S}/concepts/configuration/organize-cluster-access-kubeconfig/`,
                        },
                    ],
                },
                {
                    type: 'note',
                    text: 'Where this app sits: it is a client of the API server, like `kubectl`. It watches objects and shows them; it changes nothing until you press an action, and then it does exactly what the equivalent `kubectl` command would.',
                },
                {
                    type: 'links',
                    links: [
                        { label: 'Kubernetes concepts', href: `${K8S}/concepts/`, note: 'the official overview, from the top' },
                        { label: 'What is Kubernetes?', href: `${K8S}/concepts/overview/`, note: 'the short version, with history' },
                    ],
                },
            ],
        },
        {
            id: 'cluster',
            label: 'Cluster objects',
            icon: 'server',
            lede: 'The sidebar’s first section: the machines and the boundaries.',
            blocks: [
                {
                    type: 'terms',
                    terms: [
                        {
                            term: 'Node',
                            meaning: 'A machine in the cluster. Its status says whether it is Ready, how much CPU and memory it has, and what it is running. **Cordon** stops new pods landing on it; **drain** moves the existing ones off, which is how you take a node down for maintenance.',
                            resource: 'nodes',
                            href: `${K8S}/concepts/architecture/nodes/`,
                        },
                        {
                            term: 'Namespace',
                            meaning: 'A named partition of the cluster. Most objects live in one, and names only have to be unique within it — two teams can both have a `web` deployment in their own namespaces. Access rules, quotas and network policies are usually drawn at this line. A few kinds, such as nodes and namespaces themselves, are not namespaced.',
                            resource: 'namespaces',
                            href: `${K8S}/concepts/overview/working-with-objects/namespaces/`,
                        },
                        {
                            term: 'Event',
                            meaning: 'A note the cluster leaves when something happens to an object: a pod was scheduled, an image could not be pulled, a container was killed. Events are the first place to look when something is wrong, and they expire after an hour or so.',
                            resource: 'events',
                            href: `${K8S}/reference/kubernetes-api/cluster-resources/event-v1/`,
                        },
                        {
                            term: 'Lease',
                            meaning: 'A small object controllers use to elect a leader and to prove they are alive — the kubelet renews one per node. You rarely touch them; they are listed because a stale lease explains a controller that has stopped.',
                            resource: 'leases',
                            href: `${K8S}/concepts/architecture/leases/`,
                        },
                        {
                            term: 'LeaseCandidate',
                            meaning: 'A component standing for a lease, with the version it runs. During an upgrade the control plane uses these to pick a leader that every other component can work with.',
                            resource: 'leasecandidates',
                            href: `${K8S}/concepts/cluster-administration/coordinated-leader-election/`,
                        },
                    ],
                },
            ],
        },
        {
            id: 'workloads',
            label: 'Workloads',
            icon: 'box',
            lede: 'The things that run: pods, and the controllers that keep the right number of them running.',
            blocks: [
                {
                    type: 'p',
                    text: 'You almost never create a pod by hand. You create a **Deployment** or one of its siblings, which creates pods for you and replaces them when they fail. That indirection is the heart of the system: the pod is disposable, the controller is what you own.',
                },
                {
                    type: 'terms',
                    terms: [
                        {
                            term: 'Pod',
                            meaning: 'The smallest thing Kubernetes runs: one or more containers that share a network address and can share storage. A pod is scheduled to one node and lives until it is deleted or that node fails. Its **phase** — Pending, Running, Succeeded, Failed — and its containers’ restart counts are the first things to read.',
                            resource: 'pods',
                            href: `${K8S}/concepts/workloads/pods/`,
                        },
                        {
                            term: 'Deployment',
                            meaning: 'Keeps a given number of identical pods running and rolls out changes to them gradually. Change the image in its spec and it creates new pods and retires old ones, keeping the app up throughout. **Scale** changes the number; **restart** rolls every pod once.',
                            resource: 'deployments',
                            href: `${K8S}/concepts/workloads/controllers/deployment/`,
                        },
                        {
                            term: 'ReplicaSet',
                            meaning: 'What a Deployment uses underneath to hold N copies of a pod. Each rollout makes a new one; the old ones stay around, empty, so a rollback can reuse them. You look at these to understand a rollout, not to edit.',
                            resource: 'replicasets',
                            href: `${K8S}/concepts/workloads/controllers/replicaset/`,
                        },
                        {
                            term: 'StatefulSet',
                            meaning: 'Like a Deployment, but each pod has a stable name and its own storage that follows it — `db-0`, `db-1` — and they start and stop in order. For databases and anything else that cares which copy it is.',
                            resource: 'statefulsets',
                            href: `${K8S}/concepts/workloads/controllers/statefulset/`,
                        },
                        {
                            term: 'DaemonSet',
                            meaning: 'One pod on every node, or every node matching a selector. Log shippers, monitoring agents and network plugins run this way, which is why a cluster with three nodes has three of each.',
                            resource: 'daemonsets',
                            href: `${K8S}/concepts/workloads/controllers/daemonset/`,
                        },
                        {
                            term: 'Job and CronJob',
                            meaning: 'A Job runs pods until a number of them finish successfully, then stops: a migration, a batch. A CronJob creates a Job on a schedule.',
                            resource: 'jobs',
                            href: `${K8S}/concepts/workloads/controllers/job/`,
                        },
                        {
                            term: 'Horizontal Pod Autoscaler',
                            meaning: 'Adjusts a Deployment’s replica count from a metric, usually CPU. It needs metrics to read, which is what metrics-server provides.',
                            resource: 'horizontalpodautoscalers',
                            href: `${K8S}/tasks/run-application/horizontal-pod-autoscale/`,
                        },
                        {
                            term: 'ControllerRevision',
                            meaning: 'A snapshot of a StatefulSet’s or DaemonSet’s pod template, one per rollout — what **rollback** returns to. A Deployment keeps the same history as its old ReplicaSets instead.',
                            resource: 'controllerrevisions',
                            href: `${K8S}/reference/kubernetes-api/workload-resources/controller-revision-v1/`,
                        },
                        {
                            term: 'PodTemplate',
                            meaning: 'A pod spec stored on its own, for controllers that want to keep one as an object. Most templates live inside their workload instead, so this list is usually empty.',
                            resource: 'podtemplates',
                            href: `${K8S}/concepts/workloads/pods/#pod-templates`,
                        },
                        {
                            term: 'ReplicationController',
                            meaning: 'The ancestor of ReplicaSet. Still served, rarely used. If you see one, something old is installed.',
                            resource: 'replicationcontrollers',
                            href: `${K8S}/concepts/workloads/controllers/replicationcontroller/`,
                        },
                    ],
                },
                {
                    type: 'note',
                    text: '**Requests and limits.** Each container can say how much CPU and memory it needs (the request, which the scheduler books) and how much it may use (the limit, above which it is throttled or killed). The dashboard’s Resources panel draws both against what the nodes have.',
                },
            ],
        },
        {
            id: 'network',
            label: 'Network',
            icon: 'share',
            lede: 'How traffic finds a pod, from inside the cluster and from outside it.',
            blocks: [
                {
                    type: 'p',
                    text: 'Every pod gets its own IP address, and pods can reach each other directly. But pods come and go, so nothing should remember a pod’s address. A **Service** is the stable name in front of a changing set of pods; an **Ingress** or a **Gateway** is how traffic from outside reaches a Service.',
                },
                {
                    type: 'terms',
                    terms: [
                        {
                            term: 'Service',
                            meaning: 'A stable address and DNS name — `web.default.svc` — for the pods matching a selector. Type `ClusterIP` is reachable inside the cluster only; `NodePort` opens a port on every node; `LoadBalancer` asks the cloud for an external address. A port forward in this app is made against a Service or a pod.',
                            resource: 'services',
                            href: `${K8S}/concepts/services-networking/service/`,
                        },
                        {
                            term: 'EndpointSlice',
                            meaning: 'The list of pod addresses currently behind a Service, kept up to date by the control plane. If a Service answers nothing, an empty EndpointSlice is why: no pod matched its selector, or none were ready.',
                            resource: 'endpointslices',
                            href: `${K8S}/concepts/services-networking/endpoint-slices/`,
                        },
                        {
                            term: 'Ingress',
                            meaning: 'Rules for routing HTTP from outside the cluster to Services by host name and path, acted on by an ingress controller such as nginx or Traefik. The `IngressClass` says which controller. Being replaced by the Gateway API, but everywhere.',
                            resource: 'ingresses',
                            href: `${K8S}/concepts/services-networking/ingress/`,
                        },
                        {
                            term: 'ServiceCIDR and IPAddress',
                            meaning: 'The ranges Services take their cluster IPs from, and one object per address taken, naming the Service that holds it. A cluster that has run out of Service IPs shows it here, and a new range can be added without restarting anything.',
                            resource: 'servicecidrs',
                            href: `${K8S}/tasks/network/extend-service-ip-ranges/`,
                        },
                        {
                            term: 'NetworkPolicy',
                            meaning: 'A firewall rule for pods: which pods may talk to which, on which ports. Without any policy, everything can reach everything. It needs a network plugin that enforces it.',
                            resource: 'networkpolicies',
                            href: `${K8S}/concepts/services-networking/network-policies/`,
                        },
                        {
                            term: 'Gateway API',
                            meaning: 'The successor to Ingress: a **GatewayClass** names an implementation, a **Gateway** is a listening endpoint, and **HTTPRoute**, **GRPCRoute** and the rest attach routing rules to it. Optional; its views say it is not installed when a cluster does not serve it.',
                            resource: 'gateways',
                            href: 'https://gateway-api.sigs.k8s.io/',
                        },
                    ],
                },
            ],
        },
        {
            id: 'config',
            label: 'Config and storage',
            icon: 'database',
            lede: 'What a pod reads at start, and what survives it.',
            blocks: [
                {
                    type: 'terms',
                    terms: [
                        {
                            term: 'ConfigMap',
                            meaning: 'Key–value configuration, mounted into a pod as files or environment variables. Change it and most pods will not notice until restarted.',
                            resource: 'configmaps',
                            href: `${K8S}/concepts/configuration/configmap/`,
                        },
                        {
                            term: 'Secret',
                            meaning: 'The same, for passwords, keys and certificates. Base64-encoded rather than encrypted by default; this app redacts their values before caching and shows only the key names.',
                            resource: 'secrets',
                            href: `${K8S}/concepts/configuration/secret/`,
                        },
                        {
                            term: 'PersistentVolumeClaim',
                            meaning: 'A pod’s request for storage: this much, with this access. A **PersistentVolume** is the actual disk it is bound to, and a **StorageClass** is the kind of disk to provision — fast SSD, shared network storage — usually created on demand by a driver. Status `Bound` is the good one.',
                            resource: 'persistentvolumeclaims',
                            href: `${K8S}/concepts/storage/persistent-volumes/`,
                        },
                        {
                            term: 'CSI driver, CSI node and VolumeAttachment',
                            meaning: 'How storage reaches a pod. A **CSIDriver** is a storage plugin installed in the cluster; a **CSINode** says which drivers each node runs; a **VolumeAttachment** is one volume attached to one node. A pod stuck in ContainerCreating on a volume is usually explained by its attachment’s error.',
                            resource: 'volumeattachments',
                            href: `${K8S}/concepts/storage/volumes/#csi`,
                        },
                        {
                            term: 'VolumeAttributesClass',
                            meaning: 'A named set of volume settings — IOPS, throughput — that a claim can switch between without being recreated, the way a StorageClass is chosen when it is made.',
                            resource: 'volumeattributesclasses',
                            href: `${K8S}/concepts/storage/volume-attributes-classes/`,
                        },
                        {
                            term: 'ResourceQuota and LimitRange',
                            meaning: 'A quota caps what a namespace may use in total; a limit range sets defaults and bounds for individual containers. Both are how a shared cluster keeps one team from taking everything.',
                            resource: 'resourcequotas',
                            href: `${K8S}/concepts/policy/resource-quotas/`,
                        },
                    ],
                },
            ],
        },
        {
            id: 'access',
            label: 'Access and identity',
            icon: 'shield',
            lede: 'Who may do what, for people and for pods alike.',
            blocks: [
                {
                    type: 'terms',
                    terms: [
                        {
                            term: 'ServiceAccount',
                            meaning: 'An identity for pods, the way a user is an identity for a person. Every pod runs as one; a controller that needs to talk to the API gets its own with just the rights it needs.',
                            resource: 'serviceaccounts',
                            href: `${K8S}/concepts/security/service-accounts/`,
                        },
                        {
                            term: 'Role and ClusterRole',
                            meaning: 'A list of permissions: these verbs (get, list, create, delete) on these kinds. A Role applies in one namespace; a ClusterRole everywhere.',
                            resource: 'roles',
                            href: `${K8S}/reference/access-authn-authz/rbac/`,
                        },
                        {
                            term: 'RoleBinding and ClusterRoleBinding',
                            meaning: 'Gives a role to someone: a user, a group or a service account. Permission in Kubernetes is always these two halves, the role and the binding. `Forbidden` errors are a missing binding.',
                            resource: 'rolebindings',
                            href: `${K8S}/reference/access-authn-authz/rbac/#rolebinding-and-clusterrolebinding`,
                        },
                        {
                            term: 'CertificateSigningRequest',
                            meaning: 'A request for the cluster to sign a certificate: a kubelet asking for its serving certificate, or a new user being given a client one. It waits until something **approves** or **denies** it, which this app can do from the request’s panel.',
                            resource: 'certificatesigningrequests',
                            href: `${K8S}/reference/access-authn-authz/certificate-signing-requests/`,
                        },
                        {
                            term: 'PodCertificateRequest and ClusterTrustBundle',
                            meaning: 'A certificate asked for on a pod’s behalf, delivered into it as a projected volume, and the bundles of root certificates the cluster publishes for workloads to trust.',
                            resource: 'clustertrustbundles',
                            href: `${K8S}/reference/access-authn-authz/certificate-signing-requests/#cluster-trust-bundles`,
                        },
                    ],
                },
            ],
        },
        {
            id: 'scheduling',
            label: 'Scheduling and admission',
            icon: 'scale',
            lede: 'Where pods land, and what the API server checks before accepting an object.',
            blocks: [
                {
                    type: 'terms',
                    terms: [
                        {
                            term: 'Scheduling',
                            meaning: 'Choosing a node for a pod. The scheduler considers the pod’s requests against what each node has free, plus **node selectors** and **affinity** rules that say where a pod should or should not go.',
                            href: `${K8S}/concepts/scheduling-eviction/kube-scheduler/`,
                        },
                        {
                            term: 'Taint and toleration',
                            meaning: 'A taint on a node repels pods; a toleration on a pod lets it in anyway. Control-plane nodes are tainted so ordinary workloads stay off them, and a `NotReady` node is tainted automatically so pods are moved away.',
                            href: `${K8S}/concepts/scheduling-eviction/taint-and-toleration/`,
                        },
                        {
                            term: 'PriorityClass',
                            meaning: 'A number a pod carries. When the cluster is full, higher-priority pods are scheduled first and may evict lower ones.',
                            resource: 'priorityclasses',
                            href: `${K8S}/concepts/scheduling-eviction/pod-priority-preemption/`,
                        },
                        {
                            term: 'PodDisruptionBudget',
                            meaning: 'How many of a set of pods may be down at once during a voluntary disruption such as a drain. It is what makes a drain wait instead of taking a service out.',
                            resource: 'poddisruptionbudgets',
                            href: `${K8S}/concepts/workloads/pods/disruptions/`,
                        },
                        {
                            term: 'Workload and PodGroup',
                            meaning: 'Gang scheduling: a set of pods that is only worth running together — the workers of a training job — placed all at once or not at all. A **Workload** describes the groups; a **PodGroup** is one of them, running.',
                            resource: 'podgroups',
                            href: `${K8S}/concepts/workloads/workload-api/`,
                        },
                        {
                            term: 'EvictionRequest',
                            meaning: 'A request to move a pod that the pod’s own responders get to act on first — to hand off state, or finish what it was doing — before it is evicted. The **Eviction** beside it tracks who is working on it.',
                            resource: 'evictionrequests',
                            href: `${K8S}/concepts/scheduling-eviction/`,
                        },
                        {
                            term: 'Admission webhooks',
                            meaning: 'Hooks the API server calls before storing an object: a **mutating** one may change it, a **validating** one may refuse it. Policy tools and many operators install them. A webhook whose service is down blocks every create in its scope, which is a classic outage.',
                            resource: 'validatingwebhookconfigurations',
                            href: `${K8S}/reference/access-authn-authz/extensible-admission-controllers/`,
                        },
                    ],
                },
            ],
        },
        {
            id: 'devices',
            label: 'Devices and the API server',
            icon: 'chip',
            lede: 'GPUs and other hardware, and the API server’s own configuration.',
            blocks: [
                {
                    type: 'terms',
                    terms: [
                        {
                            term: 'Dynamic resource allocation',
                            meaning: 'How a pod asks for a device — a GPU, a network card — the way it asks for a volume. A driver publishes what each node has as **ResourceSlices**; a **DeviceClass** names a kind of device; a **ResourceClaim** asks for one, and is allocated a device when the pod is scheduled. A pod stuck Pending on a claim is explained there.',
                            resource: 'resourceclaims',
                            href: `${K8S}/concepts/scheduling-eviction/dynamic-resource-allocation/`,
                        },
                        {
                            term: 'DeviceTaintRule',
                            meaning: 'A taint on devices rather than nodes: a GPU that is failing can be kept from new pods, or have the pods using it evicted, without touching the rest of the machine.',
                            resource: 'devicetaintrules',
                            href: `${K8S}/concepts/scheduling-eviction/dynamic-resource-allocation/#device-taints-and-tolerations`,
                        },
                        {
                            term: 'APIService',
                            meaning: 'An API the API server passes on to something else — metrics-server is the common one. When one is **unavailable**, every client that lists the cluster’s APIs, `kubectl` included, reports discovery errors, and this list says which one and why.',
                            resource: 'apiservices',
                            href: `${K8S}/concepts/extend-kubernetes/api-extension/apiserver-aggregation/`,
                        },
                        {
                            term: 'API priority and fairness',
                            meaning: 'How the API server shares itself out under load. A **FlowSchema** sorts requests by who sent them; a **PriorityLevelConfiguration** says how much of the server each sort may use, and how long its queue may grow. A controller being throttled is explained here.',
                            resource: 'flowschemas',
                            href: `${K8S}/concepts/cluster-administration/flow-control/`,
                        },
                        {
                            term: 'Storage versions',
                            meaning: 'Which version each API server writes an object in, and **StorageVersionMigrations** that rewrite every stored object at the current one — what an upgrade needs before an old version can be removed.',
                            resource: 'storageversionmigrations',
                            href: `${K8S}/tasks/manage-kubernetes-objects/storage-version-migration/`,
                        },
                    ],
                },
                {
                    type: 'note',
                    text: 'Many of these are alpha or beta. A cluster that does not have one switched on says so in the kind’s tab rather than reporting an error.',
                },
            ],
        },
        {
            id: 'extending',
            label: 'Extending the cluster',
            icon: 'puzzle',
            lede: 'Custom resources, operators, Helm and GitOps: how everything beyond the built-ins gets there.',
            blocks: [
                {
                    type: 'terms',
                    terms: [
                        {
                            term: 'CustomResourceDefinition',
                            meaning: 'Teaches the API server a new kind. Install cert-manager and the cluster gains `Certificate` and `Issuer`; install Argo CD and it gains `Application`. The sidebar’s definitions section lists them by API group, and any of them opens as a table.',
                            resource: 'customresourcedefinitions',
                            href: `${K8S}/concepts/extend-kubernetes/api-extension/custom-resources/`,
                        },
                        {
                            term: 'Operator',
                            meaning: 'A controller plus its custom resources: a program running in the cluster that watches its kind and does the work. The Prometheus Operator turns a `Prometheus` object into a running server; a database operator turns a `PostgresCluster` into pods, volumes and backups.',
                            href: `${K8S}/concepts/extend-kubernetes/operator/`,
                        },
                        {
                            term: 'Helm',
                            meaning: 'The package manager. A **chart** is a templated bundle of objects; a **release** is one installation of a chart with chosen values, recorded in the cluster so it can be upgraded or rolled back. This app reads those records and can run `helm` for changes.',
                            resource: 'helmreleases',
                            href: 'https://helm.sh/docs/',
                        },
                        {
                            term: 'GitOps',
                            meaning: 'Keeping the cluster matching a Git repository. **Argo CD** and **Flux** watch a repo and apply what is there, and report whether the cluster is in sync and healthy. Both ship as plugins in this app.',
                            href: 'https://opengitops.dev/',
                        },
                        {
                            term: 'Prometheus',
                            meaning: 'The usual monitoring: it scrapes metrics from pods and nodes and answers queries about them over time. This app finds it and draws charts through the API server, and its plugin lists the Operator’s objects.',
                            href: 'https://prometheus.io/docs/introduction/overview/',
                        },
                    ],
                },
                {
                    type: 'actions',
                    actions: [
                        { kind: 'show', resource: 'customresourcedefinitions', label: 'Show the definitions in the selected cluster' },
                        { kind: 'page', page: 'help', label: 'How plugins work in this app' },
                    ],
                },
            ],
        },
        {
            id: 'glossary',
            label: 'Glossary',
            icon: 'search',
            lede: 'Words that come up everywhere, in one place.',
            blocks: [
                {
                    type: 'terms',
                    terms: [
                        { term: 'Annotation', meaning: 'Free-form metadata on an object, for tools rather than for selection: a checksum, a URL, a note. Compare label.', href: `${K8S}/concepts/overview/working-with-objects/annotations/` },
                        { term: 'Condition', meaning: 'A typed true/false in an object’s status with a reason: `Ready=True`, `Available=False`. The standard way a controller reports how things stand, and what most plugin cards count.' },
                        { term: 'Container', meaning: 'One running process from an image, with its own filesystem. A pod holds one or more.' },
                        { term: 'Controller', meaning: 'A loop that watches some objects and acts to make the cluster match them. Deployments, nodes, endpoints — nearly everything has one.', href: `${K8S}/concepts/architecture/controller/` },
                        { term: 'CrashLoopBackOff', meaning: 'A container keeps exiting and the kubelet is waiting longer and longer before starting it again. Read its logs, then its previous logs.' },
                        { term: 'Eviction', meaning: 'A pod being removed from a node: by a drain, by the node running out of memory or disk, or by a higher-priority pod needing the room.', href: `${K8S}/concepts/scheduling-eviction/` },
                        { term: 'Finalizer', meaning: 'A marker on an object that stops it being deleted until a controller has cleaned up after it. An object stuck in `Terminating` has a finalizer nobody is removing.', href: `${K8S}/concepts/overview/working-with-objects/finalizers/` },
                        { term: 'ImagePullBackOff', meaning: 'The image could not be fetched: wrong name, wrong tag, or no credentials for a private registry.' },
                        { term: 'Kind', meaning: 'The type of an object: Pod, Service, Deployment. The sidebar is organised by kind.' },
                        { term: 'kubectl', meaning: 'The official command-line client. Everything it can do, it does through the same API this app uses.', href: `${K8S}/reference/kubectl/` },
                        { term: 'Label', meaning: 'A key–value tag on an object, meant for selection: `app=web`, `tier=backend`. Services, Deployments and network policies all find their pods by labels.', href: `${K8S}/concepts/overview/working-with-objects/labels/` },
                        { term: 'Liveness and readiness', meaning: 'Two probes on a container. Liveness failing restarts it; readiness failing takes it out of its Service without restarting. A pod that is Running but not Ready is failing readiness.', href: `${K8S}/concepts/configuration/liveness-readiness-startup-probes/` },
                        { term: 'Manifest', meaning: 'An object written down, usually as YAML, ready to apply. The YAML tab in this app shows the live manifest.' },
                        { term: 'metrics-server', meaning: 'A small add-on that answers “how much CPU and memory is this pod using right now”. Autoscalers and the dashboard’s usage column depend on it.', href: 'https://github.com/kubernetes-sigs/metrics-server' },
                        { term: 'OOMKilled', meaning: 'The container used more memory than its limit and the kernel killed it. Raise the limit or fix the leak.' },
                        { term: 'Owner reference', meaning: 'The link from an object to what created it: a pod to its ReplicaSet, a ReplicaSet to its Deployment. Deleting the owner deletes the children.' },
                        { term: 'Reconcile', meaning: 'One turn of a controller’s loop: compare desired state to current state, do something about the difference.' },
                        { term: 'Rollout', meaning: 'A Deployment moving from one version of its pods to another, a few at a time.' },
                        { term: 'Selector', meaning: 'A query over labels: `app=web`, or `tier in (backend, worker)`. How one object points at a set of others.' },
                        { term: 'Sidecar', meaning: 'A second container in a pod that helps the main one: a proxy, a log shipper.' },
                        { term: 'Watch', meaning: 'An API request that stays open and streams changes as they happen. It is how this app’s tables update without polling.' },
                    ],
                },
            ],
        },
        {
            id: 'reading',
            label: 'Further reading',
            icon: 'book',
            lede: 'Where to go next, from the source.',
            blocks: [
                {
                    type: 'links',
                    links: [
                        { label: 'Kubernetes documentation', href: `${K8S}/home/`, note: 'the whole thing' },
                        { label: 'Concepts', href: `${K8S}/concepts/`, note: 'every object, explained in order' },
                        { label: 'Standardised glossary', href: `${K8S}/reference/glossary/?fundamental=true`, note: 'the official definitions' },
                        { label: 'kubectl cheat sheet', href: `${K8S}/reference/kubectl/quick-reference/`, note: 'the commands behind most of this app’s buttons' },
                        { label: 'Debugging applications', href: `${K8S}/tasks/debug/debug-application/`, note: 'what to look at when a pod will not run' },
                        { label: 'API reference', href: `${K8S}/reference/kubernetes-api/`, note: 'every field of every built-in kind' },
                        { label: 'Helm', href: 'https://helm.sh/docs/', note: 'charts, releases, values' },
                        { label: 'Argo CD', href: 'https://argo-cd.readthedocs.io/', note: 'GitOps, the Argo way' },
                        { label: 'Flux', href: 'https://fluxcd.io/flux/', note: 'GitOps, the Flux way' },
                        { label: 'Prometheus Operator', href: 'https://prometheus-operator.dev/', note: 'monitoring as Kubernetes objects' },
                        { label: 'Gateway API', href: 'https://gateway-api.sigs.k8s.io/', note: 'the successor to Ingress' },
                        { label: 'CNCF landscape', href: 'https://landscape.cncf.io/', note: 'everything else, on one very large map' },
                    ],
                },
            ],
        },
    ],
};
