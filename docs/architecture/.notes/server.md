# server 模块阅读笔记

## 1. 一句话定位

`server` 包为 ADK 智能体提供三类对外协议服务：A2A 协议适配（`adka2a`，让 ADK agent 接入 A2A 协议）、REST/HTTP 通用接口（`adkrest`，包含 sessions / runtime / artifacts / apps / debug / triggers 多个资源子集）、以及 Vertex AI Agent Engine 部署形态的 RPC 网关（`agentengine`，把 a2a 风格的方法路由为 `class_method` 调度协议）。

## 2. 子包/子目录结构

- `adka2a/`（`server/adka2a:1`，已弃用）：旧版 A2A executor 兼容层，所有函数通过 `a2av0` 在 v1↔v2 a2a-go 数据类型之间做转换，内部委托给 `v2`。
- `adka2a/v2/`（`server/adka2a/v2:1`）：当前主版本 A2A 适配器。实现 `a2asrv.AgentExecutor`，把 `a2a.Message` 转 `*genai.Content`、把 `*session.Event` 转 `a2a.TaskArtifactUpdateEvent` / `a2a.TaskStatusUpdateEvent`。还提供 parts / agent card / events / metadata / input-required / artifact / executor-plugin 等子文件。
- `adkrest/`（`server/adkrest:1`）：基于 `gorilla/mux` 的 HTTP REST 服务。入口 `NewServer(ServerConfig)` 拼装六大子路由（sessions / runtime / apps / debug / artifacts / eval），公开 `ServeHTTP`、`SpanProcessor`、`LogProcessor` 三个方法。
- `adkrest/controllers/`：各资源 HTTP handler，包含 `runtime.go`（Run/RunSSE/RunLive）、`sessions.go`、`apps.go`、`artifacts.go`、`debug.go`、`errors.go`、`handlers.go`，以及 `triggers/` 子包处理 Pub/Sub 与 Eventarc 触发器。
- `adkrest/internal/`：REST 内部实现。`routers/` 定义 route 列表（`Route` + `Router` 接口）；`models/` 是 REST DTO（`Session`、`Event`、`RunAgentRequest` 等）；`services/` 提供 `DebugTelemetry`（OpenTelemetry 内存导出 + LRU trace 缓存）和 `agentgraphgenerator`（基于 gographviz 渲染 agent 调用图）。
- `agentengine/`（`server/agentengine:1`）：Vertex AI Agent Engine 形态的 `http.Handler` 工厂。`NewHandler` 用一份 `*launcher.Config` 构建非流式 / 流式两套 controller，挂在 `/reasoning_engine` 和 `/stream_reasoning_engine` 路径上。
- `agentengine/controllers/method/`：每个 a2a 风格的 "class method"（`async_create_session` / `async_get_session` / `async_list_sessions` / `async_delete_session` / `async_stream_query`）对应一个 `MethodHandler` 实现，统一通过 `Name()` + `Handle()` + `Metadata()` 暴露。
- `agentengine/internal/`：routers、snake_case 编码 helper（`convertSnake` + `EmitJSON`）和入参 / 出参 model。

## 3. 核心类型与接口

