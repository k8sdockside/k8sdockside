# Server mode

K8s Dockside is a desktop app first. Server mode is the same app — the same Go
backend, the same Svelte frontend, every view and plugin — built with
`-tags server`, so that instead of opening a native window it serves the UI over
HTTP behind a sign-in of its own. That makes it something you can run inside a
cluster and open from a browser: for a team, or for yourself from any machine.

One promise of the desktop app does not carry over. There, the credentials are
yours and never leave your machine. In server mode they are the pod's, and
**everyone who signs in uses them**. Read [Security](#security) before putting
it on an address other people can reach.

- [How it fits together](#how-it-fits-together)
- [Quickstart with kind](#quickstart-with-kind)
- [Installing a release](#installing-a-release)
- [Signing in](#signing-in)
- [OAuth providers](#oauth-providers)
- [Clusters](#clusters)
- [Persistence](#persistence)
- [Exposing it](#exposing-it)
- [Configuration reference](#configuration-reference)
- [Security](#security)
- [Differences from the desktop app](#differences-from-the-desktop-app)
- [Building the image](#building-the-image)

## How it fits together

One pod, one container, one process:

```
browser ──▶ Ingress / Gateway ──▶ Service :80 ──▶ pod :8080
                                                  ┌─────────────────────────────┐
                                                  │ auth gateway                │
                                                  │   sign-in, sessions, OAuth, │
                                                  │   admin pages under /-/     │
                                                  │         │ loopback only     │
                                                  │         ▼                   │
                                                  │ the app (Wails HTTP server) │
                                                  └─────────────────────────────┘
```

- **The gateway is the only thing on the pod's address.** It serves the sign-in
  and first-run pages, runs the OAuth flows and the admin pages under `/-/`,
  and passes every other request — once it belongs to a signed-in user — to the
  app, which listens on a loopback port nothing outside the pod can reach.
- **Users are kept apart.** Terminals, log streams and watches belong to the
  user who opened them; one user's session never sees another's.
- **Clusters come from three places:** the pod's own ServiceAccount (the
  cluster it runs in), kubeconfig files mounted from Secrets, and kubeconfigs an
  admin uploads at `/-/admin`. See [Clusters](#clusters).
- **All state is in one directory**, `/data`: users and sessions, providers
  added in the admin UI, uploaded kubeconfigs, app settings, installed plugins
  and themes, and Helm's cache. See [Persistence](#persistence).
- **It is always one replica.** Open terminals and watches live in the
  process's memory, so the chart runs a single pod and replaces it with the
  `Recreate` strategy rather than rolling.

| Path | What |
|---|---|
| `/-/healthz` | Liveness: 200 whenever the process is serving |
| `/-/readyz` | Readiness: 200 once the app behind the gateway answers |
| `/-/admin` | Users, sign-in providers, clusters and plugins — admins only |
| `/-/oauth/<id>/callback` | Where an OAuth provider sends the user back to |
| everything else | The app, or the sign-in page |

## Quickstart with kind

Everything from a checkout, into a local [kind](https://kind.sigs.k8s.io/)
cluster. Building the image needs what `make build` does — the `wails3` CLI
(`make install-wails`) and the frontend's dependencies (`cd frontend && npm
install`) — because the bindings and the bundle are built on your machine and
only copied into the image; see [Building the image](#building-the-image).

```sh
kind create cluster
make docker-build kind-load helm-install
kubectl -n k8sdockside port-forward svc/k8sdockside 8080:80
```

Open <http://127.0.0.1:8080> and create the admin account. The kind cluster is
already there, as the context `in-cluster`, read-only. To let the app change
things in it as well:

```sh
make helm-install HELM_ARGS='--set rbac.mode=admin'
```

After changing code, rebuild and reload, then restart the pod — the image tag
(`dev`) has not changed, so Helm sees nothing to roll:

```sh
make docker-build kind-load
kubectl -n k8sdockside rollout restart deployment/k8sdockside
```

`make run-server` runs the same server without Kubernetes at all, on
<http://127.0.0.1:8080>, with its state in `bin/server-data` and the
kubeconfigs in your `~/.kube` as its clusters.

## Installing a release

Every release tag publishes the image, for `linux/amd64` and `linux/arm64`,
and the chart beside it:

```sh
helm install k8sdockside oci://ghcr.io/rogerwesterbo/helm/k8sdockside \
  --namespace k8sdockside --create-namespace \
  --version <version>
```

The chart's version and its default image tag are both the release version. Its
options are summarised in [charts/k8sdockside/README.md](../charts/k8sdockside/README.md)
and commented in full in its [values.yaml](../charts/k8sdockside/values.yaml).

## Signing in

**The first user becomes the admin.** On first start, with no users yet, the
first visitor is asked to create the admin account. On an address anyone can
reach, do not leave that open. Either:

- set a **setup token** (`auth.setupToken`, or `auth.existingSetupTokenSecret`):
  the first-run page then asks for it, so only someone who can read the Secret
  can claim admin; or
- create the admin **at startup** (`auth.bootstrapAdmin`, or its
  `existingSecret`), and the first-run page never appears. This only happens
  while no users exist, so changing the values later does not reset anyone's
  password.

**Admins and users.** Admins manage users, sign-in providers, clusters and
plugins at `/-/admin`; everyone else uses the app. Both work with the same
cluster credentials — the role decides who can change the server, not what can
be done to the clusters.

Local users sign in with a username and password; `auth.passwordLogin: false`
turns that off and leaves OAuth only. `auth.adminEmails` makes anyone who
signs in through OAuth with one of those verified addresses an admin — the
declarative way to hand out admin on an OAuth-only install.

A sign-in lasts `auth.sessionTTL`, a week by default.

## OAuth providers

Each provider has an `id`, and sends users back to

```
<publicURL>/-/oauth/<id>/callback
```

which is the redirect (or callback) URL to register with it. `<publicURL>` is
`auth.publicURL` — the address users type into their browser, such as
`https://dockside.example.com`. Set it whenever OAuth is in use; the chart
derives it from the first ingress host if you do not.

Providers come from two places:

- **The admin UI** at `/-/admin`. These are stored in `/data`, so on an
  ephemeral install they go with the pod.
- **The chart**, `auth.providers`. These are written to a Secret, mounted as a
  file (`K8SDOCKSIDE_AUTH_PROVIDERS_FILE`), and shown read-only in the admin UI.
  Or point `auth.existingProvidersSecret` at a Secret of your own holding the
  same JSON array.

Keep client secrets out of values by naming an environment variable in
`clientSecretEnv` and filling it from a Secret:

```yaml
auth:
  publicURL: https://dockside.example.com
  providers:
    - id: github
      type: github
      name: GitHub
      clientId: Ov23li...
      clientSecretEnv: GITHUB_CLIENT_SECRET
      autoSignup: true
      allowedDomains: [example.com]

# kubectl -n k8sdockside create secret generic dockside-oauth \
#   --from-literal=github-client-secret=...
extraEnv:
  - name: GITHUB_CLIENT_SECRET
    valueFrom:
      secretKeyRef:
        name: dockside-oauth
        key: github-client-secret
```

| Field | |
|---|---|
| `id` | Slug; part of the callback URL. Required |
| `type` | `github`, `google`, `facebook`, `gitlab`, `microsoft` or `oidc`. Required |
| `name` | Label on the sign-in button |
| `clientId` | Required |
| `clientSecret` | The secret itself… |
| `clientSecretEnv` | …or the name of an environment variable holding it |
| `issuer` | `oidc`: the issuer URL. `gitlab`: the instance's base URL (default `https://gitlab.com`) |
| `tenant` | `microsoft`: a tenant ID, or `common` (default), `organizations` or `consumers` |
| `scopes` | Replace the default scopes |
| `allowedDomains` | Only admit email addresses in these domains |
| `autoSignup` | Create the user on first sign-in. Without it, an admin creates users first |
| `defaultRole` | `user` (default) or `admin`, for users created by `autoSignup` |
| `enabled` | Default `true` |

### Where to create the OAuth app

In every case the redirect URL is `<publicURL>/-/oauth/<id>/callback`, with the
`id` you give the provider.

**GitHub** — *Settings → Developer settings → OAuth Apps → New OAuth App*
(<https://github.com/settings/developers>), or the same page under an
organisation's settings. *Homepage URL* is `<publicURL>`; *Authorization
callback URL* is `<publicURL>/-/oauth/github/callback`. Generate the client
secret on the app's page afterwards.

**Google** — Google Cloud console, *APIs & Services → Credentials → Create
credentials → OAuth client ID*, application type *Web application*
(<https://console.cloud.google.com/apis/credentials>). Add the callback under
*Authorized redirect URIs*. The project needs an OAuth consent screen first; an
*Internal* one limits sign-in to your Google Workspace.

**Facebook** — <https://developers.facebook.com/apps>: create an app, add the
*Facebook Login* product, and enter the callback under *Facebook Login →
Settings → Valid OAuth Redirect URIs*. The *App ID* is the `clientId`, the *App
Secret* the secret. Facebook only redirects to HTTPS addresses.

**GitLab** — *User settings → Applications* on gitlab.com
(<https://gitlab.com/-/user_settings/applications>), or the *Applications* page
of a group or a self-managed instance's admin area. Enter the callback as the
redirect URI and tick `read_user`, `openid`, `email` and `profile`. For a
self-managed GitLab, set `issuer: https://gitlab.example.com`.

**Microsoft** — *Microsoft Entra ID → App registrations → New registration*
(<https://entra.microsoft.com>). Platform *Web*, redirect URI the callback. The
*Application (client) ID* is the `clientId`; create the secret under
*Certificates & secrets*. Set `tenant` to your directory (tenant) ID to admit
only your organisation, `organizations` for any work or school account, or
leave it at `common` to allow personal Microsoft accounts too.

**OIDC** — any OpenID Connect provider: Keycloak, Dex, Authentik, Okta, Auth0,
Zitadel, or a cloud identity service through its OIDC endpoint. `issuer` is the
URL whose `/.well-known/openid-configuration` describes the provider — for
Keycloak, `https://sso.example.com/realms/<realm>`. Create a confidential client
and register the callback as a valid redirect URI.

## Clusters

### The cluster it runs in

With `clusters.inCluster.enabled` (the default) the pod's ServiceAccount is
offered as a context, named `in-cluster` unless `clusters.inCluster.name` says
otherwise. What it may do there is `rbac.mode`:

| `rbac.mode` | Grants |
|---|---|
| `readonly` (default) | `get`, `list` and `watch` on every resource in every API group, and `GET` on non-resource URLs. Browse, watch and follow logs; no edits, scaling, deletes or shells. **Includes reading Secrets** |
| `admin` | The built-in `cluster-admin`: everything the app can do |
| `custom` | A ClusterRole made of `rbac.rules` |
| `none` | Nothing from the chart; bind your own |

`rbac.extraClusterRoles` binds existing ClusterRoles — the built-in `view` or
`edit`, say — on top. Turning in-cluster access off also stops the
ServiceAccount token being mounted, so the pod holds no credentials for its own
cluster at all.

A caveat on `readonly`: it is a wildcard, so it covers subresources as well —
`pods/exec`, `pods/attach`, `nodes/proxy`. API servers before Kubernetes 1.35,
and kubelets reached through `nodes/proxy`, may allow running a command over a
WebSocket with only `get` on those. If that matters, use `custom` and list the
resources you mean.

### Kubeconfigs from Secrets

```sh
kubectl -n k8sdockside create secret generic prod-kubeconfig \
  --from-file=prod.yaml=./prod.kubeconfig
```

```yaml
clusters:
  existingSecrets:
    - name: prod-kubeconfig
    - name: team-clusters          # pick and rename keys with items
      items:
        - key: eu-west
          path: eu-west.yaml
  kubeconfigs:                     # or inline; the chart writes the Secret
    - name: staging
      kubeconfig: |
        apiVersion: v1
        kind: Config
        # ...
```

All of them are mounted as one read-only directory,
`/etc/k8sdockside/kubeconfigs`, which is named in
`K8SDOCKSIDE_KUBECONFIG_DIRS`. Every file in it is read as a kubeconfig — dot
files are skipped, so a Secret volume's own bookkeeping is not — and every
context in them appears in the app. They cannot be removed from the UI: change
the Secret instead. The chart restarts the pod when its own inline kubeconfigs
change. For a Secret it only references, the kubelet updates the mounted files
within about a minute, and the app rescans the directory on every sync, so
contexts that were added or removed show up the next time the sidebar is
refreshed. Changed credentials for a context that is already open are only
picked up by a new connection, so restart the pod after rotating them.
Key names must be unique across all the Secrets, since they share a directory.

The standard `KUBECONFIG` variable works too, through `extraEnv`.

**No credential plugins.** The image contains git and helm and nothing else,
so a kubeconfig whose user runs an `exec` plugin — `aws eks get-token`,
`gke-gcloud-auth-plugin`, `kubelogin` — cannot authenticate. Use a token or a
client certificate instead: typically a ServiceAccount in the target cluster,
bound to what it should be allowed, and a kubeconfig carrying its token. Or
build an image `FROM` this one with the plugin added.

### Uploaded in the admin UI

Admins can upload kubeconfigs at `/-/admin`. They are stored under
`/data/kubeconfigs`, so they last only as long as the data volume does.

## Persistence

**Ephemeral** (the default, `persistence.enabled: false`). `/data` is an
`emptyDir`, and everything in it is gone whenever the pod restarts or moves:
users, sessions (everyone signs in again), providers and kubeconfigs added in
the admin UI, settings and installed plugins.

That is fine — and simpler — when nothing needs to live there. An install where
the admin comes from `auth.bootstrapAdmin`, sign-in from `auth.providers` with
`autoSignup` (and `auth.adminEmails` for admins), and clusters from Secrets
comes back the same after every restart; users are simply created again the
next time they sign in.

**Persistent** (`persistence.enabled: true`). A PersistentVolumeClaim —
`ReadWriteOnce`, `persistence.size` (1Gi by default), in
`persistence.storageClass` — or one you made yourself, via
`persistence.existingClaim`. With `persistence.keep: true`, `helm uninstall`
leaves the claim behind, and reinstalling the same release into the same
namespace picks it up again.

## Exposing it

The app streams watches, logs and terminals over WebSockets. Most ingress
controllers pass them through by default, but many close a connection that has
been quiet for a minute — an idle terminal, or the log of a pod with nothing to
say. For ingress-nginx:

```yaml
ingress:
  enabled: true
  className: nginx
  annotations:
    nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"
    nginx.ingress.kubernetes.io/proxy-send-timeout: "3600"
    cert-manager.io/cluster-issuer: letsencrypt
  hosts:
    - host: dockside.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: dockside-tls
      hosts:
        - dockside.example.com

auth:
  publicURL: https://dockside.example.com
  trustForwardedHeaders: true
```

`trustForwardedHeaders` makes the server believe the `X-Forwarded-Proto`,
`X-Forwarded-Host` and `X-Forwarded-For` the ingress sets. Only turn it on
behind a proxy that sets them: reached directly, a client could claim any
address it liked.

With the Gateway API instead:

```yaml
httpRoute:
  enabled: true
  parentRefs:
    - name: public
      namespace: gateway-system
      sectionName: https
  hostnames:
    - dockside.example.com
```

## Configuration reference

The chart sets all of these. They are the whole of the server's interface, for
running it some other way.

| Variable | Default | |
|---|---|---|
| `K8SDOCKSIDE_LISTEN_ADDR` | `:8080` | Address to listen on |
| `K8SDOCKSIDE_DATA_DIR` | `/data` | Where all state is kept. `XDG_CONFIG_HOME` and Helm's cache, config and data directories are pointed inside it unless already set |
| `K8SDOCKSIDE_PUBLIC_URL` | | External URL; used for OAuth callback URLs and Secure cookies |
| `K8SDOCKSIDE_TRUST_FORWARDED` | `false` | Honour `X-Forwarded-Proto`/`-Host`/`-For` |
| `K8SDOCKSIDE_IN_CLUSTER` | `auto` | Offer the pod's ServiceAccount as a context: `auto` (when a token is mounted), `true` or `false` |
| `K8SDOCKSIDE_IN_CLUSTER_NAME` | `in-cluster` | That context's name |
| `K8SDOCKSIDE_KUBECONFIG_DIRS` | | Comma-separated directories of read-only kubeconfig files. Each regular file or symlink directly inside is read; dot files are skipped |
| `KUBECONFIG` | | Also read, as usual |
| `K8SDOCKSIDE_SESSION_TTL` | `168h` | How long a sign-in lasts (Go duration) |
| `K8SDOCKSIDE_PASSWORD_LOGIN` | `true` | Allow username and password sign-in |
| `K8SDOCKSIDE_SETUP_TOKEN` | | Required on the first-run page, when set |
| `K8SDOCKSIDE_ADMIN_USERNAME`, `K8SDOCKSIDE_ADMIN_PASSWORD` | | Create this admin at startup while there are no users |
| `K8SDOCKSIDE_ADMIN_EMAILS` | | Comma-separated; OAuth users with one of these verified addresses become admins |
| `K8SDOCKSIDE_AUTH_PROVIDERS_FILE` | | JSON array of [OAuth providers](#oauth-providers), shown read-only in the admin UI |

## Security

- **Everyone who signs in acts as the pod.** Kubernetes sees the ServiceAccount,
  or the user in the kubeconfig — not the person — and that is what its audit
  log records. There is no per-user RBAC. Give the pod only what you are happy
  for every user to do, and control who can sign in: `allowedDomains`,
  `autoSignup` off with users created by an admin, `passwordLogin`.
- **Secrets are readable if the credentials can read them.** Tables redact them,
  as in the desktop app, but the editor reads objects live — so anyone who can
  sign in can open a Secret the pod may read. `readonly` RBAC includes Secrets.
- **The admin role is the server's, not the cluster's.** Admins manage users,
  sign-in providers, clusters and plugins; a plugin an admin installs is there
  for every user.
- **Claim admin before anyone else can.** On a public address, set a setup token
  or a bootstrap admin (see [Signing in](#signing-in)).
- **Terminate TLS in front of it** and set `auth.publicURL` to the `https://`
  address, so session cookies are marked Secure.
- **Trust forwarded headers only behind a proxy** that sets them.
- **The container is locked down**: it runs as uid 65532, with a read-only root
  filesystem, no capabilities, no privilege escalation and the runtime's default
  seccomp profile. `/data` and `/tmp` are the only writable paths.

## Differences from the desktop app

- **Port forwarding is off.** A forward would open a port on the pod, not on the
  user's machine.
- **No external terminal.** Shells open in the dock in the browser; there is no
  terminal emulator on the server to hand them to.
- **Clusters come from the pod**, as described in [Clusters](#clusters), rather
  than from discovering `~/.kube` and watched folders on your machine.

## Building the image

```sh
make build-server      # the binary alone, into bin/k8sdockside-server
make docker-build      # the image, as IMG (default ghcr.io/rogerwesterbo/k8sdockside:dev)
make docker-buildx     # linux/amd64 + linux/arm64, and PUSHES to IMG
make kind-load         # load IMG into the kind cluster KIND_CLUSTER (default kind)
make helm-install      # install or upgrade the chart into HELM_NAMESPACE, running IMG
make helm-lint helm-template
```

[`build/docker/Dockerfile.server`](../build/docker/Dockerfile.server) compiles
the server with `-tags server,production` and `CGO_ENABLED=0`, and adds git,
helm and CA certificates to an Alpine base. It does **not** build the frontend:
the bindings come from the `wails3` CLI, which links against GTK on Linux, so
`make docker-build` generates them and builds `frontend/dist` on the host first,
and the Dockerfile copies the result in. The build stops early with a clear
message if `frontend/dist/index.html` is missing.

Go and Helm are built or fetched on the build platform for the target, so a
multi-architecture build only emulates the final `apk add`.

Release images and the chart are published by
[`server-image.yml`](../.github/workflows/server-image.yml) on every `v*` tag.
