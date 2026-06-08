# runner 模块阅读笔记

## 1. 一句话定位
`runner` 包是 ADK 的"会话级"运行时门面，负责把一次用户输入（Run）或一段实时流（RunLive）装配成正确的 agent 调用上下文，并把产生的 event 序列落库到 session/artifact 服务，同时串联起 plugin 的回调钩子。

## 2. 子包/子目录结构
本模块是单包模块（无子目录）。源文件清单：
- `runner/runner.go` — 主文件，包含 `Config`、`PluginConfig`、`RunOption`、核心 `Runner` 类型及其 `Run`/`RunLive` 方法（678 行）
- `runner/runner_test.go` — `findAgentToRun`、`isTransferableAcrossAgentTree`、`SaveInputBlobsAsArtifacts`、`AutoCreateSession` 四个测试（449 行）
- `runner/live_runner_test.go` — `RunLive` 三个测试：回调、EarlyExit、ChronologicalBuffering（301 行）

无 `doc.go`，包级注释见 `runner/runner.go:15` "Package runner provides a runtime for ADK agents."

## 3. 核心类型与接口
- `Config` — `runner/runner.go:44-58`。构造 Runner 的配置载体，必填 `Agent`、`SessionService`，可选 `ArtifactService`、`MemoryService`、`PluginConfig`、`AutoCreateSession`、`AppName`。
- `PluginConfig` — `runner/runner.go:60-63`。包含 `Plugins []*plugin.Plugin` 与 `CloseTimeout time.Duration`，透传给内部 `plugininternal.NewPluginManager`。
- `RunOption` / `runOptions` — `runner/runner.go:65-69`。函数式选项，目前只承载 `stateDelta map[string]any`，对应 `WithStateDelta`（`runner/runner.go:72-76`）。
- `Runner` — `runner/runner.go:116-126`。包内公开核心类型，封装 appName、rootAgent、各 Service、parentmap、pluginManager 等依赖。仅有 `Run`、`RunLive` 两个公开方法。
- `liveAgent` — `runner/runner.go:270-272`。内部小接口，仅 `RunLive` 用作类型断言，要求目标 agent 实现 `RunLive` 才能跑 Live。
- `runnerLiveSession` — `runner/runner.go:274-280`。`agent.LiveSession` 的实现，包装底层 live session；额外在 `Send` 中把用户文本内容回写 session（`runner/runner.go:282-312`）。
- `closedLiveSession` — `runner/runner.go:318-326`。EarlyExit 场景下返回的"假" live session，所有 `Send` 返回 "session is closed" 错误。
- `session.NewEvent` / `model.LLMResponse` — 来自 session/model 包的复用类型，被 runner 用于构造 user 事件和 earlyExit 事件（`runner/runner.go:220-228, 409-413`）。

## 4. 关键数据结构
- `Runner` 结构（`runner/runner.go:116-126`）字段含义：
  - `appName`：调用方提供的多租户应用名，用于所有 session/artifact 请求的命名空间。
  - `rootAgent`：会话入口 agent，作为 `findAgentToRun` 回退目标。
  - `sessionService / artifactService / memoryService`：三个可选/必选后端服务；artifact 与 memory 可为 nil。
  - `parents parentmap.Map`：在 `New` 中由 root agent 一次性建立（`runner/runner.go:88-91`），后续随 `parentmap.ToContext` 注入 ctx，供 agent 在调用时反查父链。
  - `pluginManager *plugininternal.PluginManager`：统一管理所有 plugin 的生命周期与回调派发。
  - `autoCreateSession bool`：决定 `session.Get` 失败时是否自动 Create。
- `runOptions`（`runner/runner.go:67-69`）只装 `stateDelta`，由 `WithStateDelta` 写入（`runner/runner.go:72-76`）；该 delta 会附着到 user 事件上（`runner/runner.go:580-582`），本质是一次会话状态的"补丁式"增量。
- `runnerLiveSession`（`runner/runner.go:275-280`）持有 `sess`、`r`（runner）、`iCtx`（已构造的 InvocationContext）、`storedSession`（已落库的 Session），用于在 `Send` 时把客户端文本直接追加到 session 历史。
- `bufferedEvents []*session.Event`（`runner/runner.go:435`）是 `RunLive` 包装迭代器中的局部缓冲：当 transcription 仍在进行但出现了 function call/response 时先暂存，transcription 收尾后再按时间顺序补发。

