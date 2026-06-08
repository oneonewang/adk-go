# memory 模块阅读笔记

## 1. 一句话定位

`memory` 包是 ADK 中"长期知识（long-term knowledge）"的抽象层，定义了把 `session.Session` 摄入为可检索记忆、并以关键词/向量方式搜索记忆的统一接口，内置进程内的 `InMemoryService` 内存实现与基于 Vertex AI MemoryBank 的远端实现。

## 2. 子包/子目录结构

- 根包 `memory`（`/home/wu/oneone/adk/memory`）：定义 `Service`、`Entry`、`SearchRequest/Response` 等公开类型与基于关键词的进程内实现 `InMemoryService`。
- 子包 `memory/vertexai`（`/home/wu/oneone/adk/memory/vertexai`）：基于 Vertex AI MemoryBank（`aiplatform.MemoryBankClient`）的实现，调用 `GenerateMemories` / `RetrieveMemories` 远端 API 完成摄入与检索。

## 3. 核心类型与接口

- `Service`（`memory/service.go:31-39`）：记忆服务主接口，仅两个方法 `AddSessionToMemory` 与 `SearchMemory`，是整个 ADK 中所有"长期记忆"行为的契约面。
- `InMemoryService()`（`memory/inmemory.go:30-34`）：构造函数，返回实现了 `Service` 的进程内单例；线程安全。
- `SearchRequest`（`memory/service.go:42-46`）：检索请求结构，仅含 `Query`、`UserID`、`AppName` 三个字段。
- `SearchResponse`（`memory/service.go:49-51`）：检索结果包装，承载 `[]Entry`。
- `Entry`（`memory/service.go:54-66`）：单条记忆条目，承载 `genai.Content`、作者、时间戳与自定义元数据；是 LLM 可消费的标准化结果。
- `vertexai.NewService`（`memory/vertexai/vertexai.go:46-58`）：构造远端 MemoryBank 实现，内部委托 `newVertexAIClient` 完成认证与 `MemoryBankClient` 初始化。
- `vertexai.ServiceConfig`（`memory/vertexai/vertexai.go:35-43`）：远端实现的配置容器，嵌入 `vertexaiutil.AgentEngineData` 并提供增量同步用的状态键 `StateKeySessionLastUpdateTime` 和 `WaitForCompletion`。

## 4. 关键数据结构

- `inMemoryService`（`memory/inmemory.go:54-57`）：进程内实现主体。`mu sync.RWMutex` 保护并发读写；`store map[key]map[sessionID][]value` 三级索引，第一层按 `(appName, userID)` 分区，第二层按 session 划分，第三层为该 session 内的事件值数组。
- `key`（`memory/inmemory.go:36-38`）：由 `appName + userID` 组成的复合键，起到多租户隔离的作用。
- `sessionID`（`memory/inmemory.go:40`）：`string` 的命名类型别名，仅用于在 store 中明确区分 session 维度的键。
- `value`（`memory/inmemory.go:42-51`）：内部记忆值结构，对外不可见。`words map[string]struct{}` 字段是预计算的小写词集合（以空格为分隔），用于在 `SearchMemory` 中快速判断词集合交集。
- `vertexAIService`（`memory/vertexai/vertexai.go:28-31`）：远端实现主体，仅持有 `*vertexAIClient` 与状态键名。
- `vertexAIClient`（`memory/vertexai/vertexai_client.go:35-40`）：封装 `aiplatform.MemoryBankClient` 与 `AgentEngineData`，缓存 `parent`（资源路径），减少每次请求的字符串拼接。
- `vertexAIClientConfig`（`memory/vertexai/vertexai_client.go:42-45`）：客户端配置，嵌入 `AgentEngineData` 并带 `waitForCompletion` 标志。
- `Entry` 字段语义（`memory/service.go:54-66`）：`ID` 与原始 `session.Event` 一致；`Content` 是 `*genai.Content`（含 Parts）；`Author` 透传事件作者；`Timestamp` 是事件发生时间而非入库时间；`CustomMetadata` 透传事件级自定义元数据。

## 5. 关键流程