- `adka2a/v2.Executor`（`server/adka2a/v2/executor.go:149`）— 实现 `a2asrv.AgentExecutor`，持有 `ExecutorConfig`，方法 `Execute` / `Cancel` / `Cleanup` 返回 `iter.Seq2[a2a.Event, error]`。doc-comment 详细描述了它把 `session.Event` 翻译为 TaskArtifactUpdateEvent / TaskStatusUpdateEvent 的规则（包含 input-required 与 failed 分支）。
- `adka2a.Executor`（`server/adka2a/executor.go:139`，deprecated）— v1 a2a-go 兼容壳，内部组合 `v2.Executor`，所有方法负责 `a2av0.FromV1Event` / `ToV1Event` 转换。
- `adka2a/v2.ExecutorContext`（`server/adka2a/v2/executor_context.go:29`）— `context.Context` 的扩展接口，暴露 `SessionID` / `UserID` / `AgentName` / `ReadonlyState()` / `Events()` / `UserContent()` / `RequestContext()` 给回调使用；旧 v1 端在 `server/adka2a/executor.go:326` 重新声明了一次同形接口以便外部依赖。
- `adka2a/v2.Runn` / `Runner`（`server/adka2a/v2/executor.go:74`）— `Runner` 是 `iter.Seq2[*session.Event, error]` 形式的最小接口（与 `runner.Runner` 同形），`RunnerProvider` 工厂可由用户注入；`newDefaultRunnerProvider`（`executor.go:430`）默认调用 `runner.New` 并把 `plugin.Plugin` 装到 PluginConfig 上。
- `adka2a/v2.OutputMode`（`server/adka2a/v2/executor.go:60`）— `OutputArtifactPerRun`（每次 run 一个 artifact）vs `OutputArtifactPerEvent`（每个非 partial event 一个 artifact，partial 增量 append）。
- `adka2a/v2.eventToArtifactTransform`（`server/adka2a/v2/task_artifact.go:30`）— 内部策略接口，两个实现 `artifactMaker` / `legacyArtifactMaker` 分别对应 `OutputArtifactPerEvent` / `OutputArtifactPerRun` 行为；legacy 模式对 partial event 单独维护一个 "ephemeral" artifact id，并在结束时以 `LastChunk=true` + 空 parts 复位。
- `adkrest.Server`（`server/adkrest/handler.go:81`）— `http.Handler` 实现，组合 `*mux.Router` 与 `*services.DebugTelemetry`；构造时初始化 debug telemetry、5 个业务子路由 + 1 个 eval 占位子路由。
- `adkrest.ServerConfig`（`server/adkrest/handler.go:63`）— 接收 `session.Service` / `memory.Service` / `agent.Loader` / `artifact.Service` / `runner.PluginConfig` / `DebugTelemetryConfig` / SSE 写超时。
- `agentengine.AgentEngineAPIController`（`server/agentengine/controllers/agentengine.go:32`）— 把请求里的 `class_method` 路由到 `method.MethodHandler` map；并强制 max payload size / SSE write deadline。
- `method.MethodHandler`（`server/agentengine/controllers/method/method.go:27`）— 三个方法的接口：`Name()` / `Handle(ctx, rw, payload)` / `Metadata() *structpb.Struct`；`Metadata` 给 Agent Engine 部署时声明 capability。
- `agentengine.RetriableRunner`（`server/adkrest/controllers/triggers/triggers.go:38`，被 agentengine 之外的 triggers 引用，agentengine 自身不直接复用）— 内部带 `*runner.Runner`，对 429 / ResourceExhausted 错误做指数退避重试。

## 4. 关键数据结构

