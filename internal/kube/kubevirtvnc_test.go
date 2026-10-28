package kube

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

// fakeVNC plays a QEMU VNC server's side of the handshake, then hands back
// what the client sends after it.
func fakeVNC(t *testing.T, server net.Conn, security []byte) {
	t.Helper()
	w := func(b []byte) {
		if _, err := server.Write(b); err != nil {
			t.Errorf("server write: %v", err)
		}
	}
	read := func(n int) []byte {
		b := make([]byte, n)
		if _, err := io.ReadFull(server, b); err != nil {
			t.Errorf("server read: %v", err)
		}
		return b
	}
	w([]byte("RFB 003.008\n"))
	if got := string(read(12)); got != "RFB 003.008\n" {
		t.Errorf("client version = %q", got)
	}
	w(append([]byte{byte(len(security))}, security...))
	if got := read(1); got[0] != 1 {
		t.Errorf("client chose security type %d, want 1 (none)", got[0])
	}
	w([]byte{0, 0, 0, 0}) // SecurityResult: OK
	if got := read(1); got[0] != 1 {
		t.Errorf("ClientInit shared = %d, want 1", got[0])
	}
	init := make([]byte, 2+2+16)
	binary.BigEndian.PutUint16(init[0:], 1024)
	binary.BigEndian.PutUint16(init[2:], 768)
	name := []byte("QEMU (vm)")
	w(init)
	w(binary.BigEndian.AppendUint32(nil, uint32(len(name))))
	w(name)
}

func TestRFBHandshakeAndKeys(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = client.Close() }()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fakeVNC(t, server, []byte{2, 1})
	}()

	in := bufio.NewReader(client)
	if err := rfbHandshake(in, client); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	<-done

	kb := &VMKeyboard{conn: client, out: make(chan []byte, 8), done: make(chan struct{})}
	go kb.drain(in)
	go kb.write()

	// A bell, unasked, must not upset the client.
	go func() { _, _ = server.Write([]byte{2}) }()

	if err := kb.Send([]VMKey{{Keysym: 0xffc0, Down: true}, {Keysym: 0xffc0, Down: false}}); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 16)
	_ = server.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(server, got); err != nil {
		t.Fatal(err)
	}
	want := []byte{4, 1, 0, 0, 0, 0, 0xff, 0xc0, 4, 0, 0, 0, 0, 0, 0xff, 0xc0}
	if !bytes.Equal(got, want) {
		t.Errorf("F3 down and up = % x, want % x", got, want)
	}

	_ = server.Close()
	select {
	case <-kb.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the keyboard did not notice the server going")
	}
}

func TestRFBRefusesAServerThatWantsAPassword(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = client.Close() }()
	go func() {
		_, _ = server.Write([]byte("RFB 003.008\n"))
		_, _ = io.ReadFull(server, make([]byte, 12))
		_, _ = server.Write([]byte{1, 2}) // VNC authentication only
		_ = server.Close()
	}()
	if err := rfbHandshake(bufio.NewReader(client), client); err == nil {
		t.Fatal("handshake succeeded with a server that wants a password")
	}
}
