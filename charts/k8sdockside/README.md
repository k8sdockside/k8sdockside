# k8sdockside Helm chart

Runs [K8s Dockside](https://github.com/k8sdockside/k8sdockside) in server
mode: the Kubernetes desktop app as a web app inside a cluster, behind a sign-in
of its own — local users (the first becomes the admin) and GitHub, Google,
Facebook, GitLab, Microsoft or any OIDC provider.

The full guide — how it fits together, setting up each OAuth provider, the
security model — is
[docs/server-mode.md](https://github.com/k8sdockside/k8sdockside/blob/main/docs/server-mode.md).

> **Every signed-in user acts with the pod's cluster credentials** — its
> ServiceAccount and every mounted or uploaded kubeconfig — not their own. The
> chart defaults to read-only RBAC, which still includes reading Secrets.

## Install

### Quickstart: port-forward

```sh
helm install k8sdockside oci://ghcr.io/k8sdockside/helm/k8sdockside \
  --namespace k8sdockside --create-namespace
kubectl -n k8sdockside port-forward svc/k8sdockside 8080:80
```

Open <http://127.0.0.1:8080> and create the admin account. The cluster it runs
in is there as `in-cluster`, read-only. Nothing is persisted: see
[Keeping state](#keeping-state).

From a checkout, into kind, with an image built from your working tree:

```sh
kind create cluster
make docker-build kind-load helm-install
```

### Behind an ingress, with GitHub sign-in

Create an OAuth app at <https://github.com/settings/developers> with the
callback URL `https://dockside.example.com/-/oauth/github/callback`, then:

```sh
kubectl -n k8sdockside create secret generic dockside-oauth \
  --from-literal=github-client-secret=<client secret>
kubectl -n k8sdockside create secret generic dockside-setup \
  --from-literal=setup-token="$(openssl rand -hex 24)"
```

```yaml
# values.yaml
ingress:
  enabled: true
  className: nginx
  annotations:
    # WebSockets: keep idle terminals and log streams open.
    nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"
    nginx.ingress.kubernetes.io/proxy-send-timeout: "3600"
  hosts:
    - host: dockside.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: dockside-tls
      hosts: [dockside.example.com]

auth:
  publicURL: https://dockside.example.com
  trustForwardedHeaders: true
  existingSetupTokenSecret:
    name: dockside-setup
    key: setup-token
  adminEmails: [you@example.com]
  providers:
    - id: github
      type: github
      name: GitHub
      clientId: <client id>
      clientSecretEnv: GITHUB_CLIENT_SECRET
      autoSignup: true
      allowedDomains: [example.com]

extraEnv:
  - name: GITHUB_CLIENT_SECRET
    valueFrom:
      secretKeyRef:
        name: dockside-oauth
        key: github-client-secret
```

```sh
helm upgrade --install k8sdockside oci://ghcr.io/k8sdockside/helm/k8sdockside \
  --namespace k8sdockside -f values.yaml
```

### More clusters, from kubeconfig Secrets

```sh
kubectl -n k8sdockside create secret generic prod-kubeconfig \
  --from-file=prod.yaml=./prod.kubeconfig
```

```yaml
clusters:
  existingSecrets:
    - name: prod-kubeconfig
  # Turn off the cluster it runs in, if it should only manage the others. This
  # also stops the ServiceAccount token being mounted.
  inCluster:
    enabled: false
```

Every Secret's keys are mounted together at `/etc/k8sdockside/kubeconfigs`;
keep them unique, or rename with `items`. The image has no cloud CLIs, so use
kubeconfigs with a token or client certificate rather than an `exec` plugin.

### Keeping state

By default `/data` is an `emptyDir`: users, sessions, providers and kubeconfigs
added in the admin UI, and installed plugins go with the pod. Either keep it:

```yaml
persistence:
  enabled: true
  size: 1Gi
  keep: true   # survive `helm uninstall`
```

or make the install fully declarative — `auth.bootstrapAdmin`, `auth.providers`
with `autoSignup`, `auth.adminEmails`, and `clusters.*` — so a fresh pod comes
back the same.

## Values

The most used keys. [values.yaml](values.yaml) documents every one.

| Key | Default | Description |
|---|---|---|
| `image.repository` | `ghcr.io/k8sdockside/k8sdockside` | Image |
| `image.tag` | appVersion | Image tag |
| `image.digest` | `""` | Pin by digest; wins over the tag |
| `service.type` / `service.port` | `ClusterIP` / `80` | The container listens on 8080 |
| `ingress.enabled` | `false` | Ingress (`className`, `annotations`, `hosts`, `tls`) |
| `httpRoute.enabled` | `false` | Gateway API HTTPRoute (`parentRefs`, `hostnames`) |
| `persistence.enabled` | `false` | PVC for `/data`; otherwise an `emptyDir` |
| `persistence.existingClaim` | `""` | Use this PVC instead of creating one |
| `persistence.size` | `1Gi` | |
| `persistence.storageClass` | `""` | `-` for `storageClassName: ""` |
| `persistence.keep` | `false` | Keep the PVC on `helm uninstall` |
| `clusters.inCluster.enabled` | `true` | Offer the cluster it runs in, via its ServiceAccount |
| `clusters.inCluster.name` | `in-cluster` | That context's name |
| `clusters.kubeconfigs` | `[]` | Inline kubeconfigs: `{name, kubeconfig}` |
| `clusters.existingSecrets` | `[]` | Secrets of kubeconfigs: `{name, items?, optional?}` or a name |
| `rbac.create` | `true` | RBAC for the ServiceAccount (with in-cluster access only) |
| `rbac.mode` | `readonly` | `readonly`, `admin` (cluster-admin), `custom` (`rbac.rules`) or `none` |
| `rbac.extraClusterRoles` | `[]` | Existing ClusterRoles to bind as well |
| `auth.publicURL` | `""` | External URL; derived from the first ingress host when empty |
| `auth.trustForwardedHeaders` | `false` | Honour X-Forwarded-* (behind an ingress) |
| `auth.sessionTTL` | `168h` | |
| `auth.passwordLogin` | `true` | Allow username + password sign-in |
| `auth.setupToken` / `auth.existingSetupTokenSecret` | | Token the first-run admin page asks for |
| `auth.bootstrapAdmin` | | Admin created at startup: `username`/`password` or `existingSecret` |
| `auth.adminEmails` | `[]` | OAuth users with these verified emails become admins |
| `auth.providers` | `[]` | OAuth providers; callback `<publicURL>/-/oauth/<id>/callback` |
| `auth.existingProvidersSecret` | | A providers JSON file from your own Secret |
| `updateCheck` | `true` | Let users press **Check now** on the bell to see whether a newer release exists. The server never checks on its own and never updates itself; `false` keeps it off the internet entirely |
| `resources` | 100m / 256Mi, limit 1Gi | |
| `extraEnv` / `extraEnvFrom` | `[]` | e.g. the variables named by `clientSecretEnv` |
| `extraVolumes` / `extraVolumeMounts` | `[]` | |
| `nodeSelector` / `tolerations` / `affinity` | | |

## Notes

- **One replica, always**, with `strategy: Recreate`: terminals, log streams
  and watches live in the server's memory.
- The pod restarts when anything the chart writes into a Secret changes
  (inline kubeconfigs, providers, bootstrap admin, setup token). Secrets it only
  references are not watched. A referenced kubeconfig Secret's new or removed
  contexts appear on the app's next sync without a restart. Restart the
  Deployment after rotating credentials for a context already in use, or after
  changing a referenced providers Secret: providers are read at startup.
- The container runs as uid 65532 with a read-only root filesystem, no
  capabilities and the `RuntimeDefault` seccomp profile.
