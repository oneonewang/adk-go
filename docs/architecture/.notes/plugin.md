# plugin 模块阅读笔记

模块路径：`/home/wu/oneone/adk/plugin`
锁定提交：`d06992e2b1ec2c9b95c6070e0fd12d50a43e4c99`

## 1. 一句话定位

`plugin` 包是 ADK 的"可插拔横切关注点"机制：定义统一的回调协议（覆盖 user/run/agent/model/tool 五层生命周期），由 `internal/plugininternal.PluginManager` 在 Runner / A2A executor / AdkREST 三个执行入口统一调度；模块同时内置 3 个示例 plugin（`loggingplugin`、`functioncallmodifier`、`retryandreflect`）作为最佳实践参考。

## 2. 子包/子目录结构

| 子目录 | 作用 |
|---|---|
| `functioncallmodifier/` | 示例 plugin：在 `BeforeModel` 阶段向指定工具的 schema 注入额外参数，在 `AfterModel` 阶段把这些参数从 `FunctionCall.Args` 抽取到 session state 中（典型用途：给 `transfer_to_agent` 加 `skill_id` / `rationale` 编排字段） |
| `loggingplugin/` | 示例 plugin：把 user message / run / agent / LLM / tool 五个层级的回调全部用 ANSI 灰色打印到 stdout，定位为"终端调试 + 插件开发模板" |
| `retryandreflect/` | 示例 plugin：实现自愈式工具错误恢复，超过最大重试次数后把工具错误"反射"成结构化 LLM 提示，让模型决定下一步；是 Python `adk-py` 插件的 Go 复刻 |

## 3. 核心类型与接口

- **`Plugin` struct**（`plugin/plugin.go:78`）
  - 字段：`name` + 11 个可选回调（`OnUserMessageCallback / OnEventCallback / BeforeRunCallback / AfterRunCallback / BeforeAgentCallback / AfterAgentCallback / BeforeModelCallback / AfterModelCallback / OnModelErrorCallback / BeforeToolCallback / AfterToolCallback / OnToolErrorCallback`）以及 `closeFunc`
  - 不可被外部直接构造，必须通过 `plugin.New(cfg Config)` 创建（`plugin/plugin.go:50`）。所有字段都是 unexported，外部通过同名访问器（`OnUserMessageCallback()` 等）按需读取。

- **`Config` struct**（`plugin/plugin.go:26`）
  - 一次性注入所有可选回调的"配置体"；`CloseFunc` 为 nil 时构造器自动填上 no-op（`plugin/plugin.go:69`）保证 `Plugin.Close()` 永远不 panic。

- **`OnUserMessageCallback` / `BeforeRunCallback` / `AfterRunCallback` / `OnEventCallback` 函数类型**（`plugin/plugin.go:161`-`167`）
  - 4 个 plugin 私有回调类型，专门给 plugin 复用。返回非 nil `*genai.Content` 时会被 `PluginManager` 当作"短路结果"（参见 `internal/plugininternal/plugin_manager.go:84-86` 等多处 `// Early exit` 注释），任意一个 plugin 一旦返回非空结果就停止后续 plugin 与原生流程。

- **`agent.BeforeAgentCallback` / `agent.AfterAgentCallback` / `llmagent.*` 复用类型**（`plugin/plugin.go:36`-`45`）
  - 4 个 agent / model / tool 回调直接复用 `agent` 与 `agent/llmagent` 子包定义的同名类型，确保 plugin 回调签名与 agent 内置 callback 100% 对齐，可被同样的 dispatcher 消费。

- **`retryAndReflect` struct**（`plugin/retryandreflect/plugin.go:61`）
  - 私有实现细节；持有 `sync.Mutex` 与 `scopedFailureCounters map[string]map[string]int`（scope → toolName → 失败次数），支持 `Invocation` 与 `Global` 两种作用域。

- **`FunctionCallModifierConfig` struct**（`plugin/functioncallmodifier/plugin.go:29`）
  - 三字段：`Predicate`（按工具名匹配）、`Args`（要注入的 schema map）、`OverrideDescription`（可选，改写工具描述）。

