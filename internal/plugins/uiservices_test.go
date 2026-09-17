package plugins

import (
	"path/filepath"
	"strings"
	"testing"
)

// The services a plugin's views may call are where a request can go, so the
// declaration is held to exactly what the app will send.

func TestAServiceDeclarationIsCheckedWhenTheFileIsRead(t *testing.T) {
	const views = `"views": [{ "id": "flows", "label": "Flows", "type": "custom" }]`
	for name, tc := range map[string]struct {
		service string
		refused string
	}{
		"by name":                  {`{ "id": "whisker", "namespace": "calico-system", "name": "whisker", "port": 8081, "paths": ["/whisker-backend/"] }`, ""},
		"by selector":              {`{ "id": "whisker", "selector": "k8s-app=whisker", "port": "http", "paths": ["/api"] }`, ""},
		"https and anywhere":       {`{ "id": "ui", "selector": "k8s-app=hubble-ui", "port": "443", "scheme": "HTTPS", "paths": ["/"] }`, ""},
		"no id":                    {`{ "name": "whisker", "namespace": "x", "port": 80, "paths": ["/"] }`, "service with id"},
		"nowhere":                  {`{ "id": "w", "port": 80, "paths": ["/"] }`, "give a selector"},
		"both":                     {`{ "id": "w", "namespace": "x", "name": "w", "selector": "a=b", "port": 80, "paths": ["/"] }`, "not both"},
		"a name alone":             {`{ "id": "w", "name": "whisker", "port": 80, "paths": ["/"] }`, "needs its namespace"},
		"a bad name":               {`{ "id": "w", "namespace": "x", "name": "Whisker", "port": 80, "paths": ["/"] }`, "not a Service name"},
		"a bad namespace":          {`{ "id": "w", "namespace": "x_y", "selector": "a=b", "port": 80, "paths": ["/"] }`, "not a namespace name"},
		"a bad selector":           {`{ "id": "w", "selector": "a in b", "port": 80, "paths": ["/"] }`, "label selector"},
		"an empty selector":        {`{ "id": "w", "selector": " ", "port": 80, "paths": ["/"] }`, "give a selector"},
		"no port":                  {`{ "id": "w", "selector": "a=b", "paths": ["/"] }`, "name the port"},
		"a port out of range":      {`{ "id": "w", "selector": "a=b", "port": 70000, "paths": ["/"] }`, "out of range"},
		"a bad port name":          {`{ "id": "w", "selector": "a=b", "port": "a_very_long_port_name", "paths": ["/"] }`, "not a port name"},
		"a port of the wrong type": {`{ "id": "w", "selector": "a=b", "port": true, "paths": ["/"] }`, "should be a port name or number"},
		"another scheme":           {`{ "id": "w", "selector": "a=b", "port": 80, "scheme": "ftp", "paths": ["/"] }`, "neither http nor https"},
		"no paths":                 {`{ "id": "w", "selector": "a=b", "port": 80, "paths": [] }`, "list the paths"},
		"a relative path":          {`{ "id": "w", "selector": "a=b", "port": 80, "paths": ["api/"] }`, "not a clean absolute path"},
		"a dot segment":            {`{ "id": "w", "selector": "a=b", "port": 80, "paths": ["/api/../admin"] }`, "not a clean absolute path"},
		"an escape":                {`{ "id": "w", "selector": "a=b", "port": 80, "paths": ["/api%2f"] }`, "not a clean absolute path"},
		"a query":                  {`{ "id": "w", "selector": "a=b", "port": 80, "paths": ["/api?x=1"] }`, "not a clean absolute path"},
		"an unknown field":         {`{ "id": "w", "selector": "a=b", "port": 80, "paths": ["/"], "method": "POST" }`, `unknown field "method"`},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, filepath.Join(dir, "flows"), "plugin.json", `{ "id": "flows", "name": "Flows", "ui": { "services": [`+tc.service+`] }, `+views+` }`)
			write(t, filepath.Join(dir, "flows", "ui"), "index.html", "<p>flows</p>")
			cat := Load(dir, nil, nil)
			if tc.refused == "" {
				if len(cat.Problems) > 0 {
					t.Fatalf("refused: %v", cat.Problems)
				}
				return
			}
			if len(cat.Problems) != 1 || !strings.Contains(cat.Problems[0].Message, tc.refused) {
				t.Errorf("problems %v, want one saying %q", cat.Problems, tc.refused)
			}
		})
	}
}

