package openapi_test

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/neria-cloud/meridian-base/framework/pluginshttp"
	"github.com/neria-cloud/meridian-base/framework/pluginshttp/openapi"
)

var hooks = []string{
	"HTTPTransportPreAuthHook", "HTTPTransportPreHook", "HTTPTransportPostHook", "HTTPTransportStreamChunkHook",
	"PreRequestHook", "PreLLMHook", "PostLLMHook", "PreMCPHook", "PostMCPHook", "PreMCPConnectionHook", "PostMCPConnectionHook",
	"Inject", "MarshalConfigForStorage", "RedactConfig",
}

func TestDocumentLoadsAndCoversEveryHook(t *testing.T) {
	d := openapi.MustLoad()
	if len(openapi.Spec()) == 0 {
		t.Fatal("empty embedded spec")
	}
	want := append(append([]string{}, openapi.LifecycleMethods...), hooks...)
	for _, m := range want {
		op, ok := d.Operations()[m]
		if !ok {
			t.Errorf("no operation for %s", m)
			continue
		}
		if op.HTTPMethod != "POST" || op.Path != openapi.BasePath+"/"+strings.ToLower(m) {
			t.Errorf("%s: path/method = %s %s", m, op.HTTPMethod, op.Path)
		}
		if op.RequestSchema == "" || op.ResponseSchema == "" {
			t.Errorf("%s: missing schemas: %+v", m, op)
		}
		if _, err := d.Schema(op.RequestSchema); err != nil {
			t.Error(err)
		}
		if _, err := d.Schema(op.ResponseSchema); err != nil {
			t.Error(err)
		}
	}
	for _, m := range openapi.ProbeMethods {
		op, ok := d.Operations()[m]
		if !ok || op.HTTPMethod != "GET" || op.RequestSchema != "" || op.ResponseSchema == "" {
			t.Errorf("%s: %+v", m, op)
		}
	}
	hs, ok := d.Operations()[openapi.HookStreamMethod]
	if !ok || hs.HTTPMethod != "GET" || hs.Path != openapi.BasePath+"/hookstream" {
		t.Fatalf("HookStream operation = %+v", hs)
	}
	if got := len(d.Methods()); got != len(want)+len(openapi.ProbeMethods)+1 {
		t.Fatalf("operations = %d: %v", got, d.Methods())
	}
	for _, name := range d.SchemaNames() {
		if _, err := d.Schema(name); err != nil {
			t.Error(err)
		}
	}
	if _, err := d.Schema("Nope"); err == nil {
		t.Fatal("unknown schema must error")
	}
}

// TestOperationsMatchGeneratedTable pins the embedded document to the
// generated pluginshttp.Operations table.
func TestOperationsMatchGeneratedTable(t *testing.T) {
	d := openapi.MustLoad()
	if pluginshttp.BasePath != openapi.BasePath {
		t.Fatalf("BasePath %q != %q", pluginshttp.BasePath, openapi.BasePath)
	}
	if len(pluginshttp.Operations) != len(d.Operations()) {
		t.Fatalf("generated %d operations, document %d", len(pluginshttp.Operations), len(d.Operations()))
	}
	for _, g := range pluginshttp.Operations {
		op, ok := d.Operations()[g.Method]
		if !ok {
			t.Errorf("generated operation %s not in the document", g.Method)
			continue
		}
		if op.Path != g.Path || op.HTTPMethod != g.HTTPMethod || op.RequestSchema != g.RequestSchema || op.ResponseSchema != g.ResponseSchema {
			t.Errorf("%s: document %+v, generated %+v", g.Method, op, g)
		}
	}
}

