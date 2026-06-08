# tool 模块阅读笔记

## 1. 一句话定位
`tool` 包是 ADK 中"工具（Tool / Toolset）"的契约层，定义了 agent 可调用的能力单元的统一接口、内置工具集合、Human-in-the-Loop 确认流以及 schema 反射机制；下游子包（functiontool、agenttool、mcptoolset 等）以"实现 `tool.Tool` + `Declaration() + ProcessRequest() + Run()`"的方式插入。

## 2. 子包/子目录结构
- `agenttool/` — 把另一个 agent 包装为可调用工具，支持子 agent 组合（`tool/agenttool/agent_tool.go`）
- `exampletool/` — few-shot 样例注入工具，会在请求 LLM 前把示例拼到 system instruction（`tool/exampletool/tool.go`）
- `exitlooptool/` — 循环 agent 专用的 `exit_loop` 工具，通过 `Actions().Escalate = true` 跳出（`tool/exitlooptool/tool.go`）
- `functiontool/` — 通用函数包装工具：基于反射/JSON-Schema 推断入参出参，支持流式/长时运行/HITL（`tool/functiontool/function.go` + `streaming_function.go`）
- `geminitool/` — 把 `*genai.Tool`（如 GoogleSearch、Retrieval、CodeExecution）适配成 ADK Tool（`tool/geminitool/tool.go` + `google_search.go`）
- `loadartifactstool/` — 列出/加载 artifact 的工具（`tool/loadartifactstool/load_artifacts_tool.go`）
- `loadmemorytool/` — 由模型显式触发的 memory 检索工具（`tool/loadmemorytool/tool.go`）
- `mcptoolset/` — MCP 协议桥：把外部 MCP Server 的 tool 列表转换成 ADK Tool，含自动重连（`tool/mcptoolset/{set,client,tool}.go`）
- `preloadmemorytool/` — 每次 LLM 请求前自动注入 memory 上下文的工具（`tool/preloadmemorytool/tool.go`）
- `skilltoolset/` — 完整子模块，承载"Agent Skills"概念：`skill/`（Source/Frontmatter 解析）+ `internal/skilltool/`（3 个标准工具：list/load/load_skill_resource）（`tool/skilltoolset/toolset.go`）
- `toolconfirmation/` — HITL 确认机制的数据结构与 helper（`tool/toolconfirmation/tool_confirmation.go`）

根目录下另有：
- `tool.go` — 公共接口（`Tool`、`Toolset`、`Predicate`、`ConfirmationProvider`）以及 `WithConfirmation` 装饰器
- `context.go` — 仅保留的 `NewToolContext` 兼容 wrapper（已 Deprecated，指向 `agent.NewToolContext`）
- `tool_test.go` / `context_test.go` — 单元测试

## 3. 核心类型与接口

### 3.1 `Tool` — 最小工具契约
- 位置：`tool/tool.go:38`
- 签名：
  ```go
  type Tool interface {
      Name() string
      Description() string
      IsLongRunning() bool
  }
  ```
- 设计意图：所有"可被 LLM 调用的能力"的最薄抽象；只关心身份与说明，不关心执行。

### 3.2 `Toolset` — 工具集合
- 位置：`tool/tool.go:57`
- 签名：
  ```go
  type Toolset interface {
      Name() string
      Tools(ctx agent.ReadonlyContext) ([]Tool, error)
  }
  ```
- 设计意图：让多个 Tool 聚合成一个可被 agent 加载的单元；`Tools()` 接收 `ReadonlyContext` 以支持按调用态做动态过滤。

### 3.3 `Predicate` / `AllowedToolsPredicate` / `FilterToolset`
- 位置：`tool/tool.go:67`、`tool/tool.go:76`、`tool/tool.go:89`
- 签名：
  ```go
  type Predicate func(ctx agent.ReadonlyContext, tool Tool) bool
  func AllowedToolsPredicate(allowedTools []string) Predicate
  func FilterToolset(toolset Toolset, predicate Predicate) Toolset
  ```
