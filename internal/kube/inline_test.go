package kube

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// kubeconfigWith is a one-context kubeconfig whose user and cluster carry the
// given YAML lines, already indented for their place.
func kubeconfigWith(user, cluster string) string {
	return `apiVersion: v1
kind: Config
current-context: c
clusters:
- name: k
  cluster:
    server: https://203.0.113.10:6443
` + cluster + `
contexts:
- name: c
  context:
    cluster: k
    user: u
users:
- name: u
  user:
` + user + `
`
}

const inlineKubeconfig = `apiVersion: v1
kind: Config
current-context: c
clusters:
- name: k
  cluster:
    server: https://203.0.113.10:6443
    certificate-authority-data: Zm9v
contexts:
- name: c
  context:
    cluster: k
    user: u
users:
- name: u
  user:
    token: abc
`

// runsAShell is what the security report described: a kubeconfig whose
// "credential plugin" is a shell.
var runsAShell = kubeconfigWith(`    exec:
      apiVersion: client.authentication.k8s.io/v1
      command: sh
      args: ["-c", "id > /tmp/pwned"]`, "")

func TestCheckInline(t *testing.T) {
	for _, tc := range []struct {
		name, config, want string
	}{
		{"inline", inlineKubeconfig, ""},
		{"inline client certificate", kubeconfigWith("    client-certificate-data: Zm9v\n    client-key-data: Zm9v", ""), ""},
		{"exec", runsAShell, `runs a program (exec: sh)`},
		{"auth provider", kubeconfigWith("    auth-provider:\n      name: oidc\n      config:\n        idp-issuer-url: https://idp.example.com", ""), "auth provider (oidc)"},
		{"token file", kubeconfigWith("    tokenFile: /var/run/secrets/kubernetes.io/serviceaccount/token", ""), "tokenFile: /var/run/secrets/kubernetes.io/serviceaccount/token"},
		{"client certificate file", kubeconfigWith("    client-certificate: /etc/ssl/a.crt", ""), "client-certificate: /etc/ssl/a.crt"},
		{"client key file", kubeconfigWith("    client-key: /etc/ssl/a.key", ""), "client-key: /etc/ssl/a.key"},
		{"certificate authority file", kubeconfigWith("    token: abc", "    certificate-authority: /etc/ssl/ca.crt"), `cluster "k" reads a file (certificate-authority: /etc/ssl/ca.crt)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeFile(t, t.TempDir(), "config", tc.config)
			err := CheckInline(path)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("CheckInline = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("CheckInline = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// A user no context names is still refused: which user a context picks is
// one edit away, and the check is of the file, not of today's contexts.
func TestCheckInlineRefusesAnUnusedExecUser(t *testing.T) {
	config := inlineKubeconfig + `- name: spare
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1
      command: sh
`
	if err := CheckInline(writeFile(t, t.TempDir(), "config", config)); err == nil || !strings.Contains(err.Error(), `user "spare"`) {
		t.Fatalf("CheckInline = %v, want the unused user refused", err)
	}
}

func TestDiscoverInlineOnly(t *testing.T) {
	home := realTempDir(t)
	t.Setenv("HOME", home)
	uploads := realTempDir(t)
	mounted := realTempDir(t)
	elsewhere := realTempDir(t)

	uploadedShell := writeFile(t, uploads, "evil.yaml", runsAShell)
	uploadedInline := writeFile(t, uploads, "fine.yaml", inlineKubeconfig)
	mountedShell := writeFile(t, mounted, "eks.yaml", runsAShell)
	manualShell := writeFile(t, elsewhere, "manual.yaml", runsAShell)
	fromEnv := writeFile(t, elsewhere, "env.yaml", runsAShell)
	t.Setenv("KUBECONFIG", fromEnv)

	sources := Sources{
		Files:   []string{manualShell},
		Folders: []string{uploads, mounted},
		Trusted: []string{mounted},
	}

	// The desktop app takes every one of them.
	if got := contextsByPath(Discover(sources)); len(got) != 5 {
		t.Fatalf("without InlineOnly every file must offer its contexts, got %v", got)
	}

	sources.InlineOnly = true
	files := Discover(sources)
	got := contextsByPath(files)
	for _, path := range []string{uploadedInline, mountedShell, fromEnv} {
		if got[path] != 1 {
			t.Errorf("%s must offer its context, got %v", filepath.Base(path), got)
		}
	}
	if _, ok := got[uploadedShell]; ok {
		t.Errorf("the uploaded exec kubeconfig must not be offered: %v", got)
	}
	// A file named by hand is shown as broken, with the reason, rather than
	// dropped: the user asked for it.
	var manual *File
	for i := range files {
		if files[i].Path == manualShell {
			manual = &files[i]
		}
	}
	if manual == nil || len(manual.Contexts) != 0 || !strings.Contains(manual.Error, "runs a program") {
		t.Fatalf("the manual exec kubeconfig = %+v, want it listed as broken with no contexts", manual)
	}

	if err := sources.Admit(manualShell); err == nil {
		t.Error("Admit must refuse an exec kubeconfig outside the trusted folders")
	}
	if err := sources.Admit(mountedShell); err != nil {
		t.Errorf("Admit must take one in a trusted folder: %v", err)
	}
}

// A Secret mounted as a volume is a folder of symlinks into a hidden,
// versioned sub-directory, so its files resolve to somewhere below the folder.
func TestDiscoverTrustsASecretMountedFolder(t *testing.T) {
	home := realTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("KUBECONFIG", "")
	mounted := realTempDir(t)
	writeFile(t, mounted, "..2026_11_18_10_00_00.123/eks.yaml", runsAShell)
	if err := os.Symlink("..2026_11_18_10_00_00.123", filepath.Join(mounted, "..data")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..data", "eks.yaml"), filepath.Join(mounted, "eks.yaml")); err != nil {
		t.Fatal(err)
	}

	files := Discover(Sources{Folders: []string{mounted}, InlineOnly: true, Trusted: []string{mounted}})
	if len(files) != 1 || len(files[0].Contexts) != 1 {
		t.Fatalf("a kubeconfig in a trusted, Secret-mounted folder must be offered: %+v", files)
	}
}

// A symlink in an untrusted folder that points into a trusted one is the
// trusted file, and the other way round is not.
func TestASymlinkIsJudgedByWhereItLeads(t *testing.T) {
	home := realTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("KUBECONFIG", "")
	trusted := realTempDir(t)
	untrusted := realTempDir(t)
	outside := writeFile(t, untrusted, "real.yaml", runsAShell)
	if err := os.Symlink(outside, filepath.Join(trusted, "link.yaml")); err != nil {
		t.Fatal(err)
	}

	files := Discover(Sources{Folders: []string{trusted}, InlineOnly: true, Trusted: []string{trusted}})
	if got := contextsByPath(files); len(got) != 0 {
		t.Fatalf("a link out of a trusted folder must not lend it trust: %v", got)
	}
}

func contextsByPath(files []File) map[string]int {
	out := map[string]int{}
	for _, f := range files {
		if len(f.Contexts) > 0 {
			out[f.Path] = len(f.Contexts)
		}
	}
	return out
}