- **`loggingPlugin` struct**（`plugin/loggingplugin/logging_plugin.go:75`）
  - 仅持有一个 `name` 字段，方法都是 `func(p *loggingPlugin) ...` 形式，把所有回调挂到 `plugin.Config` 上（`logging_plugin.go:49`-`63`）。

## 4. 关键数据结构

- **`Plugin`（plugin.go:78）**：纯数据 + 闭包容器；字段都是单元素（不像 Python 端支持 callback list），一次构造决定该 plugin 的全部行为。`closeFunc` 在构造时默认填充（`plugin.go:69`），是 panic-safe 设计。

- **`Config`（plugin.go:26）**：扁平化"配置-对象"模式，让用户能用 struct literal 一次性配置 11 个回调而无需维护 builder；典型用法见 `loggingplugin/logging_plugin.go:49-63` 与 `functioncallmodifier/plugin.go:37-42`。

- **`retryAndReflect.scopedFailureCounters`（retryandreflect/plugin.go:66）**：`map[scopeKey]map[toolName]int`，双重嵌套：外层 key 在 `Invocation` 模式下是 `ctx.InvocationID()`，在 `Global` 模式下是常量 `globalScopeKey`（`retryandreflect/plugin.go:48, 183-188`）；内层是 tool 维度的失败次数。`resetFailuresForTool` 在 tool 成功时清空对应 entry，scope map 为空时连同外层 key 一起删（`retryandreflect/plugin.go:195-200`）。

- **`templateData` struct（retryandreflect/plugin.go:216）**：模板渲染上下文，包含 `ToolName / ErrorDetails / ArgsSummary / RetryCount / MaxRetries` 5 字段；被两个 `//go:embed` 进来的 `.md` 模板共用（`reflection.md` 用于重试中，`exceeded.md` 用于超过上限）。

- **`PluginManager`（internal/plugininternal/plugin_manager.go:38）**：runner 端的实际驱动者；按注册顺序遍历所有 plugin，对每个 callback 依次调用，直到某个返回非空结果或非 nil 错误就 early-return（`plugin_manager.go:84-86, 100-103, ...`），是经典的"短路链式回调"实现。

- **`plugincontext.PluginManagerCtxKey`（internal/plugininternal/plugincontext/context.go:19）**：把 `*PluginManager` 塞进 `context.Context` 的 ctxKey，runner 通过 `ToContext` 注入（`plugin_manager.go:286-288`），让深层 agent / tool 也能拿到 manager。

## 5. 关键流程

### 流程 1：plugin 构造（任意子包 → `plugin.New` → `PluginManager.Register`）
- 入口：用户调用 `loggingplugin.New(name)` / `functioncallmodifier.NewPlugin(cfg)` / `retryandreflect.New(opts...)`，均返回 `*plugin.Plugin`。
- 步骤：所有内置 plugin 都是薄壳，把自己的方法实现塞到 `plugin.Config{...}` 字段，然后 `return plugin.New(cfg)`（见 `logging_plugin.go:49`、`functioncallmodifier/plugin.go:37`、`retryandreflect/plugin.go:112`）。
- 出口：返回 `*plugin.Plugin`；当 `cfg.CloseFunc == nil` 时构造器自动填上 no-op（`plugin.go:69`）。
- runner 在 `runner.New` 阶段把所有 plugin 喂给 `plugininternal.NewPluginManager`（`runner/runner.go:93`），`registerPlugin` 会按 `Name()` 去重（`plugin_manager.go:62-72`）。