- 设计意图：把"按名字白名单过滤"等通用策略从具体 Toolset 中解耦；`StringPredicate` 已 Deprecated，被 `AllowedToolsPredicate` 取代（`tool/tool.go:71`）。

### 3.4 `ConfirmationProvider` / `WithConfirmation`
- 位置：`tool/tool.go:134`、`tool/tool.go:143`
- 签名：
  ```go
  type ConfirmationProvider func(toolName string, toolInput any) bool
  func WithConfirmation(ts Toolset, requireConfirmation bool, requireConfirmationProvider ConfirmationProvider) Toolset
  ```
- 设计意图：把 HITL 行为做成 Toolset 装饰器；只对实现了内部 `runnableTool`（即同时实现 `Declaration()` + `Run()`）的成员工具生效，动态 provider 优先级高于静态标志。

### 3.5 内部 `runnableTool`（不可导出）
- 位置：`tool/tool.go:189`
- 签名：
  ```go
  type runnableTool interface {
      Tool
      Declaration() *genai.FunctionDeclaration
      Run(ctx agent.ToolContext, args any) (map[string]any, error)
  }
  ```
- 设计意图：标记一个 tool 是"可真正被 LLM 调用"的；对未实现该接口的 tool，`WithConfirmation` 会原样放行（`tool/tool.go:165-176`）。

### 3.6 内部 `FunctionTool` / `StreamingFunctionTool` / `RequestProcessor`
- 位置：`internal/toolinternal/tool.go:28-42`
- 签名：
  ```go
  type FunctionTool interface {
      tool.Tool
      Declaration() *genai.FunctionDeclaration
      Run(ctx agent.ToolContext, args any) (map[string]any, error)
  }
  type StreamingFunctionTool interface {
      tool.Tool
      Declaration() *genai.FunctionDeclaration
      RunStream(ctx agent.ToolContext, args any) iter.Seq2[string, error]
  }
  type RequestProcessor interface {
      ProcessRequest(ctx agent.ToolContext, req *model.LLMRequest) error
  }
  ```
- 设计意图：把这套"附加接口"放在 internal 包，避免污染公共 `tool.Tool` 表面；多个内置工具（`agenttool`、`functiontool`、`loadartifactstool`、`loadmemorytool`、`mcptoolset`、`geminitool`）通过实现这些接口完成 LLM 端 schema 注入与执行。

### 3.7 `skill.Source`
- 位置：`tool/skilltoolset/skill/source.go:41`
- 签名：
  ```go
  type Source interface {
      ListFrontmatters(ctx) ([]*Frontmatter, error)
      ListResources(ctx, name, subpath) ([]string, error)
      LoadFrontmatter(ctx, name) (*Frontmatter, error)
      LoadInstructions(ctx, name) (string, error)
      LoadResource(ctx, name, resourcePath) (io.ReadCloser, error)
  }
  ```
- 设计意图：把"技能文件的物理来源"抽象为 Source；提供 `fileSystemSource`（`tool/skilltoolset/skill/filesystem_source.go`）、`mergedSource`（`tool/skilltoolset/skill/merged_source.go`）、`frontmatterPreloadSource`（`tool/skilltoolset/skill/frontmatter_preload.go`）、`completePreloadSource`（`tool/skilltoolset/skill/complete_preload.go`）四种实现，可按内存/速度权衡选择。

### 3.8 `toolconfirmation.ToolConfirmation`
- 位置：`tool/toolconfirmation/tool_confirmation.go:50`
- 签名：
  ```go
  type ToolConfirmation struct {
      Hint      string `json:"hint"`
      Confirmed bool   `json:"confirmed"`
      Payload   any    `json:"payload"`
  }
  ```
- 设计意图：HITL 状态对象。模块导出常量 `FunctionCallName = "adk_request_confirmation"`（`tool/toolconfirmation/tool_confirmation.go:46`）作为模型在确认流中"伪 function call"的标准名；`OriginalCallFrom()`（`tool/toolconfirmation/tool_confirmation.go:86`）用于从模型返回的伪 call 中拆出真实工具调用。

