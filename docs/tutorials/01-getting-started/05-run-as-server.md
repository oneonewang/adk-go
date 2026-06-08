# 部署为服务：把 Agent 暴露为 REST API

> 本教程基于 [`examples/rest/main.go`](../../../examples/rest/main.go)。它展示两种把 agent 变成网络服务的方式：
> 一种是用 ADK 自带的 `prod.NewLauncher()` 通过 `restapi` 命令行模式启动；
> 另一种是直接用 `adkrest.NewServer()` 嵌入到你自己的 `net/http` 服务器里。

## 你将学到

- `prod.NewLauncher()` 与 `full.NewLauncher()` 的区别：什么时候只用 REST/A2A
- `adkrest.NewServer` 返回的是一个 `http.Handler`，可以挂到任何 `ServeMux` 上
- `ServerConfig` 关键字段：`AgentLoader`、`SessionService`、`SSEWriteTimeout`
- 用 `curl` 调用 `/apps/.../users/.../sessions/...:run` 跑通非流式接口
- 用 `curl -N` 调用 `:runSSE` 走 Server-Sent Events 流式接收事件

## 前置条件

- [x] 已完成 [04-multi-agents.md](./04-multi-agents.md)（或至少跑通 01/02/03 中的一个）
- [x] 已设置 `GOOGLE_API_KEY`（见 [00-prerequisites.md](../00-prerequisites.md)）
- [x] 本机可访问 `generativelanguage.googleapis.com`
- [x] 已 `git clone` ADK 仓库并 `go mod download`
- [x] 已安装 `curl` 与 `jq`（用于手工测试接口）

## 核心概念

**REST API Server** 是一个实现 `http.Handler` 接口的对象（[`server/adkrest/handler.go:36`](../../../server/adkrest/handler.go)）。`adkrest.NewServer(cfg)` 会组装 5 个子路由：sessions、runtime、apps、debug、artifacts（[`server/adkrest/handler.go:46`](../../../server/adkrest/handler.go)）。其中 `/run` 与 `/runSSE` 这两个 runtime 端点是 agent 与外部世界通信的"门"——`/run` 返回完整事件数组，`/runSSE`（[`server/adkrest/controllers/runtime.go:99`](../../../server/adkrest/controllers/runtime.go)）用 SSE 协议把每个事件增量推给客户端。

**SSE（Server-Sent Events）** 是浏览器与 `curl -N` 都原生支持的单向流式协议：服务端持续写 `data: <json>\n\n`，客户端按行读取。ADK 在 `RunSSEHandler` 里先调用 `rc.SetWriteDeadline` 设定自己的写超时，再 `Flush` 头部确保客户端能立刻收到响应头，避免反向代理误判"连接空闲"。

**prod.NewLauncher vs full.NewLauncher**：`full.NewLauncher()`（`cmd/launcher/full/full.go:31`）注册 4 种模式（console、restapi、a2a、webui），适合本地开发；`prod.NewLauncher()`（`cmd/launcher/prod/prod.go:31`）只注册 restapi 和 a2a，去掉 console 与 webui，产物更小，适合生产镜像。

二者关系可以用下图表示：

```mermaid
flowchart TB
    subgraph full["full.NewLauncher"]
        F1[console]
        F2[restapi]
        F3[a2a]
        F4[webui]
    end
    subgraph prod["prod.NewLauncher"]
        P1[restapi]
        P2[a2a]
    end
    F2 -.相同 adkrest.NewServer.-> RS[adkrest.Server<br/>http.Handler]
    P1 -.相同 adkrest.NewServer.-> RS
    RS --> Mux[net/http ServeMux]
    Mux --> R1[/apps/.../users/...:run/]
    Mux --> R2[/apps/.../users/...:runSSE/]
    Mux --> R3[/apps/.../sessions/]
    Mux --> R4[/debug/trace/]
```

**看图指引**：

