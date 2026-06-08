# artifact 模块阅读笔记

> 范围：`/home/wu/oneone/adk/artifact/` 整个目录（含子目录 `gcsartifact`）
> 关联：`/home/wu/oneone/adk/internal/artifact/`（高层 wrapper，承载 `agent.Artifacts` 接口实现）
> 阅读版本：commit `d06992e2b1ec2c9b95c6070e0fd12d50a43e4c99`

---

## 1. 一句话定位

`artifact` 包是 ADK 的工件（artifact）存储抽象层：定义 `Service` 接口管理按 `appName/userID/sessionID/fileName/version` 寻址的内容 blob，并提供进程内（`InMemoryService`，基于有序 map）与 Google Cloud Storage（`gcsartifact.NewService`）两种开箱即用实现。

---

## 2. 子包/子目录结构

| 路径 | 角色 |
|---|---|
| `artifact/`（根） | `Service` 接口、请求/响应 DTO、参数校验、内存实现 `InMemoryService()`、artifactKey 编解码。包内无 `doc.go`，包级注释见 `service.go:15-20`。 |
| `artifact/gcsartifact/` | Google Cloud Storage 后端实现，对外暴露 `NewService(ctx, bucketName, opts...) (artifact.Service, error)`。同时包含对 GCS Go SDK 的接口抽象（`gcsClient/gcsBucket/gcsObject/...`），便于注入 mock 客户端。 |

> 注：高层 wrapper `internal/artifact/artifacts.go`（含 `trackedArtifacts` 装饰器逻辑的部分）实际位于 `agent/callback_context.go`；本笔记也会顺带提及。

---

## 3. 核心类型与接口

### 3.1 `Service`（`artifact/service.go:31-47`）
```go
type Service interface {
    Save(ctx, *SaveRequest) (*SaveResponse, error)
    Load(ctx, *LoadRequest) (*LoadResponse, error)
    Delete(ctx, *DeleteRequest) error
    List(ctx, *ListRequest) (*ListResponse, error)
    Versions(ctx, *VersionsRequest) (*VersionsResponse, error)
    GetArtifactVersion(ctx, *GetArtifactVersionRequest) (*GetArtifactVersionResponse, error)
}
```
- 设计意图：六个方法的最小可用 CRUD+版本集合。`Delete` 文档明确"删除不存在不算错误"，`GetArtifactVersion` 是元数据通道，与 `Load`（拉内容）解耦。所有方法都接收显式 `*XxxRequest`，便于扩展字段、避免位置参数膨胀。

### 3.2 请求/响应 DTO（均位于 `artifact/service.go`）
| 类型 | 行 | 说明 |
|---|---|---|
| `SaveRequest` / `SaveResponse` | 56-66 / 122-124 | `Part *genai.Part` 是必填的载荷；`Version int64` 缺省时服务端自增。 |
| `LoadRequest` / `LoadResponse` | 127-132 / 161-164 | `Version` 0 表示"最新版本"。 |
| `DeleteRequest` | 167-172 | `Version` 0 表示"删除该文件名下的所有版本"。 |
| `ListRequest` / `ListResponse` | 201-203 / 225-227 | 仅按 session 维度返回文件名；用户命名空间下的文件会合并返回。 |
| `VersionsRequest` / `VersionsResponse` | 230-232 / 261-263 | 返回该 `(app, user, session, file)` 下所有版本号。 |
| `GetArtifactVersionRequest` / `GetArtifactVersionResponse` | 275-280 / 309-311 | 返回 `*ArtifactVersion` 元数据。 |
| `ArtifactVersion` | 266-272 | 描述单版本：`Version`/`CanonicalURI`/`CustomMetadata`/`CreateTime`（Unix 秒，float64）/`MimeType`。 |

设计意图：DTO 全部平铺，零方法；校验逻辑以 `Validate() error` 方法下沉到每个 Request 上（不在接口里），保持 `Service` 接口干净。

### 3.3 内部工具类型
- `requiredField{ Name, Value string }`（`service.go:50-53`）：仅供 `validateRequiredStrings` 使用的内部 struct，无导出必要。
- `artifactKey`（`inmemory.go:56-78`）：`AppName/UserID/SessionID/FileName/Version` 5 元组 + `Encode/Decode`，底层用 `rsc.io/ordered` 生成稳定排序的字节键（Version 用 `ordered.Rev` 反向，配合 `math.MaxInt64..0` 区间扫描）。
- `userScopedArtifactKey = "user"`（`inmemory.go:54`）：user 命名空间伪 sessionID。把所有"跨 session 共享"的 `user:` 前缀文件重定向到该常量下作为 session 维度，统一存储但保持接口一致。

