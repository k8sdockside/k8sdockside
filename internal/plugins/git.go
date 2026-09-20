package plugins

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// A plugin kept in a repository of its own is installed by cloning it into the
// plugins folder, and updated by pulling. Nothing more clever than that: the
// clone is an ordinary folder the loader reads like any other, so a plugin
// installed this way and one copied in by hand are the same thing afterwards,
// and `git` itself is the version manager.
//
// The repository's plugin.json has to be at its root, because the loader reads
// one level into the plugins folder and no deeper.

// gitTimeout bounds a clone or a pull. A plugin is a manifest and some static
// files; one that takes longer than this is not one.
const gitTimeout = 2 * time.Minute

// gitURL is what a repository address may look like: https, ssh, or the scp
// form git@host:owner/repo. Deliberately not file://, ext:: or anything else
// git would accept -- those run things, or read things, this app should not.
var gitURL = regexp.MustCompile(`^(https://[^\s]+|ssh://[^\s]+|[A-Za-z0-9._-]+@[A-Za-z0-9.-]+:[^\s]+)$`)

// ValidGitURL reports whether a repository address is one this app will clone.
func ValidGitURL(url string) bool {
	return gitURL.MatchString(url) && !strings.HasPrefix(url, "-")
}

// RepoFolder is the folder a repository is cloned into: its last path element,
// without .git, kept to what a plugin id may contain.
func RepoFolder(url string) (string, error) {
	url = strings.TrimSuffix(strings.TrimRight(url, "/"), ".git")
	at := strings.LastIndexAny(url, "/:")
	name := strings.ToLower(url[at+1:])
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		case r == '_' || r == '.':
			b.WriteRune('-')
		}
	}
	folder := strings.Trim(b.String(), "-")
	if folder == "" {
		return "", fmt.Errorf("cannot tell what to call the folder for %s", url)
	}
	return folder, nil
}

// gitPath finds git, with the reason in words when it cannot.
func gitPath() (string, error) {
	path, err := exec.LookPath("git")
	if err != nil {
		return "", errors.New("installing a plugin from a repository needs git, and git is not on this machine's PATH")
	}
	return path, nil
}

func runGit(dir string, args ...string) error {
	_, err := gitOutput(dir, args...)
	return err
}

// gitOutput runs git and returns what it printed.
func gitOutput(dir string, args ...string) (string, error) {
	git, err := gitPath()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, git, args...) // #nosec G204 -- the URL is checked by ValidGitURL and passed after --
	cmd.Dir = dir
	// Never stop to ask for a password: there is no terminal to ask in, and a
	// clone waiting on one would hang until the timeout.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=")
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if ctx.Err() != nil {
		return "", fmt.Errorf("git %s gave up after %s -- the repository is slow to answer, or this machine cannot reach it\n%s", args[0], gitTimeout, text)
	}
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return "", fmt.Errorf("%s\n%s", gitReason(args[0], text), text)
	}
	return text, nil
}

// gitReason is the one line that says what went wrong and what to do about
// it. The status bar has room for a single line and shows the first one, and
// git's own first line is progress -- "Cloning into 'descheduler'..." -- with
// the reason several lines below it. So the explanation goes first here and
// git's whole output follows underneath, where Settings shows it.
func gitReason(verb, text string) string {
	low := strings.ToLower(text)
	has := func(s string) bool { return strings.Contains(low, s) }
	switch {
	// A repository that is private and one that is not there answer the same
	// way: the host asks who is asking, and there is nobody here to ask. Worth
	// saying both, because the address looking right proves nothing.
	case has("could not read username"), has("could not read password"),
		has("terminal prompts disabled"), has("authentication failed"),
		has("repository not found"), has("access rights"):
		return "the repository is private, or is not there -- a host answers both the same way. For a private one you have a key for, install it from its ssh address instead (git@github.com:owner/repo.git)"
	case has("permission denied (publickey"), has("host key verification failed"):
		return "the repository refused this machine's ssh key -- add the key to the account that can read it, or use the https address if the repository is public"
	case has("could not resolve host"), has("name or service not known"):
		return "could not look up the host -- this machine looks to be offline, or behind a proxy git does not know about"
	case has("connection timed out"), has("connection refused"), has("failed to connect"), has("network is unreachable"):
		return "could not reach the host -- this machine looks to be offline, or behind a proxy git does not know about"
	case has("ssl certificate problem"), has("certificate verify failed"):
		return "the connection was refused over its certificate -- a proxy in the way is the usual cause"
	case has("not possible to fast-forward"), has("divergent branches"), has("local changes"), has("would be overwritten"):
		return "this clone has changes of its own, so it cannot be fast-forwarded -- yours to keep or to throw away with git reset --hard in its folder"
	case has("already exists and is not an empty directory"):
		return "the folder it clones into is already there with something else in it -- remove it first"
	}
	return fmt.Sprintf("git %s: %s", verb, gitSaid(text))
}

