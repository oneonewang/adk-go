# internal 模块阅读笔记

## 1. 一句话定位

`internal/` 收纳 ADK-Go 全部"被外部模块依赖、但不向用户公开"的实现细节——主要承担**跨包共享的状态/上下文/回调/工具/telemetry 支撑层**、**YAML/配置加载机制**、**A2A 与 LLM 内部流转逻辑**、以及**测试公用脚手架**，是 ADK 公共 API（`agent`、`runner`、`model`、`tool`、`session`、`plugin`、`telemetry`、`server` 等）背后的"内部引擎室"。

## 2. 子包/子目录结构

按职责分组，共 26 个子目录（单层，无嵌套层级结构之外的 import 关系）：

- `agent/`、`agent/parentmap/`、`agent/remoteagent/`、`agent/runconfig/`：Agent 内部状态暴露（`Reveal` 模式）、父子图、`A2A` 远端配置、`StreamingMode`/`RunConfig` ctx 传递。
- `artifact/`、`artifact/tests/`：`artifact.Service` 的轻量包装（带 AppName/UserID/SessionID）+ 跨实现共用的 service 兼容性测试套件。
- `cli/util/`：CLI 命令运行时的日志着色、`LogCommand`/`LogStartStop`、`flag.FlagSet` 文档化、字符串居中。
- `configurable/`、`configurable/conformance/`：基于 YAML 的 agent 配置工厂（`LlmAgent`/`LoopAgent`/...）、工具/工具集/回调注册表；`conformance` 子树提供回放/录制插件用于合规测试。
- `context/`：`InvocationContext`、`CallbackContext`、`ReadonlyContext` 的内部实现（带 `EndInvocation`、`LiveSessionResumptionHandle` 等私有状态）。
- `converters/`：基于 JSON marshal/unmarshal 的 `ToMapStructure`/`FromMapStructure`（避免 `mapstructure` 库缺少 genai tag 的痛点）。
- `httprr/`：源自 `golang.org/x/oscar` 的 HTTP record/replay 框架（用于 gemini 端到端录制）。
- `llminternal/`、`llminternal/converters/`、`llminternal/googlellm/`：LLM 核心流转 `Flow`、13 个 `RequestProcessor`/`ResponseProcessor`、`StreamingResponseAggregator`、Google LLM 后端识别（Vertex vs GeminiAPI）、`LiveConnection` 包装、A2A 风格 `genai.GenerateContentResponse` → `model.LLMResponse` 转换。
- `memory/`：`memory.Service` 的轻量包装。
- `plugininternal/`、`plugininternal/plugincontext/`：`PluginManager`（依次执行 13 类插件回调）+ ctx key。
- `sessionutils/`：状态 delta 拆分/合并（`app:`/`user:`/temp 三个前缀）。
- `telemetry/`：OpenTelemetry tracer + 日志桥（按 OTel semconv v1.36.0），定义 `invoke_agent`/`generate_content`/`execute_tool` span 以及 `gen_ai.*` event。
- `testutil/`：`TestAgentRunner`、`MockModel`、`CollectEvents`/`CollectParts`/`CollectTextParts`、gemini record/replay transport 工厂。
- `toolinternal/`、`toolinternal/toolutils/`：`FunctionTool`/`StreamingFunctionTool`/`RequestProcessor` 内部接口 + `PackTool` 工具。
- `typeutil/`：JSON-schema 验证 + 泛型转换 `ConvertToWithJSONSchema`。
- `utils/`：`genai.Content` 上的 `FunctionCalls`/`FunctionResponses`/`TextParts`/`FunctionDecls`、FunctionCall ID 注入/移除、SystemInstruction 拼接、`ValidateMapOnSchema`/`ValidateOutputSchema`。
- `version/`：硬编码字符串常量 `Version = "1.2.0"`（注入到 tracer 的 instrumentation version）。

顶层无 `doc.go`，仅 `internal/cli/util/doc.go` 描述 CLI util 用途（`internal/style_test.go` 是 repo-wide 的 Apache 2.0 头检查器，`package internal_test`，不构成业务子包）。

## 3. 核心类型与接口

