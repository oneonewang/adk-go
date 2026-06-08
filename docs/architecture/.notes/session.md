# session 模块阅读笔记

> 锁定 commit: `d06992e2b1ec2c9b95c6070e0fd12d50a43e4c99`
> 模块路径: `/home/wu/oneone/adk/session/`

## 1. 一句话定位

`session` 包是 ADK 的"会话与状态"层 — 定义一次 agent↔user 交互中所有可被持久化、可被回放的内存结构（`Session` / `State` / `Events` / `Event`），并对外提供 `Service` 抽象以支持 in-memory、关系型数据库 (GORM)、Vertex AI Agent Engine 三种可替换存储后端。

## 2. 子包/子目录结构

| 子目录 | 一行说明 |
| --- | --- |
| `session/` | 根包：核心接口（`Session` / `Service` / `State` / `Events` / `Event`）+ InMemory 实现 |
| `session/database/` | 基于 GORM 的关系型数据库实现（Postgres / MySQL / Spanner / SQLite），自带 `stateMap` / `dynamicJSON` 自定义列类型 |
| `session/vertexai/` | 基于 Vertex AI Agent Engine ReasoningEngine 的 gRPC 远程实现，使用 LRO（Long-Running Operation）做异步等待 |
| `session/session_test/` | 跨实现的"测试套件" — `RunServiceTests` 暴露 `Snapshot` 与 `SuiteOptions`，所有后端都跑同一套用例 |

## 3. 核心类型与接口

| 类型 | 位置 (file:line) | 签名 / 关键方法 | 设计意图 |
| --- | --- | --- | --- |
| `Service` | `session/service.go:25` | `Create/Get/List/Delete/AppendEvent` 五方法 | 统一的会话存储抽象，所有后端必须实现；`AppendEvent` 兼具事件追加 + 临时状态清理语义 |
| `Session` | `session/session.go:32` | `ID/AppName/UserID/State/Events/LastUpdateTime` | 一次用户对话的所有可观测数据；`State()` 与 `Events()` 暴露只读/可写视图 |
| `State` / `ReadonlyState` | `session/session.go:51`, `session.go:67` | `Get/Set/All`（读写）/ `Get/All`（只读） | 通用 K-V 存储；用 Go 1.23 `iter.Seq2` 流式遍历，避免一次性物化 |
| `Events` | `session/session.go:79` | `All/Len/At(i)` | 有序事件列表，强调顺序回放；底层是 `[]*Event` |
| `Event` | `session/session.go:92` | 内嵌 `model.LLMResponse` + `ID/Timestamp/InvocationID/Branch/Author/Actions/LongRunningToolIDs` | 单次"消息"或"工具调用"的完整快照；`Actions` 承载状态 delta、artifact delta、transfer/escalate 等控制动作 |
| `EventActions` | `session/session.go:143` | `StateDelta / ArtifactDelta / RequestedToolConfirmations / SkipSummarization / TransferToAgent / Escalate` | 描述"事件对会话的副作用"，是状态机推进的载体 |
| `keyPrefix*` 常量 | `session/session.go:163-176` | `KeyPrefixApp / KeyPrefixUser / KeyPrefixTemp` | 状态键命名空间：app 共享、user 私有、temp 临时 |
| `ErrStateKeyNotExist` | `session/session.go:179` | `var ... errors.New(...)` | `State.Get` 缺失键的统一错误信号 |

## 4. 关键数据结构

