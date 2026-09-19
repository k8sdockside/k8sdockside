package gateway

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/k8sdockside/k8sdockside/internal/kube"
)

// maxKubeconfigBytes bounds an uploaded kubeconfig. The largest real ones --
// dozens of clusters, certificates inline -- are a few hundred kilobytes.
const maxKubeconfigBytes = 1 << 20

var clusterNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

// clusterFile is one kubeconfig on the clusters page.
type clusterFile struct {
	Name     string
	Contexts []string
	Error    string
}

// clusterGroup is one source of kubeconfigs on the clusters page.
type clusterGroup struct {
	Title string
	Note  string
	Files []clusterFile
	// Uploaded marks the one group whose files can be removed here.
	Uploaded bool
}

// clusterGroups lists every kubeconfig the app reads, by where it comes from.
func (g *Gateway) clusterGroups() []clusterGroup {
	var groups []clusterGroup
	if _, err := os.Stat(g.cfg.inClusterFile()); err == nil {
		groups = append(groups, clusterGroup{
			Title: "The cluster this runs in",
			Note:  "The pod's own service account, with whatever access the deployment's RBAC gives it.",
			Files: []clusterFile{readKubeconfig(g.cfg.inClusterFile())},
		})
	}
	for _, dir := range g.cfg.KubeconfigDirs {
		groups = append(groups, clusterGroup{
			Title: "From the deployment",
			Note:  "Mounted into the pod from " + dir + ", typically from Kubernetes Secrets. Change them there.",
			Files: readKubeconfigDir(dir),
		})
	}
	groups = append(groups, clusterGroup{
		Title:    "Added here",
		Note:     "Kept in the data directory. On a persistent volume they survive a restart; without one they do not.",
		Files:    readKubeconfigDir(g.cfg.UploadDir()),
		Uploaded: true,
	})
	return groups
}

func readKubeconfigDir(dir string) []clusterFile {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []clusterFile{{Name: dir, Error: err.Error()}}
	}
	var out []clusterFile
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			continue
		}
		out = append(out, readKubeconfig(path))
	}
	return out
}

func readKubeconfig(path string) clusterFile {
	parsed := kube.ParseFile(path, kube.SourceFolder)
	f := clusterFile{Name: filepath.Base(path), Error: parsed.Error}
	for _, c := range parsed.Contexts {
		f.Contexts = append(f.Contexts, c.Name)
	}
	slices.Sort(f.Contexts)
	return f
}

// addKubeconfig keeps an uploaded kubeconfig, once it has been read as one,
// and has the app pick it up.
//
// Every file operation goes through an os.Root on the upload folder, so that
// whatever a name turns out to contain, nothing outside the folder can be
// written, renamed or removed.
func (g *Gateway) addKubeconfig(name string, content []byte, replace bool) (clusterFile, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, ext := range []string{".yaml", ".yml", ".kubeconfig", ".conf"} {
		name = strings.TrimSuffix(name, ext)
	}
	if !clusterNamePattern.MatchString(name) {
		return clusterFile{}, errors.New("a name is lowercase letters, digits, dots, dashes and underscores, starting with a letter or digit")
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		return clusterFile{}, errors.New("choose a kubeconfig file, or paste one")
	}
	if len(content) > maxKubeconfigBytes {
		return clusterFile{}, fmt.Errorf("a kubeconfig can be at most %d KiB", maxKubeconfigBytes>>10)
	}

	root, err := os.OpenRoot(g.cfg.UploadDir())
	if err != nil {
		return clusterFile{}, err
	}
	defer func() { _ = root.Close() }()

	final := name + ".yaml"
	if _, err := root.Stat(final); err == nil && !replace {
		return clusterFile{}, fmt.Errorf("there is already a kubeconfig called %s; tick replace to overwrite it", name)
	}

	// Written beside the final file and read from there before it is kept, so
	// what is kept is exactly what was checked. The dot keeps the app's folder
	// scan from offering it in the meantime.
	tmp := ".upload-" + randomToken(9) + ".yaml"
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return clusterFile{}, err
	}
	defer func() { _ = root.Remove(tmp) }()
	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		return clusterFile{}, err
	}
	if err := f.Close(); err != nil {
		return clusterFile{}, err
	}

	tmpPath := filepath.Join(root.Name(), tmp)
	parsed := kube.ParseFile(tmpPath, kube.SourceFolder)
	if parsed.Error != "" {
		return clusterFile{}, fmt.Errorf("that is not a kubeconfig this app can read: %s", strings.ReplaceAll(parsed.Error, tmpPath, final))
	}
	if len(parsed.Contexts) == 0 {
		return clusterFile{}, errors.New("that kubeconfig has no contexts")
	}
	if err := root.Rename(tmp, final); err != nil {
		return clusterFile{}, err
	}
	g.resync()
	return readKubeconfig(filepath.Join(root.Name(), final)), nil
}

// removeKubeconfig deletes an uploaded kubeconfig.
func (g *Gateway) removeKubeconfig(name string) error {
	noSuch := errors.New("there is no such kubeconfig")
	if name != filepath.Base(name) || strings.HasPrefix(name, ".") || !clusterNamePattern.MatchString(strings.TrimSuffix(name, filepath.Ext(name))) {
		return noSuch
	}
	root, err := os.OpenRoot(g.cfg.UploadDir())
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if err := root.Remove(name); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return noSuch
		}
		return err
	}
	g.resync()
	return nil
}

func (g *Gateway) resync() {
	if g.deps.Resync != nil {
		g.deps.Resync()
	}
}
