package knownlist

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFetchSendsTheETagAndReadsA304(t *testing.T) {
	var gotAgent, gotMatch string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAgent, gotMatch = r.UserAgent(), r.Header.Get("If-None-Match")
		if gotMatch == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte(`[{"id":"x"}]`))
	}))
	defer srv.Close()

	f := New("k8sdockside/test")
	f.URL = srv.URL
	first, err := f.Fetch(context.Background(), "")
	if err != nil || string(first.Body) != `[{"id":"x"}]` || first.ETag != `"v1"` || first.NotModified {
		t.Fatalf("first fetch: %+v, %v", first, err)
	}
	if gotAgent != "k8sdockside/test" || gotMatch != "" {
		t.Errorf("sent agent %q, If-None-Match %q", gotAgent, gotMatch)
	}
	second, err := f.Fetch(context.Background(), first.ETag)
	if err != nil || !second.NotModified || second.Body != nil || second.ETag != `"v1"` {
		t.Fatalf("second fetch: %+v, %v", second, err)
	}
}

func TestFetchRefusesFailuresAndHugeAnswers(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"a 404": func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) },
		"a 500": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
		"too big": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", maxSize+10)))
		},
	} {
		srv := httptest.NewServer(handler)
		f := New("t")
		f.URL = srv.URL
		if _, err := f.Fetch(context.Background(), ""); err == nil {
			t.Errorf("%s was accepted", name)
		}
		srv.Close()
	}
}

func TestCacheRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "known-plugins.json")
	if _, ok := Load(path); ok {
		t.Fatal("a missing file loaded")
	}
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	if err := Save(path, Cached{Body: []byte(`[1]`), ETag: `"e"`, FetchedAt: at}); err != nil {
		t.Fatal(err)
	}
	c, ok := Load(path)
	if !ok || string(c.Body) != `[1]` || c.ETag != `"e"` || !c.FetchedAt.Equal(at) {
		t.Fatalf("loaded %+v, %v", c, ok)
	}
}
