package api

import (
	"encoding/json"
	"testing"
)

func TestPluginMethods(t *testing.T) {
	server := NewServer("", func(request Request) {
		if request.Method == "plugins.control" {
			payload, ok := request.Payload.(PluginControl)
			if !ok || payload.Plugin != "example.test" || payload.Action != "stop" {
				t.Fatalf("invalid payload: %+v", request.Payload)
			}
		}
		request.Reply <- Reply{Data: map[string]bool{"ok": true}}
	}, nil)
	for _, request := range []wireRequest{{ID: "status", Method: "plugins.status"}, {ID: "control", Method: "plugins.control", Params: json.RawMessage(`{"plugin":"example.test","action":"stop"}`)}} {
		reply := server.dispatch(request)
		if reply.ID != request.ID || reply.Error != nil {
			t.Fatalf("dispatch: %+v", reply)
		}
	}
	if reply := server.dispatch(wireRequest{Method: "plugins.control", Params: json.RawMessage(`{"plugin":42}`)}); reply.Error == nil || reply.Error.Code != CodeInvalidParams {
		t.Fatal("invalid plugin control accepted")
	}
}
