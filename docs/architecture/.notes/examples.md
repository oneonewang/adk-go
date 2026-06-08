# examples 模块阅读笔记

## 1. 一句话定位
`examples` 是 ADK Go 的可执行示例集合，通过一系列独立的 `package main` 程序演示 ADK 各核心能力（quickstart、REST/A2A/Web 启动、Vertex AI 部署、MCP 工具、Skills、Telemetry、Workflow Agents、Tool Confirmation 等）的最小可运行形态，承担“文档即代码”的角色。

## 2. 子包/子目录结构
本目录不是单一 Go 包，而是多个 `package main` 子目录并列：

- `a2a/`：A2A 协议示例，进程内同时跑服务端与远程调用客户端 (`a2a/main.go`)。
- `agentengine/`：部署到 Vertex AI Agent Engine 的示例，含记忆/会话 Vertex 后端 (`agentengine/main.go`)。
- `bidi/`：双向流（Live API）相关示例根。
  - `bidi/main.go`：最简 bidi 演示（`gemini-3.1-flash-live-preview` + `RuntimeAPIController`）。
  - `bidi/streamingtool/`：流式 function tool + `stop_streaming` 控制信号。
  - `bidi/sequential/`：sequentialagent 在 bidi 场景下的两段式（Idea Generator → Story Teller）。
  - `bidi/static/`：Web 前端（HTML/JS/CSS），被三个 bidi 程序共享。
  - `bidi/assets/`：README 截图素材。
- `mcp/`：MCP 工具集（in-memory server 与 GitHub 远程 MCP server）(`mcp/main.go`)。
- `quickstart/`：最简单 quickstart，5 行装载 launcher 跑全功能 (`quickstart/main.go`)。
- `rest/`：手写 `net/http` mux 挂载 `adkrest.Server` 的 REST API 示例 (`rest/main.go`)。
- `skills/`：Skill Toolset 演示，依赖相对路径 `./skills` 下的 skill 文件夹 (`skills/main.go`)。
  - `skills/skills/`：示例 skill 数据（`grocery-prices`、`weather`）。
- `telemetry/`：OpenTelemetry 接入示例 (`telemetry/main.go`)。
- `toolconfirmation/`：完整的 console 端 tool confirmation 流程示例，含自定义 UI 循环 (`toolconfirmation/main.go`)。
- `tools/`：tool 工具族用法。
  - `tools/loadartifacts/`：使用 `loadartifactstool` 描述已保存的图片/文本。
  - `tools/loadmemory/`：使用 `preloadmemorytool` + `loadmemorytool` 检索历史会话记忆。
  - `tools/multipletools/`：在同一 agent 中混用 Google Search 和 function tool 的 workaround。
- `vertexai/`：Vertex AI 相关。
  - `vertexai/agent.go`：连 Vertex Reasoning Engine 的运行期 agent。
  - `vertexai/vertexengine/create_engine.go`：一次性脚本，创建 ReasoningEngine 资源。
  - `vertexai/imagegenerator/`：调用 Imagen 生成图片并保存到本地与 artifact。
- `web/`：WebUI 启动演示，子包 `agents` 暴露 `GetLLMAuditorAgent` 和 `GetImageGeneratorAgent`。
  - `web/agents/llmauditor.go`：批评家+编辑者 sequential agent，演示 AfterModelCallback 改写 LLM 输出。
  - `web/agents/image_generator.go`：可被根 agent 调用的图像生成子 agent。
- `workflowagents/`：workflow 类 agent 演示。
  - `workflowagents/sequential/`：自定义 `myAgent` Run 实现的 sequential。
  - `workflowagents/parallel/`：并发 sub-agent，每轮 sleep 随机秒数。
  - `workflowagents/loop/`：loop agent（`MaxIterations: 3`）。
  - `workflowagents/sequentialCode/`：3 阶段代码生成 pipeline（write→review→refactor），演示 `OutputKey` 跨 agent 共享 state。

整体 24 个 `.go` 文件，约 4173 行；没有 `doc.go`，没有 `*_test.go`。

## 3. 核心类型与接口
`examples` 不定义对外 API，它**消费** ADK 各模块导出的类型。最常出现（按出现频率）：