## 4. 关键数据结构

### 4.1 `tool.Config` (functiontool)
- 位置：`tool/functiontool/function.go:37`
- 字段：`Name`、`Description`、`InputSchema`、`OutputSchema`、`IsLongRunning`、`RequireConfirmation`、`RequireConfirmationProvider`。
- 用途：用户用 `functiontool.New(cfg, fn)` 时的唯一配置载体；schema 可显式提供也可由 `jsonschema-go` 反射推断。

### 4.2 `functionTool[TArgs, TResults any]`
- 位置：`tool/functiontool/function.go:123`
- 字段：`cfg`、`inputSchema *jsonschema.Resolved`、`outputSchema *jsonschema.Resolved`、`handler Func[TArgs, TResults]`、`requireConfirmation`、`requireConfirmationProvider func(TArgs) bool`。
- 用途：函数工具的运行时；泛型保证入参/出参与 schema 一一对应。

### 4.3 `streamingFunctionTool[TArgs any]`
- 位置：`tool/functiontool/streaming_function.go:73`
- 字段：与 4.2 类似但只保留入参 schema，`handler` 是 `StreamingFunc[TArgs] func(agent.ToolContext, TArgs) iter.Seq2[string, error]`。
- 用途：流式输出工具；通过 Go 1.23 的 `iter.Seq2` 把 `RunStream` 暴露为拉序列。

### 4.4 `filteredToolset` / `confirmationToolset` / `confirmationTool`
- 位置：`tool/tool.go:103`、`tool/tool.go:151`、`tool/tool.go:183`
- 字段：
  - `filteredToolset{ toolset, predicate }`
  - `confirmationToolset{ toolset, requireConfirmation, provider }`
  - `confirmationTool{ runnableTool, requireConfirmation, provider }`
- 用途：装饰器模式在公共包内做组合（filtering、HITL 注入）。

### 4.5 `agentTool`
- 位置：`tool/agenttool/agent_tool.go:40`
- 字段：`agent agent.Agent`、`skipSummarization bool`。
- 用途：把子 agent 当成工具调用，调用时 `runner.New` 启动子会话并把文本/JSON 输出回传为 map。

### 4.6 `mcpTool` + `set` + `connectionRefresher`
- 位置：`tool/mcptoolset/tool.go:59`、`tool/mcptoolset/set.go:88`、`tool/mcptoolset/client.go:39`
- 字段：
  - `mcpTool{ name, description, funcDeclaration, mcpClient, requireConfirmation, provider }`
  - `set{ mcpClient, toolFilter, requireConfirmation, provider }`
  - `connectionRefresher{ client, transport, mu, session }`
- 用途：MCP 协议下每个工具一个 `mcpTool`，整个 Toolset 共用一个长连接 `connectionRefresher`。

### 4.7 `exampleTool`、`artifactsTool`、`loadMemoryTool`、`preloadMemoryTool`
- 位置：`tool/exampletool/tool.go:39`、`tool/loadartifactstool/load_artifacts_tool.go:36`、`tool/loadmemorytool/tool.go:36`、`tool/preloadmemorytool/tool.go:44`
- 字段都很小：`name string + description string`，无配置；各自有独立的硬编码参数 schema。

### 4.8 `SkillToolset` + `Config` + `skill.Frontmatter`
- 位置：`tool/skilltoolset/toolset.go:57`、`tool/skilltoolset/toolset.go:48`、`tool/skilltoolset/skill/frontmatter.go:37`
- 字段：
  - `SkillToolset{ name, tools, source, systemInstruction }`，`tools` 固定为 3 个 `functiontool`（list/load/load_skill_resource）。
  - `Config{ Source skill.Source, Name, SystemInstruction }`。
  - `Frontmatter{ Name, Description, License, Compatibility, Metadata, AllowedTools }`（YAML 标签）。
- 用途：把"SKILL.md 文件夹"封装成 3 个工具 + 一段 system instruction。