### 流程 2：plugin 回调链触发（Runner.Run → PluginManager.RunXxx）
- 入口：runner 主循环调用 `pluginManager.RunOnUserMessageCallback / RunBeforeRunCallback / RunOnEventCallback / RunBeforeAgentCallback / RunAfterAgentCallback / RunBeforeToolCallback / RunAfterToolCallback / RunOnToolErrorCallback / RunBeforeModelCallback / RunAfterModelCallback / RunOnModelErrorCallback`（见 `runner/runner.go:213-243` 等）。
- 步骤：按 plugin 注册顺序 `for _, plugin := range pm.plugins` 调用对应 callback；一旦某个 plugin 返回非 nil 的 content / args / result / response 就立刻停止（"Early exit"），参见 `plugin_manager.go:76-89`、`171-185`、`204-219` 等。
- 出口：把首个非空结果返回给 runner；runner 据此决定是使用 `earlyExitResult` 跳过原生流程（`runner/runner.go:218, 404`）还是继续走默认 agent/tool 逻辑。

### 流程 3：retryandreflect 工具错误自愈
- 入口：`OnToolErrorCallback` 被触发（`retryandreflect/plugin.go:143`）→ `handleToolError`。
- 步骤：
  1. 跳过 `tool.ErrConfirmationRequired` / `tool.ErrConfirmationRejected`（`retryandreflect/plugin.go:149`）。
  2. 若 `maxRetries == 0` 且 `errorIfRetryExceeded==false`，直接走"超过上限"模板给 LLM 提示（`plugin.go:153-158`）。
  3. 拿 `scopeKey` → 加锁 → `scopedFailureCounters[scope][tool]++`（`plugin.go:160-170`）。
  4. `currentRetries <= maxRetries` → `createToolReflectionResponse` 渲染 `reflection.md`；否则渲染 `exceeded.md`（`plugin.go:172-180`）。
- 出口：把模板渲染结果（`response_type=ERROR_HANDLED_BY_REFLECT_AND_RETRY_PLUGIN` + reflection_guidance）作为工具返回值塞回去，LLM 据此决定换参数/换工具/放弃。
- 关键的反向 hook：`AfterToolCallback` 检查 `result["response_type"]`，避免把"反射式回复"误判为"成功"而清空计数（`plugin.go:128-141`）。

### 流程 4：functioncallmodifier 参数抽取
- 入口：模型请求发出前的 `BeforeModelCallback`（`functioncallmodifier/plugin.go:53`）与模型响应后的 `AfterModelCallback`（`plugin.go:91`）。
- 步骤：
  1. `beforeModel`：遍历 `req.Config.Tools[*].FunctionDeclarations`，对每个匹配 `Predicate` 的工具，把 `cfg.Args` 的 schema 用 `maps.Copy` 合并到 `decl.Parameters.Properties`，可选改写描述（`plugin.go:69-84`）。**注：若 `req.Tools` 中没有该 tool，会 continue**（`plugin.go:64-66`）。
  2. `afterModel`：遍历 `llmResponse.Content.Parts[*].FunctionCall`，对每个匹配 `Predicate` 的函数调用，把 `cfg.Args` 中列出的实际参数值从 `fc.Args` 删除，并写入 `ctx.State().Set("<fcID>/<argName>", value)`（`plugin.go:100-117`）。
- 出口：模型看到的工具 schema 多了一组"可选"参数；模型发起的实际调用少了一组参数（这些参数被搬到 session state 供下游 agent tool 读取，避免污染 LLM 工具调用接口）。

### 流程 5：plugin 关闭
- 入口：Runner 生命周期结束时 `pluginManager.Close()`（`plugin_manager.go:273-284`）。
- 步骤：依次调用每个 plugin 的 `Close()`，把任何错误包成 `fmt.Errorf("error closing plugin '%s': %w", ...)` 累积。
- 出口：返回 `nil` 或一个汇总错误（`plugin_manager.go:280-282`）；因为 `closeFunc` 在构造时已经默认填充，单个 plugin 不会 panic。

## 6. 扩展点