- `agent.Agent` 接口
  - 出现位置：每个示例的 agent 构造/装配处，如 `quickstart/main.go:44`、`a2a/main.go:52`、`toolconfirmation/main.go:214`。
  - 设计意图：所有可执行 agent 的统一抽象，custom agent 通过 `agent.New(agent.Config{ Run: ... })` 注入 `iter.Seq2[*session.Event, error]`（见 `workflowagents/sequential/main.go:58`、`workflowagents/parallel/main.go:40`）。
- `llmagent.Config` / `llmagent.New`
  - 出现位置：所有 LLM 驱动的 agent，如 `quickstart/main.go:44`、`skills/main.go:68`、`web/agents/llmauditor.go:222`。
  - 设计意图：声明式 LLM agent 描述（Name/Model/Instruction/Tools/Toolsets/OutputKey/Before/After Model Callback）。
- `launcher.Config` / `launcher` 系列
  - 出现位置：`quickstart/main.go:57`、`rest/main.go:62`（不通过 launcher）、`a2a/main.go:132`。
  - 设计意图：把 AgentLoader、SessionService、MemoryService、ArtifactService、PluginConfig 等组合起来交给 Launcher 启动。
- `full.NewLauncher()` / `prod.NewLauncher()` / `agentengine.NewLauncher(id)`
  - 出现位置：`quickstart/main.go:61`、`agentengine/main.go:201`。
  - 设计意图：分别提供「含 console/rest/a2a/webui」「生产 rest+a2a」「Vertex Agent Engine」三种运行容器。
- `tool.Tool` / `tool.Toolset` 接口
  - 出现位置：`quickstart/main.go:49`（Tool）、`mcp/main.go:120`（Toolset）、`skills/main.go:73`（Toolset）。
  - 设计意图：声明式工具/Tool 集。
- `functiontool.New` / `functiontool.NewStreaming`
  - 出现位置：`toolconfirmation/main.go:203`、`bidi/streamingtool/main.go:53`。
  - 设计意图：把普通 Go 函数或返回 `iter.Seq2[string, error]` 的流式函数包装为 `tool.Tool`。

其它关键单点类型：

- `gemini.NewModel(ctx, modelName, *genai.ClientConfig)`（`quickstart/main.go:37`）：构造 LLM 实现。
- `adkrest.NewServer(adkrest.ServerConfig{...})`（`rest/main.go:62`）：构造可挂到任意 mux 的 REST handler。
- `adka2a.NewExecutor` + `a2asrv.NewHandler`（`a2a/main.go:101-109`）：把 ADK agent 暴露为 A2A 服务。
- `remoteagent.NewA2A`（`a2a/main.go:124`）：把远程 A2A server 当成本地 agent。
- `mcptoolset.New(mcptoolset.Config{ Transport: ... })`（`mcp/main.go:107`）：用任意 `mcp.Transport` 桥接 MCP server。
- `toolconfirmation.FunctionCallName` / `toolconfirmation.OriginalCallFrom`（`toolconfirmation/main.go:241-242`）：用于解析 confirmation 包装的 function call。
- `plugin.New(plugin.Config{ BeforeRunCallback, AfterRunCallback })`（`agentengine/main.go:163`）：在每次 run 前后插入副作用。
- `controllers.NewRuntimeAPIController`（`bidi/main.go:93`、`bidi/sequential/main.go:100`、`bidi/streamingtool/main.go:132`）：构造 bidi WebSocket handler `RunLiveHandler`。
- `runner.New(runner.Config{...})`（`toolconfirmation/main.go:262`、`tools/loadartifacts/main.go:107`、`tools/loadmemory/main.go:97`）：在不走 launcher 时直接跑。
- `sequentialagent.New` / `parallelagent.New` / `loopagent.New`（`workflowagents/*`、`bidi/sequential`、`web/agents/llmauditor.go:248`）：workflow 类容器。
- `vertexai.NewSessionService` / `vertexaiMem.NewService`（`agentengine/main.go:140,150`、`vertexai/agent.go:58`）：Vertex 后端服务。