// TestSchemaPropertiesMatchGeneratedTypes pins the document to the generated
// structs: every JSON tag of a generated message is a property of the schema
// with the same name, and vice versa.
func TestSchemaPropertiesMatchGeneratedTypes(t *testing.T) {
	d := openapi.MustLoad()
	types := map[string]any{
		"ContextDetails": pluginshttp.ContextDetails{}, "ContextDelta": pluginshttp.ContextDelta{},
		"RequestDetails": pluginshttp.RequestDetails{}, "ResponseDetails": pluginshttp.ResponseDetails{},
		"StreamChunk": pluginshttp.StreamChunk{}, "BifrostError": pluginshttp.BifrostError{},
		"HookStreamRequest": pluginshttp.HookStreamRequest{}, "HookStreamResponse": pluginshttp.HookStreamResponse{},
		"CapabilitiesResponse": pluginshttp.CapabilitiesResponse{}, "InitRequest": pluginshttp.InitRequest{},
		"GetNameResponse": pluginshttp.GetNameResponse{}, "HealthResponse": pluginshttp.HealthResponse{},
		"StatusResponse":              pluginshttp.StatusResponse{},
		"HTTPTransportPreHookRequest": pluginshttp.HTTPTransportPreHookRequest{}, "HTTPTransportPreHookResponse": pluginshttp.HTTPTransportPreHookResponse{},
		"HTTPTransportPostHookRequest": pluginshttp.HTTPTransportPostHookRequest{}, "HTTPTransportPostHookResponse": pluginshttp.HTTPTransportPostHookResponse{},
		"HTTPTransportStreamChunkHookRequest": pluginshttp.HTTPTransportStreamChunkHookRequest{}, "HTTPTransportStreamChunkHookResponse": pluginshttp.HTTPTransportStreamChunkHookResponse{},
		"PreRequestHookRequest": pluginshttp.PreRequestHookRequest{}, "PreRequestHookResponse": pluginshttp.PreRequestHookResponse{},
		"PreLLMHookRequest": pluginshttp.PreLLMHookRequest{}, "PreLLMHookResponse": pluginshttp.PreLLMHookResponse{},
		"PostLLMHookRequest": pluginshttp.PostLLMHookRequest{}, "PostLLMHookResponse": pluginshttp.PostLLMHookResponse{},
		"PreMCPHookRequest": pluginshttp.PreMCPHookRequest{}, "PreMCPHookResponse": pluginshttp.PreMCPHookResponse{},
		"PostMCPHookRequest": pluginshttp.PostMCPHookRequest{}, "PostMCPHookResponse": pluginshttp.PostMCPHookResponse{},
		"PreMCPConnectionHookRequest": pluginshttp.PreMCPConnectionHookRequest{}, "PreMCPConnectionHookResponse": pluginshttp.PreMCPConnectionHookResponse{},
		"PostMCPConnectionHookRequest": pluginshttp.PostMCPConnectionHookRequest{}, "PostMCPConnectionHookResponse": pluginshttp.PostMCPConnectionHookResponse{},
		"InjectRequest": pluginshttp.InjectRequest{}, "InjectResponse": pluginshttp.InjectResponse{},
		"ConfigMapRequest": pluginshttp.ConfigMapRequest{}, "ConfigMapResponse": pluginshttp.ConfigMapResponse{},
	}
	for name, v := range types {
		props, ok := d.SchemaProperties(name)
		if !ok {
			t.Errorf("generated type %s has no schema", name)
			continue
		}
		var tags []string
		rt := reflect.TypeOf(v)
		for i := 0; i < rt.NumField(); i++ {
			tag, _, _ := strings.Cut(rt.Field(i).Tag.Get("json"), ",")
			if tag != "" && tag != "-" {
				tags = append(tags, tag)
			}
		}
		for _, f := range tags {
			if !slices.Contains(props, f) {
				t.Errorf("schema %s lacks generated field %q (has %v)", name, f, props)
			}
		}
		for _, p := range props {
			if !slices.Contains(tags, p) {
				t.Errorf("schema %s has property %q the generated type lacks", name, p)
			}
		}
	}
}

