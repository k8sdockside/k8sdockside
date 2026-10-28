package kube

// A keyboard for a virtual machine's display.
//
// The display itself is read as screenshots (see VMScreenshot), which is all
// that seeing it takes. Typing into it is another matter: the only way in is
// the VNC server QEMU runs for the guest, which virt-api serves as a websocket
// on the vnc subresource -- what `virtctl vnc` connects a viewer to.
//
// So this speaks just enough of RFB (RFC 6143) to type: the handshake, then
// KeyEvent messages, and nothing else. It never asks for the framebuffer, so
// the server never sends one; what little it does send unasked -- a bell,
// clipboard text -- is read and dropped so the connection stays in step. A
// whole VNC viewer would mean carrying every frame across the bridge to the
// window, to draw what the screenshots already show.
//
// It joins as a shared client, so it does not throw off a `virtctl vnc`
// somebody already has open.

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"

	"github.com/coder/websocket"
)

// VMKey is one key going down or coming up, as an X11 keysym -- what RFB
// carries. The window turns a KeyboardEvent into one.
type VMKey struct {
	Keysym uint32 `json:"keysym"`
	Down   bool   `json:"down"`
}

// VMKeyboard holds a VNC connection open and types into it. Send is safe to
// call from several goroutines; Close ends it.
type VMKeyboard struct {
	conn net.Conn
	out  chan []byte
	done chan struct{}
	err  error
}

// OpenVMKeyboard connects to a virtual machine's VNC server and completes the
// handshake. The connection lives until ctx is cancelled or Close is called.
func (w *Watcher) OpenVMKeyboard(ctx context.Context, kc Context, namespace, name string) (*VMKeyboard, error) {
	var kb *VMKeyboard
	err := w.withClient(kc, func(c *clusterClient) error {
		var err error
		kb, err = c.openVMKeyboard(ctx, namespace, name)
		return err
	})
	return kb, err
}

func (c *clusterClient) openVMKeyboard(ctx context.Context, namespace, name string) (*VMKeyboard, error) {
	url := c.typed.CoreV1().RESTClient().Get().
		AbsPath(subresourceGroup, "namespaces", namespace, "virtualmachineinstances", name, "vnc").
		URL()
	client, err := c.upgradingClient()
	if err != nil {
		return nil, err
	}
	ws, resp, err := websocket.Dial(ctx, url.String(), &websocket.DialOptions{
		HTTPClient:   client,
		Subprotocols: []string{consoleProtocol},
	})
	if err != nil {
		return nil, vncError(name, consoleError(name, resp, err))
	}
	ws.SetReadLimit(consoleReadLimit)
	conn := websocket.NetConn(ctx, ws, websocket.MessageBinary)

	in := bufio.NewReader(conn)
	if err := rfbHandshake(in, conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("the display of %s: %w", name, err)
	}

	kb := &VMKeyboard{conn: conn, out: make(chan []byte, 256), done: make(chan struct{})}
	go kb.drain(in)
	go kb.write()
	return kb, nil
}

// vncError rewords the console's errors for the display: the same endpoints
// refuse for the same reasons.
func vncError(name string, err error) error {
	return fmt.Errorf("typing into the display of %s: %w", name, err)
}

// Send types keys, in order.
func (k *VMKeyboard) Send(keys []VMKey) error {
	for _, key := range keys {
		msg := make([]byte, 8)
		msg[0] = 4 // KeyEvent
		if key.Down {
			msg[1] = 1
		}
		binary.BigEndian.PutUint32(msg[4:], key.Keysym)
		select {
		case k.out <- msg:
		case <-k.done:
			if k.err != nil {
				return k.err
			}
			return errors.New("the connection to the display has closed")
		}
	}
	return nil
}

// Done is closed when the connection ends, however it ends; Err says why.
func (k *VMKeyboard) Done() <-chan struct{} { return k.done }

// Err is why the connection ended, nil while it is open or when it was closed.
func (k *VMKeyboard) Err() error {
	select {
	case <-k.done:
		return k.err
	default:
		return nil
	}
}

// Close hangs up.
func (k *VMKeyboard) Close() {
	_ = k.conn.Close()
}

func (k *VMKeyboard) write() {
	for {
		select {
		case msg := <-k.out:
			if _, err := k.conn.Write(msg); err != nil {
				_ = k.conn.Close()
				return
			}
		case <-k.done:
			return
		}
	}
}

