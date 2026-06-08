# agent 模块阅读笔记

模块路径：`/home/wu/oneone/adk/agent`
锁定提交：`d06992e2b1ec2c9b95c6070e0fd12d50a43e4c99`

## 1. 一句话定位

`agent` 是 ADK 的"智能体核心抽象层"，定义了 `Agent` 基础接口、调用/回调上下文、运行配置、agent 加载器以及三类开箱即用的实现：LLM 智能体、远程 A2A 智能体、工作流编排智能体（顺序/并行/循环）。

## 2. 子包/子目录结构

| 子目录 | 作用 |
|---|---|
| `agent/llmagent/` | 基于大语言模型的智能体（`llmagent.New`），核心能力来源，所有调用 LLM 的 agent 都走这里 |
| `agent/remoteagent/` | A2A（Agent-To-Agent）协议的远程智能体（v1 兼容层），已标记 `Deprecated`，调用转发到 v2 |
| `agent/remoteagent/v2/` | 远程智能体的当前实现版本，使用 `a2a-go/v2`，支持 streaming、partial aggregation、task cleanup |
| `agent/workflowagents/loopagent/` | 循环执行子 agent 的工作流智能体，可由 `Escalate` 事件提前终止 |
| `agent/workflowagents/parallelagent/` | 并行执行子 agent 的工作流智能体，通过 `errgroup` + channel 同步 |
| `agent/workflowagents/sequentialagent/` | 顺序执行子 agent 的工作流智能体；额外实现了 `RunLive`，会自动给 LLM 子 agent 注入 `task_completed` 工具 |

## 3. 核心类型与接口

- **`Agent` 接口**（`agent/agent.go:43`）
  - 签名：`Name() / Description() / Run(InvocationContext) iter.Seq2[*session.Event, error] / SubAgents() / FindAgent / FindSubAgent / internal() *agent`
  - 全部 ADK agent 都必须实现。注释明确说未来版本可能放开自定义实现，当前推荐用 `agent.New` 或各子包构造函数。

- **`InvocationContext` 接口**（`agent/context.go:62`）
  - 嵌入 `context.Context`，暴露 `Agent / Artifacts / Memory / Session / Branch / UserContent / RunConfig / EndInvocation / Ended / WithContext`。
  - 一个 invocation 包含若干 agent call，每个 agent call 又包含若干 step（LLM 调用 + tool 调用）。

- **`ReadonlyContext` / `CallbackContext` / `ToolContext`**（`agent/context.go:108`, `:125`, `:136`）
  - 三层继承：`ReadonlyContext` → `CallbackContext`（多 `Artifacts()/State()`）→ `ToolContext`（多 `FunctionCallID/Actions/SearchMemory/ToolConfirmation/RequestConfirmation`）。
  - `ToolContext` 是 HITL（Human-in-the-Loop）确认流程的入口；`RequestConfirmation` 会写入 `actions.RequestedToolConfirmations` 并设 `SkipSummarization` 以暂停 agent 循环。

- **`Artifacts` / `Memory` 接口**（`agent/agent.go:111`, `:120`）
  - 封装 artifact 增删查（`Save/List/Load/LoadVersion`）和 memory 会话管理。
  - `callbackContext` 通过 `trackedArtifacts` 装饰器在保存时自动写入 `ArtifactDelta`（`agent/callback_context.go:243`）。

- **`BeforeAgentCallback` / `AfterAgentCallback` 函数类型**（`agent/agent.go:129`, `:137`）
  - 返回非 nil content/error 时短路 `Run` 主流程。
  - 实际执行逻辑见 `runBeforeAgentCallbacks` / `runAfterAgentCallbacks`（`agent/agent.go:247`, `:306`）。

- **`Loader` 接口及 `NewSingleLoader` / `NewMultiLoader`**（`agent/loader.go:22`, `:43`, `:70`）
  - 注册/查找 agent 树，给 runner 等外部调用方一个统一入口。
  - `multiLoader` 检测重名并返回错误；`singleLoader` 只接受 root agent 自己的名字或空字符串。

- **`RunConfig` / `StreamingMode`**（`agent/run_config.go:18`, `:29`）
  - 当前仅定义 `StreamingModeNone` 与 `StreamingModeSSE`，以及 `SaveInputBlobsAsArtifacts` 开关。

