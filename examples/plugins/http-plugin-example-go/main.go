// Command http-plugin-example-go is the smallest remote HTTP plugin for
// Meridian: it serves the lifecycle calls and PreLLMHook over plain POST
// requests and over the HookStream WebSocket, and prints every request it
// sees without changing it.
//
// Contract: framework/pluginshttp/openapi/openapi.yaml.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/bytedance/sonic"
	"github.com/fasthttp/router"
	"github.com/fasthttp/websocket"
	"github.com/valyala/fasthttp"
)

const (
	pluginName      = "GoExamplePlug"
	pluginVersion   = "1.1.0"
	protocolVersion = 1
	basePath        = "/api/meridian/plugin/v1"
	defaultAddr     = "127.0.0.1:18081"
)

var hooks = []string{"PreLLMHook"}

// Messages used by this plugin; property names follow the OpenAPI schemas.
type (
	capabilitiesResponse struct {
		Hooks           []string `json:"hooks"`
		ProtocolVersion int      `json:"protocol_version"`
		StreamSupported bool     `json:"stream_supported"`
	}
	initRequest struct {
		Config map[string]any `json:"config,omitempty"`
	}
	getNameResponse struct {
		Name string `json:"name"`
	}
	contextDetails struct {
		RequestID string `json:"request_id,omitempty"`
	}
	// llmRequest is the wire form of an LLM-side request: the typed core request
	// (schemas.BifrostChatRequest for chat_completion, ...) under `request`.
	llmRequest struct {
		RequestType string                 `json:"request_type"`
		Request     sonic.NoCopyRawMessage `json:"request,omitempty"`
	}
	preLLMHookRequest struct {
		Ctx     contextDetails `json:"ctx"`
		Request llmRequest     `json:"request"`
	}
	// preLLMHookResponse omits the request: body_changed=false tells the host to
	// keep its own copy, which is the cheapest possible reply.
	preLLMHookResponse struct {
		BodyChanged bool `json:"body_changed,omitempty"`
	}
	healthResponse struct {
		Status     string            `json:"status"`
		Components map[string]string `json:"components,omitempty"`
	}
	statusStats struct {
		CallsTotal  int64 `json:"calls_total"`
		ErrorsTotal int64 `json:"errors_total"`
		OpenStreams int32 `json:"open_streams"`
	}
	statusResponse struct {
		Name            string      `json:"name"`
		Version         string      `json:"version"`
		ProtocolVersion int         `json:"protocol_version"`
		Hooks           []string    `json:"hooks"`
		StreamSupported bool        `json:"stream_supported"`
		StartedAt       string      `json:"started_at"`
		UptimeSeconds   int64       `json:"uptime_seconds"`
		Stats           statusStats `json:"stats"`
	}
	transportError struct {
		Error string `json:"error"`
	}
)

// call is one operation: its name, its HookStream payload property and the
// function that turns the request message into the response message.
type call struct {
	name string
	prop string
	fn   func(payload []byte) (any, error)
}

// plugin holds the dispatch table shared by the POST routes and the stream.
type plugin struct {
	out   io.Writer
	calls []call

	started     time.Time
	callsTotal  atomic.Int64
	errorsTotal atomic.Int64
	openStreams atomic.Int32
}

func newPlugin(out io.Writer) *plugin {
	p := &plugin{out: out, started: time.Now()}
	p.calls = []call{
		{"Capabilities", "capabilities", func([]byte) (any, error) {
			return capabilitiesResponse{Hooks: hooks, ProtocolVersion: protocolVersion, StreamSupported: true}, nil
		}},
		{"Init", "init", func(payload []byte) (any, error) {
			var req initRequest
			if err := sonic.Unmarshal(payload, &req); err != nil {
				return nil, err
			}
			fmt.Fprintf(p.out, "init: config=%v\n", req.Config)
			return struct{}{}, nil
		}},
		{"GetName", "get_name", func([]byte) (any, error) { return getNameResponse{Name: pluginName}, nil }},
		{"Cleanup", "cleanup", func([]byte) (any, error) { return struct{}{}, nil }},
		{"PreLLMHook", "pre_llm_hook", p.preLLMHook},
	}
	return p
}

// preLLMHook prints the typed request and answers body_changed=false, so the
// host continues with its own copy untouched.
func (p *plugin) preLLMHook(payload []byte) (any, error) {
	var req preLLMHookRequest
	if err := sonic.Unmarshal(payload, &req); err != nil {
		return nil, err
	}
	printRequest(p.out, req.Ctx.RequestID, req.Request)
	return preLLMHookResponse{}, nil
}

func (p *plugin) lookup(name string) (call, bool) {
	for _, c := range p.calls {
		if strings.EqualFold(c.name, name) {
			return c, true
		}
	}
	return call{}, false
}

// handler routes POST /{method} and GET /hookstream.
func (p *plugin) handler() fasthttp.RequestHandler {
	r := router.New()
	r.GET(basePath+"/hookstream", p.serveStream)
	r.GET(basePath+"/health", p.serveHealth)
	r.GET(basePath+"/status", p.serveStatus)
	r.POST(basePath+"/{method}", p.servePost)
	r.NotFound = func(ctx *fasthttp.RequestCtx) {
		fail(ctx, fasthttp.StatusNotFound, "unknown method "+string(ctx.Method())+" "+string(ctx.Path()))
	}
	return r.Handler
}