1. `llminternal.Flow`（`internal/llminternal/base_flow.go:62`）—— LLM 一次轮询的"管道"：`Model`、`Tools`、`RequestProcessors`/`ResponseProcessors`、6 类模型/工具回调切片。提供 `Run`/`RunLive`/`preprocess`/`callLLM`/`postprocess`/`handleFunctionCalls` 等驱动函数。是 LLMAgent 心跳。
2. `llminternal.Agent` + `llminternal.State`（`internal/llminternal/agent.go:26`）—— 包外不可见接口 `internal() *State`，导出函数 `Reveal(a Agent) *State`，把 LLMAgent 的私有字段（`Instruction`、`OutputSchema`、`Toolsets` 等）暴露给本包内 processor 使用。
3. `agent.Agent` + `agent.State`（`internal/agent/state.go:18`）—— 同上模式，但作用域覆盖到任意 Agent（含 workflow 代理）。`Type` 字符串常量（`TypeLLMAgent`/`TypeLoopAgent`/...）+ `Reveal`。
4. `parentmap.Map`（`internal/agent/parentmap/map.go:24`）—— `map[string]agent.Agent`，通过 `New(root)` DFS 一次建立（校验名称唯一 + 单亲），并提供 `ToContext`/`FromContext` 让 `Instruction` 等 processor 能找到根代理。
5. `context.InvocationContext`（`internal/context/invocation_context.go:52`）—— 内部实现 `agent.InvocationContext`，额外持 `EndInvocation bool`、`LiveSessionResumptionHandle`（live 断线续传），`WithContext` 返回新对象以保证可变性隔离。
6. `context.CallbackContext`/`ReadonlyContext`（`internal/context/callback_context.go:25`、`internal/context/readonly_context.go:26`）—— 工厂方法，区别仅在 `CallbackContext` 会创建带 `EventActions` 的 delta 跟踪；`ReadonlyContext` 仅暴露只读字段。
7. `plugininternal.PluginManager`（`internal/plugininternal/plugin_manager.go:38`）—— 13 个 `RunXxxCallback` 方法，依次执行已注册插件的回调，任一返回非 nil 即短路。`ToContext` 注入到 ctx 中供 `llminternal` 取出。
8. `telemetry.tracer`（`internal/telemetry/telemetry.go:54`）—— OTel `trace.Tracer` 私有变量；外部通过 `StartInvokeAgentSpan`/`StartGenerateContentSpan`/`StartExecuteToolSpan`/`StartTrace` 等函数拿到 span。
9. `googlellm.GoogleLLM`（`internal/llminternal/googlellm/variant.go:39`）—— 单方法接口 `GetGoogleLLMVariant() genai.Backend`；`GetGoogleLLMVariant(llm)` 提供类型断言式的后端识别。
10. `toolinternal.FunctionTool`/`StreamingFunctionTool`/`RequestProcessor`（`internal/toolinternal/tool.go:28-42`）—— 扩展 `tool.Tool` 的内部接口：`Declaration()`（用于打包到 `genai.Tool`）、`Run`/`RunStream`、`ProcessRequest`（让 LLM 工具在 `preprocess` 阶段也能贡献请求字段）。
11. `httprr.RecordReplay`（`internal/httprr/rr.go:45`）—— `http.RoundTripper` 子类，支持记录/回放；`Open`/`ScrubReq`/`ScrubResp`/`Recording` 公共 API。
12. `remoteagent.A2AClient` / `A2AClientProvider` / `A2AServerConfig`（`internal/agent/remoteagent/a2a_config.go:27-58`）—— 把 `github.com/a2aproject/a2a-go/v2` 的 `a2aclient` 抽象为可被 `agent/remoteagent/v2` 注入的内部接口。
13. `configurable.registry`（`internal/configurable/configurable_utils.go:51`）—— 4 个全局 map + `registryMu sync.RWMutex`，`init()` 预注册 `LlmAgent`/`LoopAgent`/`ParallelAgent`/`SequentialAgent` 和 `exit_loop`/`google_search`/`url_context`/`google_maps_grounding`/`AgentTool`/`LongRunningFunctionTool`/`ExampleTool`/`McpToolset` 等工厂。

## 4. 关键数据结构

