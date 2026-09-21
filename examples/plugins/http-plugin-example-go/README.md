# http-plugin-example-go

The smallest remote HTTP plugin for Meridian. It answers the lifecycle calls and `PreLLMHook`,
over plain POST requests and over the HookStream WebSocket; the hook prints the request it
receives and returns it unchanged.

Built on `fasthttp` with `fasthttp/router` for the routes, `fasthttp/websocket` for the stream
and `bytedance/sonic` for JSON. One dispatch table serves both transports.

Contract: `framework/pluginshttp/openapi/openapi.yaml` (paths under `/api/meridian/plugin/v1`).

| call | reply |
|---|---|
| `POST /capabilities` | `{"hooks":["PreLLMHook"],"protocol_version":1,"stream_supported":true}` |
| `POST /init` | prints the config, replies `{}` |
| `POST /getname` | `{"name":"GoExamplePlug"}` |
| `POST /cleanup` | `{}` |
| `POST /prellmhook` | prints `request_type` and the typed request; replies `{}` (`body_changed` false = keep the host's copy) |
| `GET /health` | `{"status":"ok","components":{"stream":"ok"}}` |
| `GET /status` | name, version, protocol, hooks, uptime, call counters |
| `GET /hookstream` | WebSocket; one `HookStreamRequest` frame in, one `HookStreamResponse` out |

## Build and run

```sh
make build                      # build/http-plugin-example-go
make run                        # build, then start on 127.0.0.1:18081
make run ADDR=127.0.0.1:0       # any free port
build/http-plugin-example-go -addr 127.0.0.1:9000
```

The binary listens on `127.0.0.1:18081` unless `-addr` says otherwise. The first stdout line is
the handshake `MERIDIAN|1|tcp|<addr>`; the host reads it to find the plugin.

## Try it

```sh
curl -s -X POST localhost:18081/api/meridian/plugin/v1/capabilities -d '{}'
curl -s -X POST localhost:18081/api/meridian/plugin/v1/prellmhook -d '{
  "ctx": {"request_id": "r1"},
  "request": {"request_type": "chat_completion",
              "request": {"provider": "openai", "model": "x", "input": [{"role": "user", "content": "hi"}]}}
}'
```

The plugin prints:

```
PreLLMHook: request_id=r1 request_type=chat_completion
  input: [{"role":"user","content":"hi"}]
  model: "x"
  provider: "openai"
```

Over the stream the same call is one binary frame,
`{"id":1,"call":"PreLLMHook","pre_llm_hook":{...}}`, answered by
`{"id":1,"call":"PreLLMHook","pre_llm_hook":{}}`. Meridian calls every hook over the stream; the POST routes
stay for tooling and for `Capabilities`. A plugin that changes the request echoes it under `request` with
`"body_changed": true`; without the flag the host keeps its own copy and never decodes an echo.

## Test

```sh
make test
```