- **`LiveSession` / `LiveRequest` / `LiveRunConfig`**（`agent/live.go:22`, `:28`, `:38`）
  - 实时双向流（live/bidi）相关抽象；由 `llmagent.RunLive` / `sequentialagent.RunLive` 实现。

- **子包关键类型**
  - `llmagent.LLMAgent`（`llmagent/llmagent.go:340`）：聚合 model/tools/callbacks/Instruction 等，运行时复用 `llminternal.Flow` 驱动 LLM-tool 循环，并通过 `maybeSaveOutputToState` 把最终回答写入 `event.Actions.StateDelta[OutputKey]`。
  - `remoteagent.A2AConfig`（`v2/a2a_agent.go:88`）：配置 agent card 源、消息 converter、A2A 客户端工厂、请求/响应回调、task cleanup 回调等。
  - `workflowagents/loopagent.loopAgent`（`loopagent/agent.go:71`）、`parallelagent.run`（`parallelagent/agent.go:67`）、`sequentialagent.sequentialAgent`（`sequentialagent/agent.go:76`）：三种工作流 agent 各自实现 `Run`。

## 4. 关键数据结构

- **`agent`（私有 struct，`agent/agent.go:139`）**
  - 字段：`name/description/subAgents`、`beforeAgentCallbacks`、`run`、`afterAgentCallbacks`，并嵌入 `agentinternal.State`（用于标注 `AgentType = TypeCustomAgent`）。
  - 该 struct 是 `agent.New` 的产物，也作为 `llmagent`、`remoteagent`、`workflowagents` 的基类被包装（后者通过 `agentinternal.Reveal` 改写 `State` 字段）。

- **`invocationContext`（私有 struct，`agent/agent.go:362`）**
  - 字段：`agent/Artifacts/Memory/Session`、`invocationID/branch/userContent/runConfig/endInvocation`，并嵌入 `context.Context`。
  - 实现 `InvocationContext` 接口；`WithContext` 复制后替换底层 ctx。

- **`callbackContext`（`agent/callback_context.go:101`）**
  - 字段：`invocationContext`、`artifacts`（可能是 `trackedArtifacts`）、`actions`、`functionCallID`（tool 专用）、`toolConfirmation`（tool 专用）。
  - 同时实现 `CallbackContext` 和 `ToolContext`（注释明确说这是单一实现，靠是否被 `NewToolContext` 构造来决定是否启用 tool 字段）。

- **`callbackContextState`（`agent/callback_context.go:217`）**
  - 实现 `session.State`，把写操作路由到 `actions.StateDelta`（影响下一个事件），把读操作先看 delta 再看 session state。

- **`trackedArtifacts`（`agent/callback_context.go:243`）**
  - 装饰器：保存 artifact 成功后把 `(name → version)` 写入 `actions.ArtifactDelta`，确保回调产生的变更能反映到事件里。

- **`llmAgent`（`llmagent/llmagent.go:340`）**
  - 字段：`agent.Agent`（基类）、`llminternal.State`（含 Model/Tools/Instruction/OutputKey/InputSchema/OutputSchema 等）、`before/after/onModelCallbacks`、`before/after/onToolCallbacks`、`inputSchema/outputSchema`。
  - `agentinternal.Reveal` 在构造时把它标记为 `TypeLLMAgent`。

- **`a2aAgent`（`remoteagent/v2/a2a_agent.go:195`）**
  - 仅持有 `serverConfig`（内部 `iremoteagent.A2AServerConfig`，含 AgentCard/AgentCardProvider/ClientProvider）。
  - `Run` 函数作为闭包传给 `agent.New`，把 A2A 协议消息/事件与 ADK session.Event 互转。

- **`a2aAgentRunProcessor`（`remoteagent/v2/a2a_agent_run_processor.go:40`）**
  - 字段：`config`（A2AConfig）、`partConverter`、`request`、`aggregations`（artifactID → 累积内容）、`aggregationOrder`。
  - 负责在 streaming 模式合并 partial 事件，并在收到 terminal event 或 `Task` 快照时输出非 partial 聚合。

- **`result`（`workflowagents/parallelagent/agent.go:160`）**
  - `event/err/ackChan`：每个 sub-agent 推一个 event，runner 处理完后回 ack，再继续推下一个，实现 backpressure。