### 3.4 GCS 抽象接口（`gcsartifact/gcs_client.go`）
| 接口 | 行 | 用途 |
|---|---|---|
| `gcsClient` | 26-28 | `bucket(name) gcsBucket` |
| `gcsBucket` | 31-34 | `object(name)` / `objects(ctx, *storage.Query) gcsObjectIterator` |
| `gcsObject` | 37-42 | `newWriter` / `newReader` / `delete` / `attrs` |
| `gcsObjectIterator` | 45-47 | `next() (*storage.ObjectAttrs, error)` |
| `gcsWriter` | 50-54 | 嵌入 `io.Writer` + `io.Closer` + `SetContentType(string)` |
- 设计意图：把"不可 mock"的 `cloud.google.com/go/storage` 全部包装成接口，测试时用 `fakeClient`/`fakeBucket`/`fakeObject`（`gcs_test.go:53-196`）替换；生产侧 `gcsClientWrapper` 等 5 个 wrapper 桥接真实 SDK。文件末尾 `var _ gcsClient = ...` 等断言保证实现一致性（`gcs_client.go:141-147`）。

---

## 4. 关键数据结构

| Struct | 位置 | 字段含义 |
|---|---|---|
| `inMemoryService` | `inmemory.go:36-40` | `mu sync.RWMutex` 守护整张表；`artifacts omap.Map[string, *genai.Part]` 是 `rsc.io/omap` 提供的有序 map（按键的字典序迭代），键是 `artifactKey.Encode()` 的字符串。 |
| `artifactKey` | `inmemory.go:56-62` | 5 元组，外加 `Encode/Decode` 借用 `ordered.Encode` 给出"AppName 优先 / Version 倒序"的可比较字节序列。 |
| `gcsService` | `gcsartifact/service.go:43-47` | 持有 `bucketName string` + `storageClient gcsClient` + `bucket gcsBucket`。注意 `storageClient` 字段未被方法直接调用，仅保留便于测试时构造 fake。 |
| `SaveRequest.Part` | `service.go:58-60` | `*genai.Part`：可以是 `Text` 或 `InlineData`（带 `MIMEType`/`Data`），存储时二者选一。 |
| `ArtifactVersion` | `service.go:266-272` | 元数据快照；`CanonicalURI` 优先用 GCS 返回的 `MediaLink`，否则回退到 `gs://<bucket>/<blob>` 拼接。 |
| `gcsClientWrapper` 等 5 个 wrapper | `gcs_client.go:58-139` | 把 `*storage.Client/BucketHandle/ObjectHandle/Writer/ObjectIterator` 适配成包内接口；本身不持有额外状态。 |
| `fakeClient/fakeBucket/fakeObject/fakeWriter/fakeObjectIterator` | `gcs_test.go:54-196` | 测试替身；`fakeObject` 自带 `deleted bool` 标志 + 内存中的 `data []byte` + `contentType`，模拟 GCS 的可见/不可见/删除状态。 |

---

## 5. 关键流程