| 结构 | 位置 | 字段含义 |
| --- | --- | --- |
| `id` | `session/inmemory.go:299` | 三元组 `appName / userID / sessionID`；用 `ordered.Encode` 序列化为定长字典序键，便于 `omap.Map` 范围扫描 |
| `inMemoryService` | `session/inmemory.go:39` | `mu sync.RWMutex` + `sessions omap.Map`（按 ID 排序的有序 map）+ `userState` / `appState` 两张二级表；线程安全 |
| `session`（私有） | `session/inmemory.go:305` | 内部 `Session` 实现；自带 `mu` 保护 `events` / `state` / `updatedAt` |
| `events []*Event` | `session/inmemory.go:365` | 用 `iter.Seq` 暴露，复制时只复制切片头，调用方应自行加锁 |
| `state` | `session/inmemory.go:388` | K-V 视图；`All()` 复制 map 后释放锁，避免持锁迭代 |
| `localSession` (database) | `session/database/session.go:29` | 与 in-memory 的 `session` 几乎逐字段相同的"本地缓存"（注释里写明 TODO 期望合并到 sessioninternal） |
| `storageSession` | `session/database/storage_session.go:29` | 持久化表行，`AppName/UserID/ID` 复合主键；`State stateMap` + `Events []storageEvent` Has-Many |
| `storageEvent` | `session/database/storage_session.go:70` | 单事件行；`Actions []byte`（整体 JSON 化）+ 多个 `dynamicJSON` 列存 LLM 响应的可选部分 |
| `storageAppState` / `storageUserState` | `session/database/storage_session.go:289`, `301` | 全局与用户级 state 表；按 app / user 维度拆分持久化 |
| `vertexAiClient` | `session/vertexai/vertexai_client.go:41` | 包装 `aiplatform.SessionClient` + `vertexaiutil.AgentEngineData` 元数据 |
| `vertexAiService` | `session/vertexai/vertexai.go:28` | 实现 `Service`；`Get` 中用 `errgroup` 并发取 session + events |

## 5. 关键流程

### 5.1 Create 流程（in-memory）
入口 `inMemoryService.Create` @ `session/inmemory.go:46`
- 校验 `AppName/UserID` 非空；`SessionID` 空则用 `uuid.NewString()`
- 用 `omap.Map.Get` 查重，已存在则返回 `"session X already exists"`
- `sessionutils.ExtractStateDeltas` 把 `req.State` 按 `app:` / `user:` / 其它拆三份
- `updateAppState` / `updateUserState` 把前缀状态写到二级表，`MergeStates` 合并后赋给 session
- 返回深拷贝后的 `*session`（防外部改写）作为 `CreateResponse.Session`

### 5.2 Get 流程（in-memory）
入口 `inMemoryService.Get` @ `session/inmemory.go:95`
- 校验三键；`RLock` 查 omap
- `copySessionWithoutStateAndEvents` 复制外壳
- `mergeStates` 合并 app/user/session 三层状态
- 应用两个过滤：`NumRecentEvents`（尾部裁剪）、`After`（`sort.Search` 二分定位，因 `Events` 按时间升序）
- 复制 events 切片

### 5.3 AppendEvent 流程（所有实现）
共性步骤：
- 拒绝 `event.Partial == true`（`session/inmemory.go:204`、`session/database/service.go:327`、`session/vertexai/vertexai.go:130`）
- 调内部 `appendEvent`：先 `updateSessionState`（merge StateDelta 到 session.state），再 `trimTempDeltaState` 滤掉 `temp:` 前缀键，最后 append 到 `events` 数组
- 持久化层（in-memory 更新 omap + app/user 二级表；database 跑 GORM 事务；vertexai 调 `AppendEvent` RPC + 写本地缓存）

DB 实现特别点：会在事务内比对 `storageSess.UpdateTime.UnixMicro()` 与本地 `updatedAt`，**防止 stale 写入**（`session/database/service.go:374-382`）。事件 `Timestamp` 也会被 `Truncate(time.Microsecond)` 避免不同 DB 精度差异（`session/database/service.go:332`）。

### 5.4 List 流程
- in-memory (`session/inmemory.go:141`)：用 `omap.Map.Scan(lo, hi)` 区间扫；lo/hi 由 `id.Encode()` 编码后追加 `\x00` 实现字典序上下界
- database (`session/database/service.go:222`)：按 `appName` 过滤，可选 `userID`；`ErrRecordNotFound` 视为"空"返回，其它错误才向上抛
- vertexai (`session/vertexai/vertexai.go:107`)：走 `aiplatform.ListSessions` 迭代器；`UserID` 过滤翻译为 `Filter` 字符串

### 5.5 Delete 流程
- in-memory / database：直接按复合键 `Delete`
- vertexai：调用 `DeleteSession` LRO 并 `Wait(ctx)`

