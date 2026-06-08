# Debug Endpoint：HTTP 端点暴露 Agent 内部状态

> 本教程基于自定义 `main.go`（不基于 `examples/`），聚焦 ADK REST server 中"调试端点"的三条 URL——`/debug/trace/{event_id}`、`/debug/trace/session/{session_id}`、与 `.../events/{event_id}/graph`——它们把 span/log/agent graph 等内部状态以 HTTP 形式暴露给 ADK Web 与调试客户端。

## 你将学到

- `DebugAPIController` 的三个 handler 与它们对应的 URL 模式（[`server/adkrest/controllers/debug.go:33`](../../../server/adkrest/controllers/debug.go)）
- `DebugTelemetry` 在内存中如何按 trace / event / session 三种 key 索引 span 与 log（[`server/adkrest/internal/services/debugtelemetry.go:42`](../../../server/adkrest/internal/services/debugtelemetry.go)）
- 如何把 server 提供的 `SpanProcessor` / `LogProcessor` 注入到应用的 `TracerProvider` / `LoggerProvider`，让 debug 端点拿到运行时数据
- 端点 `/debug/trace/{event_id}` 内部按 `gen_ai.operation.name` 过滤后返回首个 `execute_tool` 或 `generate_content` span 的细节（[`server/adkrest/controllers/debug.go:59`](../../../server/adkrest/controllers/debug.go)）
- 端点 `.../events/{event_id}/graph` 如何把 agent 树渲染成 Graphviz DOT 源并高亮被调用的 tool/agent

## 前置条件

- [x] 已完成 [04-deployment/01-rest-server.md](../04-deployment/01-rest-server.md)（对 ADK REST server 的"三层装配"已经熟悉）
- [x] 已完成 [06-observability/01-telemetry.md](../06-observability/01-telemetry.md)（对 OTel `TracerProvider` / `LoggerProvider` 的最小装配有概念）
- [x] 已设置 `GOOGLE_API_KEY`（见 [00-prerequisites.md](../00-prerequisites.md)）
- [x] 本机可访问 `generativelanguage.googleapis.com`
- [x] 已 `git clone` ADK 仓库并 `go mod download`
- [x] 已安装 `curl` 与 `jq`

## 核心概念

**Debug endpoint 是一组只读 HTTP 路由**，由 `DebugAPIRouter` 暴露 3 条 URL（[`server/adkrest/internal/routers/debug.go:34`](../../../server/adkrest/internal/routers/debug.go)），由 `DebugAPIController` 实现业务（[`server/adkrest/controllers/debug.go:33`](../../../server/adkrest/controllers/debug.go)）。它们的"数据源"是一个进程内的 in-memory store——`DebugTelemetry`（[`server/adkrest/internal/services/debugtelemetry.go:42`](../../../server/adkrest/internal/services/debugtelemetry.go)），它把 OTel 的 span/log 通过 `SpanProcessor` / `LogProcessor` 抓进来，再按 `trace_id` / `event_id` / `gen_ai.conversation.id`（即 session id）三个 key 建索引（[`server/adkrest/internal/services/debugtelemetry.go:273`](../../../server/adkrest/internal/services/debugtelemetry.go)）。

**`/debug/trace/{event_id}` 端点**返回的是单个事件对应的首个"对 LLM 有意义"的 span——`execute_tool` 或 `generate_content`（[`server/adkrest/controllers/debug.go:59`](../../../server/adkrest/controllers/debug.go)）。ADK Web 在左侧时间线点击某条 event 时会调这个端点，把它可视化地"展开"成完整的 span detail（start/end time、parent span、attributes、logs）。

**`/apps/{app}/users/{u}/sessions/{sid}/events/{event_id}/graph` 端点**是另一类需求：给定一个 event，渲染"整个 agent 树"的 Graphviz DOT 源，并把"这次调用实际走过的 tool/agent"用 `DarkGreen` / `LightGreen` 颜色高亮（[`server/adkrest/internal/services/agentgraphgenerator.go:31`](../../../server/adkrest/internal/services/agentgraphgenerator.go)）。返回的 JSON 是 `{"dotSrc": "digraph { ... } }`——ADK Web 把它转成 SVG 显示。

**`/debug/trace/session/{session_id}` 端点**返回整个会话的所有 span 列表，按 `start_time` 升序排（[`server/adkrest/internal/services/debugtelemetry.go:216`](../../../server/adkrest/internal/services/debugtelemetry.go)），是"全链路 trace"的入口。

