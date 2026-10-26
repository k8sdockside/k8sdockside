package helmcli

// The four things the app asks helm to do, and the one thing it asks helm about.
//
// Every one of them is a process, not a shell: the arguments are passed as
// arguments, so nothing crossing from the frontend can be a second command, and
// the values document goes into a file rather than onto a command line where
// its quoting would have to be got right.

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/k8sdockside/k8sdockside/internal/procattr"
)

// DefaultTimeout is how long a change is given before it is called off.
//
// Five minutes, matching helm's own default for --wait, because that is what
// the number is for: an upgrade that waits is waiting on pods to come up, and
// anything this app chose instead would either cut off a slow rollout or leave
// a stuck one running.
const DefaultTimeout = 5 * time.Minute

// searchTimeout bounds asking the repositories what versions exist. It talks to
// the network, but only to index files, and a repo that has not answered in
// this long is a repo the user should be told about rather than waited on.
const searchTimeout = 30 * time.Second

// Target is the cluster a command runs against.
//
// The kubeconfig and the context are always passed, never left to helm's own
// defaults. This app's premise is a dozen kubeconfigs open at once, and a helm
// left to read $KUBECONFIG and current-context would act on whichever cluster
// the user last selected in a shell -- which is the one class of mistake that
// must not be possible here.
type Target struct {
	Kubeconfig string
	Context    string
	Namespace  string
}

// Options are the choices that apply to any change, from the settings.
type Options struct {
	// Wait holds the command open until the objects it wrote report ready,
	// rather than returning as soon as the API server has accepted them.
	Wait bool
	// Atomic rolls the release back if the upgrade fails. It implies Wait --
	// there is no way to know an upgrade failed without waiting for it -- which
	// helm enforces itself.
	Atomic bool
	// Timeout bounds the whole command. Zero means DefaultTimeout.
	Timeout time.Duration
}

func (o Options) timeout() time.Duration {
	if o.Timeout <= 0 {
		return DefaultTimeout
	}
	return o.Timeout
}

// flags renders the options helm takes on every changing command.
func (o Options) flags() []string {
	out := []string{"--timeout", o.timeout().String()}
	if o.Wait || o.Atomic {
		out = append(out, "--wait")
	}
	if o.Atomic {
		out = append(out, "--atomic")
	}
	return out
}

// UpgradeRequest is one upgrade: which release, to which chart at which
// version, with which values.
type UpgradeRequest struct {
	Release string
	// Chart is the reference to upgrade to: a repo alias and name
	// ("ingress-nginx/ingress-nginx"), an OCI or http URL, a local directory, or
	// a packaged .tgz. Helm's release record does not say where a chart came
	// from, so this cannot be derived and has to be given.
	Chart string
	// Version is the chart version. Empty means whatever the repository calls
	// latest, which is helm's own default.
	Version string
	// Values is the complete set of user-supplied values the release should
	// have after this upgrade -- the document the editor was showing, as it now
	// stands.
	//
	// Complete, not additional, and that distinction is the whole reason this
	// field is documented at length. Helm's default upgrade discards the
	// previous release's config as soon as any values are supplied, and keeps
	// it only when none are (pkg/action/upgrade.go, reuseValues). That is
	// exactly what an editor wants: what is on screen is what the release gets,
	// and deleting a line deletes the value.
	//
	// --reuse-values is deliberately not used, and would be wrong here: it
	// merges the old values back over the new ones, so a value removed in the
	// editor would come straight back and the save would look like it had
	// silently failed.
	Values string
}

// ChartVersion is one version of one chart, as the repositories offer it.
type ChartVersion struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	AppVersion  string `json:"app_version"`
	Description string `json:"description"`
}

// Upgrade re-releases one release, at a chart version and with values.
func (h *Helm) Upgrade(ctx context.Context, t Target, req UpgradeRequest, o Options) (string, error) {
	if err := checkRelease(req.Release); err != nil {
		return "", err
	}
	if err := checkChart(req.Chart); err != nil {
		return "", err
	}
	if err := checkNamespace(t.Namespace); err != nil {
		return "", err
	}

	args := []string{"upgrade", req.Release, req.Chart}
	if v := strings.TrimSpace(req.Version); v != "" {
		args = append(args, "--version", v)
	}

	// The values go to a file rather than to --set: --set has a syntax of its
	// own, in which a dot is a path separator and a comma a delimiter, so a
	// value containing either would have to be escaped into it and back out.
	// A file is the document itself.
	if strings.TrimSpace(req.Values) != "" {
		path, cleanup, err := valuesFile(req.Values)
		if err != nil {
			return "", err
		}
		defer cleanup()
		args = append(args, "--values", path)
	}

	args = append(args, o.flags()...)
	return h.run(ctx, t, o.timeout(), args)
}