func TestRealMessagesValidate(t *testing.T) {
	d := openapi.MustLoad()
	ctx := &pluginshttp.ContextDetails{RequestID: "r1", Values: map[string]json.RawMessage{"k": json.RawMessage(`"v"`)}, PluginScope: "demo"}
	preReq := &pluginshttp.PreLLMHookRequest{Ctx: ctx, Request: &pluginshttp.LLMRequest{RequestType: pluginshttp.RequestTypeChatCompletion, Request: json.RawMessage(`{"model":"x","messages":[]}`)}}
	if err := d.ValidateRequest("PreLLMHook", preReq); err != nil {
		t.Fatal(err)
	}
	preResp := &pluginshttp.PreLLMHookResponse{Request: preReq.Request, BodyChanged: true, Ctx: &pluginshttp.ContextDelta{Set: map[string]json.RawMessage{"a": json.RawMessage(`"b"`)}}}
	if err := d.ValidateResponse("PreLLMHook", preResp); err != nil {
		t.Fatal(err)
	}
	env := &pluginshttp.HookStreamRequest{ID: 1, Call: pluginshttp.CallPreLLMHook, PreLLMHook: preReq}
	if err := d.Validate("HookStreamRequest", env); err != nil {
		t.Fatal(err)
	}
	lifecycleEnv := &pluginshttp.HookStreamRequest{ID: 2, Call: pluginshttp.CallInit, Init: &pluginshttp.InitRequest{Config: pluginshttp.ConfigMap{"greeting": "hi"}}}
	if err := d.Validate("HookStreamRequest", lifecycleEnv); err != nil {
		t.Fatal(err)
	}
	reply := &pluginshttp.HookStreamResponse{ID: 1, Call: pluginshttp.CallPreLLMHook, PreLLMHook: preResp}
	if err := d.Validate("HookStreamResponse", reply); err != nil {
		t.Fatal(err)
	}
	terr := &pluginshttp.HookStreamResponse{ID: 3, TransportError: "unknown call"}
	if err := d.Validate("HookStreamResponse", terr); err != nil {
		t.Fatal(err)
	}
	var names []pluginshttp.HookName
	for _, h := range hooks {
		names = append(names, pluginshttp.HookName(h))
	}
	caps := &pluginshttp.CapabilitiesResponse{Hooks: names, ProtocolVersion: 1, StreamSupported: true}
	if err := d.ValidateResponse("Capabilities", caps); err != nil {
		t.Fatal(err)
	}
	bin := &pluginshttp.RequestDetails{RequestID: "r1", Method: "POST", Path: "/v1/x", Headers: map[string]string{"content-type": "application/octet-stream"}, BodyB64: []byte{0, 1, 2}, ClientIP: "127.0.0.1", Attempt: 1}
	if err := d.Validate("RequestDetails", bin); err != nil {
		t.Fatal(err)
	}
	post := &pluginshttp.PostLLMHookRequest{Ctx: ctx, Response: &pluginshttp.LLMResponse{RequestType: pluginshttp.RequestTypeChatCompletion, Response: json.RawMessage(`{"id":"1"}`)}, Error: &pluginshttp.BifrostError{Error: &pluginshttp.ErrorField{Message: "m"}}}
	if err := d.ValidateRequest("PostLLMHook", post); err != nil {
		t.Fatal(err)
	}
	if err := d.ValidateRequest("Inject", &pluginshttp.InjectRequest{Ctx: ctx, Trace: &pluginshttp.Trace{}}); err != nil {
		t.Fatal(err)
	}
	if err := d.ValidateRequest("RedactConfig", &pluginshttp.ConfigMapRequest{Config: pluginshttp.ConfigMap{"api_secret": "x", "n": 1}}); err != nil {
		t.Fatal(err)
	}
	if err := d.ValidateResponse("Health", &pluginshttp.HealthResponse{Status: pluginshttp.StatusOk}); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidMessagesAreRejected(t *testing.T) {
	d := openapi.MustLoad()
	cases := map[string]string{
		"two payloads":     `{"id":1,"call":"PreLLMHook","pre_llm_hook":{"ctx":{},"request":{}},"init":{}}`,
		"no payload":       `{"id":1,"call":"PreLLMHook"}`,
		"unknown call":     `{"id":1,"call":"Frobnicate","init":{}}`,
		"id zero":          `{"id":0,"call":"Init","init":{}}`,
		"unknown property": `{"id":1,"call":"Init","init":{},"bogus":true}`,
		"wrong type":       `{"id":"1","call":"Init","init":{}}`,
	}
	for name, doc := range cases {
		if err := d.ValidateJSON("HookStreamRequest", []byte(doc)); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
	if err := d.ValidateJSON("HookStreamResponse", []byte(`{"id":1}`)); err == nil {
		t.Error("response without payload or transport_error must fail")
	}
	if err := d.ValidateJSON("HookStreamResponse", []byte(`{"id":1,"transport_error":"x","init":{}}`)); err == nil {
		t.Error("response with both transport_error and payload must fail")
	}
	if err := d.ValidateJSON("CapabilitiesResponse", []byte(`{"hooks":["Nope"],"protocol_version":1}`)); err == nil {
		t.Error("unknown hook name must fail")
	}
	if err := d.ValidateJSON("HookStreamRequest", []byte(`{not json`)); err == nil {
		t.Error("invalid JSON must fail")
	}
	if err := d.ValidateRequest("Nope", struct{}{}); err == nil {
		t.Error("unknown method must fail")
	}
	if err := d.ValidateRequest(openapi.HookStreamMethod, struct{}{}); err == nil {
		t.Error("HookStream has no JSON body")
	}
	if err := d.Validate("HookStreamRequest", make(chan int)); err == nil {
		t.Error("unmarshalable value must fail")
	}
}

func TestCheckMethods(t *testing.T) {
	d := openapi.MustLoad()
	missing, err := d.CheckMethods(hooks)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Fatalf("full plugin: missing = %v", missing)
	}
	missing, err = d.CheckMethods([]string{"PreLLMHook"})
	if err != nil || len(missing) != len(hooks)-1 || slices.Contains(missing, "PreLLMHook") || slices.Contains(missing, "Init") || slices.Contains(missing, "Health") {
		t.Fatalf("partial plugin: missing = %v err = %v", missing, err)
	}
	if _, err := d.CheckMethods([]string{"PreLLMHook", "Frobnicate"}); err == nil || !strings.Contains(err.Error(), "Frobnicate") {
		t.Fatalf("err = %v", err)
	}
	if _, err := openapi.Parse([]byte(`{"openapi":"3.0.0"}`)); err == nil {
		t.Fatal("3.0 must be rejected")
	}
	if _, err := openapi.Parse([]byte(`[]`)); err == nil {
		t.Fatal("non-object must be rejected")
	}
	if _, err := openapi.Parse([]byte(`{"openapi":"3.1.0","paths":{"/x":{"post":{}}}}`)); err == nil {
		t.Fatal("operation without operationId must be rejected")
	}
}