- `adka2a/v2.ExecutorConfig`（`server/adka2a/v2/executor.go:95`）— A2A executor 的全量配置：`RunnerConfig` / `RunnerProvider` / `RunConfig` / 三个回调（`BeforeExecuteCallback` / `AfterEventCallback` / `AfterExecuteCallback`） / 两个 part 转换器（`A2APartConverter` / `GenAIPartConverter`） / `OutputMode` / `A2AExecutionCleanupCallback`。
- `adka2a/v2.invocationMeta`（`server/adka2a/v2/metadata.go:53`）— 一次执行期间共享的元数据：userID / sessionID / agentName / reqCtx / eventMeta（含 app_name、user_id、session_id 的 `adk_*` 前缀键）。
- `adka2a/v2.executorPlugin`（`server/adka2a/v2/executor_plugin.go:25`）— 拦截 `BeforeRunCallback` 把 `agent.InvocationContext.Session()` 缓存到 `invocationSession`，让 `ExecutorContext` 后续能访问 session 状态和事件。
- `adka2a/v2.eventProcessor`（`server/adka2a/v2/processor.go:35`）— 把单个 `session.Event` 转为 `a2a.TaskArtifactUpdateEvent`；持有 `terminalActions`（`session.EventActions`）和 `failedEvent` / `inputRequiredProcessor.event` 两个延迟发送的终态事件。
- `adka2a/v2.artifactMaker`（`server/adka2a/v2/task_artifact.go:26`）— 按 `event.Author` 维护 `lastAgentPartialArtifact` 表，决定 `Append` 行为与是否封口（`LastChunk = !event.Partial`）。
- `adka2a/v2.legacyArtifactMaker`（`server/adka2a/v2/task_artifact.go:70`）— `OutputArtifactPerRun` 路径下的 artifact 管理：`responseID` / `partialResponseID`；partial 事件以 `metadataPartialKey: true` 标记并在结束用 `LastChunk=true` + 空 data part 复位。
- `adkrest.RuntimeAPIController`（`server/adkrest/controllers/runtime.go:37`）— 字段：`sseTimeout` / `sessionService` / `memoryService` / `artifactService` / `agentLoader` / `pluginConfig` / `autoCreateSession`；SSE 与 WebSocket 都使用它作为入口。
- `adkrest.internal.services.spanStore`（`server/adkrest/internal/services/debugtelemetry.go:128`）— 用 `sync.RWMutex` 保护 4 张索引：`recordsByTraceID`（`hashicorp/golang-lru/v2` 实现 LRU，默认容量 10_000）、`recordsBySpanID`、`traceIDsBySessionID`、`recordsByEventID`；为避免 `GetByEventID` 绕过 LRU 触发淘汰，实现了 `touchTraces`。
- `adkrest.controllers.triggers.PubSubController` / `EventarcController`（`server/adkrest/controllers/triggers/pubsub.go:33` / `eventarc.go:34`）— 都内嵌一个 `*RetriableRunner` 并以 `chan struct{}` 计数信号量（容量 = `TriggerConfig.MaxConcurrentRuns`）做并发限流。
- `agentengine.Query` / `StreamQueryRequest` / `CreateSessionRequest` 等 DTO（`agentengine/internal/models/*.go`）— 全部以 `class_method` + `input` + `output` 形态描述，对齐 Vertex AI `aiplatform` 推理引擎 RPC。
- `agentengine.MethodHandler` 实现（`server/agentengine/controllers/method/{create,get,list,delete}_session.go`）— 都是同构 struct：`sessionService` / `agentEngineID` / `methodName` / `apiMode`；`Handle` 都走 `json.Unmarshal(payload, ...)` → 调 session service → 用 `json.NewEncoder(rw).Encode` 写回。
- `agentengine.internal.helper.emit_json.EmitJSON`（`agentengine/internal/helper/emit_json.go:47`）— 先 `ConvertSnake`（`encode.go:30`，基于 reflect + json tag 的 snake_case 转换）再 `json.NewEncoder` + `flush`。

## 5. 关键流程