// gitSaid picks the telling line out of git's output: the last of the lines it
// marks as the trouble, or failing that the last line at all.
func gitSaid(text string) string {
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "fatal:") || strings.HasPrefix(line, "error:") || strings.HasPrefix(line, "remote:") {
			return line
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return text
}

// Clone installs a plugin repository into dir and returns the folder it made.
// A folder already there is left alone: that is Update's job, and cloning over
// it would throw away whatever is in it.
func Clone(dir, url string) (string, error) {
	url = strings.TrimSpace(url)
	if !ValidGitURL(url) {
		return "", fmt.Errorf("%q is not a repository address this app will clone -- use https://, ssh:// or git@host:owner/repo", url)
	}
	folder, err := RepoFolder(url)
	if err != nil {
		return "", err
	}
	if err := EnsureDir(dir); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, folder)
	if _, err := os.Stat(dest); err == nil {
		return "", fmt.Errorf("%s is already there -- update it instead, or remove the folder first", dest)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if err := runGit(dir, "clone", "--depth", "1", "--", url, folder); err != nil {
		return "", err
	}
	return dest, nil
}

// Install puts a plugin repository in dir: a fresh clone, or -- when the
// folder it would clone into is already a clone of that same repository --
// that clone brought up to date. Returns the folder.
//
// The second case is not rare. A repository cloned before its plugin was
// pushed holds no plugin.json, so nothing loads from it, so the plugin is
// offered for installing again -- and a clone that refused because the folder
// is taken would leave no way forward in the app at all. A folder of that
// name holding anything else is still left alone.
func Install(dir, url string) (string, error) {
	url = strings.TrimSpace(url)
	if !ValidGitURL(url) {
		return "", fmt.Errorf("%q is not a repository address this app will clone -- use https://, ssh:// or git@host:owner/repo", url)
	}
	folder, err := RepoFolder(url)
	if err != nil {
		return "", err
	}
	dest := filepath.Join(dir, folder)
	if info, err := os.Stat(filepath.Join(dest, ".git")); err == nil && info.IsDir() {
		origin, err := OriginOf(dest)
		if err != nil {
			return "", fmt.Errorf("%s is already there, and which repository it came from could not be read: %w", dest, err)
		}
		if !SameRepository(origin, url) {
			return "", fmt.Errorf("%s is already there, cloned from %s rather than %s -- remove the folder first", dest, origin, url)
		}
		if err := Pull(dest); err != nil {
			return "", err
		}
		return dest, nil
	}
	return Clone(dir, url)
}

// OriginOf is the address a clone was made from.
func OriginOf(repo string) (string, error) {
	return gitOutput(repo, "config", "--get", "remote.origin.url")
}

// SameRepository reports whether two addresses name one repository, however
// each is spelt: https, ssh:// or git@host:path, with or without .git or a
// trailing slash, in any case.
func SameRepository(a, b string) bool {
	ka, kb := repoKey(a), repoKey(b)
	return ka != "" && ka == kb
}

// repoKey reduces a repository address to host/path.
func repoKey(address string) string {
	address = strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(address), "/"), ".git")
	var host, path string
	if strings.Contains(address, "://") {
		parsed, err := neturl.Parse(address)
		if err != nil {
			return ""
		}
		host, path = parsed.Hostname(), parsed.Path
	} else {
		at := strings.Index(address, "@")
		colon := strings.Index(address, ":")
		if at < 0 || colon < at {
			return ""
		}
		host, path = address[at+1:colon], address[colon+1:]
	}
	path = strings.Trim(path, "/")
	if host == "" || path == "" {
		return ""
	}
	return strings.ToLower(host + "/" + path)
}

// RepoOf is the repository a plugin's file is in, if it is in one: its own
// folder, or the folder above it for a pack kept in a subfolder.
func RepoOf(p Plugin) (string, bool) {
	if p.Builtin() || p.Origin == "" {
		return "", false
	}
	dir := filepath.Dir(p.Origin)
	for range 2 {
		if info, err := os.Stat(filepath.Join(dir, ".git")); err == nil && info.IsDir() {
			return dir, true
		}
		dir = filepath.Dir(dir)
	}
	return "", false
}

// Pull updates a cloned plugin to what its repository has now. Fast-forward
// only: a clone someone has edited in place is theirs, and merging into it is
// not a decision to make for them.
func Pull(repo string) error {
	return runGit(repo, "pull", "--ff-only")
}
