package grpccall

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

// schema is a small service: List streams Count items back, One answers one.
func schema(t *testing.T) (list, one protoreflect.MethodDescriptor, types *dynamicpb.Types) {
	t.Helper()
	str := descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()
	i32 := descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum()
	opt := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum()
	yes := true
	file := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("test.proto"),
		Package: proto.String("test"),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: proto.String("Req"), Field: []*descriptorpb.FieldDescriptorProto{{Name: proto.String("count"), Number: proto.Int32(1), Type: i32, Label: opt, JsonName: proto.String("count")}}},
			{Name: proto.String("Item"), Field: []*descriptorpb.FieldDescriptorProto{{Name: proto.String("item_name"), Number: proto.Int32(1), Type: str, Label: opt, JsonName: proto.String("itemName")}}},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: proto.String("S"),
			Method: []*descriptorpb.MethodDescriptorProto{
				{Name: proto.String("List"), InputType: proto.String(".test.Req"), OutputType: proto.String(".test.Item"), ServerStreaming: &yes},
				{Name: proto.String("One"), InputType: proto.String(".test.Req"), OutputType: proto.String(".test.Item")},
			},
		}},
	}
	files, err := protodesc.NewFiles(&descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{file}})
	if err != nil {
		t.Fatal(err)
	}
	d, err := files.FindDescriptorByName("test.S")
	if err != nil {
		t.Fatal(err)
	}
	methods := d.(protoreflect.ServiceDescriptor).Methods()
	return methods.ByName("List"), methods.ByName("One"), dynamicpb.NewTypes(files)
}

// server answers List with Count items -- or, with Count of -1, keeps the
// stream open until the deadline -- and One with a single item, or a
// NOT_FOUND status when Count is 0.
func server(t *testing.T, list protoreflect.MethodDescriptor) *httptest.Server {
	t.Helper()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/grpc" || r.ProtoMajor != 2 {
			http.Error(w, "not grpc", http.StatusUnsupportedMediaType)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		req := dynamicpb.NewMessage(list.Input())
		if err := proto.Unmarshal(raw[5:], req); err != nil {
			t.Errorf("request: %v", err)
		}
		count := int(req.Get(list.Input().Fields().ByName("count")).Int())

		w.Header().Set("Content-Type", "application/grpc")
		w.Header().Set("Trailer", "Grpc-Status, Grpc-Message")
		send := func(name string) {
			item := dynamicpb.NewMessage(list.Output())
			item.Set(list.Output().Fields().ByName("item_name"), protoreflect.ValueOfString(name))
			body, _ := proto.Marshal(item)
			frame := make([]byte, 5, 5+len(body))
			binary.BigEndian.PutUint32(frame[1:], uint32(len(body)))
			_, _ = w.Write(append(frame, body...))
			w.(http.Flusher).Flush()
		}
		switch r.URL.Path {
		case "/test.S/List":
			if count < 0 {
				send("first")
				<-r.Context().Done()
				return
			}
			for i := range count {
				send("item-" + strconv.Itoa(i))
			}
			w.Header().Set("Grpc-Status", "0")
		case "/test.S/One":
			if count == 0 {
				w.Header().Set("Grpc-Status", "5")
				w.Header().Set("Grpc-Message", "no%20such%20item")
				return
			}
			send("only")
			w.Header().Set("Grpc-Status", "0")
		default:
			w.Header().Set("Grpc-Status", "12")
		}
	})
	return h2cServer(t, handler)
}

// h2cServer serves HTTP/2 without TLS, as a gRPC server in plaintext does.
func h2cServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(handler)
	srv.Config.Protocols = new(http.Protocols)
	srv.Config.Protocols.SetHTTP1(true)
	srv.Config.Protocols.SetUnencryptedHTTP2(true)
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

func names(t *testing.T, a Answer) []string {
	t.Helper()
	var items []map[string]string
	if err := json.Unmarshal([]byte(a.Messages), &items); err != nil {
		t.Fatalf("messages %q: %v", a.Messages, err)
	}
	out := []string{}
	for _, it := range items {
		out = append(out, it["itemName"])
	}
	return out
}