## 5. 关键流程

### 5.1 一次性 Run（Run）
入口 `(*Runner).Run`（`runner/runner.go:131`），返回 `iter.Seq2[*session.Event, error]`：
1. 应用所有 `RunOption`（`runner/runner.go:136-139`）。
2. `sessionService.Get` 拿 `storedSession`；失败时按 `autoCreateSession` 决定是否 `Create`（`runner/runner.go:142-164`）。
3. `findAgentToRun` 决定本次实际执行的 agent（`runner/runner.go:166-170`），逻辑见 `runner/runner.go:592-623`。
4. 依次把 `parentmap`、`runconfig`、`pluginManager` 注入 `ctx`（`runner/runner.go:172-176`）。
5. 若配置了 `artifactService` / `memoryService`，构造对应的 `agent.Artifacts` / `agent.Memory` 实现（`runner/runner.go:178-196`）。
6. 用 `icontext.NewInvocationContext` 构造 `InvocationContext`（`runner/runner.go:198-205`）。
7. `appendMessageToSession`：plugin 拦截用户消息、可选 `SaveInputBlobsAsArtifacts` 把 inline blob 存为 artifact 并改写 msg、最终把 user event 写 session（`runner/runner.go:533-588`）。
8. 注册 `defer pluginManager.RunAfterRunCallback`（`runner/runner.go:212-216`）。
9. 调 `pluginManager.RunBeforeRunCallback`；若早退则合成一个 author=user 的 earlyExit 事件落库后 yield 一次即返回（`runner/runner.go:218-232`）。
10. 主循环 `for event, err := range agentToRun.Run(ctx)`（`runner/runner.go:234-266`）：
    - 对每个非 partial 事件，先经 `pluginManager.RunOnEventCallback`（可能改写），再 `sessionService.AppendEvent` 落库。
    - 把 `event` yield 给调用方。
    - 退出条件：调用方不再 yield（`return`）或 session 写入失败。

### 5.2 findAgentToRun（决定落到哪个 agent）
入口 `(*Runner).findAgentToRun`（`runner/runner.go:592`）：
1. 若当前用户消息是 function response，先调 `handleUserFunctionCallResponse` 找到对应 function call 的 event（`runner/runner.go:593-599`，实现在 `runner/runner.go:627-650`），其 author 即本次目标 sub-agent。
2. 否则倒序遍历 session events，跳过 user 事件，用 `rootAgent.FindAgent(event.Author)` 找 sub-agent，并经 `isTransferableAcrossAgentTree` 检查父链上每节点都没设置 `DisallowTransferToParent`（`runner/runner.go:601-619`，实现在 `runner/runner.go:653-666`）。
3. 都失败时回退到 `rootAgent`（`runner/runner.go:622`）。

### 5.3 RunLive（实时双向流）
入口 `(*Runner).RunLive`（`runner/runner.go:328`）：
1. 解析 `runOptions`（`runner/runner.go:329-332`）。
2. 拿到/创建 session（`runner/runner.go:334-355`）。
3. `findAgentToRun(storedSession, nil)`（`runner/runner.go:358`）。
4. 类型断言 `agentToRun.(liveAgent)`，否则返回 "agent does not support Live Run"（`runner/runner.go:363-366`）。
5. 注入 ctx（parentmap、runconfig 强制 `StreamingModeBidi`、`Live` 配置、pluginManager）（`runner/runner.go:368-373`）。
6. 装配 artifacts/memory 与 `iCtx`（`runner/runner.go:375-401`）。
7. 调 `pluginManager.RunBeforeRunCallback`：返回内容时合成 earlyExit 事件落库并返回 `closedLiveSession{}`（`runner/runner.go:403-423`）。
8. 调用 `lAgent.RunLive(iCtx)` 拿到 `agentSess`、`innerIter`（`runner/runner.go:425-428`）。
9. 包装 `wrappedIter`（`runner/runner.go:430-523`）做三件事：
   - `defer pluginManager.RunAfterRunCallback`（`runner/runner.go:431-433`）。
   - 经 `RunOnEventCallback` 改写 event。
   - **Chronological buffering**：当 `isTranscribing && isToolCallOrResp` 时把事件压入 `bufferedEvents`（`runner/runner.go:461-478`），等最终 transcription 落库后再按顺序 flush（`runner/runner.go:480-507`）。
   - 非 partial 且无 inline data 的事件统一 `AppendEvent` 落库（`runner/runner.go:510-517`）。