- `llminternal.State`（`internal/llminternal/agent.go:30`）—— LLMAgent 私有字段集合：`Model model.LLM` / `Tools []tool.Tool` / `Toolsets []tool.Toolset` / `IncludeContents string` / `GenerateContentConfig` / `Instruction*` / `GlobalInstruction*` / `DisallowTransfer*` / `InputSchema` / `OutputSchema` / `OutputKey`。是 processor 唯一可写的窗口。
- `llminternal.liveSessionImpl`（`internal/llminternal/base_flow.go:134`）—— Live 流会话对象，含 `inputCh`/`outputCh`/`done`/`closeOnce sync.Once`/`audioMgr *AudioCacheManager`/`mu sync.Mutex`/`activeTools map[string][]activeTask`。`activeTask = {callID, cancel context.CancelFunc}`，用于跟踪 streaming tool 以便 `stop_streaming` 中断。
- `llminternal.responseWithEventID`（`internal/llminternal/base_flow.go:802`）—— 嵌入 `*model.LLMResponse` + `eventID string`，让回调链能在不重写响应的情况下挂载事件 ID。
- `llminternal.streamingResponseAggregator`（`internal/llminternal/stream_aggregator.go:33`）—— 流式响应聚合状态：累计 `usageMetadata`/`groundingMetadata`/`citationMetadata`/`thoughtSignature`，按 `currentTextBuffer`/`currentTextIsThought` 拆分思维/正常文本，按 `currentFunctionName/ID/Args`+`PartialArgs` 累积函数调用参数（用 JSONPath 写回到 `map[string]any`）。
- `llminternal.AudioCacheManager`（`internal/llminternal/audio_cache_manager.go:31`）—— 维护 `inputCache`/`outputCache [][]byte` + `inputStartTime`/`outputStartTime` + MIME；`FlushCaches` 合并缓存，调用 `ctx.Artifacts().Save` 落盘，并写一条 `FileData` 引用事件。
- `plugininternal.PluginManager`（`internal/plugininternal/plugin_manager.go:38`）—— `plugins []*plugin.Plugin` 切片 + `closeTimeout time.Duration`。
- `telemetry` 私有 attribute key 集合（`internal/telemetry/telemetry.go:45`）—— `gcpVertexAgent*` 与 `genAI*` 命名空间混搭；`systemName = "gcp.vertex.agent"`、`systemName` 也用于 logger。
- `telemetry.genAICaptureMessageContent atomic.Bool`（`internal/telemetry/logger.go:33`）—— 全局开关：true 时记录 system/user 消息明文，否则输出 `<elided>`。
- `parentmap.Map` + `ctxKey`（`internal/agent/parentmap/map.go:24,82`）—— 父子图 + ctx key（`int` 类型常量）。
- `runconfig.RunConfig`（`internal/agent/runconfig/run_config.go:31`）—— `StreamingMode` + `*agent.LiveRunConfig`；StreamingMode 三种枚举 `none`/`sse`/`bidi`。
- `remoteagent.A2AServerConfig`（`internal/agent/remoteagent/a2a_config.go:51`）—— `AgentCard`（静态）或 `AgentCardProvider`（懒加载）+ `ClientProvider` 工厂。
- `sessionutils` 的状态前缀常量（`internal/sessionutils/utils.go:22`）—— `appPrefix = "app:"`、`userPrefix = "user:"`、`tempPrefix = "temp:"`，与 Python 对齐。
- `utils.afFunctionCallIDPrefix = "adk-"`（`internal/utils/utils.go:30`）—— ADK 注入的函数调用 ID 命名空间，便于 `RemoveClientFunctionCallID` 仅清除本框架加上的 ID。
- `configurable` 三类 yaml 配置（`internal/configurable/configurable.go:96,164,186,206`）—— `llmAgentYAMLConfig`/`loopAgentYAMLConfig`/`parallelAgentYAMLConfig`/`sequentialAgentYAMLConfig`，都嵌入 `baseAgentConfig`（`yaml:",inline"`），并通过 `toXxxConfig` 转换为对应公开 Config。
- `context.InvocationContextParams`（`internal/context/invocation_context.go:27`）—— 不可变参数包：`Artifacts`/`Memory`/`Session`/`Branch`/`Agent`/`UserContent`/`RunConfig`/`EndInvocation`/`InvocationID`/`LiveSessionResumptionHandle`。
- `artifact.Artifacts`（`internal/artifact/artifacts.go:27`）—— 包外接口 `agent.Artifacts` 的内部实现，简化 `artifact.Service` 调用。

## 5. 关键流程