- `full` 与 `prod` 是同一组子 launcher 的不同子集，底层都调用 `adkrest.NewServer` 组装 HTTP handler。
- 你完全可以跳过 launcher，直接在自己的 `main.go` 里调 `adkrest.NewServer` 后挂到 `http.ServeMux` 上——这给了你"自由加健康检查、自定义鉴权"等能力。
- `runtime.go:99` 的 `RunSSEHandler` 与 `RunHandler`（runtime.go:53）是 runtime 子路由下的两个端点，分别对应同步与流式两种调用方式。

## 完整代码

完整源码在 [`examples/rest/main.go`](../../../examples/rest/main.go)（约 80 行）：

```go
// examples/rest/main.go
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

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

	model, err := gemini.NewModel(ctx, "gemini-3.1-flash-lite", &genai.ClientConfig{
		APIKey: os.Getenv("GOOGLE_API_KEY"),
	})
	if err != nil {
		log.Fatalf("Failed to create model: %v", err)
	}

	a, err := llmagent.New(llmagent.Config{
		Name:        "weather_time_agent",
		Model:       model,
		Description: "Agent to answer questions about the time and weather in a city.",
		Instruction: "I can answer your questions about the time and weather in a city.",
		Tools:       []tool.Tool{geminitool.GoogleSearch{}},
	})
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	restServer, err := adkrest.NewServer(adkrest.ServerConfig{
		AgentLoader:     agent.NewSingleLoader(a),
		SessionService:  session.InMemoryService(),
		SSEWriteTimeout: 120 * time.Second,
	})
	if err != nil {
		log.Fatalf("Failed to create REST API server: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/api/", http.StripPrefix("/api", restServer))
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	log.Println("Starting server on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
```

## 代码逐段讲解

### 1. 创建 Model 与 Agent

`gemini.NewModel` 与 `llmagent.New` 的用法跟 [01-hello-world.md](./01-hello-world.md) 完全一致，此处不重复。要点：

- `Tools: []tool.Tool{geminitool.GoogleSearch{}}` 让 agent 能联网搜实时天气——这是后续用 `curl` 演示时能拿到"非空"回答的关键。
- `Name: "weather_time_agent"` 后面在 URL 里出现：`/apps/weather_time_agent/users/.../sessions/...:run`。

### 2. 配置 `adkrest.ServerConfig`

```go
restServer, err := adkrest.NewServer(adkrest.ServerConfig{
	AgentLoader:     agent.NewSingleLoader(a),
	SessionService:  session.InMemoryService(),
	SSEWriteTimeout: 120 * time.Second,
})
```

`ServerConfig` 是 `adkrest.NewServer` 的入口结构体，定义在 [`server/adkrest/handler.go:62`](../../../server/adkrest/handler.go)。最常用的字段：