整体数据流如下：

```mermaid
flowchart LR
 subgraph App["应用进程"]
  A[main.go: 装配 OTel SDK] -->|注册| SP[sdktrace.TracerProvider]
  A -->|注册| LP[sdklog.LoggerProvider]
  SP -->|sampler/BatchProcessor| OTel[OTel exporters]
  SP -->|SimpleSpanProcessor| DT[DebugTelemetry.store]
  LP -->|SimpleProcessor| DT
 end
 subgraph SRV["adkrest.Server"]
  DT -->|/debug/trace/event_id| H1[EventSpanHandler<br/>debug.go:49]
  DT -->|/debug/trace/session/sid| H2[SessionSpansHandler<br/>debug.go:91]
  SVC[SessionService] --> H3[EventGraphHandler<br/>debug.go:103]
  Loader[agent.Loader] --> H3
  H3 -->|GetAgentGraph| AGG[agentgraphgenerator.go]
 end
 H1 -->|JSON span| C1[ADK Web / curl]
 H2 -->|JSON spans[]| C1
 H3 -->|JSON dotSrc| C1
```

**看图指引**：

- `DT`（`DebugTelemetry`）是双角色：它既实现 `sdktrace.SpanProcessor`，又实现 `sdklog.Processor`——所以同一份内存缓存同时服务 span 与 log。
- `OTel exporters` 与 `DebugTelemetry` 是**并联**的：前者把 span 推到 Jaeger/Tempo 等外部系统；后者把 span 留在进程内供 debug 端点查询。两者互不冲突。
- `H1`、`H2` 从 `DT` 直接查内存；`H3` 需要 `SessionService`（读 event 详情） + `agent.Loader`（读 agent 树） + `agentgraphgenerator`（渲染 DOT），三件套缺一不可。

## 完整代码

> 本教程用自定义 `main.go`（不基于 `examples/`），强调"如何把 `Server.SpanProcessor()` / `Server.LogProcessor()` 接入 OTel SDK"，让 debug 端点真正能查到数据。完整源码：

```go
// 自定义 main.go — 06-observability/02-debug-endpoint/main.go
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/genai"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/adk/server/adkrest"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/geminitool"
)

func main() {
	ctx := context.Background()

	// 1. 构造 adkrest.Server 并取出它的 SpanProcessor / LogProcessor。
	restServer, err := adkrest.NewServer(adkrest.ServerConfig{
		AgentLoader:    agent.NewSingleLoader(mustBuildAgent(ctx)),
		SessionService: session.InMemoryService(),
	})
	if err != nil {
		log.Fatalf("Failed to create REST API server: %v", err)
	}

	// 2. 装配 OTel SDK：把 Server 的 Processor 挂到 TracerProvider / LoggerProvider 上。
	traceExporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		log.Fatalf("trace exporter: %v", err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(restServer.SpanProcessor()), // <-- 关键：让 debug 端点能读到 span
		sdktrace.WithSpanProcessor(sdktrace.NewBatchSpanProcessor(traceExporter)),
	)
	otel.SetTracerProvider(tp)
	defer tp.Shutdown(ctx)

	logExporter, err := stdoutlog.New()
	if err != nil {
		log.Fatalf("log exporter: %v", err)
	}
	lp := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(restServer.LogProcessor()), // <-- 关键：让 debug 端点能读到 log
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
	)
	otel.SetLoggerProvider(lp)
	defer lp.Shutdown(ctx)

	// 3. 把 *Server 挂到 net/http 上。
	mux := http.NewServeMux()
	mux.Handle("/api/", http.StripPrefix("/api", restServer))
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	log.Println("Listening on :8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func mustBuildAgent(ctx context.Context) agent.Agent {
	model, err := gemini.NewModel(ctx, "gemini-3.1-flash-lite", &genai.ClientConfig{
		APIKey: os.Getenv("GOOGLE_API_KEY"),
	})
	if err != nil {
		log.Fatalf("gemini model: %v", err)
	}
	a, err := llmagent.New(llmagent.Config{
		Name:        "weather_time_agent",
		Model:       model,
		Description: "Answers questions about time and weather.",
		Instruction: "I can answer your questions about the time and weather in a city.",
		Tools:       []tool.Tool{geminitool.GoogleSearch{}},
	})
	if err != nil {
		log.Fatalf("llmagent: %v", err)
	}
	return a
}

// 抑制未使用导入告警
var _ = time.Second
```

## 代码逐段讲解

