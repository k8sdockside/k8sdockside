package gateway

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/k8sdockside/k8sdockside/internal/kube"
)

// clearPrepareEnv makes every variable Prepare may set go back to how it was
// once the test is over.
func clearPrepareEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"XDG_CONFIG_HOME", "HELM_CACHE_HOME", "HELM_CONFIG_HOME", "HELM_DATA_HOME", "WAILS_SERVER_HOST", "WAILS_SERVER_PORT"} {
		t.Setenv(name, "")
	}
	t.Setenv("HOME", t.TempDir())
}

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("K8SDOCKSIDE_DATA_DIR", "/srv/dockside")
	t.Setenv("K8SDOCKSIDE_PUBLIC_URL", "https://dockside.example.com/")
	t.Setenv("K8SDOCKSIDE_TRUST_FORWARDED", "yes")
	t.Setenv("K8SDOCKSIDE_KUBECONFIG_DIRS", "/etc/a, /etc/b,,")
	t.Setenv("K8SDOCKSIDE_ADMIN_EMAILS", "Ops@Example.com")
	t.Setenv("K8SDOCKSIDE_SESSION_TTL", "12h")

	c, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.DataDir != "/srv/dockside" || c.ListenAddr != ":8080" || !c.TrustForwarded || !c.PasswordLogin {
		t.Fatalf("config = %+v", c)
	}
	if c.PublicURL == nil || c.PublicURL.String() != "https://dockside.example.com" {
		t.Fatalf("public URL = %v", c.PublicURL)
	}
	if !slices.Equal(c.KubeconfigDirs, []string{"/etc/a", "/etc/b"}) {
		t.Fatalf("kubeconfig dirs = %v", c.KubeconfigDirs)
	}
	if !slices.Equal(c.AdminEmails, []string{"ops@example.com"}) || c.SessionTTL != 12*time.Hour {
		t.Fatalf("config = %+v", c)
	}
}

func TestConfigFromEnvReportsEveryProblem(t *testing.T) {
	t.Setenv("K8SDOCKSIDE_DATA_DIR", "relative/dir")
	t.Setenv("K8SDOCKSIDE_PUBLIC_URL", "https://dockside.example.com/sub")
	t.Setenv("K8SDOCKSIDE_PASSWORD_LOGIN", "maybe")
	t.Setenv("K8SDOCKSIDE_IN_CLUSTER", "sometimes")
	t.Setenv("K8SDOCKSIDE_ADMIN_USERNAME", "root")

	_, err := ConfigFromEnv()
	if err == nil {
		t.Fatal("a bad configuration must be refused")
	}
	for _, want := range []string{"K8SDOCKSIDE_DATA_DIR", "K8SDOCKSIDE_PUBLIC_URL", "K8SDOCKSIDE_PASSWORD_LOGIN", "K8SDOCKSIDE_IN_CLUSTER", "K8SDOCKSIDE_ADMIN_PASSWORD"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not mention %s: %v", want, err)
		}
	}
}