### 4.9 `completePreloadSkillData`
- 位置：`tool/skilltoolset/skill/complete_preload.go:30`
- 字段：`frontmatter`、`instructions`、`resources map[string][]byte`、`sortedResourcePaths []string`。
- 用途：把每个 skill 全量预读到内存后的缓存行，二分搜索加速 `ListResources(prefix)`。

## 5. 关键流程

### 5.1 把一个工具注册到 LLM 请求（"PackTool" 流程）
- 入口：任意实现了 `Declaration() *genai.FunctionDeclaration` 的工具在 `ProcessRequest(ctx, req)` 中调用 `toolutils.PackTool(req, self)`，例如 `functionTool.ProcessRequest`（`tool/functiontool/function.go:155`）、`agentTool.ProcessRequest`（`tool/agenttool/agent_tool.go:254`）、`mcpTool.ProcessRequest`（`tool/mcptoolset/tool.go:86`）、`artifactsTool.ProcessRequest`（`tool/loadartifactstool/load_artifacts_tool.go:120`）、`loadMemoryTool.ProcessRequest`（`tool/loadmemorytool/tool.go:112`）。
- 关键步骤：
  1. `req.Tools` 是 `map[string]any`，按 `Name` 注册 tool 引用，若重名返回 `duplicate tool` 错误（`internal/toolinternal/toolutils/toolutils.go:42`）。
  2. 找到请求里第一个 `genai.Tool{ FunctionDeclarations != nil }`，没有则新建。
  3. `Declaration()` 追加到该 `genai.Tool` 的 `FunctionDeclarations` 切片中。
- 出口：同一个 LLM 请求里所有 function tool 的 schema 被合并到同一个 `genai.Tool` 中。

### 5.2 函数工具的执行（含 HITL）
- 入口：`functionTool.Run(ctx, args)`（`tool/functiontool/function.go:185`）。
- 关键步骤：
  1. 用 `recover()` 捕获 panic 并包装为带 stack 的 error（`function.go:187-191`）。
  2. `args.(map[string]any)` 强转；失败返回 `unexpected args type`。
  3. `typeutil.ConvertToWithJSONSchema[map[string]any, TArgs](m, inputSchema)` 把原始 JSON 转成强类型 `TArgs`（`function.go:197`）。
  4. 检查 `ctx.ToolConfirmation()`：若 `Confirmed==false` 返回 `ErrConfirmationRejected`；若为 `nil` 则计算 `requireConfirmation`（provider 优先），如需确认则 `ctx.RequestConfirmation(...)` 并设 `Actions().SkipSummarization = true`，返回 `ErrConfirmationRequired`（`function.go:202-225`）。
  5. `f.handler(ctx, input)` 执行用户函数；输出优先按 `OutputSchema` 转换；不匹配 schema 但 schema 为 `nil` 时退化为 `{"result": output}`（`function.go:227-246`）。
- 出口：返回 `map[string]any` 与 error；`ErrConfirmationRequired/Rejected` 由上层 `runner` 解析为 HITL 事件。

### 5.3 装饰 Toolset 过滤（FilterToolset）
- 入口：`tool.FilterToolset(ts, predicate)`（`tool/tool.go:89`）。
- 关键步骤：调用 `ts.Tools(ctx)` 拿到全部 `Tool`，再用 `predicate(ctx, tool)` 过滤；不修改原 Toolset。
- 出口：返回 `filteredToolset`，其 `Name()` 透传（`tool/tool.go:108-110`）。

### 5.4 HITL 装饰（WithConfirmation）
- 入口：`tool.WithConfirmation(ts, require, provider)`（`tool/tool.go:143`）。
- 关键步骤：
  1. 调用 `ts.Tools(ctx)` 拿全量工具。
  2. 对每个 `t`，若实现 `runnableTool`，用 `confirmationTool` 包裹（`tool/tool.go:165-171`）；否则原样放回。
  3. `confirmationTool.Run`（`tool/tool.go:203`）逻辑与 5.2 第 4 步一致：已确认则执行，否则询问。