- **摄入（InMemory）**：`AddSessionToMemory` → 遍历 `curSession.Events().All()` → 跳过 `LLMResponse.Content == nil` 的事件 → 对每条文本 Part 调用 `extractWords` 抽取小写词集合（`memory/inmemory.go:59-88`）→ 取 `(appName, userID)` 作为一级 key → 写锁下按 sessionID 覆盖式更新（`memory/inmemory.go:90-106`）。同一 session 多次摄入以"最后一次为准"，没有追加语义。
- **检索（InMemory）**：`SearchMemory` → 对 `req.Query` 抽取词集 → 读锁下读取 `(appName, userID)` 桶（`memory/inmemory.go:109-122`）→ 遍历每个 session、每个 value，调用 `checkMapsIntersect` 判断词集是否相交 → 命中则转 `Entry` 追加（`memory/inmemory.go:124-141`）。
- **摄入（VertexAI）**：`vertexAIService.AddSessionToMemory` 根据 `stateKeySessionLastUpdateTime` 是否为空分流（`memory/vertexai/vertexai.go:63-90`）：空 → `addWholeSession`；非空 → 从 session 状态读取 `time.Time` 后调用 `addEventsNewerThan`。`addSession` 构造 `GenerateMemoriesRequest` 与 `VertexSessionSource`，按 `waitForCompletion` 决定是否阻塞 `op.Wait`（`memory/vertexai/vertexai_client.go:74-104`）。
- **检索（VertexAI）**：`searchMemory` 构造 `RetrieveMemoriesRequest` + `SimilaritySearchParams` + `Parent` + 用户 Scope，调用 `RetrieveMemories` 后将每条 `RetrievedMemory.Fact` 包成 `genai.NewContentFromText(..., RoleUser)` 的 `Entry`（`memory/vertexai/vertexai_client.go:107-134`）。
- **错误吞噬**：`op.Wait` 阶段显式吞掉 `unsupported result type <nil>: <nil>`（`memory/vertexai/vertexai_client.go:95-98`），作为对底层 SDK 当前行为的工作兼容。

## 6. 扩展点

- 任何自定义长期记忆后端只需实现 `memory.Service` 两个方法，ADK 上层（`runner`、工具、`agent`）全部依赖该接口。可在 `cmd/launcher/launcher.go:61` 的 `MemoryService` 字段中注入。
- 远端实现 `vertexai.ServiceConfig` 通过嵌入 `vertexaiutil.AgentEngineData` 暴露 `Project`、`Location`、`AgentEngineID` 等连接参数，并允许外部设置增量同步的 session state 键。
- `loadmemorytool`（`/home/wu/oneone/adk/tool/loadmemorytool`）与 `preloadmemorytool` 工具把"按用户查询记忆"暴露给 LLM，可作为另一类扩展——围绕同一 `Service` 接口增加更多检索工具。
- `extractWords` 与 `checkMapsIntersect` 内部为 unexported，因此检索算法本身对调用方不可定制；如需向量/BM25 等需要替换整个 `Service` 实现。

## 7. 错误处理

- 根包 `memory` 不定义自己的 error 类型或 sentinel。
- `InMemoryService` 的两个方法在当前实现中始终返回 `nil` error，**不会**因 ctx 取消、并发冲突或空内容失败。
- `vertexai.NewService` 通过 `fmt.Errorf("...: %w", err)` 包装底层错误，常见失败点：`newVertexAIClient` 失败（认证、端点不可达），见 `memory/vertexai/vertexai.go:52`。
- `vertexai.AddSessionToMemory` 的典型失败模式：状态键存在但值不是 `time.Time`（`memory/vertexai/vertexai.go:80-83`）；`GenerateMemories` 调用失败；`op.Wait` 返回错误（除已知 `"unsupported result type <nil>: <nil>"` 之外）。
- `vertexai.SearchMemory` 失败模式：远端 `RetrieveMemories` 调用错误（`memory/vertexai/vertexai_client.go:119-121`）；该路径不会做工作兼容吞噬。
- 上层消费方应通过 `errors.Is/As` 识别；本模块未提供自己的错误类型供识别。

## 8. 并发与性能

- `inMemoryService.mu` 使用 `sync.RWMutex`：写入（`AddSessionToMemory`）持写锁，读取（`SearchMemory`）持读锁，写入期间检索会被串行化。
- `AddSessionToMemory` 中"抽取词集合"在持锁前完成，仅在最终写入 store 时才加锁（`memory/inmemory.go:67-106`），降低锁粒度。
- `SearchMemory` 没有读锁外的去重/排序，结果顺序与 `store` 内部 map 迭代顺序相关，测试中通过 `sortMemories` 转换器按时间戳排序（`memory/inmemory_test.go:175-180`）。
- 检索复杂度：O(总事件数) 的词集比较；`checkMapsIntersect` 在比较前会交换使较小 map 作为外层循环，O(min(|m1|,|m2|))（`memory/inmemory.go:143-160`）。
- 已知性能瓶颈：`InMemoryService` 全文未被压缩或裁剪，长期运行下内存随事件数线性增长；`AddSessionToMemory` 会对同一 session **覆盖**已有 entries，但已复制出的 `*genai.Content` 不会被 GC 直到下次覆盖。
- 远端实现的瓶颈在 Vertex AI MemoryBank API 自身，本包内仅在 `WaitForCompletion` 上阻塞等待 LRO。
- 模块内无显式 goroutine、无全局可变状态；`InMemoryService()` 每次返回新实例，未使用 `sync.Once`/全局单例。

## 9. 依赖与被依赖