### 1. `adkrest.NewServer` 暴露 `SpanProcessor` / `LogProcessor`

`Server.SpanProcessor()` 在 [`server/adkrest/handler.go:93`](../../../server/adkrest/handler.go) 实现，返回的是 `s.telemetryStore.SpanProcessor()`——而后者是 `sdktrace.NewSimpleSpanProcessor(d.store)`（[`server/adkrest/internal/services/debugtelemetry.go:67`](../../../server/adkrest/internal/services/debugtelemetry.go)）。"Simple" 而非 "Batch" 是有意的：避免"span 结束 → 进入 queue → 几秒后才进 store"的延迟，ADK Web 用户期待"调完 :run 就能调 :debug 拿到数据"。

同理 `Server.LogProcessor()`（[`server/adkrest/handler.go:99`](../../../server/adkrest/handler.go)）也是 `sdklog.NewSimpleProcessor`（[`server/adkrest/internal/services/debugtelemetry.go:72`](../../../server/adkrest/internal/services/debugtelemetry.go)）。把这两个 processor 注册到全局 OTel Provider 之后，**所有** 由 ADK 框架内部 `telemetry` 包发出的 span / log（见 [`server/adkrest/internal/services/debugtelemetry.go:33`](../../../server/adkrest/internal/services/debugtelemetry.go) 引用的 `internal/telemetry`）都会进入 debug 缓存。

### 2. OTel 双 Processor 模式

`TracerProvider` 可以挂多个 `SpanProcessor`——它们构成一个"链"，每个 span 会被所有 processor 各自处理一次。我们的代码同时挂了两个：

- `restServer.SpanProcessor()` → 把 span 写入进程内 store，供 `/debug/trace/*` 端点查询
- `sdktrace.NewBatchSpanProcessor(traceExporter)` → 把 span 推到 stdout（也可换 OTLP exporter 推到 Jaeger/Tempo）

这样 debug 端点与外部可观测系统**不互斥**。`log` 端同理。

### 3. URL 派发：3 条 debug 路由

`DebugAPIRouter.Routes()` 在 [`server/adkrest/internal/routers/debug.go:34`](../../../server/adkrest/internal/routers/debug.go) 注册了 3 条 URL：

| URL 模式 | 控制器方法 | 用途 |
|---|---|---|
| `GET /debug/trace/{event_id}` | `EventSpanHandler` ([`debug.go:49`](../../../server/adkrest/controllers/debug.go)) | 单事件 span 详情（ADK Web 时间线点击展开） |
| `GET /debug/trace/session/{session_id}` | `SessionSpansHandler` ([`debug.go:91`](../../../server/adkrest/controllers/debug.go)) | 整个会话的 span 列表（按 start_time 升序） |
| `GET /apps/{app}/users/{u}/sessions/{sid}/events/{event_id}/graph` | `EventGraphHandler` ([`debug.go:103`](../../../server/adkrest/controllers/debug.go)) | 当前 event 时刻的 agent 树 DOT 源（高亮调用过的 tool/agent） |

注意第二条 URL 是 `/debug/trace/session/{session_id}`，而**不是** `/apps/{app}/users/{u}/sessions/{session_id}/debug/trace`——它是"扁平"的 debug 命名空间，不遵循 ADK 标准的资源层级。这样设计是为了让 ADK Web 在嵌入 iframe 时 URL 更短。

### 4. `EventSpanHandler` 的过滤逻辑

```go
// server/adkrest/controllers/debug.go:56
spans := c.debugTelemetry.GetSpansByEventID(eventID)
key := string(semconv.GenAIOperationNameKey)
wantedOperations := []string{"execute_tool", "generate_content"}
for _, span := range spans {
    opName := span.Attributes[key]
    if slices.Contains(wantedOperations, opName) {
        response := convertEventSpan(span)
        EncodeJSONResponse(response, http.StatusOK, rw)
        return
    }
}
http.Error(rw, fmt.Sprintf("event not found: %s", eventID), http.StatusNotFound)
```

一个 event 在 LLM 视角下通常对应一个 `generate_content` span，在 tool 视角下对应一个 `execute_tool` span。ADK Web 只想看这两种"有意义的"span；其他 `invocation` / `agent_run` 等"骨架 span"会在时间线里另外显示。所以 `wantedOperations` 这个白名单（[`server/adkrest/controllers/debug.go:59`](../../../server/adkrest/controllers/debug.go)）就是把噪音过滤掉。