1. **A2A Execute（`adka2a/v2`）** — 入口 `Executor.Execute`（`executor.go:161`）。步骤：`message != nil` 检查 → `toGenAIContent` 转换入参 → `newExecutorPlugin` 建 executorPlugin（捕获 session 句柄）→ 调 `RunnerProvider` 拿 runner → `BeforeExecuteCallback` → `HandleInputRequired`（`input_required.go:152`，校验上一轮 input-required 是否所有 function call 都有响应）→ 若无 `StoredTask` 发出 `NewSubmittedTask` → `prepareSession`（Get→Create）→ 发 `TaskStateWorking` → 根据 `OutputMode` 选 `artifactMaker` / `legacyArtifactMaker` → `process` 循环跑 `r.Run(...)` → 最终 `writeFinalTaskStatus` 发送 completed / failed / input-required。出口：迭代器关闭或首个 error。
2. **A2A Cleanup（`adka2a/v2`）** — `Executor.Cleanup`（`executor.go:249`）。检测 `StoredTask.State == TaskStateInputRequired` 且刚被取消时，调 `cancelChildInputRequiredTasks`（`executor.go:286`）从 session.Events 反查每个 pending long-running call 的 taskID 并用 `iremoteagent.CreateA2AClient(...).CancelTask` 通知子 agent；最后把 `subAgentCards` 列表和 `cause` 透传给 `A2AExecutionCleanupCallback`（缺省则 `log.Warn`）。
3. **REST Run + SSE** — `RuntimeAPIRouter.Routes`（`internal/routers/runtime.go:34`）注册 `/run` 与 `/run_sse`。`RunSSEHandler`（`controllers/runtime.go:99`）用 `http.NewResponseController` 设 `SetWriteDeadline` → 解 `RunAgentRequest`（`internal/models/runtime.go:23`）→ `validateSessionExists` → `getRunner` 调 `runner.New` → 写 SSE 头并 `Flush` → 迭代 `r.Run` 输出 `data: <json>\n\n`，途中出错用 `flashErrorEvent`（`runtime.go:171`）输出 `event: error` 而不是断开连接。
4. **REST Run Live (WebSocket)** — `RunLiveHandler`（`controllers/runtime.go:247`）升级到 websocket 后同时跑两段：主循环 `eventIter → ws.WriteJSON` 推回客户端；goroutine 反向 `ws.ReadMessage` 把 binary 直接当 `audio/pcm;rate=16000` 推给 `liveSession.Send`，text 解码为 `LiveRequest`（含 `Blob` / `ActivityStart` / `ActivityEnd`）。两段任一关闭都会触发 `liveSession.Close()` 和 websocket close frame。
5. **REST Debug / Trace** — `DebugAPIController`（`controllers/debug.go:33`）暴露 `/debug/trace/{event_id}` / `/debug/trace/session/{session_id}` / `/events/{event_id}/graph`。`EventSpanHandler` 只保留 `execute_tool` 和 `generate_content` 两种 op 属性的 span；`EventGraphHandler` 用 `services.GetAgentGraph` 调 `agentgraphgenerator.GetAgentGraph`（`services/agentgraphgenerator.go:280`）用 gographviz 渲染 dot，节点区分 `agentinternal.TypeLoopAgent` / `TypeSequentialAgent` / `TypeParallelAgent` 三类 cluster。
6. **Triggers (Pub/Sub / Eventarc)** — `RetriableRunner.RunAgent`（`controllers/triggers/triggers.go:47`）先创建新 session（"each retry = new session"），再调 `runAgentWithRetry`（`triggers.go:87`）：`isResourceExhausted`（429 / ResourceExhausted 字符串匹配）触发指数退避 `calculateBackoff` = `base*2^i + 0~50% jitter`，超过 `MaxRetries` 返回错误让上游 Pub/Sub/Eventarc 重试。Eventarc 控制器额外区分 structured (`application/cloudevents+json`) 和 binary（`ce-*` headers）两种 CloudEvents 模式。
7. **Agent Engine method dispatch** — `NewHandler`（`agentengine/handler.go:39`）把 `*launcher.Config` 注入两份 controllers（流式 + 非流式），分别挂 `/reasoning_engine` 和 `/stream_reasoning_engine`。请求体先 `io.LimitReader(req.Body, maxPayloadSize)` 限大小 → `json.Unmarshal` 到 `Query{ClassMethod, Input}` → `handleQuery` 查 `handlers[classMethod]` → `MethodHandler.Handle(ctx, rw, payload)`。`streamQueryHandler.streamJSONL`（`method/stream_query.go:60`）在 headers 已写后只能走 `helper.EmitJSONError` 而不能改 HTTP 状态。
8. **Method Metadata 注册** — `ListClassMethods`（`agentengine/handler.go:100`）用空 `launcher.Config` 调 `listNonStreamHandlers + listStreamHandlers` 拿每个 handler 的 `Metadata()` 序列化为 `[]*structpb.Struct`，给 adkgo 部署命令写 capability 清单用。

## 6. 扩展点

- A2A 端：
  - `RunnerProvider`（`adka2a/v2/executor.go:81`）— 替换默认 runner 创建逻辑，可挂额外 plugin。
  - `BeforeExecuteCallback` / `AfterEventCallback` / `AfterExecuteCallback` / `A2AExecutionCleanupCallback`（`executor.go:37-57`）— 4 个生命周期回调，签名清晰。
  - `A2APartConverter` / `GenAIPartConverter`（`executor.go:49-54`）— 自定义 part 序列化；返回 nil 表示丢弃。
  - `OutputMode`（`executor.go:60-70`）— 通过 `eventToArtifactTransform`（`task_artifact.go:30`）扩展 artifact 行为。
  - `BuildAgentSkills`（`adka2a/v2/agent_card.go:33`）— 外部可读入口，doc 没说可注入；只能通过实现新的 `iagent.Agent` / `llminternal.Agent` 自动扩展。