- **`sequentialLiveSession`（`workflowagents/sequentialagent/agent.go:91`）**
  - 持有 `sync.Mutex` + 当前活跃的子 live session，让外部 `Send/Close` 始终落在"当前正在运行的子 agent"上。

## 5. 关键流程

### 5.1 自定义 agent 构造（`agent.New`）
- 入口：`agent.New(cfg Config)`（`agent/agent.go:55`）
- 步骤：去重 `subAgents` → 组装 `agent{}` 并把 `AgentType` 标为 `TypeCustomAgent`。
- 出口：返回 `Agent` 接口；失败原因 = 子 agent 在 `SubAgents` 中出现多次。

### 5.2 单个 agent 调用的执行循环
- 入口：`(*agent).Run(ctx)`（`agent/agent.go:162`）
- 步骤：
  1. `telemetry.StartInvokeAgentSpan` 启动 invoke span，包装 `yield` 让每次事件触发 `TraceAgentResult`。
  2. 构造新的 `invocationContext`（带上 `endInvocation` 状态）。
  3. `runBeforeAgentCallbacks` 顺序跑 plugin 的 `RunBeforeAgentCallback` 和用户的 `BeforeAgentCallbacks`，任意一个返回非 nil 就构造事件并 `EndInvocation`。
  4. 若 `ctx.Ended()` 提前返回，否则进入 `a.run(ctx)` 主循环（由具体子 agent 决定如何 yield events）。
  5. `runAfterAgentCallbacks` 同样顺序跑 plugin + 用户 callback，状态 delta 也会单独产出一个事件。
- 出口：所有 event 通过 `yield` 推给调用方；yield 返回 false 时立即停止。

### 5.3 FindAgent / FindSubAgent
- 入口：`(*agent).FindAgent(name)` / `FindSubAgent(name)`（`agent/agent.go:221`, `:228`）
- 步骤：先在自身匹配 → 失败时递归遍历 `SubAgents()` 调用它们的 `FindAgent`。
- 出口：找到返回 Agent 指针，否则 nil；典型用例：LLM 决定把控制权转给子 agent 时，runner 用它来解析目标 agent。

### 5.4 LLM agent 一次 step（simplified）
- 入口：`(*llmAgent).run(ctx)`（`llmagent/llmagent.go:361`）
- 步骤：
  1. 用 `icontext.NewInvocationContext` 包装 ctx（保留 branch/agent/session 等）。
  2. 构造 `llminternal.Flow`（`Model + DefaultRequestProcessors/DefaultResponseProcessors + 各类 callbacks`）。
  3. `f.Run(ctx)` 内部循环：request processor → LLM → response processor → tool 执行 → 状态更新，直到 final response 或 `EndInvocation`。
  4. 每个 event 出来都跑 `maybeSaveOutputToState`：仅当作者是本 agent、`OutputKey` 非空、event 非 partial 且有内容时，把所有非 `Thought` 文本拼接写入 `event.Actions.StateDelta[OutputKey]`。
- 出口：把 flow yield 出来的事件按原序透传给上层 `agent.Run`。

### 5.5 A2A 远程 agent streaming 处理
- 入口：`a2aAgent.run`（`remoteagent/v2/a2a_agent.go:199`）
- 步骤：
  1. 解析 agent card → 构造 `A2AClient` → 构造 `a2a.Message`（用最近的 user FunctionResponse 关联 task/context，否则用 `toMissingRemoteSessionParts` 重建历史）。
  2. `runBeforeA2ARequestCallbacks` 若短路，则把结果经 after 回调后再 yield 一次。
  3. 根据 `RunConfig.StreamingMode` 选择 `SendMessage` 或 `SendStreamingMessage`。
  4. 每条 A2A 事件经 `convertToSessionEvent`（默认 `adka2a.ToSessionEventWithParts`，可被 `Converter` 覆盖）→ `runAfterA2ARequestCallbacks`（可被替换）→ `aggregatePartial`（合并 streaming artifact update，并在 terminal/snapshot 事件处输出非 partial 聚合）→ `yield`。
  5. `defer cleanupRemoteTask`：只有 task 未到达 terminal 状态时调用 `CancelTask`（5 秒超时），或交给 `RemoteTaskCleanupCallback`。
