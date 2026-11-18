package kube

// Kubeconfigs whose author is not trusted with the machine reading them.
//
// A kubeconfig is not only data. Its `exec` stanza names a program that
// client-go runs to get a credential, and its `tokenFile`, `client-certificate`,
// `client-key` and `certificate-authority` name files that client-go reads and
// then presents to whatever server the same kubeconfig names. On a laptop the
// author of a kubeconfig is its user, and that is the point. In the web
// version, an administrator who can upload one -- or point the app at a file a
// plugin's git repository put on disk -- is not thereby trusted with the pod:
// an exec stanza runs a shell in it, and a tokenFile of the pod's own
// ServiceAccount token, beside a server of their choosing, sends them that
// token. So there, a kubeconfig the deployment itself did not provide must
// carry its credentials inline.

import (
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"k8s.io/client-go/tools/clientcmd"
)

// CheckInline reports why the kubeconfig at path cannot be used where its
// author may not run programs or read files on this machine: every exec
// plugin, auth provider and file path in it, users and clusters alike, whether
// or not a context uses them. Nil means the file carries everything inline.
func CheckInline(path string) error {
	cfg, err := clientcmd.LoadFromFile(path)
	if err != nil {
		return fmt.Errorf("its credentials cannot be read: %w", err)
	}
	var problems []string
	for _, name := range slices.Sorted(maps.Keys(cfg.AuthInfos)) {
		u := cfg.AuthInfos[name]
		if u == nil {
			continue
		}
		if u.Exec != nil {
			problems = append(problems, fmt.Sprintf("user %q runs a program (exec: %s)", name, u.Exec.Command))
		}
		if u.AuthProvider != nil {
			problems = append(problems, fmt.Sprintf("user %q uses an auth provider (%s)", name, u.AuthProvider.Name))
		}
		for _, f := range []struct{ field, path string }{
			{"tokenFile", u.TokenFile},
			{"client-certificate", u.ClientCertificate},
			{"client-key", u.ClientKey},
		} {
			if f.path != "" {
				problems = append(problems, fmt.Sprintf("user %q reads a file (%s: %s)", name, f.field, f.path))
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(cfg.Clusters)) {
		if c := cfg.Clusters[name]; c != nil && c.CertificateAuthority != "" {
			problems = append(problems, fmt.Sprintf("cluster %q reads a file (certificate-authority: %s)", name, c.CertificateAuthority))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return errors.New("only inline credentials are accepted here (token, client-certificate-data, client-key-data, certificate-authority-data), but " +
		strings.Join(problems, "; "))
}

// within reports whether path is inside one of dirs, which are already
// resolved. A Secret mounted as a volume is a folder of symlinks into a hidden
// sub-directory, and a file in it resolves to there, so a sub-directory counts.
func within(path string, dirs []string) bool {
	for _, dir := range dirs {
		rel, err := filepath.Rel(dir, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return true
		}
	}
	return false
}
