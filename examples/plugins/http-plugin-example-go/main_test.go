package main

import (
	"bytes"
	"net"
	"strings"
	"testing"

	"github.com/bytedance/sonic"
	"github.com/fasthttp/websocket"
	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/fasthttputil"
)

// serve runs the plugin on an in-memory listener and returns a client for it.
func serve(t *testing.T) (*fasthttp.Client, *fasthttputil.InmemoryListener, *bytes.Buffer) {
	t.Helper()
	var out bytes.Buffer
	ln := fasthttputil.NewInmemoryListener()
	go func() { _ = fasthttp.Serve(ln, newPlugin(&out).handler()) }()
	t.Cleanup(func() { ln.Close() })
	client := &fasthttp.Client{Dial: func(string) (net.Conn, error) { return ln.Dial() }}
	return client, ln, &out
}

func post(t *testing.T, c *fasthttp.Client, path, body string) (int, string) {
	t.Helper()
	req, resp := fasthttp.AcquireRequest(), fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)
	req.SetRequestURI("http://plugin" + basePath + path)
	req.Header.SetMethod(fasthttp.MethodPost)
	req.Header.SetContentType("application/json")
	req.SetBodyString(body)
	if err := c.Do(req, resp); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode(), strings.TrimSpace(string(resp.Body()))
}

const sample = `{"ctx":{"request_id":"r1"},"request":{"request_type":"chat_completion","request":{"provider":"openai","model":"x","input":[{"role":"user","content":"hi"}]}}}`

func get(t *testing.T, c *fasthttp.Client, path string) (int, string) {
	t.Helper()
	code, body, err := c.Get(nil, "http://plugin"+basePath+path)
	if err != nil {
		t.Fatal(err)
	}
	return code, strings.TrimSpace(string(body))
}

func TestHealthAndStatus(t *testing.T) {
	c, _, _ := serve(t)
	if code, body := get(t, c, "/health"); code != 200 || body != `{"status":"ok","components":{"stream":"ok"}}` {
		t.Fatalf("health: %d %s", code, body)
	}
	post(t, c, "/getname", "{}")
	code, body := get(t, c, "/status")
	if code != 200 {
		t.Fatalf("status: %d %s", code, body)
	}
	var st statusResponse
	if err := sonic.Unmarshal([]byte(body), &st); err != nil {
		t.Fatal(err)
	}
	if st.Name != pluginName || st.ProtocolVersion != 1 || !st.StreamSupported || len(st.Hooks) != 1 || st.Stats.CallsTotal < 1 || st.Version == "" {
		t.Fatalf("status = %+v", st)
	}
}

func TestLifecycle(t *testing.T) {
	c, _, out := serve(t)
	if code, body := post(t, c, "/capabilities", "{}"); code != 200 || body != `{"hooks":["PreLLMHook"],"protocol_version":1,"stream_supported":true}` {
		t.Fatalf("capabilities: %d %s", code, body)
	}
	if code, body := post(t, c, "/getname", "{}"); code != 200 || body != `{"name":"GoExamplePlug"}` {
		t.Fatalf("getname: %d %s", code, body)
	}
	if code, body := post(t, c, "/init", `{"config":{"greeting":"hi"}}`); code != 200 || body != "{}" {
		t.Fatalf("init: %d %s", code, body)
	}
	if !strings.Contains(out.String(), "init: config=map[greeting:hi]") {
		t.Fatalf("init output = %q", out.String())
	}
	if code, body := post(t, c, "/cleanup", "{}"); code != 200 || body != "{}" {
		t.Fatalf("cleanup: %d %s", code, body)
	}
	if code, body := post(t, c, "/nope", "{}"); code != 404 || !strings.Contains(body, "unknown method") {
		t.Fatalf("unknown: %d %s", code, body)
	}
	if code, body := post(t, c, "/init", "{"); code != 400 || !strings.Contains(body, "error") {
		t.Fatalf("bad json: %d %s", code, body)
	}
	code, resp, err := c.Get(nil, "http://plugin/other")
	if err != nil || code != 404 || !strings.Contains(string(resp), "unknown method GET /other") {
		t.Fatalf("not found: %d %s %v", code, resp, err)
	}
}