- 出口：调用方（agent.Run）收到一组 `*session.Event`，错误以 error 形式 `yield(nil, err)`。

## 6. 扩展点

- **自定义 agent**：`agent.New(cfg Config)`（`agent/agent.go:55`）接受一个 `Run` 闭包，可以插入任意业务逻辑。
- **子 agent 树**：所有 agent 的 `SubAgents` 字段支持嵌套；`workflowagents` 利用此机制组合多 agent 行为。
- **回调钩子**：
  - agent 级：`BeforeAgentCallbacks / AfterAgentCallbacks`（`agent/agent.go:97`, `:106`）。
  - LLM 级：`Before/After/OnModelCallbacks`（`llmagent/llmagent.go:176-187`）。
  - tool 级：`Before/After/OnToolCallbacks`（`llmagent/llmagent.go:264-275`）。
  - A2A 级：`Before/AfterRequestCallbacks`、`Converter`、`A2APartConverter`、`GenAIPartConverter`（`remoteagent/v2/a2a_agent.go:108-140`）。
- **HITL**：`CallbackContext.RequestConfirmation`（`agent/callback_context.go:196`）让工具主动要求用户确认。
- **Live 扩展**：`llmagent.RunLive` / `sequentialagent.RunLive`（`llmagent/llmagent.go:396`, `sequentialagent/agent.go:125`）可以重新实现 `LiveSession` 行为；任何 sub-agent 若不实现 `RunLive` 接口，在 sequential live 中会直接 yield error。
- **指令模板**：`InstructionProvider`（`llmagent/llmagent.go:490`）允许动态生成 instruction（不自动注入占位符）。
- **include contents 控制**：`llmagent.IncludeContents` 取值 `none` / `default`（`llmagent/llmagent.go:333-338`）。
- **Loader 扩展**：`NewMultiLoader` 接受任意数量的 agent，可以挂载到 runner 上作为 agent 工厂。

## 7. 错误处理

- 本模块没有定义专门的 error 类型，失败以 `fmt.Errorf` 包装（`%w`）或直接返回底层 error。
- 典型失败模式：
  - `agent.New`：sub-agent 重复（`agent.go:59`）。
  - `MultiLoader.NewMultiLoader`：agent 名重复（`loader.go:76`）。
  - `RunBeforeAgentCallback` / `RunAfterAgentCallback`（plugin 或用户 callback）返回 error 时，包装为 `"failed to run ... callback: %w"`（`agent.go:258,275,316,333`）。
  - `llmagent.New`：底层 `agent.New` 失败（`llmagent.go:105`）；`agentinternal.Reveal` 转换失败（`llmagent.go:120`）。
  - `remoteagent.NewA2A`（v1）：必须提供 `AgentCard` 或 `AgentCardSource`（`remoteagent/a2a_agent.go:132`）；`MessageSendConfig` 转换失败（`:179`）。
  - `remoteagent/v2.NewA2A`：必须提供 `AgentCard` 或 `AgentCardProvider`（`v2/a2a_agent.go:158`）；内部类型断言失败（`:186`）。
  - `parallelagent.run`：`runSubAgent` 出错 → 包装为 `failed to run sub-agent %q: %w`（`parallelagent/agent.go:95`）。
  - `sequentialagent.RunLive`：sub-agent 不支持 `RunLive` 时 yield error 并终止（`sequentialagent/agent.go:173`）。
  - `SearchMemory` 在 `Memory` 为 nil 时返回 `memory service is not set`（`callback_context.go:180`）。
  - `RequestConfirmation` 在 `functionCallID` 为空时返回 `error function call id not set when requesting confirmation for tool`（`callback_context.go:198`）。

## 8. 并发与性能

- **goroutine / 锁**：
  - `parallelagent.run` 用 `errgroup.WithContext` + 独立 goroutine 跑每个 sub-agent（`parallelagent/agent.go:71-100`），通过 `resultsChan + ackChan` 串行回推 event，backpressure 模型。
  - `sequentialagent.sequentialLiveSession` 用 `sync.Mutex` 保护 `activeSess / closed`（`sequentialagent/agent.go:91-123`）。
  - 主 agent 不维护可变共享状态；`invocationContext.endInvocation` 是单 goroutine 内的标记。
