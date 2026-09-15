package gateway

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// Config is how the web version is set up. It is read from the environment,
// which is how a container is told anything -- see ConfigFromEnv for the names.
type Config struct {
	// ListenAddr is where the gateway listens for browsers.
	ListenAddr string
	// DataDir holds everything the web version writes: users and sessions,
	// uploaded kubeconfigs, the app's own settings, helm's cache. On a
	// persistent volume it survives a restart; on an emptyDir it does not.
	DataDir string
	// PublicURL is the address people reach the app at, used to build the
	// callback addresses sign-in providers send people back to. Nil to work it
	// out from each request.
	PublicURL *url.URL
	// TrustForwarded honours X-Forwarded-Proto, -Host and -For, for a
	// deployment behind an ingress that sets them.
	TrustForwarded bool
	// InCluster is "auto", "true" or "false": whether the pod's own service
	// account is offered as a cluster.
	InCluster string
	// InClusterName is the context name that cluster is listed under.
	InClusterName string
	// KubeconfigDirs are read-only folders of kubeconfigs mounted into the pod,
	// typically from Secrets.
	KubeconfigDirs []string
	// SessionTTL is how long a sign-in lasts.
	SessionTTL time.Duration
	// PasswordLogin allows signing in with a username and password.
	PasswordLogin bool
	// SetupToken, when set, is required to create the first administrator, so
	// that whoever finds a fresh deployment first does not get to own it.
	SetupToken string
	// AdminUsername and AdminPassword create the first administrator at
	// startup, when both are set and nobody has been created yet.
	AdminUsername string
	AdminPassword string
	// AdminEmails become administrators on their first sign-in through a
	// provider, when the provider vouches for the address.
	AdminEmails []string
	// ProvidersFile is a JSON list of sign-in providers managed by the
	// deployment rather than through the admin page.
	ProvidersFile string
}

// ConfigFromEnv reads the configuration from K8SDOCKSIDE_* variables. Every
// problem found is reported, not only the first.
func ConfigFromEnv() (Config, error) {
	c := Config{
		ListenAddr:     envOr("K8SDOCKSIDE_LISTEN_ADDR", ":8080"),
		DataDir:        envOr("K8SDOCKSIDE_DATA_DIR", "/data"),
		InCluster:      strings.ToLower(envOr("K8SDOCKSIDE_IN_CLUSTER", "auto")),
		InClusterName:  envOr("K8SDOCKSIDE_IN_CLUSTER_NAME", "in-cluster"),
		KubeconfigDirs: envList("K8SDOCKSIDE_KUBECONFIG_DIRS"),
		SetupToken:     os.Getenv("K8SDOCKSIDE_SETUP_TOKEN"),
		AdminUsername:  strings.TrimSpace(os.Getenv("K8SDOCKSIDE_ADMIN_USERNAME")),
		AdminPassword:  os.Getenv("K8SDOCKSIDE_ADMIN_PASSWORD"),
		AdminEmails:    envList("K8SDOCKSIDE_ADMIN_EMAILS"),
		ProvidersFile:  strings.TrimSpace(os.Getenv("K8SDOCKSIDE_AUTH_PROVIDERS_FILE")),
	}

	var problems []error
	var err error
	if c.TrustForwarded, err = envBool("K8SDOCKSIDE_TRUST_FORWARDED", false); err != nil {
		problems = append(problems, err)
	}
	if c.PasswordLogin, err = envBool("K8SDOCKSIDE_PASSWORD_LOGIN", true); err != nil {
		problems = append(problems, err)
	}

	ttl := envOr("K8SDOCKSIDE_SESSION_TTL", "168h")
	if c.SessionTTL, err = time.ParseDuration(ttl); err != nil || c.SessionTTL < time.Minute {
		problems = append(problems, fmt.Errorf("K8SDOCKSIDE_SESSION_TTL: %q is not a duration of a minute or more", ttl))
	}

	if raw := strings.TrimSpace(os.Getenv("K8SDOCKSIDE_PUBLIC_URL")); raw != "" {
		u, err := url.Parse(strings.TrimRight(raw, "/"))
		switch {
		case err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https"):
			problems = append(problems, fmt.Errorf("K8SDOCKSIDE_PUBLIC_URL: %q is not an http or https address", raw))
		case u.Path != "" || u.RawQuery != "":
			// The app's own paths are absolute, so it cannot live under a
			// prefix.
			problems = append(problems, fmt.Errorf("K8SDOCKSIDE_PUBLIC_URL: %q has a path; K8s Dockside must be served at the root of its host", raw))
		default:
			c.PublicURL = u
		}
	}

	switch c.InCluster {
	case "auto", "true", "false":
	default:
		problems = append(problems, fmt.Errorf("K8SDOCKSIDE_IN_CLUSTER: %q is not auto, true or false", c.InCluster))
	}
	if strings.Contains(c.InClusterName, "::") {
		problems = append(problems, errors.New("K8SDOCKSIDE_IN_CLUSTER_NAME must not contain \"::\""))
	}
	if !filepath.IsAbs(c.DataDir) {
		problems = append(problems, fmt.Errorf("K8SDOCKSIDE_DATA_DIR: %q is not an absolute path", c.DataDir))
	}
	if (c.AdminUsername == "") != (c.AdminPassword == "") {
		problems = append(problems, errors.New("K8SDOCKSIDE_ADMIN_USERNAME and K8SDOCKSIDE_ADMIN_PASSWORD are set together or not at all"))
	}
	if c.AdminPassword != "" {
		if err := validatePassword(c.AdminPassword); err != nil {
			problems = append(problems, fmt.Errorf("K8SDOCKSIDE_ADMIN_PASSWORD: %w", err))
		}
	}
	for i, email := range c.AdminEmails {
		c.AdminEmails[i] = strings.ToLower(email)
	}
	return c, errors.Join(problems...)
}

