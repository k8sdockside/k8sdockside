package kube

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"k8s.io/client-go/tools/clientcmd"
)

const twoClusters = `apiVersion: v1
kind: Config
current-context: prod
clusters:
- name: dev-cluster
  cluster:
    server: https://dev.example:6443
    certificate-authority: certs/dev-ca.crt
- name: prod-cluster
  cluster:
    server: https://prod.example:6443
contexts:
- name: dev
  context:
    cluster: dev-cluster
    user: dev-user
    namespace: team-a
- name: prod
  context:
    cluster: prod-cluster
    user: prod-user
users:
- name: dev-user
  user:
    token: dev-token
- name: prod-user
  user:
    token: prod-token
`

func TestWriteScopedKubeconfig(t *testing.T) {
	src := t.TempDir()
	file := filepath.Join(src, "config")
	if err := os.WriteFile(file, []byte(twoClusters), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "terminal")

	path, err := WriteScopedKubeconfig(Context{Name: "dev", File: file}, out)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != out {
		t.Fatalf("written to %s, want it in %s", path, out)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("mode = %o, want 0600", perm)
		}
	}

	cfg, err := clientcmd.LoadFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CurrentContext != "dev" {
		t.Fatalf("current-context = %q, want dev", cfg.CurrentContext)
	}
	if len(cfg.Contexts) != 1 || cfg.Contexts["dev"] == nil {
		t.Fatalf("contexts = %v, want only dev", cfg.Contexts)
	}
	if len(cfg.Clusters) != 1 || cfg.Clusters["dev-cluster"] == nil {
		t.Fatalf("clusters = %v, want only dev-cluster", cfg.Clusters)
	}
	if len(cfg.AuthInfos) != 1 || cfg.AuthInfos["dev-user"] == nil || cfg.AuthInfos["dev-user"].Token != "dev-token" {
		t.Fatalf("users = %v, want only dev-user", cfg.AuthInfos)
	}
	if cfg.Contexts["dev"].Namespace != "team-a" {
		t.Fatalf("namespace = %q, want team-a kept", cfg.Contexts["dev"].Namespace)
	}
	// The relative certificate path is made absolute, since the copy lives
	// somewhere else.
	if ca := cfg.Clusters["dev-cluster"].CertificateAuthority; ca != filepath.Join(src, "certs", "dev-ca.crt") {
		t.Fatalf("certificate-authority = %q, want it resolved against %s", ca, src)
	}

	// The original is untouched.
	data, err := os.ReadFile(file)
	if err != nil || string(data) != twoClusters {
		t.Fatalf("the original kubeconfig changed: %v", err)
	}

	// Removing it is the caller's job, and leaves nothing behind.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(out); len(entries) != 0 {
		t.Fatalf("left behind: %v", entries)
	}
}

func TestWriteScopedKubeconfigUnknownContext(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(file, []byte(twoClusters), 0o600); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if _, err := WriteScopedKubeconfig(Context{Name: "staging", File: file}, out); err == nil {
		t.Fatal("want an error for a context the file does not have")
	}
	if entries, _ := os.ReadDir(out); len(entries) != 0 {
		t.Fatalf("a failed write left %v behind", entries)
	}
}