1. **LLM 一次"思考→行动"循环（`llminternal.Flow.runOneStep`）**
   - 入口：`Flow.Run` → `Flow.runOneStep`（`internal/llminternal/base_flow.go:101,528`）
   - 关键步骤：① 校验 `f.Model != nil`，否则返回 `ErrModelNotConfigured`（`base_flow.go:48,530`）；② 调 `preprocess` 串联 12 个 `DefaultRequestProcessors`（`base_flow.go:77-94`）；③ `callLLM` 依次跑 `RunBeforeModelCallback`(plugin→用户回调)、`generateContent`（带 `telemetry.StartGenerateContentSpan` + `LogRequest`/流式 `LogResponse`）、`runAfterModelCallbacks`/`runOnModelErrorCallbacks`（`base_flow.go:722,857,879`）；④ `postprocess` 跑 `DefaultResponseProcessors`（`base_flow.go:901`）；⑤ 跳过空 content/ErrorCode 的中间事件（`base_flow.go:570`）；⑥ `handleFunctionCalls` 并行 `go` 执行所有函数调用（`base_flow.go:1012`），合并为单事件 `mergeParallelFunctionResponseEvents`（`base_flow.go:1287`）；⑦ 检查 `TransferToAgent` 字段并向 `agentToRun` 让步（`base_flow.go:642`）。
   - 出口：产生一系列 `*session.Event`，遇 `IsFinalResponse()` 或错误终止。

2. **内容构建（把会话历史喂给 LLM）**
   - 入口：`ContentsRequestProcessor`（`internal/llminternal/contents_processor.go:37`）
   - 关键步骤：① `buildContentsDefault` 过滤掉空 content、非本 branch、`shouldExcludeEvent`（`requestEUCFunctionCallName`/`toolconfirmation.FunctionCallName`）、聚合连续的 input/output transcription（`contents_processor.go:67-141`）；② `rearrangeEventsForLatestFunctionResponse` 检查最末是 FunctionResponse 时回溯匹配 call 并合并（`contents_processor.go:195-317`）；③ `rearrangeEventsForFunctionResponsesInHistory` 重新组织整条历史使每个 call 后紧跟一个合并 response（`contents_processor.go:331-411`）；④ 删零 part + `RemoveClientFunctionCallID`（`contents_processor.go:162-171`）。
   - 出口：`req.Contents` 被填充。
   - 特殊分支：`IncludeContents == "none"` 时切换到 `buildContentsCurrentTurnContextOnly`（`contents_processor.go:501`）。

3. **Instruction 模板注入**
   - 入口：`instructionsRequestProcessor`（`internal/llminternal/instruction_processor.go:41`）
   - 关键步骤：① 先用 `parentmap.FromContext` 找到 root（`instruction_processor.go:48-53`）；② 调 `appendGlobalInstructions` + `appendInstructions`（`instruction_processor.go:56,62`），优先使用 `*Provider`，否则走 `InjectSessionState` 正则 `placeholderRegex` 替换 `{var}`/`{artifact.file}`/`{var?}`（`instruction_processor.go:70,204`）；③ `replaceMatch` 支持 `artifact.` 前缀回查 `ctx.Artifacts().Load` 和 `app:`/`user:`/`temp:` 状态前缀（`instruction_processor.go:121-164`）。
   - 出口：把处理后的指令追加到 `req.Config.SystemInstruction`。

4. **Plugin 回调链**
   - 入口：`PluginManager.RunXxxCallback` 系列（`internal/plugininternal/plugin_manager.go`）
   - 关键步骤：对每个 `plugin`，若对应回调非空则执行；返回值非 nil 立即短路。`ToContext` 把 manager 塞到 ctx；`llminternal` 在调用 LLM/工具前后通过 `pluginManagerFromContext`（`base_flow.go:1361`）取出。
   - 出口：可能返回替代内容（OnUserMessage/Before/After Model/Tool 等），或原样放行。

5. **Live session 双向流 + 断线续传**
   - 入口：`Flow.RunLive`（`internal/llminternal/base_flow.go:251`）
   - 关键步骤：① 仅当 `f.Model` 实现了 `Client() *genai.Client` 私有接口时可用（`base_flow.go:252-257`）；② 启动 `liveSessionImpl` 后台 goroutine：先 `preprocess` 生成 system prompt/tools；③ 构造 `genai.LiveConnectConfig`（含 `SessionResumption`、`InputAudioTranscription` 等）；④ 在 for 循环里建连 `client.Live.Connect` → `NewLiveConnection` 包装，启动两个 goroutine：read（`Recv` → 缓存到 `audioMgr` → 推到 `eventsChan`）和 write（消费 `sess.inputCh` 调用 `SendContent`/`SendRealtime`）（`base_flow.go:361-421`）；⑤ 主循环 select 处理 `eventsChan`（含 `handleFunctionCalls` + 100ms 后 `task_completed` 中断）和 `errChan`（`isResumable` 字符串匹配 `1008`/`GoAway`/`broken pipe` 时设 `reconnect=true`，否则 `pushError` + cleanup）；⑥ 收到 `SessionResumptionUpdate` 时回写 `iCtx.SetLiveSessionResumptionHandle`（`base_flow.go:369`）。
   - 出口：`LiveSession`（`Send`/`Close`）+ `recvIter` 序列；`stop_streaming` 函数调用通过 `liveSessionImpl.CancelAllStreamingTools` 取消 streaming tool（`base_flow.go:1048-1056`）。