10. 返回 `&runnerLiveSession{...}` 与 `wrappedIter`（`runner/runner.go:525-530`）。
11. `Send` 收到非 function response 的客户端文本时，也直接写一个 author=user 的 event 到 session（`runner/runner.go:289-309`）。

### 5.4 appendMessageToSession（Run 前的"准备 + 落库"）
入口 `(*Runner).appendMessageToSession`（`runner/runner.go:533`）：
1. `nil` msg 直接返回（`runner/runner.go:534-536`）。
2. `pluginManager.RunOnUserMessageCallback` 可改写 msg，并按需用新 user content 重建 InvocationContext（`runner/runner.go:537-555`）。
3. `SaveInputBlobsAsArtifacts` 为 true 时遍历 `msg.Parts`，对每个 `InlineData` 调用 `artifacts.Save` 存为 `artifact_<invocationID>_<i>`，并把该 part 替换为文本占位（`runner/runner.go:557-572`）。
4. 构造 author=user、Content=msg、可选 StateDelta 的 event，`sessionService.AppendEvent` 落库（`runner/runner.go:574-587`）。

## 6. 扩展点
- `Config` 中的 `PluginConfig`（`runner/runner.go:60-63`）允许插入任意 `*plugin.Plugin`，通过 `plugininternal.PluginManager`（见 `runner/runner.go:93-99`）派发 `BeforeRunCallback` / `AfterRunCallback` / `OnEventCallback` / `OnUserMessageCallback`。
- `RunOption` 是当前唯一的"每次调用"扩展点，目前只暴露 `WithStateDelta`（`runner/runner.go:72-76`）；新增 RunOption 只需追加一个 setter 即可被 `Run`/`RunLive` 自动接收（`runner/runner.go:136-139, 329-332`）。
- `liveAgent` 接口（`runner/runner.go:270-272`）是 agent 自定义 RunLive 能力的钩子；任何 `Agent` 只要实现该方法即可在 `RunLive` 中被识别。
- `Config.Agent` 接受任意 `agent.Agent`，root agent 可携带子 agent 树，由 `parentmap.New` 一次性建立（`runner/runner.go:88-91`）。

## 7. 错误处理
- 构造期：`New` 校验 `Agent == nil` 返回 `"root agent is required"`，`SessionService == nil` 返回 `"session service is required"`（`runner/runner.go:80-86`）。
- 会话期：`sessionService.Get` 失败且未启用 `AutoCreateSession` 时，直接把 error yield（`runner/runner.go:147-151`）；`Create` 失败同样 yield error（`runner/runner.go:157-160`）。
- 落库失败：`sessionService.AppendEvent` 失败统一包成 `"failed to add event to session: %w"`（`runner/runner.go:225-228, 257-260, 414-417, 484-489, 495-499, 511-515`）；`RunLive` 的 `Send` 中对应错误信息是 `"failed to add user event to session: %w"`（`runner/runner.go:305-307`）。
- Plugin 回调失败：`RunOnUserMessageCallback` 错误包成 `"error running on run user message callback : %w"`（`runner/runner.go:539-541`）；`RunOnEventCallback` 错误会 yield 一个 `nil, err`（`runner/runner.go:243-249, 446-453`）。
- Agent tree 构建失败：`parentmap.New` 包成 `"failed to create agent tree: %w"`（`runner/runner.go:88-91`）。
- Live 不支持：`RunLive` 找不到 `liveAgent` 实现时返回 `"agent does not support Live Run"`（`runner/runner.go:365`）。
- EarlyExit 后的 `closedLiveSession.Send` 必返回 `"session is closed"`（`runner/runner.go:320-322`），用错误告诉客户端"agent 已被 plugin 终止"。
- 整体无自定义 error 类型，全是 `fmt.Errorf` 包装；典型失败模式是"插件早退"、"会话不存在且未开启自动创建"、"AppendEvent 写入失败"。