// Rollback returns a release to an earlier revision.
//
// It needs no chart: the revision being rolled back to has its own rendered
// manifest and its own values stored in its record, which is what helm applies.
// That is why this works for a release whose chart nobody can find any more.
func (h *Helm) Rollback(ctx context.Context, t Target, release string, revision int, o Options) (string, error) {
	if err := checkRelease(release); err != nil {
		return "", err
	}
	if err := checkNamespace(t.Namespace); err != nil {
		return "", err
	}
	if revision <= 0 {
		return "", fmt.Errorf("%d is not a revision to roll back to", revision)
	}

	args := append([]string{"rollback", release, strconv.Itoa(revision)}, o.flags()...)
	return h.run(ctx, t, o.timeout(), args)
}

// Uninstall removes a release and everything it installed.
func (h *Helm) Uninstall(ctx context.Context, t Target, release string, keepHistory bool, o Options) (string, error) {
	if err := checkRelease(release); err != nil {
		return "", err
	}
	if err := checkNamespace(t.Namespace); err != nil {
		return "", err
	}

	args := []string{"uninstall", release, "--timeout", o.timeout().String()}
	if o.Wait || o.Atomic {
		args = append(args, "--wait")
	}
	// Keeping the history leaves the release's records in place, so it is still
	// listed -- as "uninstalled" -- and can still be rolled back. Off by
	// default, which is helm's own default and what "uninstall" reads as.
	if keepHistory {
		args = append(args, "--keep-history")
	}
	return h.run(ctx, t, o.timeout(), args)
}

// Versions lists the versions of a chart the configured repositories offer,
// newest first, for the upgrade form's version picker.
//
// It searches the repos this machine has added rather than any index of its
// own, because that is where an upgrade would fetch from: a version offered
// here is one `helm upgrade` can actually reach. A chart installed from an OCI
// registry or a local directory will find nothing, which is not a failure --
// the form still takes a version typed by hand.
func (h *Helm) Versions(ctx context.Context, chart string) ([]ChartVersion, error) {
	if err := checkChart(chart); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()

	// #nosec G204 -- the program is the located helm and every argument is
	// either a constant or a chart reference checked above; no shell is
	// involved, so nothing here can become a second command.
	cmd := exec.CommandContext(ctx, h.path, "search", "repo", chart, "--versions", "--output", "json")
	procattr.Hide(cmd)
	var out, errs bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errs

	if err := cmd.Run(); err != nil {
		// `helm search repo` exits non-zero when nothing matched, which is an
		// answer rather than a fault: the chart came from somewhere the repos
		// do not cover.
		if strings.Contains(errs.String(), "no results found") {
			return []ChartVersion{}, nil
		}
		return nil, commandError(err, errs.String())
	}

	var found []ChartVersion
	if err := json.Unmarshal(out.Bytes(), &found); err != nil {
		return nil, fmt.Errorf("reading what helm found: %w", err)
	}
	if found == nil {
		found = []ChartVersion{}
	}
	return found, nil
}

// valuesFile writes a values document somewhere helm can read it, and returns
// how to take it away again.
//
// 0600 and a temporary directory of this process's own: a values document is
// the part of a release most likely to hold a password, and the world-readable
// /tmp is not where it goes.
func valuesFile(values string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "k8sdockside-helm-")
	if err != nil {
		return "", nil, fmt.Errorf("preparing the values file: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }

	path := dir + string(os.PathSeparator) + "values.yaml"
	if err := os.WriteFile(path, []byte(values), 0o600); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("writing the values file: %w", err)
	}
	return path, cleanup, nil
}

// run executes one helm command against a cluster and returns what it said.
func (h *Helm) run(ctx context.Context, t Target, timeout time.Duration, args []string) (string, error) {
	// A little longer than helm's own --timeout, so that a command which runs
	// out of time reports helm's message about what it was waiting for rather
	// than this process killing it without one.
	ctx, cancel := context.WithTimeout(ctx, timeout+30*time.Second)
	defer cancel()

	args = append(args, "--namespace", t.Namespace)
	if t.Kubeconfig != "" {
		args = append(args, "--kubeconfig", t.Kubeconfig)
	}
	if t.Context != "" {
		args = append(args, "--kube-context", t.Context)
	}

	// #nosec G204 -- the program is the located helm; the arguments are this
	// package's own constants plus a release name, namespace and chart
	// reference already checked, and a path to a file written above. Nothing
	// goes through a shell.
	cmd := exec.CommandContext(ctx, h.path, args...)
	procattr.Hide(cmd)
	var out, errs bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errs

	if err := cmd.Run(); err != nil {
		return out.String(), commandError(err, errs.String())
	}
	return strings.TrimSpace(out.String()), nil
}

// commandError turns a failed run into the message worth showing.
//
// helm's own words, where it said anything: "cannot re-use a name that is still
// in use" or "UPGRADE FAILED: timed out waiting for the condition" is the
// answer to what went wrong, and no wording this app could invent would improve
// on it. The exit status is only reported when helm was silent.
func commandError(err error, stderr string) error {
	if text := strings.TrimSpace(stderr); text != "" {
		return fmt.Errorf("%s", text)
	}
	return fmt.Errorf("helm failed: %w", err)
}
