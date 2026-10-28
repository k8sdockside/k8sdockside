// Looking at a virtual machine's screens: its serial console, and a picture of
// its display.
//
// Both go through virt-api's subresources, the way virtctl's console and vnc
// commands do, and neither goes near the guest's network -- which is the point:
// a machine that never got an address, or is stuck in its bootloader, is
// exactly the one somebody needs to look at.
//
// The serial console is a websocket carrying the raw bytes of the guest's first
// serial port in both directions, under the "plain.kubevirt.io" subprotocol. It
// is not an exec: there is no remotecommand framing and no window size, so it
// is dialled here directly rather than through client-go's executors. The
// display is one GET that answers with a PNG, which is enough to read a
// bootloader, a kernel panic or a Talos dashboard without a VNC client.
package kube

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/coder/websocket"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"
)

// consoleProtocol is the subprotocol virt-api speaks on the console endpoint:
// binary frames, each one bytes of the serial port and nothing else.
const consoleProtocol = "plain.kubevirt.io"

// consoleReadLimit is the largest frame accepted from virt-api. The library's
// default is 32KiB; a guest dumping its boot log can send more in one frame.
const consoleReadLimit = 1 << 20

// VMConsole attaches to a virtual machine's serial console and blocks until
// either end hangs up. The name is the machine's, which is also its instance's.
//
// Sizes are not taken: a serial port has no window, and the guest's idea of
// its terminal is whatever `stty` was told inside it.
func (w *Watcher) VMConsole(ctx context.Context, kc Context, namespace, name string, stdin io.Reader, stdout io.Writer) error {
	return w.withClient(kc, func(c *clusterClient) error {
		return c.vmConsole(ctx, namespace, name, stdin, stdout)
	})
}

func (c *clusterClient) vmConsole(ctx context.Context, namespace, name string, stdin io.Reader, stdout io.Writer) error {
	url := c.typed.CoreV1().RESTClient().Get().
		AbsPath(subresourceGroup, "namespaces", namespace, "virtualmachineinstances", name, "console").
		URL()
	client, err := c.upgradingClient()
	if err != nil {
		return err
	}

	conn, resp, err := websocket.Dial(ctx, url.String(), &websocket.DialOptions{
		HTTPClient:   client,
		Subprotocols: []string{consoleProtocol},
	})
	if err != nil {
		return consoleError(name, resp, err)
	}
	conn.SetReadLimit(consoleReadLimit)
	// Closing the net.Conn closes the websocket, and cancelling ctx does both.
	stream := websocket.NetConn(ctx, conn, websocket.MessageBinary)
	defer func() { _ = stream.Close() }()

	go func() {
		_, _ = io.Copy(stream, stdin)
		// The user's side has gone -- the tab was closed. Hang up, so the
		// copy below returns and the console is free for the next person:
		// virt-api allows one connection to a serial port at a time.
		_ = stream.Close()
	}()

	_, err = io.Copy(stdout, stream)
	if err == nil || ctx.Err() != nil || errors.Is(err, io.EOF) {
		return nil
	}
	switch websocket.CloseStatus(err) {
	case websocket.StatusNormalClosure, websocket.StatusGoingAway:
		return nil
	}
	return fmt.Errorf("console of %s: %w", name, err)
}

// upgradingClient is an HTTP client that carries this cluster's credentials and
// can upgrade a request to a websocket.
//
// The one the rest config builds cannot: it negotiates HTTP/2 wherever the
// server offers it, and an HTTP/2 connection has no Upgrade. So the TLS
// settings are taken from the config, pinned to HTTP/1.1, and the config's
// wrappers -- bearer tokens, exec plugins, impersonation -- put back on top.
func (c *clusterClient) upgradingClient() (*http.Client, error) {
	tlsConfig, err := rest.TLSConfigFor(c.cfg)
	if err != nil {
		return nil, err
	}
	if tlsConfig == nil {
		tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	tlsConfig.NextProtos = []string{"http/1.1"}

	transport := &http.Transport{TLSClientConfig: tlsConfig, Proxy: http.ProxyFromEnvironment}
	if c.cfg.Proxy != nil {
		transport.Proxy = c.cfg.Proxy
	}
	if c.cfg.Dial != nil {
		transport.DialContext = c.cfg.Dial
	}
	wrapped, err := rest.HTTPWrappersForConfig(c.cfg, transport)
	if err != nil {
		return nil, err
	}
	return &http.Client{Transport: wrapped}, nil
}

// consoleError explains a console that would not open. The status of the
// refused upgrade says more than the library's message about it does.
func consoleError(name string, resp *http.Response, err error) error {
	if resp != nil {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		_ = resp.Body.Close()
		text := strings.TrimSpace(string(body))
		switch resp.StatusCode {
		case http.StatusNotFound:
			return fmt.Errorf("%s has no running instance to attach to -- start it first", name)
		case http.StatusForbidden:
			return fmt.Errorf("not allowed to open the console of %s -- that needs virtualmachineinstances/console in subresources.kubevirt.io", name)
		case http.StatusBadRequest, http.StatusConflict, http.StatusInternalServerError:
			// virt-api says why in the body: no serial console on this
			// machine, or somebody else already attached to it.
			if text != "" {
				return fmt.Errorf("console of %s: %s", name, text)
			}
		}
	}
	return kubevirtError("open the console of", name, err)
}

// VMScreenshot is a picture of a virtual machine's display, as a data URL the
// window can show as it is.
//
// moveCursor nudges the pointer first, which wakes a guest whose screen has
// blanked; virtctl does the same for its screenshot.
func (w *Watcher) VMScreenshot(kc Context, namespace, name string) (string, error) {
	var out string
	err := w.withClient(kc, func(c *clusterClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()

		png, err := c.typed.CoreV1().RESTClient().Get().
			AbsPath(subresourceGroup, "namespaces", namespace, "virtualmachineinstances", name, "vnc", "screenshot").
			Param("moveCursor", "true").
			DoRaw(ctx)
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("%s is not running -- there is no screen to show", name)
		}
		if err != nil {
			return kubevirtError("take a screenshot of", name, err)
		}
		out = "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
		return nil
	})
	return out, err
}