- **span / telemetry**：`StartInvokeAgentSpan` + `WrapYield` 把每次事件上报到 OpenTelemetry（`agent/agent.go:164-171`）。`llmagent.run` 也可继承根 span 上下文。
- **性能注意**：
  - `a2aAgentRunProcessor.updateAggregation` 合并 partial text 时按"前一个 part 也是同 Thought 类型的 text"做 `+=` 拼接，避免产生过碎 part（`remoteagent/v2/a2a_agent_run_processor.go:133-148`）。
  - `parallelagent` 的 ack 机制意味着一次只有一个 sub-agent 推下一个 event，并行收益主要在 sub-agent 内部 LLM/tool 阶段，event 序列化仍是单线程。
  - `trackedArtifacts.Save` 注释里有 TODO：当前没有锁，多个工具并发写同一个 artifact 时版本号可能不是最新（`callback_context.go:257`）。

## 9. 依赖与被依赖

- **本模块导入**（节选主要包）：
  - 标准库：`context` / `fmt` / `iter` / `sync` / `sync/atomic`（间接）。
  - 外部：`google.golang.org/genai`、`github.com/google/uuid`、`go.opentelemetry.io/otel/trace`。
  - ADK 内部包：
    - `google.golang.org/adk/artifact`、`memory`、`model`、`session`（核心数据面）。
    - `agentinternal "google.golang.org/adk/internal/agent"`（共享 `State` + `Reveal` + `AgentType`，用来标记 `TypeLLMAgent/TypeLoopAgent/TypeSequentialAgent/TypeParallelAgent/TypeRemoteAgent/TypeCustomAgent`）。
    - `icontext "google.golang.org/adk/internal/context"`（生产真正可用的 `InvocationContext`）。
    - `llminternal "google.golang.org/adk/internal/llminternal"`（`Flow`、`DefaultRequestProcessors/DefaultResponseProcessors`，被 llmagent 使用）。
    - `google.golang.org/adk/internal/telemetry`（`StartInvokeAgentSpan` / `WrapYield` / `TraceAgentResult`）。
    - `google.golang.org/adk/internal/plugininternal/plugincontext`（用于从 ctx 取 plugin manager）。
    - `google.golang.org/adk/server/adka2a`、`/v2`（A2A 工具函数 + `NewRemoteAgentEvent`）。
    - `github.com/a2aproject/a2a-go/v2/a2a` 等（remoteagent v2 的协议栈）；v1 同时 import 旧版 `a2a-go`。

- **被依赖**（按 grep 统计，部分）：
  - 框架核心：`runner/runner.go`、`runner/live_runner_test.go`、`internal/context/invocation_context.go`（实现 `InvocationContext`）、`internal/configurable/*`（conformance 录制）、`plugin/*`（`plugin.go`、`functioncallmodifier`、`loggingplugin`、`retryandreflect` 等）。
  - 示例：`examples/quickstart/main.go`、`examples/web/main.go`、`examples/a2a/main.go`、`examples/vertexai/agent.go`、`examples/mcp/main.go`、`examples/bidi/*` 等所有可运行 demo。
  - 子包被本模块使用：`tool`、`tool/functiontool`（`sequentialagent` 注入 `task_completed` 工具）、`session`、`memory`、`artifact`。
  - 测试：`agent/llmagent/llmagent_test.go`、`state_agent_test.go`、`dynamic_events_test.go`、`workflowagents/*/agent_test.go`、`remoteagent/v2/*_test.go`、`remoteagent/a2a_agent_compat_test.go`。

## 10. 测试与可观察性