6. **HTTP record/replay 加载**
   - 入口：`httprr.Open(file, real)`（`internal/httprr/rr.go`）
   - 关键步骤：① 检测 `-httprecord` flag 决定 mode；② 注册 `ScrubReq`（去掉 `X-Goog-Api-Key`/`x-goog-api-client`/`User-Agent` 等）保证录像可重放；③ `testutil.NewGeminiTestClientConfig`（`internal/testutil/testutil/genai.go:64`）在 `httprr.Recording` 为真时使用真实 client，否则用 `fakekey` 触发鉴权失败被 rr 拦截。
   - 出口：可注入到 `genai.ClientConfig.HTTPClient` 的 `RoundTripper`。

7. **Configurable 工厂加载**
   - 入口：`FromConfig(ctx, configPath)`（`internal/configurable/configurable_utils.go:286`）
   - 关键步骤：① 读 yaml；② 抽 `baseAgentConfig.AgentClass` 字段；③ 在 `registry` 中查工厂（默认 `LlmAgent`）；④ 委托到 `newLLMAgent` → `llmAgentYAMLConfig.toLLMAgentConfig` → 调 `ResolveToolReference`/`ResolveCallbackReference`/`ResolveAgentReference`（缓存到 `agentRegistry` map 中去重）；⑤ 调对应公开工厂（`llmagent.New`）建代理。
   - 出口：`agent.Agent`。

## 6. 扩展点

- `llminternal.Flow.RequestProcessors`/`ResponseProcessors` 字段：可注入自定义 processor（注意顺序敏感）。
- `llminternal.Flow.{Before,After,On}*Model/Tool*Callbacks` 字段：自定义业务级回调（区别于插件系统）。
- `plugin.Plugin` + `plugin.PluginManager`：13 类回调是 ADK 跨模块扩展主入口。
- `configurable.Register` / `RegisterToolFactory` / `RegisterToolsetFactory` / `RegisterCallback`：注册新的 yaml agent 类、工具、工具集、回调。`init()` 自动预注册一批，外部包可重复 `init` 追加。
- `toolinternal.FunctionTool.Declaration` + `toolinternal.RequestProcessor.ProcessRequest`：工具可在 preprocess 阶段贡献 `req.Tools` / `req.Config.Tools`。
- `llminternal.googlellm.GoogleLLM` 接口：自定义 LLM 实现只需满足 `GetGoogleLLMVariant() genai.Backend` 即可享受 Gemini 专属 processor（OutputSchema/DisplayName 处理等）。
- `llminternal.InstructionProvider` 签名：动态生成指令文本。
- `remoteagent.A2AClientProvider`：把 `a2a-go` 替换成自有 SDK 适配器。
- `httprr.ScrubReq`/`ScrubResp`：可附加新的脱敏/规范化函数。
- `telemetry.SetGenAICaptureMessageContent(bool)`：运行时切换日志的明文/脱敏模式。

## 7. 错误处理

- `internal/llminternal/base_flow.go:48`：`ErrModelNotConfigured = errors.New("model not configured; ensure Model is set in llmagent.Config")`——`Flow.runOneStep` 入口检查的唯一一个 sentinel。
- `internal/agent/parentmap/map.go:38,41`：构建 parent map 时若 agent 重复出现父子或名字冲突返回 `fmt.Errorf`。
- `internal/llminternal/base_flow.go:969-985`：`newToolNotFoundError` 输出 Python 风格的"工具未找到"诊断信息（列出可用工具 + 排查建议）。
- `internal/llminternal/contents_processor.go:251,269,433-449,463`：`rearrangeEventsForLatestFunctionResponse` / `mergeFunctionResponseEvents` 对畸形 history 显式报错（call 与 response ID 不匹配、空合并事件等）。
- `internal/llminternal/request_confirmation_processor.go:79,83,89,94`：`RequestConfirmationRequestProcessor` 在 user 消息中无法解析 confirmation JSON 时直接 yield error。
- `internal/llminternal/outputschema_processor.go:57,87,134,137`：schema 解析 / 校验失败时包装 error。
- `internal/configurable/configurable_utils.go:244,257,267,277,319,329,346,360,366`：所有 Register 重复、yaml 解析、reference 解析错误都集中到 `fmt.Errorf("%w", err)` 返回。
- `internal/llminternal/stream_aggregator.go:34`（注释）承认目前对 SDK 流的 partial/empty event 容错（gemini-3 SSE 偶发空 entry），通过 `reflect.ValueOf(*part).IsZero()` 静默跳过。
- `internal/agent/remoteagent/a2a_config.go:62,73,85`：`CreateA2AClient` / `ResolveAgentCard` 用 `fmt.Errorf` 包装 `a2a-go` 错误。
- `internal/utils/schema_utils.go:30,60,80,87,104,111,117,126,131`：JSON schema 校验时按字段拼出 `argType`（input/output）+ 路径，提供可定位的错误。
- 多个 `httprr` 调用在 `testutil.genai.go:34` 用 `fmt.Errorf("httprr.Open(%q) failed: %w", ...)` 包装。