### 5.1 内存版 `Save` 写入（自增版本号）
入口：`inMemoryService.Save`（`inmemory.go:142-163`）
1. `req.Validate()` —— 检查 `AppName/UserID/SessionID/FileName` 非空、`Part` 非 nil、`Part.Text` 或 `Part.InlineData` 至少一个非空、文件名不含 `/`/`\`。
2. 若 `fileHasUserNamespace(fileName)`（即以 `user:` 开头），把 `sessionID` 替换为常量 `userScopedArtifactKey` —— 所有"用户级"文件统一落到伪 session 下。
3. 加 `mu.Lock()`，调内部 `find(...)` 找当前最新版本（`math.MaxInt64..0` 区间扫描返回第一个匹配），无则从 1 开始。
4. `set(...)` 写入 `omap`；返回 `{Version: nextVersion}`。
出口：`&SaveResponse{Version: nextVersion}, nil`。
> 注意：版本号仅在锁内读出，写时版本竞争窗口在锁内被串行化（`inmemory.go:154-161`），并发 `Save` 同一文件仍是顺序的。

### 5.2 内存版 `Load`（按版本或最新）
入口：`inMemoryService.Load`（`inmemory.go:194-222`）
1. 校验 + user namespace 重写。
2. 加 `mu.RLock()`。
3. 若 `req.Version > 0`：直接 `get` 精确查找；找不到返回 `fs.ErrNotExist`。
4. 否则 `find` 返回最新版本；同样缺失则 `fs.ErrNotExist`。
出口：`*LoadResponse{Part: artifact}`，err 包装为 `fmt.Errorf("artifact not found: %w", fs.ErrNotExist)`。

### 5.3 内存版 `List`（合并 session + user 命名空间）
入口：`inMemoryService.List`（`inmemory.go:225-259`）
1. 构造两次扫描区间：会话级 `[lo, hi=sessionID+"\x00")`，用户级 `[lo_user, hi_user+"\x00")`。
2. 用 `scan(lo, hi)` 拉 iter.Seq2，跳过 `key.SessionID` 不在目标集（避免 `hi` 端点越界）的项。
3. 合并到 `map[string]bool`，最后排序得到 `[]string`。
出口：`&ListResponse{FileNames: ...}`。

### 5.4 GCS 版 `Save`（无原子事务）
入口：`gcsService.Save`（`gcsartifact/service.go:94-137`）
1. 校验（同一 `req.Validate()`）。
2. **先**调 `versions()` 拉已有版本集合，取 `slices.Max(...)+1` 作为新版本号。源码注释（`service.go:104-105`）显式承认存在竞态：GCS 没有跨多对象的事务，多消费者并发写同文件可能版本号冲突。
3. `buildBlobName(...)` 生成对象名（`service.go:71-76`）。user 文件路径模板 `app/user/user/fileName/version`，普通文件 `app/user/session/fileName/version`。
4. `s.bucket.object(blobName).newWriter(ctx)` 拿到 `gcsWriter`，`defer writer.Close()` 在 `err == nil` 前提下捕获 close 错误。
5. 根据 `Part.InlineData` 是否非空分支写二进制或纯文本；写时 `SetContentType`。
出口：`*SaveResponse{Version: nextVersion}, nil`。

### 5.5 GCS 版 `Delete`（并行删多版本）
入口：`gcsService.Delete`（`gcsartifact/service.go:140-182`）
1. 校验。
2. 若指定版本：直接删一个对象。
3. 否则先 `versions()` 列出所有版本号，**用 `errgroup.WithContext` 并发删除**（`service.go:165-181`）。捕获 loop 变量 `v := version` 防止 goroutine 共享。
4. `g.Wait()` 收集错误。
出口：`g.Wait()` 的 err（包装为 `failed to delete artifact <blob>`）。

### 5.6 GCS 版 `Load` / `GetArtifactVersion`（共享 `resolveVersion`）
入口：`Load`（`service.go:185-229`）/`GetArtifactVersion`（`service.go:362-407`）
1. 校验。
2. `resolveVersion`（`service.go:345-359`）：若 `req.Version==0`，拉版本列表取 max，否则直接用；空列表报 `fs.ErrNotExist`。
3. 构造 blob 名 → `blob.attrs(ctx)` 探测存在性（`storage.ErrObjectNotExist` 翻译为 `fs.ErrNotExist`）。
4. `Load` 进一步 `newReader` + `io.ReadAll` → `genai.NewPartFromBytes(data, attrs.ContentType)` 返回。`GetArtifactVersion` 则组装 `CanonicalURI`（`MediaLink` 优先，否则 `gs://` 拼接）+ `CustomMetadata` + `CreateTime`（`float64(attrs.Created.Unix())`）。

---

## 6. 扩展点

| 扩展维度 | 接口/类型 | 说明 |
|---|---|---|
| 新增存储后端 | 实现 `artifact.Service`（6 个方法） | 接口非常小；只要后端支持按 key 寻址 + 版本号自增都能接入。当前 `gcsartifact` 是官方参考实现。 |
| 注入 GCS 客户端 | `gcsartifact.gcsClient` 等 5 个内部接口 | `gcs_client.go:26-54` 定义。仅供测试 mock，**不是稳定 API**（包内未导出，外部难直接构造 `gcsService{}`）。生产用 `NewService(ctx, bucketName, opts...)`。 |
| 包装 `agent.Artifacts` 装饰 | 实现 `agent.Artifacts` 接口（`agent/agent.go:111-116`） | 已有官方装饰器 `trackedArtifacts`（`agent/callback_context.go:241-261`）把 `Save` 的 `Version` 写入 `EventActions.ArtifactDelta`；用户可仿造实现审计、限流等。 |
| 自定义请求校验 | 替换/扩展每个 `*Request.Validate()` | 当前是 `Validate() error` 方法形式；如需更复杂的 schema 校验，可在调用方加 wrapper。 |
| 高层便捷 API | `internal/artifact/artifacts.go:27-71` | `Artifacts{ Service, AppName, UserID, SessionID }` 把三元组固化到结构体；适用于 runner 注入。 |