### 5.6 State 命名空间拆分（共享点）
`sessionutils.ExtractStateDeltas` (`/home/wu/oneone/adk/internal/sessionutils/utils.go:26`) 是状态拆分唯一来源：把 `app:` 写到 app 表、`user:` 写到 user 表、其余写到 session 内；`temp:` 直接丢弃。`MergeStates` 反向操作，给 app/user 状态加回前缀，与 session 状态合并成对外视图。

## 6. 扩展点

- **替换存储后端**：实现 `session.Service` 五方法即可。最简模板是 `inMemoryService`（`session/inmemory.go:39`）。
- **自定义 state / events 视图**：`State` / `Events` 是接口，可被装饰（例如加入压缩、加密、自定义索引）。
- **GORM Dialector**：`database.NewSessionService` 接受任意 `gorm.Dialector`，已支持 postgres / mysql / spanner / sqlite（在 `gorm_datatypes.go` 中按方言返回 `JSONB / LONGTEXT / STRING(MAX)`）。
- **Vertex AI 资源命名**：`getReasoningEngineID` (`session/vertexai/vertexai_client.go:420`) 接受三种输入：直接 numeric ID、完整 `projects/.../reasoningEngines/N` 路径、或事先在 `VertexAIServiceConfig.ReasoningEngine` 配置好。
- **Tool 端运行长任务标识**：`Event.LongRunningToolIDs` 字段可由 agent 调用方填充，配合 `IsFinalResponse()` 跳过 summarization。
- **动作控制**：`EventActions.TransferToAgent` / `Escalate` / `SkipSummarization` 是 agent 流程控制的钩子，详见 `session/session.go:143-160`。
- **Test harness**：`session_test.RunServiceTests` (`session/session_test/service_suite.go:76`) + `SuiteOptions` 让任何新后端可零成本接入"行为契约"测试集。

## 7. 错误处理

- `ErrStateKeyNotExist` (`session/session.go:179`)：状态键缺失的统一哨兵错误。In-memory 与 database、vertexai 三处的 `state.Get` 都返回它。
- 参数校验错误：所有 `Service` 方法都先做 `app_name/user_id/session_id` 非空检查，返回 `fmt.Errorf("... are required, got ...: %q", ...)`。
- 重复创建：`inMemoryService.Create` 返回 `"session X already exists"`；`database.Create` 抛 GORM 唯一约束错误；`vertexai.Create` 不允许用户给 `SessionID` (`session/vertexai/vertexai.go:60`)。
- 找不到：`inMemoryService.Get` 返回 `"session %+v not found"`；`database.Get` 把 `gorm.ErrRecordNotFound` 包装后返回（**注意**：不像 in-memory，这里不区分"业务不存在"与"系统错误"）。
- stale 写入：`database.applyEvent` (`session/database/service.go:374`) 用 `UpdateTime` 做乐观锁；冲突返回 `"stale session error: ..."`。
- LRO 超时：`vertexAiClient.waitForOperation` (`session/vertexai/vertexai_client.go:116`) 最多 10 次重试，baseDelay 1s / maxDelay 5s；超过返回 `"LRO '%s' timed out after %d retries"`。
- LRO 阶段映射：`sessionIDByOperationName` (`session/vertexai/vertexai_client.go:375`) 对 Vertex 返回的 `/sessions/X/operations/Y` 长名做严格切片校验，避免越界 panic。

## 8. 并发与性能

- **in-memory** 用 `sync.RWMutex`：
  - `inMemoryService.mu` 保护 `sessions` / `appState` / `userState`（`session/inmemory.go:40`）
  - 每个 `session` 内还有独立 `sync.RWMutex`（`session/inmemory.go:309`），让不同 session 的事件写入可并行
  - `state.All()` 先 `RLock` → `maps.Clone` → 释放锁 → 再迭代（`session/inmemory.go:405-418`），避免持锁遍历
- **`omap.Map` 有序** + `Scan(lo, hi)` 范围查询使得 List 在 in-memory 后端是 O(log n + k) 而不是 O(n)（`session/inmemory.go:160`）。
- **vertexai.Get 并发拉取**：`errgroup` 同时拉 session 详情与 events 列表（`session/vertexai/vertexai.go:75-103`）。
- **GORM `PrepareStmt: true`** 在 `database/service_test.go:38` 等使用例里开启，可缓存 prepared statement。
- **DB 写入单事务**：`database.Create` / `AppendEvent` 都用 `s.db.WithContext(ctx).Transaction(...)` 包住 app/user/session 多表更新（`session/database/service.go:97, 360`）。
- 已知瓶颈点：in-memory 端所有读返回的 events 都是新切片（`make + append`），长会话 + `NumRecentEvents` 过滤时分配可观；DB 端 `Get` 用 `timestamp DESC` + `LIMIT` 倒着取再翻转，依赖 SQL 层支持 `ORDER BY ... LIMIT` 优化器下推。