## 8. 并发与性能

- 顶层 goroutine / 锁：
  - `plugininternal.PluginManager` 无显式锁（其内部 `plugins` 切片只在 `NewPluginManager` 时填充，之后只读）。
  - `configurable.registry` 用 `sync.RWMutex` 保护 4 张 map。
  - `llminternal.liveSessionImpl`（`base_flow.go:140`）用 `sync.Mutex` 保护 `activeTools`，并用 `sync.Once` 做 `close` 单次保护。
  - `llminternal.AudioCacheManager`（`audio_cache_manager.go:32`）用 `sync.Mutex` 保护 `inputCache`/`outputCache`。
  - `telemetry.genAICaptureMessageContent`（`logger.go:33`）用 `atomic.Bool` 暴露开关。
- 后台 goroutine 触发点：
  - `Flow.RunLive` 启动 1 个主 goroutine + 内部 2 个子 goroutine（`read`/`send` 循环）。
  - `Flow.handleFunctionCalls` 对每个函数调用 `go func(i, fnCall)` 启动 N 个 worker（`base_flow.go:1031`），用 `sync.WaitGroup` 同步。
  - `Flow.handleFunctionCalls` 对 streaming tool 启动独立 goroutine 执行并通过 `liveSess.Send` 反馈（`base_flow.go:1077`），并由 `cancelledToolContext`（`base_flow.go:987`）支持随时取消。
- 性能调优点：
  - `llminternal.stream_aggregator` 用 `maps.Clone` 浅拷贝 function args、`reflect` 深拷贝只用于 config clone（`basic_processor.go:60`）。
  - `internal/utils/utils.go:135 AppendInstructions` 在已有末段非空文本时直接拼接，避免反复构造 `genai.Content`。
  - `sessionutils.ExtractStateDeltas` 提前 `make(map[string]any, totalSize)` 减少 rehash。
  - `internal/converters/map_structure.go` 走 JSON 往返而非 `mapstructure`（注释明示避开 genai 缺 tag 的问题），代价是性能但语义稳定。
- 已知瓶颈/坑：
  - `handleFunctionCalls` 的并行是 fan-out 不限并发——若一次返回大量 tool call 可能 OOM。
  - `base_flow.go:1319 mergeEventActions` 注释 `TODO add similar logic for state`——StateDelta 合并只对 map 嵌套递归做了一级。
  - `tools_processor.go:31-33` 注释 `if f.Tools != nil { return }`——`Tools` 字段只被填充一次，重复 Run 共享；并发执行多个 Run 会相互干扰。
  - `llminternal.Flow.runOneStep` 注释 `TODO: check feasibility of running tool.Run concurrently.` 仍未实现。

## 9. 依赖与被依赖

- 本模块导入的主要外部包：`google.golang.org/genai`（几乎每个文件）、`github.com/google/uuid`、`github.com/a2aproject/a2a-go/v2/...`、`github.com/google/safehtml/template`、`go.opentelemetry.io/otel/...`、`github.com/google/jsonschema-go/jsonschema`、`github.com/modelcontextprotocol/go-sdk/mcp`、`gopkg.in/yaml.v3`、`github.com/google/go-cmp/cmp`（仅 test）、`github.com/google/safehtml/template`。
- 内部依赖（来自同一 repo）：
  - `internal/llminternal` 依赖 `internal/agent/parentmap`、`internal/agent/runconfig`、`internal/context`（`icontext`）、`internal/llminternal/googlellm`、`internal/plugininternal/plugincontext`、`internal/telemetry`、`internal/toolinternal`、`internal/utils`、`internal/llminternal/converters`、`internal/toolinternal/toolutils`。
  - `internal/agent` 被 `internal/agent/parentmap` 引用外部 `google.golang.org/adk/agent` 公共包。
  - `internal/configurable` 依赖 `internal/llminternal/googlellm`（判断 Gemini 模型）和 ADK 公共子包 `agent/llmagent` / `workflowagents/*` / `tool/*` / `model/gemini`。
  - `internal/telemetry` 依赖 ADK 公共 `model`、`session` 与 `internal/version`。
  - `internal/testutil` 依赖 `internal/httprr`、`internal/llminternal`、`runner`（公共）、`session`（公共）、`agent`（公共）、`model`（公共）。