- **自定义 plugin**：第三方代码只需要实现自己的回调逻辑，用 `plugin.New(plugin.Config{...})` 包装即可，所有字段可选。
- **复用 callback 类型**：4 个 agent/model/tool 类回调直接复用 `agent` / `llmagent` 包的类型（`plugin.go:36-45`），意味着任何能传给 `llmagent.New(...)` 的回调也能直接挂到 plugin。
- **PluginManager 的 early-exit 语义**：所有 `Run*` 方法的 "首个非空结果短路" 行为是稳定契约（`plugin_manager.go:84-86` 等），可用于做"用 plugin 替换默认行为"的副作用；不会因为增加 plugin 而改变既有 plugin 顺序。
- **retryandreflect 的可选配置**：`PluginOption` 函数式选项（`WithMaxRetries / WithErrorIfRetryExceeded / WithTrackingScope`，`retryandreflect/plugin.go:73-93`）是扩展点，可以照此模式继续加 `WithReflectionTemplate` 之类的定制。
- **模板替换**：`reflection.md` 与 `exceeded.md` 是 `//go:embed` 进来的（`plugin.go:38, 42`），业务方如需完全控制提示词可拷贝整个子包并改 embed 文件。
- **scope 拓展**：`TrackingScope` 当前只定义了 `Invocation / Global` 两种（`retryandreflect/plugin.go:54-59`），但 `scopeKey` 完全可以基于 `ctx.UserID()` / `ctx.Session().ID()` 等自定义。

## 7. 错误处理

- **`plugin.New` 自身永不返回 error**（`plugin.go:50` 仍声明 `(*Plugin, error)` 是为了与 `llmagent.New` 等保持签名一致 + 给 `MustNew` 留位置）；构造时唯一可能 panic 的来源是 `CloseFunc` 为 nil，但已被 `plugin.go:69-72` 兜底。
- **`retryandreflect.New` 在 `maxRetries < 0` 时返回 error**（`plugin.go:108-110`），是本模块少有的显式错误返回。
- **`handleToolError` 跳过 `tool.ErrConfirmationRequired / tool.ErrConfirmationRejected`**（`retryandreflect/plugin.go:149`），避免对 HITL 流程产生副作用；不重试、不计数。
- **`functioncallmodifier.afterModelCallback` 在 `ctx.State().Set` 失败时返回 `fmt.Errorf("failed to set state: %w", err)`**（`functioncallmodifier/plugin.go:112-114`），让上层 plugin manager 透传给 runner。
- **PluginManager 短路语义**：plugin 返回 `err` 会立即终止整个调用链并把 err 透传给 runner（`plugin_manager.go:81-83, 97-99, ...`），这是一种"plugin 故障 = 整个 run 失败"的设计。
- **Close 错误聚合**：`plugin_manager.go:274-282` 收集所有 plugin 的 close 错误，包装为 `failed to close plugins: [...]`，方便上层一次看到所有问题。

## 8. 并发与性能

- **PluginManager 不持锁**：`registerPlugin` / 各 `Run*` 都不是并发安全的（`plugin_manager.go:62, 76`），需要在 runner 启动前完成所有 `registerPlugin`；调用期只读遍历 `pm.plugins`，天然支持并发。
- **retryandreflect 显式加锁**：`retryAndReflect.mu sync.Mutex` 保护 `scopedFailureCounters`（`retryandreflect/plugin.go:62, 161-162, 193-194`）；`scopeKey` 在 `Invocation` 模式下是 `ctx.InvocationID()`，并发 invocation 不会互相干扰；`Global` 模式下共享一份 counter，是有意的"全局节流"语义。
- **loggingplugin 全部 `fmt.Printf` 到 stdout**：没有任何缓冲/异步日志；如果在 hot path 上开启可能成为瓶颈，但当前定位是"调试 + 示例"（`logging_plugin.go:30-36`）。
- **functioncallmodifier 的 schema 合并**：`maps.Copy` 复制的是 schema 指针，浅拷贝，无额外序列化开销（`functioncallmodifier/plugin.go:79`）；但 `BeforeModel` 每次 LLM 请求都会遍历所有 tools。
- **模板执行**：`retryandreflect.createToolReflectionResponse` / `createToolRetryExceedMsg` 走 `text/template`，每次失败 / 超过上限都会执行一次模板（`retryandreflect/plugin.go:236-238, 261-263`），开销低但**注意：模板执行失败时直接返回 `nil`**，可能导致 LLM 收到空响应（`retryandreflect/plugin.go:239-240, 264-265`）。
- **embed 模板只解析一次**：`reflectionTemplate = template.Must(...)` 是包级变量（`retryandreflect/plugin.go:40, 44`），不会每次调用都重新解析。