## 9. 依赖与被依赖

### 9.1 session 导入的其它模块（用 grep）

| 依赖 | 出现在 |
| --- | --- |
| `google.golang.org/adk/model` | `session/session.go:24`（`Event` 内嵌 `model.LLMResponse`） |
| `google.golang.org/adk/tool/toolconfirmation` | `session/session.go:25`（`EventActions.RequestedToolConfirmations`） |
| `google.golang.org/adk/internal/sessionutils` | `session/inmemory.go:32`（状态拆分/合并） |
| `google.golang.org/adk/util/vertexai` | `session/vertexai/vertexai_client.go:35`（资源名构造） |
| `cloud.google.com/go/aiplatform/apiv1beta1` | `session/vertexai/vertexai_client.go:37`（gRPC 客户端） |
| `gorm.io/gorm` + `gorm.io/gorm/schema` | `session/database/service.go:26` / `session/database/gorm_datatypes.go:24` |
| `github.com/glebarez/sqlite` | 测试 `session/database/service_test.go:20` |
| `rsc.io/omap` + `rsc.io/ordered` | `session/inmemory.go:29-30`（有序 map + 定长编码） |
| `github.com/google/uuid` | 多数文件中 |
| `golang.org/x/sync/errgroup` | `session/vertexai/vertexai.go:21` |
| `google.golang.org/api/iterator` / `option` | `session/vertexai/vertexai_client.go:25-26` |
| `google.golang.org/genai` | `session/database/storage_session.go:22` |
| `cloud.google.com/go/rpcreplay` | 测试 `session/vertexai/service_test.go:25` |

### 9.2 反向引用：哪些模块导入 session

按层次分组（仅列代表性引用，详细列表已 grep）：

- **核心执行/上下文**：`agent/agent.go`、`agent/context.go`、`agent/callback_context.go`、`internal/context/invocation_context.go`、`internal/context/readonly_context.go`、`internal/context/callback_context.go`
- **LLM 流程**：`internal/llminternal/*`（`base_flow.go`、`functions.go`、`agent_transfer.go`、`contents_processor.go`、`instruction_processor.go`、`identity_request_processor.go`、`request_confirmation_processor.go`、`tools_processor.go`、`outputschema_processor.go`、`basic_processor.go`、`file_uploads_processor.go`、`audio_cache_manager.go`、`other_processors.go` 等共 15+ 个文件）
- **memory**：`memory/service.go`、`memory/inmemory.go`、`memory/vertexai/*`、`internal/memory/memory.go`
- **server / launcher**：`cmd/launcher/launcher.go`、`cmd/launcher/console/console.go`、`cmd/launcher/web/web.go`、`cmd/launcher/web/a2a/*`、`server/adka2a/executor.go`、`server/adka2a/conversions.go`、`server/adka2a/v2/*`、`server/adkrest/handler.go`
- **plugin / telemetry**：`plugin/plugin.go`、`plugin/loggingplugin/logging_plugin.go`、`plugin/functioncallmodifier/*`、`internal/plugininternal/plugin_manager.go`、`internal/telemetry/telemetry.go`
- **配置/回放**：`internal/configurable/conformance/replayplugin/*`、`internal/configurable/conformance/recordplugin/*`、`internal/configurable/conformance/callbacks.go`
- **示例**：几乎所有 `examples/**/main.go` 都导入

## 10. 测试与可观察性

### 10.1 测试文件位置