- REST 端：
  - `ServerConfig`（`adkrest/handler.go:63`）— 注入所有 service；`DebugConfig.TraceCapacity` 调 LRU 容量。
  - `routers.Router`（`adkrest/internal/routers/routers.go:36`）— `Routes() Routes` 模式可注册新子路由；`EvalAPIRouter` 是占位实现（`eval.go:23`），所有路径都返回 501。
  - `controllers.NewErrorHandler`（`controllers/handlers.go:43`）— 任何 `func(http.ResponseWriter, *http.Request) error` 都可被包成标准错误返回。
  - `triggers.PubSubController` / `EventarcController`（`pubsub.go:33` / `eventarc.go:34`）— 通过构造函数注入并发上限和重试参数 `TriggerConfig`（`triggers/config.go:20`）。
- Agent Engine 端：
  - `MethodHandler`（`agentengine/controllers/method/method.go:27`）— 三方法接口；新建方法只需实现并加入 `listNonStreamHandlers` / `listStreamHandlers`。
  - `AgentEngineAPIController` 强制 `Name()` 唯一（`agentengine.go:43`），所以命名就是 RPC endpoint 名字。
  - `helper.ConvertSnake`（`agentengine/internal/helper/encode.go:30`）— 通过 `pathToName` hook（`encode.go` 中有相关函数）可定制字段名映射。

## 7. 错误处理

- A2A 端：executor 内所有错误通过 yield `(nil, err)` 透传；失败语义被 `writeFinalTaskStatus` 收敛为 `TaskStateFailed` / `TaskStateInputRequired` 终态事件（`executor.go:373`），调用方在迭代器里看到首个 `(nil, err)` 即终止。`errorFromResponse`（`processor.go:187`）把 `model.LLMResponse.ErrorCode/ErrorMessage` 包装为 error，`toTaskFailedUpdateEvent`（`processor.go:178`）把 error 注入 `Message.Parts` 并加 `metadataIsErrMessageKey=true` 标记。
- REST 端：
  - `statusError`（`adkrest/controllers/errors.go:17`）— 自定义 `error` 类型，带 `Code int` 字段。`NewErrorHandler` 探测该类型决定 HTTP status。
  - 解码错误一律 `http.StatusBadRequest`；`sessionService.Get` / `runner.New` 失败 → `http.StatusInternalServerError`（或 `StatusNotFound` 当 Get 失败）。
  - SSE 流中（`runtime.go:99`）不走 `NewErrorHandler`，直接 `http.Error` + `flashErrorEvent` + `log.Printf`；这是因为 header 已经发出不能再换 status。
- Triggers 端：`respondError` / `respondSuccess`（`triggers/triggers.go:120-128`）统一以 `models.TriggerResponse{Status: ...}` 写回；PubSub 默认 userID `pubsub-caller`、Eventarc 默认 `eventarc-caller`。
- Agent Engine 端：`method.Handle` 内部 `json.Unmarshal` / `session.X` 失败都包成 `fmt.Errorf("... failed: %v", err)` 透传；在 `streamQueryHandler` 中 "from this moment on we must not return error. Instead, it should be handled by using helper.EmitJSONError"（`stream_query.go:96`）— 即 SSE header 已发后只能 `EmitJSONError`，不能再用 HTTP status。
- 典型失败模式：
  - A2A 长 function call 未匹配响应 → `makeInputMissingErrorMessage` 报 `no input provided for function call ID %q`（`input_required.go:245`）。
  - A2A subagent 取消失败 → `errors.Join(failures...)` 聚合并 `log.Warn`（`executor.go:266`）。
  - REST `validateSessionExists` 失败 → 404（`runtime.go:196`）。
  - PubSub 重试用尽 → 把 `runErr` 返回给上游触发器（`triggers/triggers.go:117`）。
  - Debug API `EventSpanHandler` 找不到匹配 op 的 span → 404 `"event not found: %s"`（`debug.go:69`）。

## 8. 并发与性能

