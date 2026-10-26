package kube

import (
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// WriteScopedKubeconfig writes a kubeconfig holding one context -- that
// context, its cluster and its user, and nothing else from the file -- into
// dir, readable by this user only, and returns its path.
//
// It is what a local terminal opened on a cluster is pointed at with
// KUBECONFIG, so that kubectl, helm and anything else that reads a kubeconfig
// talk to the cluster the terminal was opened for. The user's own files are
// not touched, and nothing typed in the terminal (`kubectl config
// use-context`, say) can change them or reach another cluster through them.
//
// Relative paths in the original -- a certificate beside the file, an exec
// plugin in a folder next to it -- are made absolute, since the copy is
// somewhere else. The caller removes the file when the terminal closes.
func WriteScopedKubeconfig(kc Context, dir string) (string, error) {
	rules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kc.File}
	full, err := rules.Load()
	if err != nil {
		return "", fmt.Errorf("could not read %s: %w", kc.File, err)
	}
	scoped, err := scopeTo(full, kc.Name)
	if err != nil {
		return "", fmt.Errorf("%w in %s", err, kc.File)
	}
	data, err := clientcmd.Write(*scoped)
	if err != nil {
		return "", fmt.Errorf("could not write a kubeconfig for %s: %w", kc.Name, err)
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("could not make %s: %w", dir, err)
	}
	f, err := os.CreateTemp(dir, "kubeconfig-*.yaml")
	if err != nil {
		return "", fmt.Errorf("could not create a kubeconfig in %s: %w", dir, err)
	}
	path := f.Name()
	// CreateTemp makes it 0600 already; said again, because this file holds
	// credentials and the umask is not ours to rely on.
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return filepath.Clean(path), nil
}

// scopeTo is the part of a kubeconfig that one context needs.
func scopeTo(full *clientcmdapi.Config, name string) (*clientcmdapi.Config, error) {
	ctx, ok := full.Contexts[name]
	if !ok || ctx == nil {
		return nil, fmt.Errorf("there is no context %q", name)
	}
	out := clientcmdapi.NewConfig()
	out.CurrentContext = name
	out.Contexts[name] = ctx.DeepCopy()
	if cluster, ok := full.Clusters[ctx.Cluster]; ok && cluster != nil {
		out.Clusters[ctx.Cluster] = cluster.DeepCopy()
	}
	if user, ok := full.AuthInfos[ctx.AuthInfo]; ok && user != nil {
		out.AuthInfos[ctx.AuthInfo] = user.DeepCopy()
	}
	return out, nil
}