- **测试文件位置**（均为 `*_test.go` 与 source 同目录）：
  - `agent/agent_test.go`：覆盖 `BeforeAgentCallbacks` / `AfterAgentCallbacks` 的 short-circuit、`EndInvocation` 提前/延后结束、`WithContext`、`FindAgent` 嵌套解析。
  - `agent/loader_test.go`：覆盖 single/multi loader。
  - `agent/llmagent/llmagent_test.go`（1195 行）+ `state_agent_test.go`（719 行）+ `dynamic_events_test.go` + `llmagent_saveoutput_test.go`：通过 `testdata/*.httprr` 录制/回放 LLM HTTP 流量，覆盖 flow、callbacks、state、outputKey、includeContents 等。
  - `agent/workflowagents/loopagent/agent_test.go`、`parallelagent/agent_test.go`、`sequentialagent/agent_test.go`：分别覆盖循环/并行/顺序工作流的 run 与事件顺序。
  - `agent/remoteagent/v2/a2a_agent_test.go`（1437 行）+ `a2a_e2e_test.go`（1276 行）+ `a2a_agent_run_processor_test.go` + `utils_test.go`：A2A 协议事件聚合、streaming vs non-streaming、part 转换、错误处理、cleanup 等。
  - `agent/remoteagent/a2a_agent_compat_test.go`：v1 兼容层把 v1 callback/card 适配到 v2 的回归测试。

- **telemetry 埋点**：
  - `agent/agent.go:164`：`telemetry.StartInvokeAgentSpan` 启动 "invoke agent" span，span 名带 `a.Name()`，`session.ID()` 和 `invocationID` 作为属性。
  - `agent/agent.go:165-170`：`telemetry.WrapYield` 包装 yield，使得每个 `(event, err)` 退出后自动调用 `TraceAgentResult`，把响应/错误写回 span。
  - 其余 span 来自子模块（`llminternal.Flow`、`remoteagent/v2` 内部 converter 等）。

## 11. 文档写作提示

- **必须写清楚**：
  - `Agent` 接口的扩展约束（当前推荐用 `agent.New` 而不是直接实现接口，注释里有 TODO 提到未来会放开）。
  - 三个上下文接口（`InvocationContext / ReadonlyContext / CallbackContext / ToolContext`）的层级关系和"为什么 `ToolContext` 在 `callbackContext` 同一 struct 上"。
  - `Run` 是基于 `iter.Seq2` 的懒迭代式，调用方需要明确 `yield` 返回 `false` 时的语义（"立即终止"）。
  - `Before/AfterAgentCallback` 的 short-circuit 行为：返回非 nil 时会构造新事件并触发 `EndInvocation`，但**不**自动终止已经 yield 出去的事件。
  - `OutputKey` 的写入时机：仅当作者是本 agent、event 非 partial、内容非空、且跳过 `Thought` 标记的 part。
  - `parallelagent` 的串行回推 vs 真并行的差异（事件序列化在主 goroutine）。
  - `remoteagent` 包本身是 deprecated，文档应明确指向 `remoteagent/v2`。
  - A2A streaming 的 partial 聚合：`a2aAgentRunProcessor.aggregatePartial` 在 `Task` 快照或 terminal `TaskStatusUpdateEvent` 时输出非 partial 聚合。

- **可省略**：
  - 私有 struct 字段的逐行解释。
  - 错误码字符串本身（一般是 wrap 而非分类）。
  - `httprr` 测试录制细节（属测试基础设施，不属于公共 API）。

- **潜在坑**：
  - `agent.New` 不会校验 `Name` 非空（注释说"Name 必须非空且唯一"，但代码没显式 check），调用方需要自行保证。
  - `SubAgents` 重复检测只查 *同一* 次 `Config.SubAgents` 内的重复，跨层同名仍可能通过。
  - `trackedArtifacts.Save` 没有锁，并发场景下版本号可能不是最新（注释 TODO）。
  - `llmagent.New` 中存在 `agentinternal.Reveal` 重新设置 `AgentType` 和 `Config` 的"hack"（`llmagent/llmagent.go:113-126`），目的是让 `BeforeAgentCallback` 能拿到 LLM 相关字段——文档里要解释这套过渡期设计。
  - `parallelagent.run` 中若 `yield` 返回 `false`，会 `break` 事件循环并通过 `defer close(doneChan)` 通知 sub-agent 退出（`parallelagent/agent.go:112-127`），但错误仍会传播给 `errGroup`。
  - `sequentialagent.RunLive` 会**就地修改** LLM 子 agent 的 `Tools` 和 `Instruction`（追加 `task_completed` 工具和提示语），这是一个有副作用的扩展点（`sequentialagent/agent.go:147-163`），文档需明确提醒。
  - A2A 协议在 `Message` 类型事件上不会触发 `cleanupRemoteTask`（`remoteagent/v2/a2a_agent.go:314-316`），因为 Message 不是长任务。