// UploadDir is where kubeconfigs added through the admin page are kept.
func (c Config) UploadDir() string { return filepath.Join(c.DataDir, "kubeconfigs") }

// generatedDir holds the kubeconfig describing the cluster the pod runs in.
func (c Config) generatedDir() string { return filepath.Join(c.DataDir, "generated") }

// authFile holds users, sessions and the providers added through the admin
// page.
func (c Config) authFile() string { return filepath.Join(c.DataDir, "auth.json") }

// Prepare makes the data directory ready, points the app's settings, helm and
// git at it, and writes the in-cluster kubeconfig when there is a cluster to
// describe. It returns the folders the app should read kubeconfigs from.
//
// It changes the process environment, so it runs once, before anything reads
// it: before the settings store is opened, and before Wails starts.
func (c Config) Prepare() ([]string, error) {
	for _, dir := range []string{c.DataDir, c.UploadDir(), c.generatedDir()} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}

	defaults := []struct{ name, value string }{
		{"XDG_CONFIG_HOME", filepath.Join(c.DataDir, "config")},
		{"HELM_CACHE_HOME", filepath.Join(c.DataDir, "helm", "cache")},
		{"HELM_CONFIG_HOME", filepath.Join(c.DataDir, "helm", "config")},
		{"HELM_DATA_HOME", filepath.Join(c.DataDir, "helm", "data")},
	}
	for _, d := range defaults {
		if os.Getenv(d.name) != "" {
			continue
		}
		if err := os.Setenv(d.name, d.value); err != nil {
			return nil, err
		}
	}
	// git and helm both want a home to write to, and an image's non-root user
	// often has none.
	if home := os.Getenv("HOME"); home == "" || !writable(home) {
		home = filepath.Join(c.DataDir, "home")
		if err := os.MkdirAll(home, 0o700); err != nil {
			return nil, err
		}
		if err := os.Setenv("HOME", home); err != nil {
			return nil, err
		}
	}
	// Wails listens wherever these say, and in the web version it must listen
	// on loopback alone, behind the gateway -- never where a browser could
	// reach it without signing in.
	for _, name := range []string{"WAILS_SERVER_HOST", "WAILS_SERVER_PORT"} {
		if err := os.Unsetenv(name); err != nil {
			return nil, err
		}
	}

	var folders []string
	wrote, err := c.writeInCluster()
	if err != nil {
		return nil, err
	}
	if wrote {
		folders = append(folders, c.generatedDir())
	}
	folders = append(folders, c.UploadDir())
	return append(folders, c.KubeconfigDirs...), nil
}

// serviceAccountDir is where Kubernetes mounts a pod's service account. A
// variable so a test can stand one up.
var serviceAccountDir = "/var/run/secrets/kubernetes.io/serviceaccount"

// inClusterFile is the kubeconfig describing the cluster the pod runs in.
func (c Config) inClusterFile() string {
	return filepath.Join(c.generatedDir(), "in-cluster.yaml")
}

// writeInCluster writes a kubeconfig for the pod's own service account, and
// reports whether it did.
//
// The token is referenced by path rather than copied in: Kubernetes rotates a
// projected token, and client-go rereads a token file, so the context keeps
// working for as long as the pod runs.
func (c Config) writeInCluster() (bool, error) {
	path := c.inClusterFile()
	token := filepath.Join(serviceAccountDir, "token")
	_, statErr := os.Stat(token)

	want := c.InCluster == "true" || (c.InCluster == "auto" && statErr == nil)
	if !want {
		// A file left from a run that had the cluster would go on offering it.
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
		return false, nil
	}
	if statErr != nil {
		return false, fmt.Errorf("K8SDOCKSIDE_IN_CLUSTER is true, but there is no service account token at %s: %w", token, statErr)
	}

	host, port := os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" || port == "" {
		return false, errors.New("KUBERNETES_SERVICE_HOST and KUBERNETES_SERVICE_PORT are not set, so the cluster this pod runs in cannot be reached")
	}
	namespace := ""
	if raw, err := os.ReadFile(filepath.Join(serviceAccountDir, "namespace")); err == nil {
		namespace = strings.TrimSpace(string(raw))
	}

	name := c.InClusterName
	cfg := clientcmdapi.NewConfig()
	cfg.Clusters[name] = &clientcmdapi.Cluster{
		Server:               "https://" + net.JoinHostPort(host, port),
		CertificateAuthority: filepath.Join(serviceAccountDir, "ca.crt"),
	}
	cfg.AuthInfos[name] = &clientcmdapi.AuthInfo{TokenFile: token}
	cfg.Contexts[name] = &clientcmdapi.Context{Cluster: name, AuthInfo: name, Namespace: namespace}
	cfg.CurrentContext = name
	if err := clientcmd.WriteToFile(*cfg, path); err != nil {
		return false, err
	}
	return true, nil
}

// LoopbackPort finds a free port on the loopback interface, for the app to
// listen on behind the gateway.
func LoopbackPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }()
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("unexpected listener address %v", l.Addr())
	}
	return addr.Port, nil
}

func envOr(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}

func envBool(name string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	switch strings.ToLower(raw) {
	case "yes", "on":
		return true, nil
	case "no", "off":
		return false, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback, fmt.Errorf("%s: %q is not true or false", name, raw)
	}
	return v, nil
}

// envList splits a comma-separated variable, dropping empty entries.
func envList(name string) []string {
	var out []string
	for _, part := range strings.Split(os.Getenv(name), ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// writable reports whether a directory can be written to.
func writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".k8sdockside-probe-*") // #nosec G703 -- $HOME, probed for whether it can be written to
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name) // #nosec G703 -- the probe file made just above
	return true
}