---

## 7. 错误处理

- **不存在的资源**：所有实现对 `Load/Delete/Versions/GetArtifactVersion` 命中"未找到"时都返回 `fmt.Errorf("...: %w", fs.ErrNotExist)`（内存：`inmemory.go:212/219/283/311`；GCS：`service.go:203/339/354/379`）。调用方可统一用 `errors.Is(err, fs.ErrNotExist)` 判断。
- **校验错误**：每个 `*Request.Validate()` 在缺失必填或 `Part.Text/InlineData` 都为空、文件名含路径分隔符时返回 `fmt.Errorf("invalid xxx request: missing required fields: ...") `（`service.go:99-110, 147-156, 186-194, 217-220, 247-254, 295-302`）。服务层再包装一层 `request validation failed: %w`。
- **GCS 后端错误**：所有 `bucket.objects/attrs/delete/Write` 失败都被 `fmt.Errorf("...: %w", err)` 透传；额外把 `storage.ErrObjectNotExist` 归一化为 `fs.ErrNotExist`（`gcsartifact/service.go:202-205, 378-381`）。
- **`gcsService.Save` 关闭写流的错误**：通过命名返回值 `(_ *artifact.SaveResponse, err error)` + `defer` 在 `err == nil` 时覆盖 err（`gcsartifact/service.go:118-122, 213-217`）—— 不覆盖业务错误以避免掩盖。
- **In-Memory Save 的"必填 Part 内容"**：`service.go:103-105` 单独校验 `Part.Text == "" && Part.InlineData == nil`；这是业务级断言，缺一即拒。
- **典型失败模式**：
  1. 文件名含 `/` 或 `\` → 校验失败（防止破坏 user 命名空间路径）。
  2. `user:` 前缀但请求仍带真实 `sessionID` —— 会被忽略，使用户文件跨 session 共享（这是设计而非错误，但容易让调用方误以为 session 隔离）。
  3. GCS 并发 `Save` 同一文件可能版本号冲突（`service.go:104-105` 注释明文）。
  4. `Delete` 指定 `Version=0` 在 GCS 实现下会拉一次 `versions()` 列表，列表为空时静默成功（`service.go:158-163`，GCS 的 `versions()` 不像 `Versions` 那样把空当 err）。

---

## 8. 并发与性能

### 内存实现
- 整张表受 `sync.RWMutex`（`inmemory.go:37`）保护：读操作 `Load/List/Versions/GetArtifactVersion` 用 `RLock`；写 `Save/Delete` 用 `Lock`。
- 底层容器是 `rsc.io/omap`（红黑树实现的有序 map），支持 `Scan(lo, hi)` 区间迭代；`find` / `Versions` 都通过 `scan` 在 `lo..hi` 区间内拿到全部匹配。
- `find` 通过让 `Version` 反向编码 + 区间 `[MaxInt64, 0)`，自然得到"最大版本号"为第一个命中。
- TODO 注释 `inmemory.go:82, 237, 248, 278`：作者希望 `omap` 扩展为支持"仅查键不解码 value"以加速 `List/Versions`。
- 性能瓶颈：`Versions` 每次都把全量版本号装到 `[]int64` 再排（实际 `scan` 已经按 Version 倒序，但后续未利用）。`List` 用 `map[string]bool` 去重再排序，单次扫描成本可控。

### GCS 实现
- `Delete`（`service.go:165-181`）用 `errgroup` 并发删除多版本对象；`Save`/`Load` 不并发。
- 每次 `Save` 都会先 `versions()` 拉一次对象列表——意味着写放大至少 2× API 调用。
- 注释（`service.go:104-105`）明文说没有跨多对象事务，所以 `Save` 存在"两个并发写可能取到同一 nextVersion"的竞态。
- `Load` 把整个对象一次性 `io.ReadAll` 到内存，再包成 `genai.Part`。大文件会一次性驻留内存；目前没有流式 `Part` API。

### 全局状态
- 内存版是 **进程内单例**（`omap`），重启即丢；用 `InMemoryService()` 工厂方法返回。
- GCS 版无本地状态，全部落到 bucket；`bucketName` 字段仅用于构造 `gs://` URI fallback（`service.go:386-389`）。

