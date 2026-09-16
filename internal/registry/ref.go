package registry

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// DockerHub is the name Docker Hub goes by in a Ref, whatever alias the
// reference used.
const DockerHub = "docker.io"

// Ref is an image reference taken apart the way a container runtime reads it
// -- the rules of github.com/distribution/reference, which containerd, CRI-O
// and Docker share, and which the example plugin's image-ref.ts follows too:
//
//	nginx                          docker.io / library/nginx : latest
//	ghcr.io/org/app:v1@sha256:...  ghcr.io / org/app : v1 @ sha256:...
//	localhost:5000/tools/cli       localhost:5000 / tools/cli : latest
type Ref struct {
	// Registry is the host, lowercased, with Docker Hub's aliases folded
	// into DockerHub.
	Registry string
	// Repository is the path in the registry, library/ included for Docker
	// Hub's official images.
	Repository string
	// Tag is the tag the runtime asks for: latest when none is written, and
	// empty when only a digest is.
	Tag string
	// Digest is the pinned digest, or empty.
	Digest string
}

// Key is the repository with its registry: docker.io/library/nginx.
func (r Ref) Key() string {
	return r.Registry + "/" + r.Repository
}

// Tagged is the key with the tag, when there is one: docker.io/library/nginx:1.27.
func (r Ref) Tagged() string {
	if r.Tag == "" {
		return r.Key()
	}
	return r.Key() + ":" + r.Tag
}

var hubAliases = map[string]bool{"index.docker.io": true, "registry-1.docker.io": true, "registry.hub.docker.com": true}

var (
	tagPattern       = regexp.MustCompile(`^[\w][\w.-]{0,127}$`)
	componentPattern = regexp.MustCompile(`^[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*$`)
	// hostPattern keeps anything that is not a host and port out of the
	// addresses built from a reference.
	hostPattern = regexp.MustCompile(`^(?:\[[0-9a-fA-F:.]+\]|[a-zA-Z0-9](?:[a-zA-Z0-9.-]*[a-zA-Z0-9])?)(?::[0-9]{1,5})?$`)
)

// Parse takes an image reference apart, refusing one a runtime would refuse.
func Parse(image string) (Ref, error) {
	name := strings.TrimSpace(image)
	if name == "" {
		return Ref{}, errors.New("no image is named")
	}
	if strings.ContainsAny(name, " \t\r\n") {
		return Ref{}, fmt.Errorf("%q has a space in it", image)
	}

	var ref Ref
	if at := strings.IndexByte(name, '@'); at >= 0 {
		ref.Digest = name[at+1:]
		name = name[:at]
		if !digestPattern.MatchString(ref.Digest) {
			return Ref{}, fmt.Errorf("%q is not a digest", ref.Digest)
		}
	}
	if colon := strings.LastIndexByte(name, ':'); colon > strings.LastIndexByte(name, '/') {
		ref.Tag = name[colon+1:]
		name = name[:colon]
		if !tagPattern.MatchString(ref.Tag) {
			return Ref{}, fmt.Errorf("%q is not a valid tag", ref.Tag)
		}
	}

	ref.Registry = DockerHub
	if first, rest, ok := strings.Cut(name, "/"); ok &&
		(strings.ContainsAny(first, ".:") || first == "localhost" || first != strings.ToLower(first)) {
		if !hostPattern.MatchString(first) {
			return Ref{}, fmt.Errorf("%q is not a registry host", first)
		}
		ref.Registry = strings.ToLower(first)
		if hubAliases[ref.Registry] {
			ref.Registry = DockerHub
		}
		name = rest
	}
	if ref.Registry == DockerHub && name != "" && !strings.Contains(name, "/") {
		name = "library/" + name
	}
	if name == "" {
		return Ref{}, fmt.Errorf("%q names a registry but no repository", image)
	}
	for part := range strings.SplitSeq(name, "/") {
		if !componentPattern.MatchString(part) {
			return Ref{}, fmt.Errorf("%q is not a valid repository name", name)
		}
	}
	ref.Repository = name

	if ref.Tag == "" && ref.Digest == "" {
		ref.Tag = "latest"
	}
	return ref, nil
}