func (p *plugin) servePost(ctx *fasthttp.RequestCtx) {
	c, ok := p.lookup(ctx.UserValue("method").(string))
	if !ok {
		fail(ctx, fasthttp.StatusNotFound, "unknown method "+string(ctx.Path()))
		return
	}
	p.callsTotal.Add(1)
	resp, err := c.fn(ctx.PostBody())
	if err != nil {
		p.errorsTotal.Add(1)
		fail(ctx, fasthttp.StatusBadRequest, err.Error())
		return
	}
	write(ctx, fasthttp.StatusOK, resp)
}

func (p *plugin) serveHealth(ctx *fasthttp.RequestCtx) {
	write(ctx, fasthttp.StatusOK, healthResponse{Status: "ok", Components: map[string]string{"stream": "ok"}})
}

func (p *plugin) serveStatus(ctx *fasthttp.RequestCtx) {
	write(ctx, fasthttp.StatusOK, statusResponse{
		Name: pluginName, Version: pluginVersion, ProtocolVersion: protocolVersion, Hooks: hooks, StreamSupported: true,
		StartedAt: p.started.UTC().Format(time.RFC3339), UptimeSeconds: int64(time.Since(p.started).Seconds()),
		Stats: statusStats{CallsTotal: p.callsTotal.Load(), ErrorsTotal: p.errorsTotal.Load(), OpenStreams: p.openStreams.Load()},
	})
}

var upgrader = websocket.FastHTTPUpgrader{CheckOrigin: func(*fasthttp.RequestCtx) bool { return true }}

// serveStream answers every HookStreamRequest frame with one HookStreamResponse.
func (p *plugin) serveStream(ctx *fasthttp.RequestCtx) {
	err := upgrader.Upgrade(ctx, func(conn *websocket.Conn) {
		defer conn.Close()
		p.openStreams.Add(1)
		defer p.openStreams.Add(-1)
		for {
			_, frame, err := conn.ReadMessage()
			if err != nil {
				return
			}
			reply, err := sonic.Marshal(p.streamReply(frame))
			if err != nil {
				log.Printf("encode: %v", err)
				return
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, reply); err != nil {
				return
			}
		}
	})
	if err != nil {
		log.Printf("hookstream: %v", err)
	}
}

// streamReply dispatches one envelope. The payload property named by `call`
// carries the request message; the reply carries the response under the same
// property, or `transport_error` when the call could not be dispatched.
func (p *plugin) streamReply(frame []byte) map[string]any {
	var env map[string]sonic.NoCopyRawMessage
	if err := sonic.Unmarshal(frame, &env); err != nil {
		return map[string]any{"id": 0, "transport_error": "decode: " + err.Error()}
	}
	var id uint64
	var name string
	_ = sonic.Unmarshal(env["id"], &id)
	_ = sonic.Unmarshal(env["call"], &name)
	reply := map[string]any{"id": id, "call": name}
	c, ok := p.lookup(name)
	if !ok {
		reply["transport_error"] = "unknown call " + name
		return reply
	}
	payload, ok := env[c.prop]
	if !ok {
		reply["transport_error"] = "missing payload " + c.prop
		return reply
	}
	p.callsTotal.Add(1)
	resp, err := c.fn(payload)
	if err != nil {
		p.errorsTotal.Add(1)
		reply["transport_error"] = err.Error()
		return reply
	}
	reply[c.prop] = resp
	return reply
}

// printRequest writes the request type and the typed request JSON (the core
// struct's own tags, e.g. provider/model/input for chat_completion).
func printRequest(out io.Writer, requestID string, r llmRequest) {
	fmt.Fprintf(out, "PreLLMHook: request_id=%s request_type=%s\n", requestID, r.RequestType)
	if len(r.Request) == 0 {
		fmt.Fprintln(out, "  request: empty")
		return
	}
	var fields map[string]sonic.NoCopyRawMessage
	if err := sonic.Unmarshal(r.Request, &fields); err != nil {
		fmt.Fprintf(out, "  request (undecodable): %s\n", r.Request)
		return
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(out, "  %s: %s\n", k, fields[k])
	}
}

func fail(ctx *fasthttp.RequestCtx, status int, msg string) {
	write(ctx, status, transportError{Error: msg})
}

func write(ctx *fasthttp.RequestCtx, status int, v any) {
	b, err := sonic.Marshal(v)
	if err != nil {
		log.Printf("encode: %v", err)
		b, status = []byte(`{"error":"encode failed"}`), fasthttp.StatusInternalServerError
	}
	ctx.SetContentType("application/json")
	ctx.SetStatusCode(status)
	ctx.SetBody(b)
}

func main() {
	addr := flag.String("addr", defaultAddr, "listen address; port 0 picks a free one")
	flag.Parse()

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	// Handshake line the host reads from stdout to find the plugin.
	fmt.Printf("MERIDIAN|1|tcp|%s\n", ln.Addr())
	log.SetOutput(os.Stdout)
	if err := fasthttp.Serve(ln, newPlugin(os.Stdout).handler()); err != nil && !errors.Is(err, net.ErrClosed) {
		log.Fatal(err)
	}
}
