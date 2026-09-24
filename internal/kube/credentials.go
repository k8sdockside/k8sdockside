package kube

// When a context's credentials stop working.
//
// An admin kubeconfig made by kubeadm, Talos or k3s carries a client
// certificate that expires -- a year is the common default -- and nothing warns
// before it does: one morning every tab on that cluster fails with
// "Unauthorized" and the only fix is a new kubeconfig from whoever can still
// get in. So this reads the dates off the kubeconfig and off the API server's
// own certificate, which is the other thing that expires on its own.
//
// Nothing here runs an exec plugin or an auth provider. The kubeconfig is read
// as a file, and the server's certificate is read from a TLS handshake made
// with no client credentials at all: a server shows its certificate before it
// asks for one, so the handshake needs nothing that could prompt for a login.

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// The kinds of credential a context is checked for. They are the words the
// report shows, not credentials, whatever their names suggest to a scanner.
const (
	CredentialClientCert = "Client certificate"     // #nosec G101 -- a label, not a secret
	CredentialClusterCA  = "Cluster CA"             // #nosec G101 -- a label, not a secret
	CredentialToken      = "Token"                  // #nosec G101 -- a label, not a secret
	CredentialServerCert = "API server certificate" // #nosec G101 -- a label, not a secret
	CredentialExec       = "Login plugin"           // #nosec G101 -- a label, not a secret
)

// serverCertTimeout bounds the handshake that reads the API server's
// certificate. It is one round trip; a server that has not answered in this
// long is not going to, and the rest of the report does not wait for it.
const serverCertTimeout = 5 * time.Second

// Credential is one thing about a context that expires, or that cannot be
// told to.
type Credential struct {
	// Kind is what it is: one of the Credential* constants.
	Kind string `json:"kind"`
	// Subject names it -- a certificate's subject, a token's user -- so two
	// of the same kind can be told apart.
	Subject string `json:"subject"`
	// NotAfter is when it stops working, RFC3339, and empty when it does not
	// say. DaysLeft is the same as a count, negative once it has passed.
	NotAfter string `json:"notAfter"`
	DaysLeft int    `json:"daysLeft"`
	// Note is said instead of a date when there is none to give: a login
	// plugin mints its own credentials, and when they end is its business.
	Note string `json:"note"`
	// Error is why this one could not be read.
	Error string `json:"error"`
}

// Credentials is every expiring thing about one context, soonest first.
type Credentials struct {
	ContextID string       `json:"contextId"`
	Items     []Credential `json:"items"`
	// Error is why the kubeconfig could not be read at all.
	Error string `json:"error"`
}

// CheckCredentials reads when a context's credentials expire. With server
// set it also reads the API server's certificate, which is the one part that
// goes over the network.
func CheckCredentials(kc Context, server bool) Credentials {
	return checkCredentials(kc, server, time.Now())
}

func checkCredentials(kc Context, server bool, now time.Time) Credentials {
	out := Credentials{ContextID: kc.ID, Items: []Credential{}}

	rules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kc.File}
	raw, err := rules.Load()
	if err != nil {
		out.Error = err.Error()
		return out
	}
	named, ok := raw.Contexts[kc.Name]
	if !ok {
		out.Error = fmt.Sprintf("context %q is not in %s", kc.Name, kc.File)
		return out
	}

	if user, ok := raw.AuthInfos[named.AuthInfo]; ok {
		out.Items = append(out.Items, userCredentials(user, now)...)
	}
	if cluster, ok := raw.Clusters[named.Cluster]; ok {
		out.Items = append(out.Items, clusterCredentials(cluster, now)...)
	}

	if server {
		out.Items = append(out.Items, serverCertificate(kc, now))
	}

	sortCredentials(out.Items)
	return out
}

// sortCredentials puts the soonest to expire first, and those with no date
// after every one that has.
func sortCredentials(items []Credential) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if (a.NotAfter == "") != (b.NotAfter == "") {
			return a.NotAfter != ""
		}
		return a.NotAfter < b.NotAfter
	})
}

func userCredentials(user *clientcmdapi.AuthInfo, now time.Time) []Credential {
	var out []Credential

	if pemData, err := fileOrData(user.ClientCertificateData, user.ClientCertificate); err != nil {
		out = append(out, Credential{Kind: CredentialClientCert, Error: err.Error()})
	} else if len(pemData) > 0 {
		out = append(out, certificates(CredentialClientCert, pemData, now, true)...)
	}

	token := user.Token
	if token == "" && user.TokenFile != "" {
		// #nosec G304 -- the path is the one the user's own kubeconfig names.
		if b, err := os.ReadFile(user.TokenFile); err == nil {
			token = strings.TrimSpace(string(b))
		} else {
			out = append(out, Credential{Kind: CredentialToken, Error: err.Error()})
		}
	}
	if token != "" {
		out = append(out, tokenCredential(token, now))
	}

	if user.Exec != nil {
		out = append(out, Credential{
			Kind:    CredentialExec,
			Subject: user.Exec.Command,
			Note:    "Credentials are issued by the plugin each time, and it decides when they end.",
		})
	} else if user.AuthProvider != nil {
		out = append(out, Credential{
			Kind:    CredentialExec,
			Subject: user.AuthProvider.Name,
			Note:    "Credentials come from the auth provider, which decides when they end.",
		})
	}
	return out
}