func TestCallStreamsUntilTheServerEnds(t *testing.T) {
	list, _, types := schema(t)
	srv := server(t, list)
	conn := Dial(strings.TrimPrefix(srv.URL, "http://"))
	defer conn.Close()

	a, err := conn.Call(context.Background(), Request{Method: "test.S/List", Descriptor: list, Types: types, Body: `{"count": 3}`})
	if err != nil {
		t.Fatal(err)
	}
	if a.End != EndComplete || a.Status != 0 || a.Count != 3 {
		t.Fatalf("answer = %+v", a)
	}
	if got := strings.Join(names(t, a), ","); got != "item-0,item-1,item-2" {
		t.Fatalf("items = %s", got)
	}
}

func TestCallStopsAtTheMessagesAskedFor(t *testing.T) {
	list, _, types := schema(t)
	srv := server(t, list)
	conn := Dial(strings.TrimPrefix(srv.URL, "http://"))
	defer conn.Close()

	a, err := conn.Call(context.Background(), Request{Method: "test.S/List", Descriptor: list, Types: types, Body: `{"count": 50}`, Messages: 5})
	if err != nil {
		t.Fatal(err)
	}
	if a.End != EndMessages || a.Count != 5 {
		t.Fatalf("answer = %+v", a)
	}
}

func TestCallEndsAFollowingStreamAtItsTime(t *testing.T) {
	list, _, types := schema(t)
	srv := server(t, list)
	conn := Dial(strings.TrimPrefix(srv.URL, "http://"))
	defer conn.Close()

	start := time.Now()
	a, err := conn.Call(context.Background(), Request{Method: "test.S/List", Descriptor: list, Types: types, Body: `{"count": -1}`, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if a.End != EndTime || a.Count != 1 || a.Status != 0 {
		t.Fatalf("answer = %+v", a)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("took %s", took)
	}
}

func TestCallReportsTheServersStatus(t *testing.T) {
	list, one, types := schema(t)
	srv := server(t, list)
	conn := Dial(strings.TrimPrefix(srv.URL, "http://"))
	defer conn.Close()

	a, err := conn.Call(context.Background(), Request{Method: "test.S/One", Descriptor: one, Types: types, Body: `{"count": 0}`})
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != 5 || a.Message != "no such item" || a.Count != 0 || a.Messages != "[]" {
		t.Fatalf("answer = %+v", a)
	}

	a, err = conn.Call(context.Background(), Request{Method: "test.S/One", Descriptor: one, Types: types, Body: `{"count": 1}`})
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != 0 || strings.Join(names(t, a), ",") != "only" {
		t.Fatalf("answer = %+v", a)
	}
}

func TestCallRefusesARequestThatIsNotTheMessage(t *testing.T) {
	list, _, types := schema(t)
	conn := Dial("127.0.0.1:1")
	_, err := conn.Call(context.Background(), Request{Method: "test.S/List", Descriptor: list, Types: types, Body: `{"nope": 1}`})
	if err == nil || !strings.Contains(err.Error(), "test.Req") {
		t.Fatalf("err = %v", err)
	}
}

func TestCallSaysWhenTheServiceIsNotGRPC(t *testing.T) {
	list, _, types := schema(t)
	srv := h2cServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>"))
	}))
	conn := Dial(strings.TrimPrefix(srv.URL, "http://"))
	defer conn.Close()

	_, err := conn.Call(context.Background(), Request{Method: "test.S/List", Descriptor: list, Types: types})
	if err == nil || !strings.Contains(err.Error(), "not gRPC") {
		t.Fatalf("err = %v", err)
	}
}

func TestCallMarksAnUnreachableServiceAsTransport(t *testing.T) {
	list, _, types := schema(t)
	srv := httptest.NewServer(http.NotFoundHandler())
	addr := strings.TrimPrefix(srv.URL, "http://")
	srv.Close()
	conn := Dial(addr)
	_, err := conn.Call(context.Background(), Request{Method: "test.S/List", Descriptor: list, Types: types})
	if err == nil || !strings.Contains(err.Error(), ErrTransport.Error()) {
		t.Fatalf("err = %v", err)
	}
}