- 本模块导入：
  - `context`、`time` 标准库
  - `golang.org/x/exp/maps`（仅 `inmemory.go:19`，用于 `maps.Copy`）
  - `google.golang.org/genai`（`Content`、`NewContentFromText` 等）
  - `google.golang.org/adk/session`（`session.Session`）
  - 远端子包导入 `google.golang.org/api/option`、`google.golang.org/protobuf/types/known/timestamppb`
  - `google.golang.org/adk/util/vertexai`（`AgentEngineData`、`SessionResource`、`AgentEngineResource`）
  - `google.golang.org/adk/util/aiplatform`（`HostPortURL`）
  - 第三方 SDK：`cloud.google.com/go/aiplatform/apiv1beta1` 与其 pb 包
- 引用本模块的位置（grep 关键命中）：
  - `agent/agent.go:29, 122`、`agent/callback_context.go:26`、`agent/context.go:22`：把 `memory.Service` 暴露为 `agent.Memory` 视图，供回调中按用户查询。
  - `runner/runner.go:37, 53, 106, 121`：在 `runner.Config` 中暴露 `MemoryService`，并在 `runner` 内部保存该字段。
  - `cmd/launcher/launcher.go:25, 61`：CLI 启动时把配置项装配为 `memory.Service`。
  - `server/adkrest/handler.go:28` 与 `controllers/*`：HTTP/REST 层同样依赖该接口。
  - `tool/loadmemorytool/tool.go:29`、`tool/preloadmemorytool/tool.go:31`、`tool/agenttool/agent_tool.go:32`：把记忆查询/加载包装成 LLM 工具。
  - `examples/tools/loadmemory/main.go:30`：示例。
  - 内部 `internal/configurable/conformance/{replayplugin,recordplugin}/*_test.go`：录制/回放插件的测试，用真实 `memory.Service` 验证事件回放。

## 10. 测试与可观察性

- 测试文件位置：
  - `memory/inmemory_test.go`：`Test_inMemoryService_SearchMemory` 表驱动测试，覆盖"能找到事件"、"不同 appName/user 隔离"、"无匹配"、"空 store" 等场景；通过 `testSession` 桩实现 `session.Session` 接口。
  - 远端 `vertexai` 子包**未提供**单元测试，仅有 `var _ memory.Service = &vertexAIService{}` 编译期断言（`memory/vertexai/vertexai.go:60`）。
- 工具/集成层面的测试在 `tool/loadmemorytool/tool_test.go`、`tool/preloadmemorytool/tool_test.go`、`tool/tool_test.go` 等文件中覆盖到本模块的端到端用法。
- Telemetry：本模块源码中**未出现**对 `internal/telemetry`、`tracer`、`span`、`metric` 的引用，也未使用 `slog`。可观察性靠上层（runner / callback_context）注入；本模块本身无埋点。

## 11. 文档写作提示

- 必须写：根包的核心抽象（`Service` 接口、`Entry`/`SearchRequest`/`SearchResponse`）、两种内置实现（`InMemoryService` 的关键词匹配 vs `vertexai.MemoryBank` 的语义检索）的语义差异、`AddSessionToMemory` 覆盖式语义（同一 session 多次摄入只保留最新快照）。
- 必须写：检索算法是"按空格分词 + 小写化 + 集合交集"——明确**没有**词干化、停用词、模糊匹配或排序；明确返回顺序未定义。远端实现才是基于相似度的语义检索。
- 必须写：VertexAI 实现的 `StateKeySessionLastUpdateTime` 用法：空字符串 → 全量；非空 → 仅消费该 session state 中 `time.Time` 之后的事件，需要消费者在 `BeforeRunCallback` 中维护时间戳。
- 必须写：VertexAI 实现对 LRO 已知错误的兼容（`unsupported result type <nil>: <nil>`），避免使用者误以为是 bug。
- 必须写：并发模型——`InMemoryService` 线程安全且覆盖式写入；词集预计算在锁外。
- 可以省略：`extractWords`、`checkMapsIntersect` 的逐行实现细节；`vertexaiutil` / `aiplatformutil` 的辅助函数实现；`genai.NewContentFromText` 用法。
- 潜在的坑：
  - `Entry.ID` 在 VertexAI 实现下不填（`memory/vertexai/vertexai_client.go:127-131`），仅 `Content/Author/Timestamp` 字段类型不同——上层若依赖 `ID` 做去重，需注意。
  - VertexAI 实现中 `Content.Role` 固定为 `genai.RoleUser`（检索结果"以用户口吻"呈现），与原始事件角色不一致。
  - `InMemoryService` 在 `SearchMemory` 未对 ctx 取消做检查，长查询时无法被抢占。
  - `addEventsNewerThan` 期望 `time.Time`，若 Session 状态中存的是 `*time.Time` 或 RFC3339 字符串，将报错（`memory/vertexai/vertexai.go:80-83`）。
  - 远端实现 `parent` 在 `NewService` 时一次性计算，跨 project/region 切换只能重建 service。
