# Network traffic and privacy

What K8s Dockside connects to, when, and what it sends. Each claim here points
at the code that does it, so you can check it rather than take it on trust.

## In short

- **No telemetry.** No analytics, no usage statistics, no crash reports, no
  account. The project runs no servers, so the app has nowhere to report to.
- **Your clusters are the main traffic.** The app talks to the Kubernetes API
  servers named in your kubeconfigs, with the credentials in those kubeconfigs.
- **One automatic request goes anywhere else:** a check with GitHub for a newer
  release. It carries no data of yours, and you can switch it off.
- **Everything else happens only when you ask for it:** installing a plugin,
  upgrading a Helm release, opening a link.

## Every connection the desktop app makes

| Goes to | When | What is sent | How to stop it |
|---|---|---|---|
| **Your Kubernetes API servers**, as named in your kubeconfigs | When you open a context or a view on it. At launch: a quick check that the selected context's cluster answers, and the tabs you left open last time. | Kubernetes API requests, authenticated with that context's own credentials, with `User-Agent: k8sdockside`. | Don't open the context, or *Disconnect* it (the power button on its row). *Settings → Behaviour → Restore tabs at launch*. |
| **Credential helpers in your kubeconfig** (`exec:` plugins such as `aws`, `gcloud`, `kubelogin`) | When a context that uses one connects | Whatever that tool sends to its own identity provider. The app runs it the same way `kubectl` would. | Your kubeconfig decides this. |
| **`api.github.com`**: the update check | 5 seconds after launch, then every 6 hours; or when you press *Check for updates* under *About* | One `GET` of the public "latest release" endpoint for this repository. No body, no cookies, no token. The only identifying header is `User-Agent: k8sdockside/<version> (+https://github.com/k8sdockside/k8sdockside)`. GitHub sees your IP address, as it would for any web request. | *Settings → Behaviour → Check for new versions*. It never runs in the web version. |
| **`github.com`**: plugins | Only when you install or update a plugin | `git clone` or `git pull` of that plugin's repository. The plugins offered in *Settings → Plugins* are a list built into the app, all on github.com. | Don't install plugins from repositories. |
| **Container registries** (Docker Hub, `ghcr.io`, `quay.io` and the like) | Only while a page of a plugin that declares `"registries": true` is open. Today that is only the optional **image-inventory** plugin. | Anonymous, read-only requests for the tags and digests of the public images your cluster runs. No credentials are sent; a private image just shows as needing authentication. | Don't install that plugin, or switch it off in *Settings → Plugins*. |
| **A Prometheus address you typed in** | Only if you set one in a cluster's settings. By default, charts reach Prometheus through the API server instead. | PromQL queries. No cluster credentials are sent to it. | Clear the address. |
| **Helm chart repositories** | Only when you upgrade a Helm release from the app | Your own `helm` binary fetches the chart from the repositories in *your* Helm configuration. | Don't upgrade from the app. |
| **Your web browser** | When you click a link, *Open release*, *Download*, or open a port forward | Your browser opens the address. The app itself sends nothing. | — |

That is the complete list.

### Traffic that stays between you and your cluster

- **Charts** go through the API server's service proxy to the Prometheus
  running inside the cluster.
- **Port forwards** listen on `localhost` only, and tunnel through the API
  server.
- **Logs, shells, search, edits and events** are all API server requests.
- **Node shells** create a debug pod. The *node* pulls that pod's image
  (`busybox` from Docker Hub by default; change it in *Settings → Terminal →
  Node shell image*). That download is made by your cluster, not by the app.
- **Plugin pages** cannot make network requests at all. They run in a
  sandboxed frame whose Content-Security-Policy includes `connect-src 'none'`,
  and they read the cluster only through the app: only the kinds they declare,
  and never Secrets. One limit remains: a page could navigate its own frame
  elsewhere and carry what it read with it. So install plugin pages only from
  people you would trust with read access to the kinds they declare.
- **Everything the window shows ships with the app**: fonts, icons, plugin
  logos and the start page pictures. The window loads nothing from the
  internet: no CDNs, no web fonts, no remote images.

## What never leaves your machine

- **Kubeconfigs and credentials.** They go only to the API server they belong
  to, through the standard Kubernetes client library. The desktop app never
  writes your kubeconfig and never stores credentials of its own.
- **Your settings, themes and plugins.** They live in
  `~/.config/k8sdockside/` (`%AppData%\k8sdockside\` on Windows).
- **What you do in the app.** No usage data, no error reports and no
  identifiers are sent anywhere.

## "Is it sending my data to the USA, or to China?"

The project runs no servers, so there is nowhere of the project's own for the
app to send anything. The only places it connects to are:

- **your clusters**, wherever you run them;
- **GitHub**, a US company owned by Microsoft, for the update check and for
  plugins. The update check sends nothing but the app's version. With it
  switched off, the app contacts GitHub only when you install or update a
  plugin;
- **anything else you asked for**: a Helm repository, the registry of an image
  you run, or a Prometheus address you entered.

The app does not contact any server in China, and it does not send data to
any other third party.

## The web version

The web version runs in your own cluster, behind a sign-in gateway, and differs
from the desktop app in these ways:

- **No update check.** Whoever deploys it upgrades it.
- **Sign-in.** Local accounts need no outside connection. If an administrator
  configures GitHub, Google, Facebook, GitLab, Microsoft or an OpenID Connect
  provider, the gateway talks to that provider during sign-in.
- **Your browser talks only to the gateway.** The app reaches clusters through
  the pod's ServiceAccount, kubeconfig Secrets you mount, or kubeconfigs an
  admin uploads.

See [server-mode.md](server-mode.md) for its security model.

## Check it yourself

- **Watch it.** Run the app behind an outbound firewall: Little Snitch or LuLu
  on macOS, OpenSnitch on Linux, or Windows Firewall. With update checks off and
  no plugin being installed, the only connections you will see are to your
  clusters' API servers, plus whatever your credential helpers call.
- **Block it.** Blocking `api.github.com` breaks nothing except the "new
  version" notice.
- **Read it.** Every outbound connection is made in one of these places:

| What | Where |
|---|---|
| Cluster connections | [`internal/kube/client.go`](../internal/kube/client.go) |
| Update check | [`internal/updates/updates.go`](../internal/updates/updates.go), [`internal/services/updateservice.go`](../internal/services/updateservice.go) |
| Plugin install and update | [`internal/plugins/git.go`](../internal/plugins/git.go) |
| Registry lookups | [`internal/registry/registry.go`](../internal/registry/registry.go) |
| A Prometheus address you entered | `directFetch` in [`internal/kube/prometheus.go`](../internal/kube/prometheus.go) |
| Helm | [`internal/helmcli/commands.go`](../internal/helmcli/commands.go) |
| The plugin page sandbox | `contentPolicy` in [`internal/plugins/ui.go`](../internal/plugins/ui.go) |
| Web version sign-in | [`internal/gateway/oauth.go`](../internal/gateway/oauth.go) |

The frontend makes no network requests of its own. It has no `fetch`, XHR or
WebSocket calls, and it reaches the backend only through the app's built-in
bridge.

Found something that contradicts this page? Report it privately as described
in [SECURITY.md](../SECURITY.md).