## 4. 关键数据结构
- `RequestVacationArgs`（`toolconfirmation/main.go:45`）：请假天数工具入参 `{Days int, UserID string}`。
- `ConfirmationPayload`（`toolconfirmation/main.go:50`）：审批回传负载 `{DaysApproved int}`。
- `RequestVacationResults`（`toolconfation/main.go:55`）：工具首次返回 `{Status, DaysApproved, RequestID}`。
- `VacationRequest`（`toolconfirmation/main.go:61`）：内存中跟踪的工单，含 `ID/UserID/Days/Status/CallID/DaysApproved/Confirmation`。
- `requestsByReqID` / `requestsByCallID` / `requestCounter`（`toolconfirmation/main.go:72-77`）：进程级 map，用于把 LLM 发出的 function call ID 与人类审批关联起来。
- `Input/Output`（`agentengine/main.go:42,47`、`mcp/main.go:50,54`、`workflowagents/sequentialCode/main.go` 的内嵌结构）：所有 function tool 的 I/O schema，靠 struct tag 反射。
- `generateImageInput/Result`（`web/agents/image_generator.go:69,74`、`vertexai/imagegenerator/main.go:120,125`）：图片生成工具的入参出参。
- `saveImageInput/Result`（`vertexai/imagegenerator/main.go:169,173`）：落盘到 `output/` 目录的辅助工具。
- `generateImage` / `saveImage`（`vertexai/imagegenerator/main.go:94,132`）：函数实现，调用 `genai.NewClient` + `Models.GenerateImages`，把结果通过 `ctx.Artifacts().Save` 落 artifact。
- `myAgent` + `Run`（`workflowagents/sequential/main.go:35,39`、`workflowagents/parallel/main.go:79,83`）：自定义 agent 的最小样板，只产生一个或多个 greeting event。
- `CustomAgentRun`（`workflowagents/loop/main.go:34`）：loop agent 用到的最简 Run 函数。
- `AuthInterceptor`（`web/main.go:53-61`）：实现 `a2asrv.CallInterceptor`，把 a2a 调用标记为已认证用户，共享 session。
- `saveReportfunc`（`web/main.go:39-50`）：实现 `llmagent.AfterModelCallback`，把每段 LLM 输出都存成 artifact。
- `afterCritic` / `afterReviser`（`web/agents/llmauditor.go:156,207`）：拼装 grounding references + 截断 `---END-OF-EDIT---` 标记。
- `CriticPrompt` / `ReviserPrompt`（`web/agents/llmauditor.go:31,81`）：批评家/编辑者的 system prompt 常量。
- `stateKeySessionLastUpdateTime`（`agentengine/main.go:52`）：agent engine 用来追踪 session 最后更新时间的 state key。
- `userID` / `appName` 常量（`toolconfirmation/main.go:222-225`）：console 模式硬编码用户。

## 5. 关键流程

### 5.1 quickstart 启动全栈 launcher
- 入口：`quickstart/main.go:34` `main()`。
- 步骤：
  1. `gemini.NewModel` 创建 LLM（`quickstart/main.go:37`）。
  2. `llmagent.New` 创建 `weather_time_agent` 装载 `geminitool.GoogleSearch{}`（`quickstart/main.go:44`）。
  3. 装入 `launcher.Config{ AgentLoader: agent.NewSingleLoader(a) }`（`quickstart/main.go:57`）。
  4. `full.NewLauncher().Execute(ctx, config, os.Args[1:])`（`quickstart/main.go:61`）。
- 出口：`Execute` 内部根据命令行参数决定 console / rest / a2a / webui 行为。

### 5.2 进程内 A2A 客户端/服务端
- 入口：`a2a/main.go:119` `main()`。
- 步骤：
  1. `startWeatherAgentServer()` 监听 127.0.0.1:0 随机端口（`a2a/main.go:67`）。
  2. goroutine 内 `a2asrv.NewStaticAgentCardHandler` + `adka2a.NewExecutor` + `a2asrv.NewJSONRPCHandler` 暴露 `/invoke`（`a2a/main.go:99-109`）。
  3. 主线程 `remoteagent.NewA2A` 包装远端地址为本地 agent（`a2a/main.go:124`）。
  4. 交给 `full.NewLauncher()` 启动（`a2a/main.go:136`）。
- 出口：客户端可像调用本地 agent 一样调用远端服务。

### 5.3 手写 REST 入口
- 入口：`rest/main.go:36` `main()`。
- 步骤：
  1. 创建 model + llmagent（`rest/main.go:40,48`）。
  2. `adkrest.NewServer` 配置 `SSEWriteTimeout`（`rest/main.go:62`）。
  3. `mux.Handle("/api/", http.StripPrefix("/api", restServer))` 挂载（`rest/main.go:76`）。
  4. `http.ListenAndServe(":8080", mux)`（`rest/main.go:91`）。