## 9. 依赖与被依赖

- 本模块导入：
  - `plugin/plugin.go` → `google.golang.org/genai`、`google.golang.org/adk/agent`、`google.golang.org/adk/agent/llmagent`、`google.golang.org/adk/session`
  - `plugin/functioncallmodifier/plugin.go` → `genai`、`adk/agent`、`adk/model`、`adk/plugin`
  - `plugin/loggingplugin/logging_plugin.go` → `genai`、`adk/agent`、`adk/model`、`adk/plugin`、`adk/session`、`adk/tool`
  - `plugin/retryandreflect/plugin.go` → `adk/agent`、`adk/plugin`、`adk/tool`（+ 内置 `bytes/encoding/json/sync/text/template`）
- 关联的内部实现（不在本模块下但强耦合）：
  - `internal/plugininternal/plugin_manager.go` — `PluginManager` 是 plugin 的真正调度器，由 runner / adka2a / adkrest 共同调用
  - `internal/plugininternal/plugincontext/context.go` — 唯一的 `ctxKey`
- 哪些模块导入本模块（`grep` 结论）：
  - `runner/runner.go`（`PluginConfig`、`pluginManager` 字段）— `runner/runner.go:39, 61, 93`
  - `runner/live_runner_test.go` — 集成测试用法
  - `server/adka2a/executor.go` 与 `server/adka2a/v2/executor.go`、`v2/executor_plugin.go` — A2A 入口也用 plugin
  - `server/adkrest/controllers/runtime_test.go` — REST 入口
  - `cmd/internal/adkcli/main.go` — CLI 工具
  - `examples/agentengine/main.go` — 示例
  - `internal/configurable/conformance/{recordplugin,replayplugin}/` — 一致性录制 / 回放插件也用 `plugin.Plugin` 包装
  - 三个内置子包（互相不依赖，且在 `examples/agentengine` 等示例中作为参考用法）
- 注意点：当前代码库没有在生产代码中直接 import `loggingplugin / functioncallmodifier / retryandreflect`（`grep` 结果仅命中 plugin 自身子目录），它们是"参考实现 + 测试用例"角色。

## 10. 测试与可观察性

- 测试文件分布：
  - `plugin/plugin_test.go`（98 行）— `TestNew` 覆盖 Config 字段映射 + `CloseFunc` nil 安全。
  - `plugin/plugin_manager_test.go`（878 行）— 跨多个 test case 覆盖 `PluginManager.RunXxx` 的链式行为、early-exit、对称执行顺序等（`TestCallTool` 起在 `:50`）。
  - `plugin/functioncallmodifier/plugin_test.go`（399 行）— 单元测试 `BeforeModel` / `AfterModel` 的 schema 合并与 state 抽取逻辑。
  - `plugin/functioncallmodifier/integration_test.go`（236 行）— 端到端 `httprr` 回放测试，使用 `gemini-2.5-flash` + 真 `runner.Run`，验证 `skill_id` / `rationale` 被实际写入 session state。
  - `plugin/functioncallmodifier/testdata/` — 3 份 `*.httprr` 回放文件（HTTP 录制，配合 `go test -httprecord=...` 生成）。
  - `plugin/retryandreflect/plugin_test.go`（239 行）— 覆盖 `maxRetries / errorIfRetryExceeded / scope` 三种 option、计数重置、`onToolError` → `afterTool` 的反射响应识别。
  - `plugin/loggingplugin/` 当前没有测试文件（目录下只有 `logging_plugin.go`）。
