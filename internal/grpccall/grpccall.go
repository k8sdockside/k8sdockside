// Package grpccall makes gRPC calls for a plugin's views, whose messages are
// known only from the descriptors the plugin ships.
//
// gRPC is HTTP/2 with each message prefixed by its length and the outcome in
// the trailers, so a call is one POST: the request message framed, the
// response messages read off the body until it ends. That is all a page needs
// -- unary calls and streams from the server -- and it keeps a whole RPC
// framework out of the app for the sake of it.
//
// Messages cross to the page as JSON, in protobuf's canonical JSON mapping,
// and are turned into protobuf here with dynamicpb from the descriptors.
package grpccall

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

// Limits on one call.
const (
	// DefaultTimeout is how long a call runs when the page does not say.
	DefaultTimeout = 10 * time.Second
	// MaxTimeout is the longest a call may run. A page is waiting on it.
	MaxTimeout = 30 * time.Second
	// DefaultMessages and MaxMessages bound how many messages a stream is
	// read for.
	DefaultMessages = 1000
	MaxMessages     = 10000
	// maxMessageBytes bounds one message as it arrives.
	maxMessageBytes = 4 << 20
	// maxAnswerBytes bounds what is handed back, as JSON: it crosses the
	// webview's bridge in one message.
	maxAnswerBytes = 8 << 20
	// maxRequestBytes bounds the request message the page sends.
	maxRequestBytes = 1 << 20
)

// Why a call stopped reading.
const (
	// EndComplete: the server ended the call, with the status given.
	EndComplete = "complete"
	// EndMessages: as many messages as were asked for had arrived.
	EndMessages = "messages"
	// EndTime: the call's time ran out with the stream still open -- what a
	// stream that follows new events always ends with.
	EndTime = "time"
	// EndSize: the answer reached the most that can be handed back.
	EndSize = "size"
)

// Answer is what a call came to.
type Answer struct {
	// Messages are the response messages, as a JSON array.
	Messages string `json:"messages"`
	// Count is how many there are.
	Count int `json:"count"`
	// Status is the gRPC status code: 0 is OK. Meaningful when End is
	// complete; a call cut short by the app has none of its own.
	Status int `json:"status"`
	// Message is what the server said with a status that is not OK.
	Message string `json:"message"`
	// End is why reading stopped: complete, messages, time or size.
	End string `json:"end"`
}

// Conn calls one server, over HTTP/2 without TLS: what the app reaches
// through a port-forward is a pod's own port.
type Conn struct {
	addr      string
	transport *http.Transport
	client    *http.Client
}

// Dial makes a Conn to addr, host:port. Nothing is opened until a call.
func Dial(addr string) *Conn {
	// HTTP/2 alone, without TLS: what gRPC calls plaintext, and what
	// net/http calls unencrypted HTTP/2 with prior knowledge.
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	transport := &http.Transport{
		Protocols:   protocols,
		DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		HTTP2: &http.HTTP2Config{
			SendPingTimeout: 30 * time.Second,
			PingTimeout:     10 * time.Second,
		},
	}
	return &Conn{addr: addr, transport: transport, client: &http.Client{Transport: transport}}
}

// Close drops the connection.
func (c *Conn) Close() {
	c.transport.CloseIdleConnections()
}

// Request is one call.
type Request struct {
	// Method is "package.Service/Method".
	Method string
	// Descriptor is the method, from the plugin's descriptors.
	Descriptor protoreflect.MethodDescriptor
	// Types resolves the messages an Any names.
	Types *dynamicpb.Types
	// Body is the request message as JSON; empty is the empty message.
	Body string
	// Timeout and Messages bound the call; zero takes the defaults.
	Timeout  time.Duration
	Messages int
}

// ErrTransport marks an error in reaching the server rather than in the call:
// the connection may be worth opening again.
var ErrTransport = errors.New("could not reach the service")