- A2A：每个 Execute 是独立 goroutine，无共享状态（executor 字段只读 `config`）；`executorPlugin.invocationSession` 写入是 per-execute 的（plugin 在 `NewExecutor` 创建，但被 `BeforeRunCallback` 写入）。`cancelChildInputRequiredTasks` 注释里 `TODO(yarolegovich): run in parallel (how to limit?)`（`executor.go:311`），目前是串行取消并 `clientCache` 复用 A2A client。
- REST `RuntimeAPIController.RunLiveHandler` 显式起 1 个 read goroutine 读 WebSocket，主 goroutine 写；任一关闭都 `liveSession.Close()`。
- `DebugTelemetry.spanStore`（`debugtelemetry.go:128`）：`sync.RWMutex` 保护 4 张表；`getSpansByEventID` 走 `RLock` + `slices.Clone` 防 race；`recordsByTraceID` 用 `hashicorp/golang-lru/v2` 容量限速，默认 10_000 traces。
- REST `triggers`：用 `chan struct{}` 信号量限制 `MaxConcurrentRuns`。
- 性能调优点：
  - `RuntimeAPIController` 每次请求都 `runner.New`（`runtime.go:214`），高频场景下会重复注入 plugin / 解析 config，缺缓存。
  - `convertSnake`（`encode.go:42`）用 reflect 递归；流式接口下每个 event 都会走一遍。
  - `StreamReasoningEngineAPIRouter` 路径：每个请求再 new runner（`stream_query.go:194`），无连接级 runner 复用。

## 9. 依赖与被依赖

- 本模块导入（按出现频率高到低）：
  - 内部：`google.golang.org/adk/{agent,artifact,memory,runner,session,plugin,model,cmd/launcher,internal/agent,internal/llminternal,internal/agent/remoteagent,internal/converters,internal/utils,internal/telemetry}`。
  - 外部：`github.com/a2aproject/a2a-go/{a2a,a2asrv,eventqueue,log}`（v1 + v2 双版本共存，v1 只在 `adka2a` deprecated 兼容层）、`github.com/gorilla/mux`、`github.com/gorilla/websocket`、`github.com/awalterschulze/gographviz`、`github.com/mitchellh/mapstructure`、`github.com/hashicorp/golang-lru/v2`、`go.opentelemetry.io/otel/{sdk/log,sdk/trace,semconv/v1.36.0,attribute,trace}`、`google.golang.org/genai`、`google.golang.org/protobuf/types/known/structpb`。
- 哪些模块导入本模块：
  - `cmd/launcher/web/api/api.go` 导入 `adkrest`（`NewServer`）。
  - `cmd/launcher/web/a2a/a2a.go` 导入 `adka2a/v2`（`BuildAgentSkills` + `NewExecutor`）。
  - `cmd/launcher/web/agentengine/agentengine.go` 导入 `agentengine`（`NewHandler`）。
  - `cmd/launcher/web/triggers/eventarc/eventarc.go` 导入 `adkrest/controllers/triggers`。
  - `cmd/adkgo/internal/deploy/agentengine/agentengine.go` 导入 `agentengine`（`ListClassMethods`）。
  - `agent/remoteagent/a2a_agent.go` 与 `agent/remoteagent/v2/a2a_agent.go` 用 `ToSessionEventWithParts` / `EventToMessage`（`adka2a` / `adka2a/v2`）做 client 侧事件互转。
  - `examples/{rest,a2a,bidi,bidi/streamingtool,bidi/sequential}` 直接示例调用。
- 注意：`adkrest/controllers` 和 `adkrest/internal/*` 当前是公开包（`controllers/handlers.go:23` 注释 "TODO: Move to an internal package"），未来可能改路径。

## 10. 测试与可观察性

- 测试文件：
  - `adka2a/v2/{parts_test.go,events_test.go,executor_test.go,metadata_test.go,processor_test.go,agent_card_test.go}` — 全量单测覆盖 parts、事件、metadata、agent card 等纯函数。
  - `adkrest/controllers/{sessions_test.go,runtime_test.go,debug_test.go}` — REST 集成测试，使用 `testsessionservice` fake。
  - `adkrest/controllers/triggers/{pubsub_test.go,eventarc_test.go}`。
  - `adkrest/internal/services/agentgraphgenerator_test.go` (692 行) + `debugtelemetry_test.go` (474 行) — 较厚。
  - `adkrest/internal/fakes/testsessionservice.go` — 共享 fake。
  - `agentengine/controllers/method/stream_query_test.go`。
  - `agentengine/internal/helper/encode_test.go`。