- Telemetry 埋点：本模块没有显式的 metrics / trace 埋点；`loggingplugin` 的 stdout 打印是"穷人版"可观察性，但属于业务输出而非 OTel span。
- 诊断信息：`loggingplugin.onUserMessage / beforeRun / onEvent / beforeModel / afterModel` 都会打印 `InvocationID / SessionID / UserID / Agent Name` 等关键标识（`logging_plugin.go:121-281`），足以在没有 OTel 的环境里做串联分析。
- 一致性记录器：`internal/configurable/conformance/{recordplugin,replayplugin}` 是 ADK 自身的 plugin（在本目录外），用于录制 / 回放完整 invocation 流，是测试基础设施而非业务 telemetry。

## 11. 文档写作提示

- **必须写**：
  - 解释 plugin 协议与 agent/llmagent 既有 callback 的关系（plugin 回调是 `agent/llmagent` 同名类型的"可选项"实现）。
  - 解释 `PluginManager` 的 early-exit 语义（首个非空结果短路）和 `Name()` 去重（`plugin_manager.go:67-69`）。
  - 解释 11 个回调的触发时机（user message / run start&end / event yield / agent start&end / LLM request&response&error / tool start&end&error），最好配一张时序图。
  - 解释 `closeFunc` 的 nil-safe 默认行为（`plugin.go:69`），避免文档暗示 "必须传 CloseFunc"。
  - 三个内置 plugin 的"示例 vs 可用"定位：用户完全可以 `import` 它们，但要说明它们没有进入任何核心执行路径（`grep` 结果显示生产代码未直接引用）。
  - `retryandreflect` 的两个常量字符串 `response_type = "ERROR_HANDLED_BY_REFLECT_AND_RETRY_PLUGIN"` 是它与 LLM 之间的协议，文档需明确：这是给模型看的"自我提示"，不是给人看的日志。
- **可以省略**：
  - `Plugin` struct 的具体字段顺序（暴露行为即可）。
  - `templateData` 字段列表（与两段 markdown 模板内容重复）。
  - 测试用例细节（除非在讲如何写 plugin）。
- **潜在坑**：
  - **回调返回语义不对称**：`OnUserMessageCallback / BeforeRunCallback / OnEventCallback / Before/AfterAgentCallback / Before/AfterModelCallback` 返回 `*genai.Content` / `*session.Event` / `*model.LLMResponse`；`AfterToolCallback / OnToolErrorCallback / BeforeToolCallback` 返回 `map[string]any`。文档要明确"非空即短路"对每种类型意味着什么。
  - **`BeforeModel` 短路**：`functioncallmodifier` 必须返回 `nil, nil` 而不是空 `*model.LLMResponse{}`，否则会让 runner 误以为模型"已经被处理过"（参见 `functioncallmodifier/plugin.go:87, 118`）。这是常见踩坑点。
  - **`OnToolErrorCallback` 与 `AfterToolCallback` 的执行顺序**：`OnToolError` 先于 `AfterTool` 触发（`runner/runner.go`），且 `retryandreflect` 在 `AfterTool` 里检查 `response_type` 来避免误清计数，文档必须配图。
  - **PluginManager 错误传播**：任意 plugin 返回 err 会立刻终止整个 run 链路（`plugin_manager.go:81-83`），与 Python 端"忽略 plugin 错误"的默认行为不同（若有差异，文档需明确）。
  - **`Plugin` 不可在 `Register` 之后再修改**：所有字段都是 unexported，构造后即冻结；要修改行为只能 `Close` + 重新 `plugin.New`。
  - **`scopedFailureCounters` 内存增长**：`Global` 模式下理论上会无限累积 tool 维度的计数（虽然 `resetFailuresForTool` 会清空），但不会主动释放 scope key；多租户长跑场景下需要外部清理。
  - **`closeFunc` panic-safe 是"双保险"**：因为 plugin manager 关闭时若某个 plugin 的 `Close()` panic，整个 manager 也会被中断。文档可建议 plugin 实现方自行 `recover()`。
