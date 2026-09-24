package kube

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// selfSigned makes a certificate that expires at notAfter, as PEM.
func selfSigned(t *testing.T, cn string, org []string, notAfter time.Time) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn, Organization: org},
		NotBefore:    notAfter.Add(-365 * 24 * time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func jwt(claims string) string {
	enc := base64.RawURLEncoding
	return enc.EncodeToString([]byte(`{"alg":"none"}`)) + "." + enc.EncodeToString([]byte(claims)) + ".sig"
}

func writeKubeconfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCheckCredentialsReadsTheKubeconfigsDates(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	client := selfSigned(t, "admin", []string{"system:masters"}, now.Add(10*24*time.Hour+time.Hour))
	ca := selfSigned(t, "kubernetes", nil, now.Add(3650*24*time.Hour))
	token := jwt(`{"sub":"system:serviceaccount:ci:deployer","exp":` + itoa(now.Add(-2*24*time.Hour).Unix()) + `}`)

	path := writeKubeconfig(t, `apiVersion: v1
kind: Config
clusters:
- name: prod
  cluster:
    server: https://127.0.0.1:6443
    certificate-authority-data: `+b64(ca)+`
users:
- name: admin
  user:
    client-certificate-data: `+b64(client)+`
    client-key-data: `+b64([]byte("unused"))+`
- name: ci
  user:
    token: `+token+`
- name: sso
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1
      command: kubelogin
contexts:
- name: admin@prod
  context: {cluster: prod, user: admin}
- name: ci@prod
  context: {cluster: prod, user: ci}
- name: sso@prod
  context: {cluster: prod, user: sso}
`)

	admin := checkCredentials(Context{ID: "a", Name: "admin@prod", File: path}, false, now)
	if admin.Error != "" {
		t.Fatal(admin.Error)
	}
	if len(admin.Items) != 2 {
		t.Fatalf("items = %+v", admin.Items)
	}
	// Soonest first: the client certificate before the CA.
	cert := admin.Items[0]
	if cert.Kind != CredentialClientCert || cert.DaysLeft != 10 || cert.Subject != "admin (system:masters)" {
		t.Errorf("client cert = %+v", cert)
	}
	if admin.Items[1].Kind != CredentialClusterCA || admin.Items[1].Subject != "kubernetes" {
		t.Errorf("ca = %+v", admin.Items[1])
	}

	ci := checkCredentials(Context{ID: "c", Name: "ci@prod", File: path}, false, now)
	if ci.Items[0].Kind != CredentialToken || ci.Items[0].DaysLeft != -2 || ci.Items[0].Subject != "system:serviceaccount:ci:deployer" {
		t.Errorf("token = %+v", ci.Items[0])
	}

	sso := checkCredentials(Context{ID: "s", Name: "sso@prod", File: path}, false, now)
	var exec *Credential
	for i := range sso.Items {
		if sso.Items[i].Kind == CredentialExec {
			exec = &sso.Items[i]
		}
	}
	if exec == nil || exec.Subject != "kubelogin" || exec.Note == "" || exec.NotAfter != "" {
		t.Errorf("exec = %+v", exec)
	}
	// Anything with a date comes before anything without one.
	if sso.Items[len(sso.Items)-1].Kind != CredentialExec {
		t.Errorf("order = %+v", sso.Items)
	}
}

func TestCheckCredentialsReadsTheAPIServersCertificate(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})

	path := writeKubeconfig(t, `apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    server: `+server.URL+`
    certificate-authority-data: `+b64(ca)+`
users:
- name: u
  user:
    token: not-a-jwt
contexts:
- name: u@test
  context: {cluster: test, user: u}
`)

	got := CheckCredentials(Context{ID: "t", Name: "u@test", File: path}, true)
	var found *Credential
	for i := range got.Items {
		if got.Items[i].Kind == CredentialServerCert {
			found = &got.Items[i]
		}
	}
	if found == nil {
		t.Fatalf("no server certificate in %+v", got.Items)
	}
	if found.Error != "" || found.NotAfter == "" {
		t.Errorf("server cert = %+v", found)
	}
	want := server.Certificate().NotAfter.UTC().Format(time.RFC3339)
	if found.NotAfter != want {
		t.Errorf("notAfter = %s, want %s", found.NotAfter, want)
	}
}

func TestCheckCredentialsSaysWhenTheContextIsMissing(t *testing.T) {
	path := writeKubeconfig(t, "apiVersion: v1\nkind: Config\n")
	got := CheckCredentials(Context{ID: "x", Name: "gone", File: path}, false)
	if !strings.Contains(got.Error, `"gone"`) {
		t.Errorf("error = %q", got.Error)
	}
}

func TestDaysLeftRoundsDown(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	cases := map[time.Duration]int{
		6 * time.Hour:       0,
		36 * time.Hour:      1,
		-6 * time.Hour:      -1,
		-48 * time.Hour:     -2,
		30 * 24 * time.Hour: 30,
	}
	for d, want := range cases {
		if got := daysLeft(now.Add(d), now); got != want {
			t.Errorf("daysLeft(%s) = %d, want %d", d, got, want)
		}
	}
}

func itoa(n int64) string { return big.NewInt(n).String() }