- `AgentLoader`：必须填。`agent.NewSingleLoader(a)` 把单个 agent 包装成 `Loader`（详见 [01-hello-world.md](./01-hello-world.md#3-配置-loader)）。
- `SessionService`：管理会话状态。`session.InMemoryService()` 把 session 存在内存里，重启进程后丢失；想要持久化就换成 `session/database`（[05-session.md](../../architecture/03-modules/05-session.md)）。
- `SSEWriteTimeout`：流式响应的写超时。`120s` 适合长任务；如果 agent 经常要跑几分钟，相应调大。
- `MemoryService` / `ArtifactService` / `PluginConfig` / `DebugConfig`：本教程保持零值（不传）即可。

`NewServer` 内部会用这些字段构造 5 个 controller 并挂到 gorilla/mux 路由上（[`server/adkrest/handler.go:46`](../../../server/adkrest/handler.go)）。

### 3. 挂载到 `net/http` ServeMux

```go
mux := http.NewServeMux()
mux.Handle("/api/", http.StripPrefix("/api", restServer))
mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
})
```

`adkrest.Server` 实现了 `http.Handler`（`ServeHTTP` 在 [`server/adkrest/handler.go:87`](../../../server/adkrest/handler.go)），所以它**不绑定任何 web 框架**——你可以挂到 `net/http`、gorilla/mux、chi、gin 等等。`http.StripPrefix("/api", restServer)` 把所有 `/api/*` 请求转发给 REST server，去掉 `/api` 前缀后 ADK 内部按 `/run`、`/runSSE` 等路由分发。

`/health` 是顺手加的健康检查端点，云上部署时给负载均衡器探活用。

### 4. 启动 HTTP 服务

```go
log.Println("Starting server on :8080")
log.Fatal(http.ListenAndServe(":8080", mux))
```

标准的 `net/http` 启动方式。如果你想换端口，改成 `:9090` 即可。

### 5.（可选）用 `prod.NewLauncher` 命令行模式

```go
// 替代 2~4 段的等价写法
import "google.golang.org/adk/cmd/launcher/prod"

config := &launcher.Config{AgentLoader: agent.NewSingleLoader(a)}
l := prod.NewLauncher()
log.Fatal(l.Execute(ctx, config, []string{"restapi", "--port=8080"}))
```

`prod.NewLauncher()` 只注册 `restapi` + `a2a` 两种子 launcher（[`cmd/launcher/prod/prod.go:31`](../../../cmd/launcher/prod/prod.go)），启动后接受 `--port`（[`cmd/launcher/web/web.go:233`](../../../cmd/launcher/web/web.go)）、`--webui_address`（[`cmd/launcher/web/api/api.go:142`](../../../cmd/launcher/web/api/api.go)）、`--sse-write-timeout` 等参数。**这种模式适合你只想让"一份 agent 代码"既能本地 console 调试、又能上生产 REST 服务**的场景。

## 准备与运行

### 步骤 1：确认 API key

```bash
echo $GOOGLE_API_KEY   # 应输出 AIza...
```

未设置时回到 [00-prerequisites.md §3](../00-prerequisites.md) 获取。

### 步骤 2：启动服务

```bash
go run ./examples/rest
```

成功时日志末尾会打印：

```
Starting server on :8080
API available at http://localhost:8080/api/
Health check at http://localhost:8080/health
```

### 步骤 3：测试 `/health`

```bash
curl -s http://localhost:8080/health
# 期望：OK
```

### 步骤 4：测试非流式 `:run`

先创建 session，再发起 run：

```bash
APP=weather_time_agent
USER=u1
SID=s1

curl -s -X POST http://localhost:8080/api/apps/$APP/users/$USER/sessions \
  -H "Content-Type: application/json" -d '{}'

curl -s -X POST http://localhost:8080/api/apps/$APP/users/$USER/sessions/$SID:run \
  -H "Content-Type: application/json" \
  -d '{
    "appName": "'"$APP"'",
    "userId": "'"$USER"'",
    "sessionId": "'"$SID"'",
    "newMessage": {
      "role": "user",
      "parts": [{"text": "What is the weather in Tokyo?"}]
    }
  }' | jq .
```

期望：返回 JSON 数组，里面有一条 `author=weather_time_agent` 且 `content.parts[*].text` 形如 `Currently in Tokyo ...` 的事件。

### 步骤 5：测试流式 `:runSSE`

```bash
curl -N -X POST http://localhost:8080/api/apps/$APP/users/$USER/sessions/$SID:runSSE \
  -H "Content-Type: application/json" \
  -d '{
    "appName": "'"$APP"'",
    "userId": "'"$USER"'",
    "sessionId": "'"$SID"'",
    "newMessage": {
      "role": "user",
      "parts": [{"text": "And in Paris?"}]
    }
  }'
```

`-N` 关闭 curl 的输出缓冲。期望：立刻看到 `data: {...}` 多行连续打印，agent 边思考边吐事件——这是 `RunSSEHandler` 在 [`server/adkrest/controllers/runtime.go:99`](../../../server/adkrest/controllers/runtime.go) 的行为。

## 常见错误

- **`404 Not Found: .../sessions/s1:run`** —— URL 里冒号转义。`curl` 默认不会转义，需要 `:run` 整段在路径里直接出现；或检查 `http.StripPrefix` 是否把 `/api` 剥干净。
- **`Failed to create REST API server`** —— `ServerConfig` 字段缺失。`AgentLoader` 与 `SessionService` 至少要填一个（虽然不强制，但实际请求会失败）。`InMemoryService` 一定要传。
- **`write tcp: i/o timeout`** —— `SSEWriteTimeout` 太小，被网关（nginx / GCP Load Balancer）断了。把它调到 `300s` 或在网关侧加 `proxy_read_timeout 300s`。
- **`429 Too Many Requests` 从 Gemini 返回** —— 短时间内并发 `:run` 太多。Gemini 1.x 系列按 RPM 限流；可以加一个 `chan struct{}` 限流或换更高配额 key。
- **`http: invalid header` 在 SSE 模式下** —— 你在 `RunSSEHandler` 之前手动 `WriteHeader` 了。SSE 必须**先**设置 `Content-Type: text/event-stream` 并 `Flush` 头，**再**开始写 body。

## 关键 API 小结

| API | 位置 | 作用 |
|---|---|---|
| `adkrest.NewServer` | [`server/adkrest/handler.go:37`](../../../server/adkrest/handler.go) | 组装 5 个子路由，返回 `*Server`（实现 `http.Handler`） |
| `adkrest.ServerConfig` | [`server/adkrest/handler.go:62`](../../../server/adkrest/handler.go) | REST server 配置：`AgentLoader` / `SessionService` / `SSEWriteTimeout` 等 |
| `RuntimeAPIController.RunHandler` | [`server/adkrest/controllers/runtime.go:53`](../../../server/adkrest/controllers/runtime.go) | 处理 `:run`——非流式，返回完整事件数组 |
| `RuntimeAPIController.RunSSEHandler` | [`server/adkrest/controllers/runtime.go:99`](../../../server/adkrest/controllers/runtime.go) | 处理 `:runSSE`——SSE 流式，事件增量推送 |
| `prod.NewLauncher` | [`cmd/launcher/prod/prod.go:31`](../../../cmd/launcher/prod/prod.go) | 返回只含 `restapi` + `a2a` 的轻量 launcher |
| `web.go --port` | [`cmd/launcher/web/web.go:233`](../../../cmd/launcher/web/web.go) | `restapi` 子命令的监听端口（默认 8080） |
| `session.InMemoryService` | [`session/service.go`](../../../session/service.go) | 进程内 session 存储（重启即丢） |

## 延伸阅读

- 架构文档：[核心抽象一览](../../architecture/00-overview.md#3-核心抽象一览) —— 理解 `Agent` / `Runner` / `Session` / `Tool` 在 REST 请求中各自扮演的角色
- 架构文档：[F4 SSE 流式响应](../../architecture/01-core-flows.md#f4-sse-流式响应)（如该章节尚未发布，先看 [`server/adkrest/controllers/runtime.go:99`](../../../server/adkrest/controllers/runtime.go) 的源码）
- 源码：[`examples/rest/main.go`](../../../examples/rest/main.go) —— 本教程讲解的 80 行可运行示例
- 源码：[`server/adkrest/handler.go`](../../../server/adkrest/handler.go) —— `adkrest.NewServer` 完整实现
- 源码：[`cmd/launcher/prod/prod.go`](../../../cmd/launcher/prod/prod.go) —— 生产 launcher 的子集定义
- 未来子项目深读占位：`adkrest` 内部 gorilla/mux 路由分组、Debug Telemetry 与 `cmd/launcher/web/api/api.go` 的 CORS 中间件