func TestPrepareWritesTheInClusterKubeconfig(t *testing.T) {
	clearPrepareEnv(t)
	sa := t.TempDir()
	for name, content := range map[string]string{"token": "tok", "ca.crt": "ca", "namespace": "tools\n"} {
		if err := os.WriteFile(filepath.Join(sa, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := serviceAccountDir
	serviceAccountDir = sa
	t.Cleanup(func() { serviceAccountDir = old })
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")
	t.Setenv("KUBERNETES_SERVICE_PORT", "443")
	t.Setenv("WAILS_SERVER_HOST", "0.0.0.0")

	c := Config{DataDir: t.TempDir(), InCluster: "auto", InClusterName: "home", KubeconfigDirs: []string{"/mnt/kc"}}
	folders, err := c.Prepare()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{c.generatedDir(), c.UploadDir(), "/mnt/kc"}
	if !slices.Equal(folders, want) {
		t.Fatalf("folders = %v, want %v", folders, want)
	}
	if os.Getenv("WAILS_SERVER_HOST") != "" {
		t.Fatal("Prepare must stop Wails listening anywhere but loopback")
	}
	if got := os.Getenv("XDG_CONFIG_HOME"); got != filepath.Join(c.DataDir, "config") {
		t.Fatalf("XDG_CONFIG_HOME = %q", got)
	}

	parsed := kube.ParseFile(c.inClusterFile(), kube.SourceFolder)
	if parsed.Error != "" || len(parsed.Contexts) != 1 || parsed.Contexts[0].Name != "home" {
		t.Fatalf("the in-cluster kubeconfig = %+v", parsed)
	}
	raw, err := os.ReadFile(c.inClusterFile())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"https://10.0.0.1:443", filepath.Join(sa, "token"), "namespace: tools"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("the kubeconfig does not mention %s:\n%s", want, raw)
		}
	}

	// Switched off, the file from last time goes.
	c.InCluster = "false"
	if _, err := c.Prepare(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(c.inClusterFile()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the in-cluster kubeconfig must be removed when switched off")
	}
}

func TestPrepareInsistsWhenTold(t *testing.T) {
	clearPrepareEnv(t)
	old := serviceAccountDir
	serviceAccountDir = t.TempDir()
	t.Cleanup(func() { serviceAccountDir = old })

	c := Config{DataDir: t.TempDir(), InCluster: "true", InClusterName: "home"}
	if _, err := c.Prepare(); err == nil {
		t.Fatal("in-cluster set to true without a service account must fail")
	}
	c.InCluster = "auto"
	if _, err := c.Prepare(); err != nil {
		t.Fatalf("auto without a service account must just skip it: %v", err)
	}
}

func TestManagedProviders(t *testing.T) {
	dir := t.TempDir()
	write := func(content string) string {
		path := filepath.Join(dir, "providers.json")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	t.Setenv("TEST_GITHUB_SECRET", "from-env")

	got, err := loadManagedProviders(write(`[
		{"id": "github", "type": "github", "clientId": "abc", "clientSecretEnv": "TEST_GITHUB_SECRET", "autoSignup": true, "allowedDomains": ["@Example.com"]},
		{"id": "sso", "type": "oidc", "issuer": "https://sso.example.com/", "clientId": "x", "clientSecret": "y", "enabled": false}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got[0].Enabled || got[1].Enabled || !got[0].Managed {
		t.Fatalf("providers = %+v", got)
	}
	if got[0].secret() != "from-env" || got[0].Name != "GitHub" || !slices.Equal(got[0].AllowedDomains, []string{"example.com"}) {
		t.Fatalf("github = %+v", got[0])
	}
	if got[1].Issuer != "https://sso.example.com" {
		t.Fatalf("the issuer must lose its trailing slash: %q", got[1].Issuer)
	}

	for name, content := range map[string]string{
		"no secret": `[{"id": "gh", "type": "github", "clientId": "abc"}]`,
		"twice":     `[{"id": "a", "type": "github", "clientId": "c", "clientSecret": "s"}, {"id": "a", "type": "google", "clientId": "c", "clientSecret": "s"}]`,
		"bad type":  `[{"id": "a", "type": "myspace", "clientId": "c", "clientSecret": "s"}]`,
		"http":      `[{"id": "a", "type": "oidc", "issuer": "http://sso.example.com", "clientId": "c", "clientSecret": "s"}]`,
	} {
		if _, err := loadManagedProviders(write(content)); err == nil {
			t.Errorf("%s: must be refused", name)
		}
	}
}

func TestPasswords(t *testing.T) {
	hash, err := hashPassword(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if !checkPassword(hash, testPassword) || checkPassword(hash, testPassword+"!") {
		t.Fatal("a hash must match its password and nothing else")
	}
	if checkPassword("plain", "plain") || checkPassword("", "") {
		t.Fatal("something that is not a hash matches nothing")
	}
	if validatePassword("short") == nil || validatePassword(strings.Repeat("x", maxPasswordLength+1)) == nil || validatePassword(testPassword) != nil {
		t.Fatal("password rules")
	}
}

func TestStoreKeepsAnAdministratorAndSurvivesARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	st, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	alice, err := st.createUser(User{Username: "alice", Role: roleAdmin}, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.createUser(User{Username: "bob"}, true); !errors.Is(err, errAlreadySetUp) {
		t.Fatalf("a second first user = %v", err)
	}
	if _, err := st.createUser(User{Username: "ALICE"}, false); !errors.Is(err, errUsernameTaken) {
		t.Fatalf("a username differing only in case = %v", err)
	}
	if _, err := st.createUser(User{Username: "../x"}, false); err == nil {
		t.Fatal("a username with a path in it must be refused")
	}
	if err := st.deleteUser(alice.ID); !errors.Is(err, errLastAdmin) {
		t.Fatalf("removing the last administrator = %v", err)
	}
	if _, err := st.updateUser(alice.ID, func(u *User) error { u.Disabled = true; return nil }); !errors.Is(err, errLastAdmin) {
		t.Fatalf("switching off the last administrator = %v", err)
	}

	token, _, err := st.newSession(alice.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), token) {
		t.Fatal("the session token itself must not be written down")
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("the store must be readable only by the app, is %v", info.Mode().Perm())
	}

	again, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, u, ok := again.sessionFor(token); !ok || u.ID != alice.ID {
		t.Fatal("users and sessions must survive a restart")
	}

	again.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	if _, _, ok := again.sessionFor(token); ok {
		t.Fatal("an expired session must not stand")
	}
}

func TestFreeUsername(t *testing.T) {
	st, err := openStore(filepath.Join(t.TempDir(), "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.createUser(User{Username: "dana", Role: roleAdmin}, false); err != nil {
		t.Fatal(err)
	}
	if got := st.freeUsername("", "dana", "x"); got != "dana-2" {
		t.Fatalf("a taken name = %q, want dana-2", got)
	}
	if got := st.freeUsername("Dana Smith!"); got != "Dana-Smith-" {
		t.Fatalf("a name with spaces = %q", got)
	}
	if got := st.freeUsername("", "---"); got != "user" {
		t.Fatalf("nothing usable = %q, want user", got)
	}
}