- 外部模块反向引用 `internal/` 的位置（命中 69 个非 internal 文件）：
  - `agent/agent.go`：`internal/agent`（Reveal）、`internal/agent/parentmap`、`internal/agent/remoteagent`、`internal/agent/runconfig`、`internal/utils`。
  - `agent/llmagent/llmagent.go`：`internal/llminternal`（构造 Flow）、`internal/llminternal/converters`、`internal/context`、`internal/agent`、`internal/utils`。
  - `agent/workflowagents/{loop,parallel,sequential}agent/agent.go`：`internal/agent`、`internal/llminternal`、`internal/utils`。
  - `agent/remoteagent/v2/{a2a_agent.go,a2a_agent_run_processor.go,client.go}`：`internal/agent/remoteagent`、`internal/llminternal`、`internal/telemetry`、`internal/utils` 等。
  - `plugin/plugin_manager_test.go`、`plugin/functioncallmodifier/{plugin_test.go,integration_test.go}`：`internal/plugininternal`、`internal/plugininternal/plugincontext`。
  - `runner/runner.go`、`session/inmemory.go`、`telemetry/telemetry.go`、`tool/tool.go`、`tool/*/...`、`server/adka2a/v2/*`、`cmd/launcher/*`、`util/instructionutil/instruction.go`、`model/gemini/gemini.go`、`artifact/gcsartifact/gcs_test.go` 等均以 `google.golang.org/adk/internal/...` 形式导入。
- 由于 `internal/` 是 Go 私有命名空间，只允许 `google.golang.org/adk` 同 module 使用；外部仓库无法 import，验证包边界正确。

## 10. 测试与可观察性

- 风格/版权检查：`internal/style_test.go`（`package internal_test`）在 `chdir ..` 后 walk 整个 repo，检查 `Copyright 2025..` Apache 2.0 头（支持 `-fix` 模式补全），并白名单 `internal/jsonschema`、`internal/util`、`internal/httprr`、`vendor`。
- 各子包均有 _test.go：
  - `internal/context/context_test.go`、`internal/llminternal/*_test.go`（agent_transfer、audio_cache_manager、base_flow、base_flow_telemetry、contents_processor、handle_function_calls_async、identity_request_processor、instruction_processor、outputschema_processor、parallel_function_call、request_confirmation_processor、stream_aggregator、streaming_tool、functions、clone、helpers）、`internal/memory/memory_test.go`、`internal/utils/{utils_test.go,schema_test.go}`、`internal/artifact/artifacts_test.go`、`internal/artifact/tests/service_suite.go`（公共 service 兼容性套件）、`internal/httprr/rr_test.go`、`internal/telemetry/{telemetry_test.go,logger_test.go,converters_test.go}`、`internal/configurable/conformance/{replayplugin,recordplugin}/*_test.go`、`internal/configurable/conformance/replayplugin/replay_plugin_internal_test.go`。