// Call makes one call and reads its answer.
func (c *Conn) Call(ctx context.Context, r Request) (Answer, error) {
	if r.Descriptor == nil {
		return Answer{}, errors.New("no method descriptor")
	}
	if r.Descriptor.IsStreamingClient() {
		return Answer{}, fmt.Errorf("%s streams from the client, which cannot be called", r.Method)
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	timeout = min(timeout, MaxTimeout)
	limit := r.Messages
	if limit <= 0 {
		limit = DefaultMessages
	}
	limit = min(limit, MaxMessages)
	if !r.Descriptor.IsStreamingServer() {
		limit = 1
	}

	payload, err := encodeRequest(r)
	if err != nil {
		return Answer{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	size := len(payload)
	if size > maxMessageBytes {
		return Answer{}, fmt.Errorf("the request is %d bytes as protobuf; at most %d MiB", size, maxMessageBytes>>20)
	}
	frame := make([]byte, 5+size)
	binary.BigEndian.PutUint32(frame[1:5], uint32(size))
	copy(frame[5:], payload)

	target := (&url.URL{Scheme: "http", Host: c.addr, Path: "/" + r.Method}).String()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(frame))
	if err != nil {
		return Answer{}, err
	}
	req.Header.Set("Content-Type", "application/grpc")
	req.Header.Set("TE", "trailers")
	req.Header.Set("User-Agent", "k8sdockside-grpccall")
	// The server is told the deadline too, so a stream that follows stops
	// there rather than running on for nobody.
	req.Header.Set("Grpc-Timeout", strconv.FormatInt(timeout.Milliseconds(), 10)+"m")

	resp, err := c.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return Answer{}, fmt.Errorf("%w: no answer within %s", ErrTransport, timeout)
		}
		return Answer{}, fmt.Errorf("%w: %v", ErrTransport, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return Answer{}, fmt.Errorf("the service answered HTTP %d, not gRPC -- is the port the gRPC one?", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/grpc") {
		return Answer{}, fmt.Errorf("the service answered %q, not gRPC -- is the port the gRPC one?", ct)
	}

	out := answerWriter{}
	end := EndComplete
	marshal := protojson.MarshalOptions{Resolver: r.Types}
read:
	for {
		msg, err := readFrame(resp.Body)
		switch {
		case errors.Is(err, io.EOF):
			break read
		case err != nil:
			if ctx.Err() != nil {
				end = EndTime
				break read
			}
			return Answer{}, fmt.Errorf("%w: the stream broke: %v", ErrTransport, err)
		}
		m := dynamicpb.NewMessage(r.Descriptor.Output())
		if err := (proto.UnmarshalOptions{Resolver: r.Types}).Unmarshal(msg, m); err != nil {
			return Answer{}, fmt.Errorf("a message from %s does not match the plugin's descriptors: %v", r.Method, err)
		}
		text, err := marshal.Marshal(m)
		if err != nil {
			return Answer{}, fmt.Errorf("a message from %s could not be written as JSON: %v", r.Method, err)
		}
		if !out.add(text) {
			end = EndSize
			break
		}
		if out.count >= limit && r.Descriptor.IsStreamingServer() {
			end = EndMessages
			break
		}
	}

	answer := Answer{Messages: out.finish(), Count: out.count, End: end}
	if end == EndComplete {
		// A trailers-only answer carries the status in its headers.
		status := resp.Trailer.Get("Grpc-Status")
		message := resp.Trailer.Get("Grpc-Message")
		if status == "" {
			status, message = resp.Header.Get("Grpc-Status"), resp.Header.Get("Grpc-Message")
		}
		code, err := strconv.Atoi(status)
		if err != nil {
			code = codeUnknown
			if message == "" {
				message = "the service ended the call without a gRPC status"
			}
		}
		// A deadline the server reports is the app's own time running out on
		// a stream that would have gone on.
		if code == codeDeadlineExceeded && r.Descriptor.IsStreamingServer() {
			answer.End = EndTime
			code, message = 0, ""
		}
		answer.Status = code
		answer.Message, _ = url.PathUnescape(message)
	}
	return answer, nil
}

const (
	codeUnknown          = 2
	codeDeadlineExceeded = 4
)

// encodeRequest turns the page's JSON into the method's request message.
func encodeRequest(r Request) ([]byte, error) {
	if len(r.Body) > maxRequestBytes {
		return nil, fmt.Errorf("the request is %d bytes; at most %d KiB", len(r.Body), maxRequestBytes>>10)
	}
	body := strings.TrimSpace(r.Body)
	if body == "" {
		body = "{}"
	}
	m := dynamicpb.NewMessage(r.Descriptor.Input())
	if err := (protojson.UnmarshalOptions{Resolver: r.Types}).Unmarshal([]byte(body), m); err != nil {
		return nil, fmt.Errorf("the request is not a %s: %v", r.Descriptor.Input().FullName(), err)
	}
	return proto.Marshal(m)
}

// readFrame reads one length-prefixed message.
func readFrame(body io.Reader) ([]byte, error) {
	var header [5]byte
	if _, err := io.ReadFull(body, header[:]); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, errors.New("a message was cut off")
		}
		return nil, err
	}
	if header[0]&1 != 0 {
		return nil, errors.New("the service compressed a message, which was not asked for")
	}
	size := binary.BigEndian.Uint32(header[1:])
	if size > maxMessageBytes {
		return nil, fmt.Errorf("a message is %d bytes; at most %d MiB is read", size, maxMessageBytes>>20)
	}
	msg := make([]byte, size)
	if _, err := io.ReadFull(body, msg); err != nil {
		return nil, errors.New("a message was cut off")
	}
	return msg, nil
}

// answerWriter builds the JSON array the messages go back in.
type answerWriter struct {
	buf   bytes.Buffer
	count int
}

// add appends one message, or reports that it would not fit.
func (w *answerWriter) add(text []byte) bool {
	if w.buf.Len()+len(text)+2 > maxAnswerBytes {
		return false
	}
	if w.count == 0 {
		w.buf.WriteByte('[')
	} else {
		w.buf.WriteByte(',')
	}
	w.buf.Write(text)
	w.count++
	return true
}

func (w *answerWriter) finish() string {
	if w.count == 0 {
		return "[]"
	}
	w.buf.WriteByte(']')
	return w.buf.String()
}