---

## 9. 依赖与被依赖

### 本模块导入（直接 import）
| 路径 | 出现位置 |
|---|---|
| `google.golang.org/genai` | `service.go:27`（`genai.Part`）、`inmemory.go:29` |
| `io/fs` | `inmemory.go:20`（`fs.ErrNotExist`） |
| `rsc.io/omap` | `inmemory.go:30`（有序 map 容器） |
| `rsc.io/ordered` | `inmemory.go:31`（artifactKey 编解码） |
| `cloud.google.com/go/storage` | `gcsartifact/service.go:33`、`gcsartifact/gcs_client.go:21` |
| `golang.org/x/sync/errgroup` | `gcsartifact/service.go:34`（并发删） |
| `google.golang.org/api/iterator` | `gcsartifact/service.go:35`、`gcsartifact/gcs_client.go:36` |
| `google.golang.org/api/option` | `gcsartifact/service.go:36`（`NewService` 的可选 client option） |
| `slices/maps/iter/sort/sync/strconv/strings/...` | 标准库 |

### 哪些模块导入本模块（反向引用，来源 `grep -rln "adk/artifact" --include="*.go"`）
- `agent/agent.go:25`、`agent/callback_context.go:25` —— 定义/实现 `agent.Artifacts` 接口。
- `runner/runner.go:28` —— 通过 `artifactinternal.Artifacts{Service, AppName, UserID, SessionID}` 注入。
- `cmd/launcher/launcher.go:24` —— `Config.ArtifactService` 字段类型为 `artifact.Service`。
- `server/adkrest/handler.go:27`、`server/adkrest/controllers/runtime.go:29`、`server/adkrest/controllers/artifacts.go:23`、`server/adkrest/controllers/triggers/*.go` —— REST/gRPC 端点把 HTTP 请求翻译为 `Service` 调用。
- `tool/agenttool/agent_tool.go:28`、`tool/loadartifactstool/load_artifacts_tool_test.go` —— 工具层。
- `internal/artifact/artifacts.go:23`、`internal/artifact/tests/service_suite.go:28` —— 官方高层 wrapper + 一致性测试套件。
- `examples/tools/loadartifacts/main.go`、`examples/web/main.go`、`examples/vertexai/imagegenerator/main.go` —— 示例。
- `internal/llminternal/audio_cache_manager_test.go`、`internal/llminternal/instruction_processor_test.go` —— 内部 LLM 模块的测试。

> 由此可见 `artifact` 是 ADK 二级核心包：被 `agent`、`runner`、`server`、`tool`、各种 example 普遍消费。

---

## 10. 测试与可观察性

### 测试文件清单
| 路径 | 行数 | 覆盖点 |
|---|---|---|
| `artifact/artifact_key_test.go` | 39 | `artifactKey.Encode/Decode` 往返。 |
| `artifact/inmemory_test.go` | 29 | 仅一行 `TestInMemoryArtifactService`，把 `InMemoryService()` 喂给 `internal/artifact/tests.TestArtifactService`。 |
| `artifact/request_validation_test.go` | 447 | 6 个 Request 的 `Validate()` 边界（必填、Part 缺失、文件名含路径分隔符）。 |
| `artifact/gcsartifact/gcs_test.go` | 204 | `TestGCSArtifactService` 同样跑 `TestArtifactService` 一致性套件；同时给出 `fakeClient/Bucket/Object/Writer/Iterator` 的完整替身。 |
| `internal/artifact/tests/service_suite.go` | 411 | 跨实现的 conformance 套件：常规/空服务/user 命名空间 三组场景（`testArtifactService`、`testArtifactService_Empty`、`testArtifactService_UserScoped`）。 |
| `internal/artifact/artifacts_test.go` | 124 | 高层 `Artifacts` wrapper 的单元测试（不再展开）。 |

### 一致性测试入口
- `inmemory_test.go:25-28` 工厂：`func(t) (artifact.Service, error) { return artifact.InMemoryService(), nil }`
- `gcs_test.go:35-43` 工厂：`newGCSArtifactServiceForTesting` → `newFakeClient()`，绕过真 GCS。
- 两个工厂的命名分别是 `InMemory` / `GCS`，套件运行时输出 `TestInMemoryArtifactService`、`TestGCSArtifactService` 等子测试（`service_suite.go:32-58`）。