- Telemetry 埋点位置（OTel semconv v1.36.0）：
  - `telemetry.StartInvokeAgentSpan`（`internal/telemetry/telemetry.go:67`）→ span 名 `invoke_agent <name>`，属性 `gcp.vertex.agent.invocation_id` + `gen_ai.operation.name=invoke_agent` + `gen_ai.agent.description` + `gen_ai.agent.name` + `gen_ai.conversation.id`。
  - `telemetry.StartGenerateContentSpan`（`telemetry.go:99`）→ span 名 `generate_content <model>`，属性 `gcp.vertex.agent.invocation_id` + `gen_ai.operation.name=generate_content` + `gen_ai.request.model`。
  - `telemetry.StartExecuteToolSpan`（`telemetry.go:148`）→ span 名 `execute_tool <tool>`，属性 `gen_ai.operation.name=execute_tool` + `gen_ai.tool.name` + `gcp.vertex.agent.tool_call_args`（序列化为 JSON）。
  - `telemetry.TraceToolResult`（`telemetry.go:165`）/ `TraceMergedToolCallsResult`（`telemetry.go:244`）→ 写 `gcp.vertex.agent.event_id` + `gen_ai.tool.call.id` + `gcp.vertex.agent.tool_response`。
  - `telemetry.LogRequest`（`logger.go:56`）→ 发 `gen_ai.system.message` / `gen_ai.user.message` event；`telemetry.LogResponse`（`logger.go:69`）→ 发 `gen_ai.choice` event；全局 logger 名称 `gcp.vertex.agent`，schema URL `semconv/v1.36.0`。
  - `telemetry.WrapYield`（`telemetry.go:223`）→ 包装 `iter.Seq2` yield，确保 span 在 yield 返回时关闭。
  - 在 `llminternal` 各处可见调用：`base_flow.go:811 generateContent` 启动 span、结束时记录 token usage + finish reason；`base_flow.go:1018` 在并行 tool call 时启动 `execute_tool (merged)` span；`base_flow.go:1034,1165` 每个 tool 启动子 span 并 `TraceToolResult`；`base_flow.go:848` 流式 LogResponse；`base_flow.go:372` 记录 resumption handle。

## 11. 文档写作提示

- **必写**：
  - `llminternal.Flow` 的"管道"模型：13 个 processor + 6 类回调的拓扑关系（processor 顺序敏感、回调链是短路早返回）。
  - `parentmap.Map` 的构建语义（单亲 + 名称唯一），是 `transfer_to_agent` 与 `InstructionsRequestProcessor` 找 root 的唯一方式。
  - `Reveal` 模式（`internal/agent/state.go:40`、`internal/llminternal/agent.go:58`）—— ADK 私有字段暴露的惯用句式，文档要明确"包内可见、调用方应通过公共 API 修改配置"。
  - `LiveConnection` 的 resumption / reconnect 机制与 `isResumable` 字符串匹配（`base_flow.go:297-310`）。
  - `configurable` 工厂注册表是 yaml 配置的入口；要写清 4 类注册函数与 `parentPath` ctx key 的传播。
  - `httprr` 的 scrubbing 约定（`X-Goog-Api-Key` 等敏感 header 与 `User-Agent`/`x-goog-api-client` 等版本 header），这是 gemini 录制回放稳定性的关键。
  - `telemetry` 暴露的"日志明文/脱敏"开关（`SetGenAICaptureMessageContent`）和 OTel instrumentation name。
- **可省略**：
  - 大量 `_test.go` 内的具体断言；只需指引到 `service_suite.go` 这类"实现需满足的公共测试套件"。
  - 各 yaml struct 字段的 yaml tag（除非讲到嵌套语义）。
  - 颜色 ANSI 常量（`cli/util/oscmd.go:30-38`）仅在描述 CLI 输出时提及。
- **潜在的坑**：
  - `tools_processor.go:31-33` 的"Tools 仅填充一次"是隐式全局状态，并发 Run 可能互相覆盖；写文档需点明 Flow 不可跨 goroutine 复用。
  - `outputschema_processor.go:98 needOutputSchemaProcessor` 依赖"Gemini 2.5 及以下 + Gemini API + 有工具"三个条件同时满足才插入 set_model_response 工具，新版本 Gemini 升级到 3.x 时此 fallback 路径会被自动绕过。
  - `contents_processor.go:50-62` 中"context only 模式"分支以"最近 user/other-agent event 之后的 slice"为基准，与 Python 行为可能略有差异。
  - `sessionutils` 状态前缀 `app:`/`user:`/`temp:` 与 public `session.State` 强耦合，外部不能复用这套拆分逻辑。
  - `internal/llminternal/converters/converters.go:60-72` 注释的"gemini-3 SSE 偶发空 entry"是已知的实测现象，不要在文档中表达为"正常 chunk 必须有 parts"。
  - `internal/llminternal/agent_transfer.go:185 transferTargets` 与 `shouldUseAutoFlow`/`asLLMAgent`（同文件 213, 223）共同决定 transfer 目标集合；如要改 transfer 规则，要连这三个函数一起改。
- **建议的文档结构（给写作者）**：
  1. 概览：内部包全景图（4 大领域：LLM 流 / 上下文与回调 / 工具与状态 / 配置与可观察性）。
  2. 每子包一节，按"类型 → 流程 → 扩展点 → 注意点"四段。
  3. 末尾单列"私有 API 与公共 API 的对应表"（如 `internal/context` 提供 `CallbackContext` 工厂，公共 `agent.CallbackContext` 是接口；`internal/llminternal.Agent` 是私有入口）。