func TestAServiceIsReadWithItsDefaults(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "flows"), "plugin.json", `{
    "id": "flows",
    "name": "Flows",
    "ui": { "services": [
        { "id": "whisker", "selector": "k8s-app=whisker", "port": 8081, "paths": [" /whisker-backend/ "] },
        { "id": "relay", "label": "Hubble Relay", "namespace": "kube-system", "name": "hubble-relay", "port": "grpc", "paths": ["/"] }
    ] },
    "views": [{ "id": "flows", "label": "Flows", "type": "custom" }]
}`)
	write(t, filepath.Join(dir, "flows", "ui"), "index.html", "<p>flows</p>")
	cat := Load(dir, nil, nil)
	if len(cat.Problems) > 0 {
		t.Fatalf("problems: %v", cat.Problems)
	}
	p, _ := cat.Find("flows")
	whisker, ok := p.Service("whisker")
	if !ok {
		t.Fatal("the whisker service is not found by its id")
	}
	if whisker.Port != "8081" || whisker.Scheme != SchemeHTTP || whisker.Label != "whisker" || whisker.Paths[0] != "/whisker-backend/" {
		t.Errorf("whisker read as %+v", whisker)
	}
	if got := whisker.Describe(); got != "the service labelled k8s-app=whisker:8081" {
		t.Errorf("described as %q", got)
	}
	relay, _ := p.Service("relay")
	if relay.Label != "Hubble Relay" || relay.Describe() != "kube-system/hubble-relay:grpc" {
		t.Errorf("relay read as %+v, described as %q", relay, relay.Describe())
	}
	if _, ok := p.Service("prometheus"); ok {
		t.Error("an undeclared service was found")
	}
}

func TestTwoServicesMayNotShareAnID(t *testing.T) {
	_, err := validateServices("flows", []UIService{
		{ID: "w", Selector: "a=b", Port: "80", Paths: []string{"/"}},
		{ID: "w", Selector: "c=d", Port: "80", Paths: []string{"/"}},
	})
	if err == nil || !strings.Contains(err.Error(), `two services with id "w"`) {
		t.Errorf("error %v, want the duplicate named", err)
	}
}

// A prefix is a whole number of path segments, and a path is taken only as it
// would be read at the other end.
func TestAServiceAllowsOnlyPathsUnderItsPrefixes(t *testing.T) {
	svc := UIService{Paths: []string{"/api", "/whisker-backend/"}}
	for path, want := range map[string]bool{
		"/api":                              true,
		"/api/":                             true,
		"/api/v1/flows":                     true,
		"/api/v1/flows/":                    true,
		"/apikeys":                          false,
		"/whisker-backend/":                 true,
		"/whisker-backend/flows":            true,
		"/whisker-backend":                  false,
		"/":                                 false,
		"":                                  false,
		"api/v1":                            false,
		"/api/../admin":                     false,
		"/api/./flows":                      false,
		"/api//flows":                       false,
		"/api/%2e%2e/admin":                 false,
		"/api/flows?x=1":                    false,
		"/api/flows#top":                    false,
		"/api/fl ows":                       false,
		"/api/flows\n":                      false,
		`/api\..\admin`:                     false,
		"/api/" + strings.Repeat("a", 1100): false,
	} {
		if got := svc.Allows(path); got != want {
			t.Errorf("Allows(%q) = %v, want %v", path, got, want)
		}
	}

	everything := UIService{Paths: []string{"/"}}
	for path, want := range map[string]bool{"/": true, "/anything/at/all": true, "/a/../b": false, "//host": false} {
		if got := everything.Allows(path); got != want {
			t.Errorf("with /: Allows(%q) = %v, want %v", path, got, want)
		}
	}
}