- 可观察性埋点：
  - `adkrest/internal/services/debugtelemetry.go` 暴露 `SpanProcessor()` / `LogProcessor()`（`debugtelemetry.go:67-75`），是 `sdktrace.NewSimpleSpanProcessor(d.store)` 的薄包装；用户注册到自己的 TracerProvider/LoggerProvider 即可让 `/debug/trace` 抓到数据。
  - A2A 端用 `github.com/a2aproject/a2a-go/log`（`executor.go:26` 之类）记录错误；不入 OTel。
  - REST `runtime.go` 用 `log.Printf`（标准 log 包），不接 OTel。
  - Agent Engine `agent_engine.go:60-72` 用 `log.Println` 打印方法清单；错误同样 `log.Printf`。
  - `triggers/triggers.go:131` 用 `strings.Contains(err.Error(), "429" || "ResourceExhausted")` 字符串嗅探判定 rate-limit，没有专门的 metric。
  - LRU 容量默认 10_000（`debugtelemetry.go:37`）由 `DebugTelemetryConfig.TraceCapacity` 调；无显式命中指标。
- span / log 注入约定：debug 包只关心 op name `execute_tool` 和 `generate_content`（`debug.go:59`），其它 span 会被 `EventSpanHandler` 过滤掉。

## 11. 文档写作提示

- 必须写：
  - A2A v2 是唯一受支持的版本；`adka2a`（根目录）已 deprecated，所有外部集成应走 `adka2a/v2`（`executor.go:17` 注释 "Deprecated"）。
  - REST 是 ADK 自带的 HTTP 接口，但 `controllers` 子包 `TODO: Move to an internal package`（`controllers/handlers.go:23`），API 表面可能在未来变；建议文档标注 "experimental"。
  - Agent Engine 协议是 a2a-style `class_method` 调度，不是 OpenAPI；每个方法通过 `Metadata()` 暴露 schema。`ListClassMethods` 是部署前的能力发现入口。
  - `OutputMode` 两种模式的语义差异（artifact id 分配方式、partial 事件如何增量）需要写清楚，否则用户会被 `LastChunk=true` 的部分复位事件迷惑。
  - Debug API 的 span/log 必须注册到 `Server.SpanProcessor()` / `LogProcessor()` 才会出现在 `/debug/trace/*`；这是隐式配置。
- 可以省略：
  - v1 `adka2a` 兼容壳的具体转换细节（`a2av0.FromV1Event` 等），对终端用户不直接可见。
  - `convertSnake` 的 reflect 递归细节（只影响 agentengine 输出 JSON 命名）。
  - `agentgraphgenerator` 内部 cluster/edge 颜色常量（`DarkGreen` / `LightGreen` 等）。
- 潜在坑：
  - `adkrest/handler.go:46` 的 TODO：`/run`、`/run_sse` 等路径未来可能允许 prefix 配置。
  - `Executor.Cleanup` 中取消 subagent 仍是串行（`executor.go:311` TODO），高频场景需自限流。
  - A2A executor 在 `runner.New` 时会自动把 `executorPlugin` 加入 `PluginConfig.Plugins`（`executor.go:440`），如用户 RunnerProvider 不复用 default path 容易漏挂。
  - `RuntimeAPIController.RunLiveHandler` 默认 `MaxLLMCalls=100`（`runtime.go:302`）且 `ResponseModalities=[ModalityAudio]`、transcription 打开，是 Gemini Live API 友好默认值，不是任意 agent 都适用。
  - Debug LRU 容量只调 `TraceCapacity`；过多事件会让 LRU 抖动。
  - Triggers 字符串嗅探 "429" / "ResourceExhausted" 对错误信息不稳定的 LLM 错误码不鲁棒。
  - `controller` 包还在公开路径，引用时尽量走 `adkrest` 顶层入口。
- 建议章节顺序（文档作者参考）：
  1. server 模块作用总览（3 种协议 + 关系）
  2. A2A — v2 API + lifecycle 回调 + OutputMode
  3. REST — 路由表 + 错误模型 + SSE/WebSocket 注意
  4. Agent Engine — class_method 调度协议 + MethodHandler 扩展
  5. Debug / Trace / 触发器
  6. 弃用与迁移说明（v1 adka2a）