- 出口：监听 :8080，REST + SSE 流式响应。**本示例未走 launcher**，展示最低耦合的嵌入方式。

### 5.4 Tool Confirmation 完整循环
- 入口：`toolconfirmation/main.go:79` `main()`。
- 步骤：
  1. console 菜单读取（`toolconfirmation/main.go:100-125`）。
  2. chat mode 直接 `runTurn`（`toolconfirmation/main.go:230`）。
  3. vacation mode 调 `requestVacationDays`（`toolconfirmation/main.go:129`），首次调用 `ctx.RequestConfirmation(...)`（`toolconfirmation/main.go:152`），LLM 收到 confirmation function call。
  4. `runTurn` 中识别 `toolconfirmation.FunctionCallName` 并把 `fc.ID` 存到 `req.CallID`（`toolconfirmation/main.go:241-251`）。
  5. 用户在 console 输入 `approve <ID> <days>`，`processApproval` 构造 `FunctionResponse` 并再次 `runTurn`（`toolconfirmation/main.go:332-368`）。
- 出口：LLM 第二次进入 `requestVacationDays` 时 `ctx.ToolConfirmation() != nil`，进入批准/拒绝分支（`toolconfirmation/main.go:166-199`）。

### 5.5 Bidi Live 流式 + 流式工具
- 入口：`bidi/main.go:40` 与 `bidi/streamingtool/main.go:41`。
- 步骤：
  1. 用 `gemini.NewModel(... "gemini-3.1-flash-live-preview" ...)`（`bidi/main.go:44`、`bidi/streamingtool/main.go:45`）。
  2. 注册流式工具 `functiontool.NewStreaming`，handler 返回 `iter.Seq2[string, error]`，内部 `yield` 边睡边吐（`bidi/streamingtool/main.go:53-67`）。
  3. 同时注册 `stop_streaming` 工具供模型中止（`bidi/streamingtool/main.go:78`）。
  4. `controllers.NewRuntimeAPIController(... true)` + `http.HandleFunc("/run_live", RunLiveHandler)` 暴露 WebSocket（`bidi/main.go:93-100`）。
- 出口：浏览器连 :8081 即可双向收发音频/文本/事件。

### 5.6 Workflow Agent 流水线
- 入口：`workflowagents/sequentialCode/main.go:33`。
- 步骤：
  1. 三个 `llmagent`：`CodeWriterAgent`(OutputKey=generated_code) → `CodeReviewerAgent`(OutputKey=temp:review_comments) → `CodeRefactorerAgent`(OutputKey=refactored_code)（`workflowagents/sequentialCode/main.go:45,61,92`）。
  2. `sequentialagent.New` 把三个 agent 串成 `CodePipelineAgent`（`workflowagents/sequentialCode/main.go:123`）。
  3. prompt 中通过 `{generated_code}` / `{temp:review_comments}` 模板从 session state 拿上一阶段产物（`workflowagents/sequentialCode/main.go:69,103`）。
- 出口：用户给需求一次跑完生成-审查-重构三步。

## 6. 扩展点
`examples` 本身**没有**对外扩展点——它只是参考实现。要扩展 ADK 能力，应：
- 改 `agent.Config{ Run: <func> }` 注入自定义 agent（`workflowagents/sequential/main.go:58`）。
- 在 `llmagent.Config` 增加 `Tools` / `Toolsets` / `Before/After Model Callback` / `Before/After Agent Callback` / `OutputKey`。
- 把 launcher 替换为 `prod.NewLauncher()` 或自写 `http.Server + adkrest` 入口（参考 `rest/main.go`）。
- 自定义 `SessionService`/`MemoryService`/`ArtifactService`/`Plugin` 注入 `launcher.Config`（参考 `agentengine/main.go:140-199`）。
- 用 `a2asrv.WithCallInterceptors` 注入 a2a 请求拦截器（参考 `web/main.go:105-107`）。
- 在 `controllers.NewRuntimeAPIController` 前后插入 `RunnerPluginConfig`（参考 `bidi/main.go:93`）。