// drain reads whatever the server sends, so it never blocks on a full socket,
// and ends the keyboard when the connection goes.
func (k *VMKeyboard) drain(in *bufio.Reader) {
	err := rfbDiscard(in)
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		err = nil
	}
	switch websocket.CloseStatus(err) {
	case websocket.StatusNormalClosure, websocket.StatusGoingAway:
		err = nil
	}
	k.err = err
	close(k.done)
	_ = k.conn.Close()
}

// ---- RFB --------------------------------------------------------------------

// rfbHandshake takes a fresh connection as far as the normal protocol: the
// version, the security handshake (KubeVirt's VNC has none -- virt-api has
// already checked who is asking), and the init messages.
func rfbHandshake(in *bufio.Reader, out io.Writer) error {
	version := make([]byte, 12)
	if _, err := io.ReadFull(in, version); err != nil {
		return fmt.Errorf("reading the server's version: %w", err)
	}
	var major, minor int
	if _, err := fmt.Sscanf(string(version), "RFB %03d.%03d\n", &major, &minor); err != nil || major != 3 {
		return fmt.Errorf("not a VNC server: %q", version)
	}
	// 3.8 if the server has it, otherwise 3.3: the two this needs to tell
	// apart, since 3.7 is 3.8 without the reason on a failed handshake.
	reply := "RFB 003.008\n"
	old := minor < 7
	if old {
		reply = "RFB 003.003\n"
	}
	if _, err := io.WriteString(out, reply); err != nil {
		return err
	}

	if old {
		var kind uint32
		if err := binary.Read(in, binary.BigEndian, &kind); err != nil {
			return err
		}
		if kind != 1 {
			return fmt.Errorf("the server wants a password (security type %d), which KubeVirt's does not", kind)
		}
	} else {
		count, err := in.ReadByte()
		if err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("the server refused: %s", rfbReason(in))
		}
		kinds := make([]byte, count)
		if _, err := io.ReadFull(in, kinds); err != nil {
			return err
		}
		found := false
		for _, kind := range kinds {
			found = found || kind == 1
		}
		if !found {
			return fmt.Errorf("the server offers no way in without a password (%v)", kinds)
		}
		if _, err := out.Write([]byte{1}); err != nil {
			return err
		}
		var result uint32
		if err := binary.Read(in, binary.BigEndian, &result); err != nil {
			return err
		}
		if result != 0 {
			return fmt.Errorf("the server refused: %s", rfbReason(in))
		}
	}

	// ClientInit: shared, so a viewer already connected is not thrown off.
	if _, err := out.Write([]byte{1}); err != nil {
		return err
	}
	// ServerInit: the size, the pixel format and the desktop's name. Only
	// read to get past it.
	head := make([]byte, 2+2+16)
	if _, err := io.ReadFull(in, head); err != nil {
		return err
	}
	var nameLen uint32
	if err := binary.Read(in, binary.BigEndian, &nameLen); err != nil {
		return err
	}
	_, err := io.CopyN(io.Discard, in, int64(nameLen))
	return err
}

func rfbReason(in *bufio.Reader) string {
	var n uint32
	if err := binary.Read(in, binary.BigEndian, &n); err != nil || n > 4096 {
		return "no reason given"
	}
	reason := make([]byte, n)
	if _, err := io.ReadFull(in, reason); err != nil {
		return "no reason given"
	}
	return string(reason)
}

// rfbDiscard reads server messages until the connection ends. Since no
// framebuffer is ever asked for, a FramebufferUpdate is not expected; one that
// comes anyway cannot be skipped without decoding it, so it ends the
// connection rather than letting the stream fall out of step.
func rfbDiscard(in *bufio.Reader) error {
	for {
		kind, err := in.ReadByte()
		if err != nil {
			return err
		}
		switch kind {
		case 1: // SetColourMapEntries: padding, first colour, count, then 6 bytes each.
			head := make([]byte, 5)
			if _, err := io.ReadFull(in, head); err != nil {
				return err
			}
			count := binary.BigEndian.Uint16(head[3:])
			if _, err := io.CopyN(io.Discard, in, int64(count)*6); err != nil {
				return err
			}
		case 2: // Bell
		case 3: // ServerCutText: padding, length, text.
			head := make([]byte, 7)
			if _, err := io.ReadFull(in, head); err != nil {
				return err
			}
			if _, err := io.CopyN(io.Discard, in, int64(binary.BigEndian.Uint32(head[3:]))); err != nil {
				return err
			}
		default:
			return fmt.Errorf("the display sent a message this keyboard cannot read (type %d)", kind)
		}
	}
}