## 8. 并发与性能
- **没有显式锁、没有 goroutine**。`Runner` 本身线程安全假设由调用方保证；`Run` / `RunLive` 都是普通同步函数，通过 Go 1.23 的 `iter.Seq2` 把执行权以 lazy pull-based 迭代的方式交给调用方（`runner/runner.go:131, 328`）。
- **全局状态**：仅在 `findAgentToRun` / `handleUserFunctionCallResponse` 中使用 `log.Printf` 打印 unknown agent / function call（`runner/runner.go:598, 612`），没有其它全局可写状态。
- **潜在性能点**：
  - `findAgentToRun` 倒序遍历 session events 直到命中可转移 agent（`runner/runner.go:601-619`），session 越长命中越慢；不过 `isTransferableAcrossAgentTree` 一次就会失败早退。
  - `RunLive` 的 chronological buffer（`runner/runner.go:435-505`）只对 transcription 期间出现 tool call 才会堆积事件，最坏情况受 transcription 长度限制。
  - `appendMessageToSession` 会对每个 InlineData part 走一次 `artifacts.Save`，大文件多时同步串行（`runner/runner.go:557-572`）。
- **缓存**：唯一被缓存的是 `parentmap.Map`，在 `New` 时一次构建（`runner/runner.go:88-91`），随 `parentmap.ToContext` 注入 ctx，避免每次调用重建。
- **TODO 提示**：`runner/runner.go:132-134` 留有两个未实现：校验 `cfg` 与 agent 的兼容性、对接 tracer；这部分后续可能引入 OTel/性能采样。

## 9. 依赖与被依赖
**导入（`runner/runner.go:18-41`）**：
- 标准库：`context`、`fmt`、`iter`、`log`、`time`
- 外部：`google.golang.org/genai`（用于 `*genai.Content`）
- ADK 内部：
  - `google.golang.org/adk/agent`、`artifact`、`memory`、`model`、`plugin`、`session`（公开 API）
  - `google.golang.org/adk/internal/agent/parentmap`、`internal/agent/runconfig`（注入 ctx）
  - `artifactinternal "google.golang.org/adk/internal/artifact"`（包装 `artifact.Service` 为 `agent.Artifacts`）
  - `icontext "google.golang.org/adk/internal/context"`（构造 `InvocationContext`）
  - `llminternal`（用于在 `isTransferableAcrossAgentTree` 中读 `DisallowTransferToParent`）
  - `imemory "google.golang.org/adk/internal/memory"`（包装 `memory.Service` 为 `agent.Memory`）
  - `plugininternal`（`PluginManager` + ctx 注入）
  - `utils`（`FunctionResponses` / `FunctionCalls` 工具）

**被依赖**（grep `google.golang.org/adk/runner` 结果）：
- `cmd/launcher/console/console.go` — CLI 启动器直接 `runner.New`
- `cmd/launcher/web/a2a/a2a.go` — web A2A 启动器用 `runner.Config`
- `cmd/internal/adkcli/main.go` — adkcli 主程序
- `server/adka2a/executor.go` 与 `server/adka2a/v2/executor.go` — A2A 协议执行器，把 `Runner`/`RunnerConfig` 暴露成 `v2.Runner` 别名
- `server/adkrest/handler.go`、`server/adkrest/controllers/runtime.go`、`server/adkrest/controllers/triggers/eventarc.go` — REST API 控制器
- `server/agentengine/controllers/method/stream_query.go` — Agent Engine 兼容接口
- `plugin/plugin_manager_test.go`、`plugin/functioncallmodifier/integration_test.go` — 插件测试使用 runner
- `agent/workflowagents/*/agent_test.go`、`agent/llmagent/state_agent_test.go`、`agent/remoteagent/*` — agent 子包测试
- `internal/llminternal/*_test.go` — 并行 function call 集成测试
- `internal/testutil/test_agent_runner.go` — 通用 `TestAgentRunner`，几乎所有模块的测试都通过它走 runner.Run
- `examples/bidi/*`、`examples/toolconfirmation/main.go`、`examples/a2a/main.go`、`examples/agentengine/main.go`、`examples/tools/loadartifacts/main.go`、`examples/tools/loadmemory/main.go` — 全部 examples
- 总结：runner 是 ADK 的"调度总入口"，server/cmd 层和 testutil 都直接依赖它；agent 子包在测试中也依赖它。