`convertEventSpan`（[`server/adkrest/controllers/debug.go:74`](../../../server/adkrest/controllers/debug.go)）做了"扁平化"：把 `Attributes map[string]string` 的 key 直接平铺到响应 map 的顶层，这是 ADK Web 期待的格式——见注释"ADK web expects different format than in `SessionSpansHandler`"。

### 5. `EventGraphHandler` 与 `agentgraphgenerator`

`EventGraphHandler` 拿到 event 后，从 `event.LLMResponse.Content.Parts` 里抽出 `FunctionCall` / `FunctionResponse` 的 `Name`（[`server/adkrest/controllers/debug.go:139`](../../../server/adkrest/controllers/debug.go)），构造一个 `highlightedPairs` 列表，然后调 `services.GetAgentGraph(agent, highlightedPairs)`（[`server/adkrest/controllers/debug.go:163`](../../../server/adkrest/controllers/debug.go)）。

`GetAgentGraph` 在 [`server/adkrest/internal/services/agentgraphgenerator.go`](../../../server/adkrest/internal/services/agentgraphgenerator.go) 里用 `awalterschulze/gographviz` 把 agent 树（包括 `sub_agents` 与 `tools`）画成 DOT 源。被 `highlightedPairs` 命中的节点用 `DarkGreen`（[`agentgraphgenerator.go:31`](../../../server/adkrest/internal/services/agentgraphgenerator.go)）填充，被命中的边用 `LightGreen` 描色并保留箭头方向；非高亮节点用 `LightGray` 边线。

返回的 JSON 是 `{"dotSrc": "digraph agent_graph { ... } }`，ADK Web 拿到后调 `viz.js` 渲染成 SVG。

## 准备与运行

### 步骤 1：获取 `GOOGLE_API_KEY`

参见 [00-prerequisites.md §3](../00-prerequisites.md)。导出：

```bash
export GOOGLE_API_KEY="AIza..."
```

### 步骤 2：保存自定义 main.go 并跑

把上面"完整代码"段保存到 `examples/debug_endpoint/main.go`（或仓库外的任何 Go module），然后：

```bash
go mod init debug-endpoint-demo
go get google.golang.org/adk@$(git -C /path/to/adk rev-parse HEAD)
go get go.opentelemetry.io/otel@latest
go get go.opentelemetry.io/otel/sdk@latest
go get go.opentelemetry.io/otel/exporters/stdout/stdouttrace@latest
go get go.opentelemetry.io/otel/exporters/stdout/stdoutlog@latest
go run .
```

服务启动后日志末尾会有 `Listening on :8080`。

### 步骤 3：触发一次 agent 调用

```bash
SESSION=$(curl -s -X POST http://localhost:8080/api/apps/weather_time_agent/users/u1/sessions \
  -H 'Content-Type: application/json' -d '{}' | jq -r .id)
echo "session: $SESSION"

curl -s -X POST "http://localhost:8080/api/apps/weather_time_agent/users/u1/sessions/$SESSION:run" \
  -H 'Content-Type: application/json' \
  -d '{"app_name":"weather_time_agent","user_id":"u1","session_id":"'"$SESSION"'","new_message":{"role":"user","parts":[{"text":"What is the weather in Tokyo?"}]}}' \
  | jq '.[] | {id: .id, author: .author, content: .content.parts[0].text}'
```

记下某个 event 的 `id`（下文用 `$EVENT_ID` 占位）。

### 步骤 4：测试 3 条 debug 端点

```bash
# 1) 单事件 span 详情
curl -s "http://localhost:8080/api/debug/trace/$EVENT_ID" | jq .

# 2) 整个会话的 span 列表
curl -s "http://localhost:8080/api/debug/trace/session/$SESSION" | jq '.[].name'

# 3) 事件时刻的 agent graph (DOT 源)
curl -s "http://localhost:8080/api/apps/weather_time_agent/users/u1/sessions/$SESSION/events/$EVENT_ID/graph" | jq -r .dotSrc
```

