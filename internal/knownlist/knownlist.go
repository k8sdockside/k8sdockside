// Package knownlist fetches the list of plugins the app knows of from the
// app's own repository, and keeps the last good copy on disk.
//
// It is the second of the app's own requests to anything but the user's
// clusters, beside package updates, and it is kept as plain: one
// unauthenticated GET of a public file, carrying a User-Agent naming the app
// and the ETag of the copy already held, so an unchanged list costs GitHub a
// 304 and the app nothing. What the file says is package plugins' business;
// this only fetches and keeps it.
package knownlist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// URL is known.json on the main branch: the file compiled into the app, as it
// stands now. Adding a plugin there is what lists it.
const URL = "https://raw.githubusercontent.com/k8sdockside/k8sdockside/main/internal/plugins/known.json"

// maxSize is the most read of an answer. The list is a few tens of kilobytes;
// anything near a megabyte is not the list.
const maxSize = 1 << 20

// Fetcher fetches the list. Use New rather than the zero value.
type Fetcher struct {
	Client    *http.Client
	URL       string
	UserAgent string
}

// New returns a fetcher for URL that names the app and its version.
func New(userAgent string) *Fetcher {
	return &Fetcher{
		Client:    &http.Client{Timeout: 15 * time.Second},
		URL:       URL,
		UserAgent: userAgent,
	}
}

// Answer is what one fetch found.
type Answer struct {
	// Body is the file, nil when NotModified.
	Body []byte
	// ETag identifies this copy, for asking next time.
	ETag string
	// NotModified is true when the copy held (the etag given) is still current.
	NotModified bool
}

// Fetch asks for the list, sending etag when a copy is already held.
func (f *Fetcher) Fetch(ctx context.Context, etag string) (Answer, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.URL, nil)
	if err != nil {
		return Answer{}, err
	}
	req.Header.Set("User-Agent", f.UserAgent)
	req.Header.Set("Accept", "application/json, text/plain")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := f.Client.Do(req)
	if err != nil {
		return Answer{}, fmt.Errorf("reaching GitHub for the plugin list: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusNotModified:
		return Answer{ETag: etag, NotModified: true}, nil
	case http.StatusOK:
	default:
		return Answer{}, fmt.Errorf("GitHub answered %s for the plugin list", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSize+1))
	if err != nil {
		return Answer{}, fmt.Errorf("reading the plugin list: %w", err)
	}
	if len(body) > maxSize {
		return Answer{}, errors.New("the plugin list is larger than any list would be; ignored")
	}
	return Answer{Body: body, ETag: resp.Header.Get("ETag")}, nil
}

// Cached is the last good copy, as kept on disk.
type Cached struct {
	Body      []byte    `json:"body"`
	ETag      string    `json:"etag,omitempty"`
	FetchedAt time.Time `json:"fetchedAt"`
}

// Load reads the copy kept at path. A missing or unreadable file is no copy.
func Load(path string) (Cached, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Cached{}, false
	}
	var c Cached
	if err := json.Unmarshal(data, &c); err != nil || len(c.Body) == 0 {
		return Cached{}, false
	}
	return c, true
}

// Save keeps a copy at path, written to a temporary file first so a crash
// mid-write leaves the old copy rather than half a new one.
func Save(path string, c Cached) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