## 10. 测试与可观察性
- 测试文件：
  - `runner/runner_test.go` — `TestRunner_findAgentToRun`（4 个表驱动用例，验证 rootAgent 路由规则）、`Test_isTransferrableAcrossAgentTree`（3 个用例，覆盖 DisallowTransferToParent 与非 LLM agent）、`TestRunner_SaveInputBlobsAsArtifacts`（端到端验证 artifact 落盘与 msg 改写）、`TestRunner_AutoCreateSession`（4 用例覆盖 autoCreate 开关）。
  - `runner/live_runner_test.go` — `TestRunner_RunLive_Callbacks`（验证 before/after callback 时机）、`TestRunner_RunLive_EarlyExit`（验证 plugin 早退路径）、`TestRunner_RunLive_ChronologicalBuffering`（验证 transcription+tool call 顺序）。`mockLiveAgent`/`dummyLiveSession` 是测试 fixture。
- Telemetry/Tracing：`runner/runner.go:134` 显式 `// TODO: setup tracer.`，当前 runner 不打任何 span/trace。`tracer` 的注入位置在 `Run` 函数体里（还没实现）。
- 日志：只有 `log.Printf` 两种（`runner/runner.go:598, 612`），分别记录 "Function call from an unknown agent" 与 "Event from an unknown agent"，用于排查 agent tree 失配。
- 共享测试：跨模块的 `internal/testutil/test_agent_runner.go` 提供 `NewTestAgentRunner` / `NewTestAgentRunnerWithPluginManager`，封装 `runner.New` + 后续 `Run` 调用，绝大多数 agent 内部测试都通过它跑端到端（`internal/testutil/test_agent_runner.go:40, 94, 103, 122`）。

## 11. 文档写作提示
**必须写**：
- Runner 的双重身份：既能 Run（一次性同步生成）又能 RunLive（实时双向流），用对比表或双段落写清楚，引用 `runner.go:131` / `runner.go:328`。
- `findAgentToRun` 的路由规则三条优先级：function response 找对应 function call 的 author → 倒序扫 events 取最后一个可转移 agent → 兜底 rootAgent。配 `isTransferableAcrossAgentTree` 的 DisallowTransferToParent 检查一起讲。
- Plugin 钩子四个时机的触发点（before/after run、on event、on user message），并指出 on event 只对非 partial 事件落库。
- `WithStateDelta` 的语义：写到 user 事件上、由 session service 在 AppendEvent 时合并。
- `SaveInputBlobsAsArtifacts`：哪一段把 inline data 替换成文字占位（`runner.go:557-572`），用一段示例展示。
- `AutoCreateSession` 的开关语义。
- Live 模式的 chronological buffering 与 "user 文本直接写 session" 的 `Send` 副作用（`runner.go:282-312, 430-523`），这是非常容易踩坑的点。

**可以省略**：
- `Config` / `PluginConfig` / `RunOption` 的字段定义本身（一句话指向源码即可）。
- `closedLiveSession` 的实现细节（提到"早退时返回的禁用句柄"即可）。

**潜在坑**：
- `RunLive` 内部 `iCtx` 重新声明覆盖了外层 `ctx`（`runner.go:198` 与 `runner.go:395`），写文档时不要误把两处 ctx 当成同一个变量讲。
- `Run` 中 `defer RunAfterRunCallback` 是 defer 在 yield 闭包里，只有等调用方把迭代器消费完才触发；这与 Python 版 `adk-python` 不完全一致，文档要明确。
- "plugin earlyExit" 时 `Run` 把事件的 `Author` 设为 `"user"`（`runner.go:221`），而 `RunLive` 把 `Author` 设为 agent 名称（`runner.go:410`）——两路语义不同，写出来避免使用者误解。
- `Run` 与 `RunLive` 在 plugin `OnEventCallback` 里都可能改写 event，但 `Run` 只对非 partial 事件落库；`RunLive` 还受"是否有 inline data"过滤（`runner.go:510-517`）。
- `findAgentToRun` 在 user 是 function response 时只看一次 history 找到匹配 ID 的 function call，不会再走"可转移"判定；这点容易与普通轮次混淆。
- `parentmap` 是在 `New` 时构建的，对 rootAgent 之外的 agent 必须在 `New` 之前已通过 `SubAgents` 挂上树；中途 mutate rootAgent 不会被 runner 感知。

