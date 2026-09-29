package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/k8sdockside/k8sdockside/internal/knownlist"
	"github.com/k8sdockside/k8sdockside/internal/plugins"
)

const listed = `[{
	"id": "brandnew", "name": "Brand new", "tagline": "t", "category": "platform", "description": "d",
	"repo": "https://github.com/k8sdockside/brandnew.git", "author": "K8s Dockside",
	"links": [{ "label": "Source", "url": "https://github.com/k8sdockside/brandnew" }]
}]`

func TestKnownListerFetchesKeepsAndFallsBack(t *testing.T) {
	t.Cleanup(func() { plugins.UseFetchedKnown(nil) })
	answer := listed
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("ETag", `"1"`)
		_, _ = w.Write([]byte(answer))
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "known-plugins.json")
	changes := 0
	l := newKnownLister(path, func() bool { return true }, func() { changes++ })
	l.fetcher.URL = srv.URL

	l.refresh(context.Background())
	if _, ok := plugins.FindKnown("brandnew"); !ok {
		t.Fatal("the fetched plugin is not offered")
	}
	if changes != 1 {
		t.Errorf("changed was called %d times, want 1", changes)
	}
	if st := l.status(true); st.Source != "fetched" || st.Error != "" {
		t.Errorf("status %+v", st)
	}
	if c, ok := knownlist.Load(path); !ok || c.ETag != `"1"` {
		t.Error("the accepted list was not kept on disk")
	}

	// A broken answer leaves the list, and the copy on disk, as they were.
	answer = `<html>oops</html>`
	l.refresh(context.Background())
	if _, ok := plugins.FindKnown("brandnew"); !ok {
		t.Error("a broken answer emptied the list")
	}
	if st := l.status(true); !strings.Contains(st.Error, "not a JSON list") {
		t.Errorf("the failure was not recorded: %+v", st)
	}
	if c, _ := knownlist.Load(path); !strings.Contains(string(c.Body), "brandnew") {
		t.Error("a broken answer overwrote the copy on disk")
	}

	// A new launch offers the copy on disk before any network.
	plugins.UseFetchedKnown(nil)
	next := newKnownLister(path, func() bool { return false }, func() {})
	ctx, cancel := context.WithCancel(context.Background())
	next.start(ctx)
	cancel()
	if _, ok := plugins.FindKnown("brandnew"); !ok {
		t.Error("the copy on disk was not used at launch")
	}
	_ = os.Remove(path)
}