- 出口：所有可执行工具都被注入 HITL 检查；不可执行工具（例如 geminitool 的 `geminiTool`）原样保留。

### 5.5 MCP 工具集（mcptoolset）
- 入口：`mcptoolset.New(cfg)`（`tool/mcptoolset/set.go:49`）。
- 关键步骤：
  1. `set.Tools(ctx)` 调 `mcpClient.ListTools(ctx)`，该方法内部走 `connectionRefresher` 的 `withRetry`（`tool/mcptoolset/client.go:78`）。
  2. `connectionRefresher` 单 session 复用，遇到 `mcp.ErrConnectionClosed / ErrSessionMissing / io.EOF / io.ErrClosedPipe` 等"可恢复"错误时 `refreshConnection` 重新 `client.Connect`（`tool/mcptoolset/client.go:114`、`client.go:165`）。
  3. 拿到 MCP tools 列表后用 `convertTool` 包装为 `mcpTool`（`tool/mcptoolset/tool.go:32`），可选 `toolFilter` 再过滤。
  4. `mcpTool.Run` 走与 functiontool 相同的 HITL 检查，调用 `mcpClient.CallTool(ctx, &mcp.CallToolParams{Name, Arguments})`（`tool/mcptoolset/tool.go:94`）。
  5. `CallTool` 也通过 `withRetry` 失败时重连一次（`tool/mcptoolset/client.go:68`）。
- 出口：每个 MCP 工具作为一个 ADK `Tool` 暴露给 LLM；整个 toolset 的连接是 lazy + 自动重连的。

### 5.6 加载 artifacts（loadartifactstool）
- 入口：`artifactsTool.Run(ctx, args)`（`tool/loadartifactstool/load_artifacts_tool.go:88`）。
- 关键步骤：
  1. `ProcessRequest` 先 `PackTool`，再 `appendInitialInstructions` 把当前 artifact 文件名列到 system instruction（`load_artifacts_tool.go:130`）。
  2. `processLoadArtifactsFunctionCall` 检查 `req.Contents` 最后一条是否含有 `load_artifacts` 的 `FunctionResponse`（`load_artifacts_tool.go:154-178`）。
  3. 若是，用 `errgroup` 并发调 `artifactsService.Load(...)` 加载每个 artifact（`load_artifacts_tool.go:184-198`）。
  4. 把每个 artifact 包成 `genai.Content{Parts: [text + part]}` 追加到 `req.Contents`。
- 出口：artifact 内容并入后续 LLM 调用。

### 5.7 加载 memory（loadmemorytool / preloadmemorytool）
- loadmemorytool：模型显式调用 `load_memory(query)`；`Run` 中调 `ctx.SearchMemory(...)` 并把 `Memories` 字段返回（`tool/loadmemorytool/tool.go:83-108`）；`ProcessRequest` 追加 `memoryInstructions`（`tool/loadmemorytool/tool.go:112-117`）。
- preloadmemorytool：模型不可见；`ProcessRequest` 自动用 `ctx.UserContent().Parts[0].Text` 作 query 调 `SearchMemory`（`tool/preloadmemorytool/tool.go:74-98`），把结果格式化为 `<PAST_CONVERSATIONS>...</PAST_CONVERSATIONS>` 追加到 system instruction。

### 5.8 Skill Toolset（三工具 + 一次注入）
- 入口：`skilltoolset.New(ctx, cfg)`（`tool/skilltoolset/toolset.go:65`）。
- 关键步骤：
  1. 用 `skilltool.ListSkills / LoadSkill / LoadSkillResource` 三个 factory（基于 `functiontool.New`）构造三个 Tool（`toolset.go:77-88`）。
  2. `SkillToolset.ProcessRequest` 在请求 LLM 前 `source.ListFrontmatters(ctx)`，把 frontmatter 列表 XML 化（`skilltool.SkillsToXML`，`tool/skilltoolset/internal/skilltool/list_skills.go:56`），并附加 `systemInstruction`（默认是告知模型先 `load_skill` 的说明，`toolset.go:31-45`）。
  3. 三个工具的 `Run` 各自走 `Source` 接口；其中 `LoadSkillResource` 用 `io.LimitReader` 限制 10 MiB（`tool/skilltoolset/internal/skilltool/load_skill_resource.go:69`）。