## 7. 错误处理
- **统一风格**：所有示例都用 `log.Fatalf("... %v", err)` 直接退出（`quickstart/main.go:41,54`、`a2a/main.go:48,60,68,128` 等）。`telemetry/main.go` 是少数把错误作为返回值上抛的示例（`telemetry/main.go:39-90`），更接近生产代码风格。
- **确认流程错误**（`toolconfirmation/main.go`）：
  - `requestVacationDays` 对 `args.Days <= 0` 返回 `fmt.Errorf("invalid days to request %d", args.Days)`（`toolconfirmation/main.go:132-134`）。
  - confirmation 解析失败：`json.Marshal`/`Unmarshal` 失败、`payload` 缺失等都包成 wrapped error（`toolconfirmation/main.go:169-180`）。
  - `processApproval` 中 `strconv.Atoi` 失败时只打印 `Invalid number of days. Approval cancelled.`（`toolconfirmation/main.go:344`），不中断进程。
- **MCP 连接错误**（`mcp/main.go:71-72`）：`server.Connect` 失败直接 `log.Fatal`。
- **Vertex 资源创建**（`vertexai/vertexengine/create_engine.go:54,77,83`）：把 gRPC `CreateReasoningEngine` 与 `op.Wait` 错误包成 `fmt.Errorf("failed ...: %w", err)`。
- **环境变量缺失**：`vertexai/agent.go:42-52`、`vertexai/vertexengine/create_engine.go:32-38`、`a2a/main.go` 等都直接 `log.Fatalf`。
- **artifact/memory 缺失**：`vertexai/imagegenerator/main.go:140-143` 打印 `Artifact '%s' has no inline data` 后直接返回错误。

## 8. 并发与性能
- **goroutine**：
  - `a2a/main.go:76`：`go func(){ ... http.Serve(listener, mux) }()` 跑服务端。
  - `bidi/streamingtool/main.go:62`：`for i := 1; i <= args.N; i++ { time.Sleep(5s); yield(...) }` — Live API 的 streaming tool 在独立 goroutine 中推流。
  - `agentengine/main.go` 配合 Vertex session/memory：所有 IO 走 Vertex 后端，无显式并发管理。
- **锁/全局状态**：
  - `toolconfirmation/main.go:71-77` 三个全局 `map` + `int` 计数器在 console REPL 单线程下安全；多用户场景会 race（**典型坑**）。
- **内存压力**：
  - `vertexai/imagegenerator/main.go:104-117` 把整张图片字节放进 `genai.NewPartFromBytes` 再保存，无流式分块。
  - `web/main.go:44` `saveReportfunc` 把每段 LLM 输出都 `uuid.NewString()` 命名存 artifact，**会话长跑会膨胀**。
- **延迟**：
  - `bidi/streamingtool/main.go:62` 每次 yield 固定 sleep 5s，是 demo 节奏，生产中应改事件驱动。
  - `workflowagents/parallel/main.go:100-102` 用 `time.Sleep(1 + rand.IntN(5))` 模拟并发差异。
- **启动开销**：所有示例都每次新建 `gemini.NewModel` / `vertexai.NewSessionService` 等，无单例缓存。

## 9. 依赖与被依赖
**本模块导入的 ADK 包**（grep `google.golang.org/adk`）：
- `agent`、`agent/llmagent`、`agent/remoteagent/v2`
- `agent/workflowagents/{loopagent,parallelagent,sequentialagent}`
- `artifact`、`memory`、`memory/vertexai`
- `model`、`model/gemini`
- `runner`、`session`、`session/vertexai`
- `tool`、`tool/{agenttool,functiontool,geminitool,loadartifactstool,loadmemorytool,mcptoolset,preloadmemorytool,skilltoolset,skilltoolset/skill,toolconfirmation}`
- `cmd/launcher`、`cmd/launcher/{full,agentengine}`
- `server/adka2a/v2`、`server/adkrest`、`server/adkrest/controllers`
- `telemetry`、`plugin`、`util/vertexai`
- 自身子包 `examples/web/agents`（仅 `web/main.go:31` 引用）

**本模块被谁导入**：通过 `grep -lE "examples/web/agents"` 全仓搜索，仅 `examples/web/main.go:31` 反向引用了子包。`examples` 整体上**不被 ADK 库代码引用**，是端到端入口层而非库内部依赖。

**外部第三方依赖**（`go.mod` 中由各 main 引入）：
- `github.com/a2aproject/a2a-go/v2/{a2a,a2asrv}`（`a2a/main.go:26-27`、`web/main.go:22`）
- `github.com/google/uuid`（`web/main.go:23`）
- `github.com/modelcontextprotocol/go-sdk/mcp` + `golang.org/x/oauth2`（`mcp/main.go:26-27`）
- `cloud.google.com/go/aiplatform/apiv1` + `google.golang.org/api/option`（`vertexai/vertexengine/create_engine.go:23-25`）
- `go.opentelemetry.io/otel/sdk/resource` + `semconv/v1.36.0`（`telemetry/main.go:24-25`）
- `google.golang.org/genai`（所有 LLM 客户端）