把第 3 条的输出复制到 [viz-js.com](https://viz-js.com/) 即可看到高亮的 agent 树。

## 常见错误

1. **忘记把 `restServer.SpanProcessor()` 加到 `TracerProvider`**：结果是 `/debug/trace/*` 端点返回 404 或空数组。检查代码里 `sdktrace.WithSpanProcessor(restServer.SpanProcessor())` 是否存在。
2. **混用 `BatchSpanProcessor` 与 `DebugTelemetry`**：如果只挂 `BatchSpanProcessor`，span 不会立刻进入 debug 缓存，必须等 batch 周期（默认 5s）后才有数据。这是为什么 `DebugTelemetry` 内部坚持用 `SimpleSpanProcessor`（[`server/adkrest/internal/services/debugtelemetry.go:67`](../../../server/adkrest/internal/services/debugtelemetry.go)）。
3. **URL 写错层级**：第二条 debug 端点是 `/debug/trace/session/{session_id}`（**不**带 app_name / user_id 前缀），混淆这两者会 404。详见 [`server/adkrest/internal/routers/debug.go:52`](../../../server/adkrest/internal/routers/debug.go)。
4. **Graph 端点返回 500 "event not found"**：通常是因为 `:run` 还没产生目标 event，或者 event 不在当前 session 内。可以用 `/debug/trace/session/{sid}` 先查一遍所有 event id。
5. **span 的 `gen_ai.operation.name` 不在白名单内**：`wantedOperations`（[`server/adkrest/controllers/debug.go:59`](../../../server/adkrest/controllers/debug.go)）只放过 `execute_tool` 与 `generate_content`；自定义 agent 如果发 span 时没用 `semconv.GenAIOperationNameExecuteTool` 等标准 attribute，会被静默丢弃。这是设计而非 bug——但容易被误以为"端点坏了"。

## 关键 API 小结

| API | 位置 | 作用 |
|---|---|---|
| `adkrest.NewServer` | [`server/adkrest/handler.go:37`](../../../server/adkrest/handler.go) | 构造 `*Server`，内部创建 `DebugTelemetry` 并装配 `DebugAPIRouter` |
| `Server.SpanProcessor` | [`server/adkrest/handler.go:93`](../../../server/adkrest/handler.go) | 返回一个 `SimpleSpanProcessor`，挂到 `TracerProvider` 后 span 进入 debug 缓存 |
| `Server.LogProcessor` | [`server/adkrest/handler.go:99`](../../../server/adkrest/handler.go) | 返回一个 `SimpleProcessor`，挂到 `LoggerProvider` 后 log 进入 debug 缓存 |
| `DebugAPIController.EventSpanHandler` | [`server/adkrest/controllers/debug.go:49`](../../../server/adkrest/controllers/debug.go) | 处理 `GET /debug/trace/{event_id}` |
| `DebugAPIController.SessionSpansHandler` | [`server/adkrest/controllers/debug.go:91`](../../../server/adkrest/controllers/debug.go) | 处理 `GET /debug/trace/session/{session_id}` |
| `DebugAPIController.EventGraphHandler` | [`server/adkrest/controllers/debug.go:103`](../../../server/adkrest/controllers/debug.go) | 处理 `GET /apps/{app}/users/{u}/sessions/{sid}/events/{event_id}/graph` |
| `DebugTelemetry.GetSpansByEventID` | [`server/adkrest/internal/services/debugtelemetry.go:78`](../../../server/adkrest/internal/services/debugtelemetry.go) | 按 event id 查 span 列表 |
| `DebugTelemetry.GetSpansBySessionID` | [`server/adkrest/internal/services/debugtelemetry.go:83`](../../../server/adkrest/internal/services/debugtelemetry.go) | 按 session id 查 span 列表 |
| `services.GetAgentGraph` | [`server/adkrest/internal/services/agentgraphgenerator.go`](../../../server/adkrest/internal/services/agentgraphgenerator.go) | 把 agent 树 + 高亮对渲染为 DOT 源 |
| `semconv.GenAIOperationNameKey` | [`server/adkrest/controllers/debug.go:57`](../../../server/adkrest/controllers/debug.go) | 用于识别 `execute_tool` / `generate_content` span 的 OTel semconv key |

## 延伸阅读

- [04-deployment/01-rest-server.md](../04-deployment/01-rest-server.md) — adkrest REST server 的"三层装配"（`handler.go` → `controllers` → `routers`）
- [04-deployment/03-web-ui.md](../04-deployment/03-web-ui.md) — ADK Web 怎么消费这 3 条 debug 端点
- [06-observability/01-telemetry.md](../06-observability/01-telemetry.md) — OTel SDK 的最小装配与 `semconv` 字段含义
- 子项目深读占位：架构文档"§3.10 server 模块"对应章节（待架构文档完成）
- 子项目深读占位：架构文档"§3.9 telemetry 模块"对应章节（待架构文档完成）
- 参考示例：[examples/rest/main.go](../../../examples/rest/main.go)（最简版 REST server，未启用 debug 端点的 OTel 注入）