- 出口：模型可以列出→加载→取资源，三步完成对 SKILL.md 的探索。

### 5.9 触发循环退出（exitlooptool）
- 入口：`exitlooptool.New()`（`tool/exitlooptool/tool.go:33`）。
- 关键步骤：内部实现 `func exitLoop(ctx, myArgs struct{}) (map[string]string, error)` 设置 `ctx.Actions().Escalate = true` 与 `SkipSummarization = true` 后返回空 map（`exitlooptool/tool.go:26`）。
- 出口：被 `LoopAgent` 检测到 Escalate 后跳出循环。

### 5.10 Gemini 原生工具（geminitool）
- 入口：`geminitool.New(name, desc, *genai.Tool)`（`tool/geminitool/tool.go:43`）或预置 `geminitool.GoogleSearch{}`（`tool/geminitool/google_search.go:28`）。
- 关键步骤：`ProcessRequest` 调 `setTool(req, t)`，直接把 `*genai.Tool` 追加到 `req.Config.Tools`（`tool/geminitool/tool.go:78`）。
- 出口：与 function calling 走的 schema 注入路径不同——`*genai.Tool` 整段透传给 Gemini。

## 6. 扩展点
- 实现 `tool.Tool` 即可注册一个新工具：把 `Name/Description/IsLongRunning` 写好后，单独实现 `Declaration()` 即可被 LLM 识别；额外实现 `Run` 才能被实际调用。
- 实现 `tool.Toolset` 可一次提供多个工具；`Tools(ReadonlyContext)` 允许按调用态动态筛选。
- `Predicate` 拦截器 + `FilterToolset` 可对任何 Toolset 做白/黑名单。
- `WithConfirmation(ts, require, provider)` 给整个 Toolset 加 HITL；也支持逐工具 `functiontool.Config.RequireConfirmation` / `RequireConfirmationProvider` / `mcptoolset.Config` 同名字段。
- `skill.Source` 是 skill 系统的最大扩展点：自带 `fileSystemSource`（`tool/skilltoolset/skill/filesystem_source.go`），可实现网络/GCS 来源；`WithFrontmatterPreloadSource` / `WithCompletePreloadSource` 是装饰器，把冷 IO 转为热内存。
- `MCPClient` 接口（`tool/mcptoolset/client.go:31`）允许注入自定义 MCP client（`Config.Client`）；`mcp.Transport` 字段允许选 stdin/stdout、HTTP、SSE 等任何 MCP 传输。
- `toolconfirmation.OriginalCallFrom` 提供"模型把真实调用伪装成 `adk_request_confirmation`"事件的解码能力，前端可重写这一段协议。

## 7. 错误处理
- `ErrInvalidArgument`（`tool/functiontool/function.go:75`）：函数工具入参类型非 struct/map 时的构造错误。
- `ErrConfirmationRequired` / `ErrConfirmationRejected`（`tool/tool.go:32-35`）：HITL 流式控制的两个 sentinel，函数工具、MCP 工具、装饰器统一返回。
- `toolconfirmation.FunctionCallName = "adk_request_confirmation"`（`tool/toolconfirmation/tool_confirmation.go:46`）：HITL 协议用伪 function name，配套 `OriginalCallFrom` 解析。
- skill 错误（`tool/skilltoolset/skill/source.go:24-31`）：`ErrInvalidSkillName / ErrInvalidFrontmatter / ErrSkillNotFound / ErrDuplicateSkill / ErrInvalidResourcePath / ErrResourceNotFound`，`mergedSource` 显式 `errors.Is` 它们做"未找到→继续下一 source"逻辑。
- `loadartifactstool.loadIndividualArtifact` 失败用 `errgroup.Wait()` 立即返回（`tool/loadartifactstool/load_artifacts_tool.go:200`）；不会部分提交。
- `connectionRefresher` 把 `mcp.ErrConnectionClosed / ErrSessionMissing / io.EOF / io.ErrClosedPipe` 视作可重连（`tool/mcptoolset/client.go:48`），其它错误直接透传。
- `functionTool.Run` 用 `recover()` 兜住 handler 内部 panic，把 stack 一并返回（`tool/functiontool/function.go:187-191`）；`streamingFunctionTool` 同样处理（`tool/functiontool/streaming_function.go:132-136`）。
- `agentTool.Run` 把子 agent 任意 `ErrorCode/ErrorMessage` 转成普通 error 返回（`tool/agenttool/agent_tool.go:210-211`）。