- `session/inmemory_test.go` — InMemory 单元 + 并发 + 死锁回归（`Test_inMemoryService_CreateConcurrentAccess` 16 协程 / 32 次竞争写、`TestInMemorySession_AppendEvent_Deadlock` 防止 `updateSessionState` 重复加锁）
- `session/database/service_test.go` — DB 后端跑同一 `RunServiceTests` 套件 + AutoMigrate + 用 `WHERE true` 清理避免 Spanner DELETE 限制
- `session/vertexai/service_test.go` + `vertexai_test.go` — 用 `cloud.google.com/go/rpcreplay` 录制/回放 RPC，零网络依赖；testdata/ 下有 30 个 `.replay` 文件
- `session/session_test/service_suite.go` — 共享的 `RunServiceTests` + `Snapshot` + `SuiteOptions`（约 600 行覆盖 Create/Get/List/Delete/AppendEvent/StateManagement 六大分组）

### 10.2 Telemetry 埋点

session 包自身**没有**直接打 span / metric；`Event` 作为数据载体被 `internal/telemetry/telemetry.go` 引用：
- `ResponseEvent *session.Event`（`telemetry.go:81, 160`）— 把 Event 作为 span 属性透传给 OTel
- `TraceMergedToolCallsResult(span, fnResponseEvent, err)`（`telemetry.go:244`）— 工具调用合并后的追踪钩子

## 11. 文档写作提示

**必须写**：
1. 三层 state 命名空间（`app:` / `user:` / `temp:`）— 这是 `Service.AppendEvent` 副作用的核心；不讲清楚用户会把临时变量当持久化。
2. `EventActions` 的所有字段语义，特别是 `TransferToAgent` / `Escalate` / `SkipSummarization` 与 agent 流程控制的关系。
3. `Event.IsFinalResponse()` 的判定逻辑（`session/session.go:124`）— 影响 `runner` 是否把它当作本轮终结。
4. 三种后端的差异：in-memory 测试友好 / GORM 通用持久化 / Vertex AI 远程托管（含 LRO 等待、不支持自定义 SessionID）。
5. `Service.AppendEvent` 的"必清 temp"的合同（`inmemory.go:204`、`database/service.go:327`、`vertexai.go:130` 都有 `Partial==true` 短路）。
6. `service_test.RunServiceTests` 的复用方式 — 给"想接新后端"的开发者。

**可省略**：
- 内部 `id.Encode/Decode` 用 `ordered.Encode` 的具体字节布局 — 属于实现细节。
- `vertexai.aiplatformToGenaiContent` 等价转换的逐字段映射 — 太冗长，提及"等价双向转换"即可。
- `rsc.io/omap` 的具体复杂度 — 一句话"有序 map，便于按字典序区间扫"。

**潜在坑**：
- `database.Get` 没有像 in-memory 那样把"业务不存在"和"系统错误"区分开 — 都是 `fmt.Errorf("database error while fetching session: %w", err)`，调用方需要 unwrap 才能判断。
- `vertexAiClient.waitForOperation` 用 `getSession` 轮询 LRO（`session/vertexai/vertexai_client.go:116`），注释里写明 `// TODO replace with LRO wait when it's fixed`，未来行为可能变化。
- `inMemoryService.Create` 中 `copiedSession.state = maps.Clone(val.state)`（`inmemory.go:87`）— 返回的是浅克隆的 map，外部 `Set` 仍可能改到原 session 的 state map（取决于下游是否 `Set`），文档应提醒。
- `keyPrefix` 的 `app:` / `user:` / `temp:` 三个常量是公开 API（`session.go:163-176`），但实际值在 `internal/sessionutils` 里又被重复定义为 `appPrefix`/`userPrefix`/`tempPrefix`（`internal/sessionutils/utils.go:9-13`），写文档时务必引用 `session.KeyPrefix*`，避免给用户暴露 internal 常量。
- `localSession` 在 database / vertexai 包内各有一份，文件顶部的 `// TODO localSession is identical to session.session. Move to sessioninternal`（`session/database/session.go:28`、`session/vertexai/session.go:28`）提示未来会抽取到 internal，文档可注明"目前存在代码重复"。
- `Events.At` 越界返回 nil 而不是 panic（`session/inmemory.go:381-386`），不熟悉的人可能误以为没找到。
- 数据库 `ApplyEvent` 内用 `time.Truncate(time.Microsecond)`（`session/database/service.go:332`）— 用户传入纳秒时间戳会被悄悄裁掉，跨时钟源写入需要心里有数。

---

**覆盖小节**: 11/11