func clusterCredentials(cluster *clientcmdapi.Cluster, now time.Time) []Credential {
	pemData, err := fileOrData(cluster.CertificateAuthorityData, cluster.CertificateAuthority)
	if err != nil {
		return []Credential{{Kind: CredentialClusterCA, Error: err.Error()}}
	}
	if len(pemData) == 0 {
		return nil
	}
	return certificates(CredentialClusterCA, pemData, now, false)
}

// fileOrData is a kubeconfig's embedded bytes when it has them and the file it
// names when it does not -- the same order clientcmd itself reads them in.
func fileOrData(data []byte, path string) ([]byte, error) {
	if len(data) > 0 {
		return data, nil
	}
	if path == "" {
		return nil, nil
	}
	// #nosec G304 -- the path is the one the user's own kubeconfig names.
	return os.ReadFile(path)
}

// certificates reads every certificate in a PEM bundle. first keeps only the
// leaf: a client certificate file may carry its chain after it, and the chain
// is not what stops the user logging in.
func certificates(kind string, pemData []byte, now time.Time, first bool) []Credential {
	var out []Credential
	for rest := pemData; ; {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			out = append(out, Credential{Kind: kind, Error: err.Error()})
			continue
		}
		out = append(out, certCredential(kind, cert, now))
		if first {
			break
		}
	}
	if len(out) == 0 {
		out = append(out, Credential{Kind: kind, Error: "no certificate found in the PEM data"})
	}
	return out
}

func certCredential(kind string, cert *x509.Certificate, now time.Time) Credential {
	subject := cert.Subject.CommonName
	if subject == "" {
		subject = cert.Subject.String()
	}
	if len(cert.Subject.Organization) > 0 && kind == CredentialClientCert {
		subject += " (" + strings.Join(cert.Subject.Organization, ", ") + ")"
	}
	return Credential{
		Kind:     kind,
		Subject:  subject,
		NotAfter: cert.NotAfter.UTC().Format(time.RFC3339),
		DaysLeft: daysLeft(cert.NotAfter, now),
	}
}

// daysLeft counts whole days to a moment, rounding down, so a certificate
// with six hours left says 0 rather than 1.
func daysLeft(at, now time.Time) int {
	d := at.Sub(now)
	days := int(d / (24 * time.Hour))
	if d < 0 && d%(24*time.Hour) != 0 {
		days--
	}
	return days
}

// tokenCredential reads a bearer token's expiry when it is a JWT, which is
// what a service account token is. The signature is not checked: nothing is
// being trusted here, only a date being read that the API server will check
// for itself.
func tokenCredential(token string, now time.Time) Credential {
	out := Credential{Kind: CredentialToken}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		out.Note = "Not a JWT, so it carries no expiry of its own."
		return out
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		out.Note = "Not a JWT, so it carries no expiry of its own."
		return out
	}
	var claims struct {
		Sub string  `json:"sub"`
		Exp float64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		out.Note = "Not a JWT, so it carries no expiry of its own."
		return out
	}
	out.Subject = claims.Sub
	if claims.Exp == 0 {
		out.Note = "The token has no expiry."
		return out
	}
	at := time.Unix(int64(claims.Exp), 0)
	out.NotAfter = at.UTC().Format(time.RFC3339)
	out.DaysLeft = daysLeft(at, now)
	return out
}

// serverCertificate reads the certificate the API server presents, through a
// handshake that carries no credentials of the user's and verifies against the
// kubeconfig's own CA -- see the note at the top of this file.
func serverCertificate(kc Context, now time.Time) Credential {
	out := Credential{Kind: CredentialServerCert}

	rules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kc.File}
	overrides := &clientcmd.ConfigOverrides{CurrentContext: kc.Name}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
	if err != nil {
		out.Error = err.Error()
		return out
	}
	anonymous := rest.AnonymousClientConfig(cfg)
	tlsConfig, err := rest.TLSConfigFor(anonymous)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	if tlsConfig == nil {
		out.Note = "The API server is not reached over TLS."
		return out
	}

	host, err := hostPort(cfg.Host)
	if err != nil {
		out.Error = err.Error()
		return out
	}

	ctx, cancel := context.WithTimeout(context.Background(), serverCertTimeout)
	defer cancel()
	dialer := &tls.Dialer{NetDialer: &net.Dialer{Timeout: serverCertTimeout}, Config: tlsConfig}
	conn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	defer func() { _ = conn.Close() }()

	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		out.Error = "not a TLS connection"
		return out
	}
	peers := tlsConn.ConnectionState().PeerCertificates
	if len(peers) == 0 {
		out.Error = "the API server presented no certificate"
		return out
	}
	found := certCredential(CredentialServerCert, peers[0], now)
	return found
}

// hostPort turns a kubeconfig server URL into the address to dial.
func hostPort(server string) (string, error) {
	u, err := url.Parse(server)
	if err != nil {
		return "", err
	}
	if u.Host == "" {
		return "", errors.New("the server has no host")
	}
	if u.Port() != "" {
		return u.Host, nil
	}
	return net.JoinHostPort(u.Hostname(), "443"), nil
}