## 8. 并发与性能
- `connectionRefresher` 用 `sync.Mutex` 保护 session（`tool/mcptoolset/client.go:43`）；`getSession` / `refreshConnection` 串行化；`refreshConnection` 会先 `Ping` 验证（`client.go:172`），避免多个 goroutine 同时重连。
- `frontmatterPreloadSource` / `completePreloadSource` 用 `sync.RWMutex`（`tool/skilltoolset/skill/frontmatter_preload.go:27`、`tool/skilltoolset/skill/complete_preload.go:42`），`reload` 时短暂持写锁；读路径完全在锁内。
- `completePreloadSource` 预读所有资源到 `map[string][]byte`，用 `slices.Sort`+`slices.BinarySearch` 加速 `ListResources` 前缀查询（`tool/skilltoolset/skill/complete_preload.go:127`）。
- `loadartifactstool.processLoadArtifactsFunctionCall` 用 `errgroup` 并发加载多个 artifact，受 `ctx` 取消控制（`tool/loadartifactstool/load_artifacts_tool.go:185-198`）。
- 性能瓶颈：
  - `functiontool.New` 启动时反射生成 `jsonschema.Resolved`（`tool/functiontool/function.go:267`），工具很多时构造期偏重，可缓存。
  - `agentTool` 每次调用都会 `runner.New` + 创建子 session（`tool/agenttool/agent_tool.go:170-198`），开销较大；不适合高频调用。
  - `mcptoolset.ListTools` 在重连后必须 `cursor=""` 重新分页（MCP 规范不允许跨会话 cursor），大工具集下可能放大开销（`tool/mcptoolset/client.go:90-99`）。
  - `completePreloadSource` 把所有资源读入内存，10 MiB 单文件上限（`tool/skilltoolset/skill/complete_preload.go:28`），技能库大时内存压力高。
- 调优点：可选择 `frontmatterPreloadSource`（仅元数据）替代 `completePreloadSource`（全量），按访问模式权衡。

## 9. 依赖与被依赖

### 9.1 本模块导入
- 公共依赖：`google.golang.org/genai`（几乎所有文件）、`google.golang.org/adk/agent`（上下文）、`google.golang.org/adk/model`（LLMRequest）、`google.golang.org/adk/internal/toolinternal/toolutils`、`google.golang.org/adk/internal/typeutil`、`google.golang.org/adk/internal/utils`、`google.golang.org/adk/internal/llminternal`（agenttool 用）、`google.golang.org/adk/internal/converters`（toolconfirmation 用）。
- 第三方依赖：
  - `github.com/google/jsonschema-go/jsonschema`（functiontool 反射）
  - `github.com/modelcontextprotocol/go-sdk/mcp`（mcptoolset）
  - `golang.org/x/sync/errgroup`（loadartifactstool 并发）
  - `gopkg.in/yaml.v3`（skill Frontmatter 解析）
  - `html`、`bufio` 标准库。

### 9.2 反向引用（本模块被谁导入）
- 框架核心：`agent`（`agent/context.go`、`agent/callback_context.go`），`llmagent` 通过 `agent` 间接使用。
- 插件层：`plugin/functioncallmodifier`、`plugin/loggingplugin`、`plugin/plugin_manager_test.go`、`plugin/retryandreflect`。
- 示例：`examples/{quickstart,telemetry,rest,web,bidi,skills,tools/*,mcp,toolconfirmation,vertexai/*,a2a,agentengine}` 大量引用。
- workflow agents 的测试：`agent/workflowagents/{parallelagent,loopagent}/agent_test.go`、远程 agent 的 `a2a_e2e_test.go`。
- `session/session.go` 也用到了 `tool` 相关类型。