func TestPreLLMHookPrintsAndKeepsBody(t *testing.T) {
	c, _, out := serve(t)
	code, body := post(t, c, "/prellmhook", sample)
	if code != 200 || body != "{}" {
		t.Fatalf("prellmhook: %d %s", code, body)
	}
	for _, line := range []string{
		"PreLLMHook: request_id=r1 request_type=chat_completion",
		`  input: [{"role":"user","content":"hi"}]`,
		`  model: "x"`,
	} {
		if !strings.Contains(out.String(), line) {
			t.Errorf("output lacks %q:\n%s", line, out.String())
		}
	}
	out.Reset()
	if code, _ := post(t, c, "/prellmhook", `{"ctx":{},"request":{"request_type":"list_models"}}`); code != 200 || !strings.Contains(out.String(), "request: empty") {
		t.Fatalf("empty request: %d %q", code, out.String())
	}
	if code, body := post(t, c, "/prellmhook", `{"ctx":{},"request":[]}`); code != 400 || !strings.Contains(body, "error") {
		t.Fatalf("bad request field: %d %s", code, body)
	}
}

func TestHookStream(t *testing.T) {
	_, ln, out := serve(t)
	dialer := websocket.Dialer{NetDial: func(string, string) (net.Conn, error) { return ln.Dial() }}
	conn, _, err := dialer.Dial("ws://plugin"+basePath+"/hookstream", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	roundTrip := func(frame string) map[string]sonic.NoCopyRawMessage {
		t.Helper()
		if err := conn.WriteMessage(websocket.BinaryMessage, []byte(frame)); err != nil {
			t.Fatal(err)
		}
		mt, reply, err := conn.ReadMessage()
		if err != nil || mt != websocket.BinaryMessage {
			t.Fatalf("read: %v %d", err, mt)
		}
		var env map[string]sonic.NoCopyRawMessage
		if err := sonic.Unmarshal(reply, &env); err != nil {
			t.Fatal(err)
		}
		return env
	}

	env := roundTrip(`{"id":1,"call":"GetName","get_name":{}}`)
	if string(env["id"]) != "1" || string(env["get_name"]) != `{"name":"GoExamplePlug"}` || env["transport_error"] != nil {
		t.Fatalf("get_name = %v", env)
	}
	env = roundTrip(`{"id":2,"call":"Capabilities","capabilities":{}}`)
	if !strings.Contains(string(env["capabilities"]), `"stream_supported":true`) {
		t.Fatalf("capabilities = %v", env)
	}
	env = roundTrip(`{"id":3,"call":"PreLLMHook","pre_llm_hook":` + sample + `}`)
	if string(env["id"]) != "3" || string(env["call"]) != `"PreLLMHook"` {
		t.Fatalf("pre_llm_hook envelope = %v", env)
	}
	if string(env["pre_llm_hook"]) != "{}" {
		t.Fatalf("pre_llm_hook reply = %s, want no echo", env["pre_llm_hook"])
	}
	if !strings.Contains(out.String(), "PreLLMHook: request_id=r1 request_type=chat_completion") {
		t.Fatalf("stream output = %q", out.String())
	}
	env = roundTrip(`{"id":4,"call":"Frobnicate","frobnicate":{}}`)
	if string(env["id"]) != "4" || !strings.Contains(string(env["transport_error"]), "unknown call") {
		t.Fatalf("unknown call = %v", env)
	}
	env = roundTrip(`{"id":5,"call":"Init"}`)
	if !strings.Contains(string(env["transport_error"]), "missing payload init") {
		t.Fatalf("missing payload = %v", env)
	}
	env = roundTrip(`{"id":6,"call":"Init","init":[]}`)
	if env["transport_error"] == nil || env["init"] != nil {
		t.Fatalf("bad payload = %v", env)
	}
	env = roundTrip(`not json`)
	if string(env["id"]) != "0" || !strings.Contains(string(env["transport_error"]), "decode") {
		t.Fatalf("bad frame = %v", env)
	}
}