### 可观察性 / Telemetry
- 整个 `artifact/` 目录 `grep "telemetry|tracer|metric|span|otel"` 命中为 0 —— **本模块不涉及任何 telemetry/metrics/tracing 埋点**。错误路径仅靠 Go 标准 `error` 透传，调用方需自己加观测。

---

## 11. 文档写作提示

### 必须写
1. **`Service` 6 方法的语义**：`Version=0` 在 `Load/Delete` 里分别代表"最新版本"/"所有版本"；`GetArtifactVersion` 是元数据通道（`CanonicalURI`/`MimeType`/`CreateTime`）。
2. **user 命名空间约定**：文件名以 `user:` 开头时，存储层把 `sessionID` 重写为常量 `"user"`，跨 session 共享；这点在 `List` 时一并返回。建议给出一个端到端小示例（`Save("user:profile.json", ...)` → 多个 session 的 `List` 都能看到）。
3. **GCS 后端对象布局**：`{appName}/{userID}/{sessionID | user}/{fileName}/{version}` —— 写文档时直接给出示意图，便于运维定位和 IAM 配置。
4. **校验约束**：`AppName/UserID/SessionID/FileName` 必填非空；`Part.Text` 与 `Part.InlineData` 至少一个非空；`FileName` 不允许 `/`/`\`（防止破坏路径）。
5. **并发与一致性**：`InMemoryService` 进程内安全但重启即丢；GCS 版 `Save` 没有事务保护，多消费者并发写同一文件可能版本号冲突（源码已显式声明，文档应复述以免误用）。
6. **`agent.Artifacts` vs `artifact.Service` 的差异**：前者是会话级便捷接口（`internal/artifact/artifacts.go`），后者是底层存储抽象；runner 注入的是高层 wrapper。`Save` 的 `Version` 会被 `trackedArtifacts` 自动写入 `EventActions.ArtifactDelta`（`agent/callback_context.go:241-261`），影响事件回放语义。
7. **`fs.ErrNotExist` 是约定**：调用方用 `errors.Is(err, fs.ErrNotExist)` 判断"不存在"；不要字符串匹配。

### 可以省略
- `gcsClient/gcsBucket/...` 内部 mock 接口（包内未导出，外部读者无需关注，除非要做自定义 GCS 客户端注入）。
- `artifactKey.Encode/Decode` 字节级细节（实现细节），只点出"键由 5 元组用 ordered 编码生成且 Version 倒序"即可。
- `gcsWriter/gcsWriterWrapper` 字段映射（同上）。

### 潜在的坑
1. **"user:" 前缀与 sessionID 的交互**：`Save` 写入 user 文件时即便请求带了 `sessionID`，存储层也会忽略并使用伪 `sessionID="user"`；但 `Load` 返回的 `BlobName`/错误信息会显示用户原始 `sessionID`？需要再核验 —— 实际上 GCS 写出来就是 `user` 路径，所以读回一致；但调用方拿到 `LoadResponse` 时只能拿到 `Part`，不会看到路径。如果文档要解释"为什么我在 session A 写入，能在 session B 读到"，必须解释这条重写规则。
2. **`List` 合并的语义**：返回的 `FileNames` 包含 session 内 + user 命名空间下的所有文件名；版本感知是 `Versions` 的事。文档要避免让用户误以为 `List` 能区分版本。
3. **`GCS Delete` 指定 `Version=0` 会先 `versions()`**：当后端真的有大量历史版本时，会触发全量前缀扫描，文档应提醒性能。
4. **`InMemoryService` 没有持久化**：单测/本地 dev 没问题，prod 必走 GCS；文档里加粗强调。
5. **路径分隔符的 `\` 也被拒**（`service.go:115`）：Windows 风格反斜杠也禁止，避免不同 OS 下行为分叉。
6. **`GetArtifactVersion` 的 `CreateTime` 是 `float64` Unix 秒**（`gcsartifact/service.go:403`）：精度只到秒；如果需要纳秒或时区显示，调用方需自行转换。

---

> 阅读完成。本笔记覆盖结构（1-2）、类型/接口（3-4）、流程（5）、扩展（6）、错误（7）、并发（8）、依赖（9）、测试（10）、写作建议（11），共 11 节。