## 10. 测试与可观察性
- **测试文件**：本模块**无任何 `*_test.go`**（`find examples -name "*_test.go"` 为空），是纯示例代码。
- **telemetry**：
  - `telemetry/main.go:69-72` 显式构造 `resource.Resource` 并通过 `telemetry.WithResource(r)` 注入 `launcher.Config.TelemetryOptions`。
  - `tools/multipletools/main.go:112-115` 使用 `telemetry.WithGenAICaptureMessageContent(true)` 开启消息内容捕获。
  - `agentengine/main.go:163-185` 通过 `plugin.New` 暴露 `BeforeRunCallback` / `AfterRunCallback`，演示如何把 session 信息写回 memory。
- **可观察性 hook**：
  - `web/main.go:39-50` `AfterModelCallback` 把 LLM 响应落入 artifact。
  - `web/agents/llmauditor.go:156,207` `AfterModelCallback` 改写 LLM 输出。
  - `toolconfirmation/main.go:374-396` 自定义 `printEventSummary` 在 console 中输出可读日志。

## 11. 文档写作提示

**必须写**：
- **Launcher 三选项**：`full.NewLauncher()`（含 console+rest+a2a+webui）、`prod.NewLauncher()`（rest+a2a）、`agentengine.NewLauncher(id)` 三种启动方式的差异与适用场景（README 已经粗略说明，可扩展到代码片段引用）。
- **三种嵌入姿势**：
  1. `full.NewLauncher().Execute(ctx, config, args)`（`quickstart/main.go:61`）。
  2. 直接 `http.Serve(":8080", mux)` + `adkrest.NewServer`（`rest/main.go:62-93`）。
  3. `controllers.NewRuntimeAPIController(...).RunLiveHandler`（`bidi/main.go:93-103`）。
- **Tool Confirmation 完整闭环**：流程（`toolconfirmation/main.go`）值得一图——首次 function call / confirmation 包装 / 二次 function response。
- **Vertex AI 部署差异**：`agentengine/main.go:140-199` 用 `vertexai.NewSessionService` + `vertexaiMem.NewService` + `plugin`，展示「如何在托管环境跑 ADK」。

**可以省略**：
- 单一 business function 内部的 if/else 细节（如 `vertexai/imagegenerator/main.go` 的目录创建）。
- `workflowagents/sequential/main.go:39-53` 这种「只输出 hello」的 myAgent 重复样板。
- `vertexai/vertexengine/create_engine.go:46-90` 的纯 Vertex SDK 调用（与 ADK 关系较弱）。

**潜在的坑**（提醒文档作者）：
- `toolconfirmation/main.go:71-77` 的全局 map **不是线程安全**，多用户会 race；示例里只用 console 单线程。
- `bidi/streamingtool/main.go:62` 的 `time.Sleep(5*time.Second)` 是固定节奏，文档若引用需说明实际由 ADK Live Control Plane 通过 `stop_streaming` 触发取消。
- `quickstart/main.go:37` 用的模型名 `gemini-3.1-flash-lite` 在不同时点可能不可用——README 中明确这是「示例模型，请按需替换」。
- `examples/skills/main.go:35-37` 注释强调「**必须**在 `examples/skills` 目录下运行」，因为它读相对路径 `./skills`。
- `examples/vertexai/*` 系列要求 `GOOGLE_CLOUD_PROJECT` / `GOOGLE_CLOUD_LOCATION` 等环境变量；缺一会 `log.Fatalf`，需要前置说明。
- `a2a/main.go` 与 `web/main.go` 都同时拉起 server + 客户端，前者用 goroutine + 随机端口，后者把拦截器挂在同一个 launcher 上；文档若讲「如何把远端 A2A 嵌入本地 agent」应指向前者。
- `web/agents/llmauditor.go:29` 的 `EndMark = "---END-OF-EDIT---"` 是模型和回调之间的协议标记，文档里要说明 prompt 必须输出该标记，回调按标记裁剪。

---

**统计**：笔记覆盖小节 11/11；含约 60 处 `path:line` 引用；总行数约 250。