## 10. 测试与可观察性
- 测试文件：根目录 `tool/tool_test.go`（约 226 行）、`tool/context_test.go`（约 109 行）；每个子目录都自带 `*_test.go`，如：
  - `tool/agenttool/agent_tool_test.go`
  - `tool/functiontool/function_test.go`（约 922 行，最厚）+ `long_running_function_test.go`（流式/长时运行）
  - `tool/loadartifactstool/load_artifacts_tool_test.go`
  - `tool/mcptoolset/set_test.go`（约 740 行，含 testdata 协议样本）
  - `tool/skilltoolset/{toolset_test.go, internal/skilltool/tools_test.go, skill/*_test.go}`（含合并源/文件系统源/preload 的完整单测）
- 测试数据：`tool/mcptoolset/testdata/`、`tool/functiontool/testdata/`，分别用于 MCP JSON-RPC 录制和函数工具 schema 样本。
- 可观察性：模块本身不直接发 telemetry；埋点集中在 `agent` 与 `runner` 层（如 examples/telemetry 演示）。`telemetry` 字段（如 `tool_call` span）由 `runner`/`agent` 在调用 `ProcessRequest` 与 `Run` 的前后生成。

## 11. 文档写作提示
- 重点要写：把"工具 = 一个可命名、可声明 schema、可执行的 Go 对象"这个心智模型讲清楚；说明 `Tool`（最小）/`FunctionTool`（可执行）/`RequestProcessor`（注入）/`StreamingFunctionTool`（流式）四层接口如何组合。
- 必写：HITL 装饰器 `WithConfirmation` 与 `toolconfirmation` 的关系（`ErrConfirmationRequired/Rejected`、`FunctionCallName` 常量）；`Predicate`/`FilterToolset`/`AllowedToolsPredicate` 的标准用法。
- 推荐覆盖：每个子包给一个 2-3 句的"用途 + 关键入口"小节。`functiontool` 的反射 schema 推断需要强调"入参必须是 struct/map 指针"这一硬约束（`tool/functiontool/function.go:88`）。
- 易踩的坑：
  - `functiontool.Config.RequireConfirmationProvider` 类型是 `any`，实际签名必须 `func(TArgs) bool`，否则 `New` 时直接报错（`function.go:103-110`）。
  - `mcptoolset` 装饰过的 tool 也会再被 `WithConfirmation` 包装——HITL 顺序会影响行为。
  - `agentTool` 内部 `runner.New` 每次新建子 session，会话状态与父 agent 不共享（除了显式拷贝的非 `_adk` 前缀 state，`agent_tool.go:184-189`）。
  - `loadartifactstool.processLoadArtifactsFunctionCall` 仅检查 `req.Contents` 最后一条的 `FunctionResponse`，对中间插入的响应不处理。
  - `skilltoolset` 默认 system instruction 强制模型先 `load_skill`，如果用户覆盖 `SystemInstruction` 要自己保留该约束。
  - `completePreloadSource.ListResources` 用前缀匹配时要求 `subpath` 不以 `/` 结尾（`skill/complete_preload.go:118-122`），与文件系统源语义略有差异。
  - `toolconfirmation.OriginalCallFrom` 同时支持 `*genai.FunctionCall` 和 `map[string]any` 两种 args 形式（`tool_confirmation.go:97-110`），文档里要提醒前端两种 JSON 都能命中。
  - `tool.Context` 已 Deprecated，新代码直接用 `agent.ToolContext`（`tool/tool.go:48-53`）。
- 建议省略：每个子包测试细节；MCP `connectionRefresher` 内部锁顺序；`completePreloadSource` 二分搜索实现细节——除非读者关心性能。
