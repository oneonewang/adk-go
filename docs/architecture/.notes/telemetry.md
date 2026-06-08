# telemetry 模块阅读笔记

> 阅读对象: `/home/wu/oneone/adk/telemetry/` (public 包)
> 辅助阅读: `/home/wu/oneone/adk/internal/telemetry/` (内部 instrumentation)
> 锁版 commit: `d06992e2b1ec2c9b95c6070e0fd12d50a43e4c99`
> 阅读日期: 2026-06-05

## 1. 一句话定位

`google.golang.org/adk/telemetry` 是 ADK 对 OpenTelemetry 的一站式引导 (bootstrap) 入口, 负责把 trace / log 两种 OTel SDK 装配好 (可选用 GCP Cloud Trace 导出), 暴露给应用层一次性注册到全局 Provider, 让所有 ADK 内部 instrumentation 拿到正确的 Tracer/Logger。

## 2. 子包/子目录结构

`telemetry` 本身是单层 flat 包, 不含子目录, 但强依赖一个 internal sibling:

| 路径 | 角色 |
| --- | --- |
| `/home/wu/oneone/adk/telemetry/` (本模块) | 公开 API, 负责创建、配置、销毁 OTel Provider |
| `/home/wu/oneone/adk/internal/telemetry/` | 内部 instrumentation: 提供 `StartInvokeAgentSpan`、`StartGenerateContentSpan`、`StartExecuteToolSpan`、`LogRequest`、`LogResponse` 等业务埋点; 不对外, 头部明确写着 attributes 尚未稳定 |

> 文档写作者请注意: 这两个包同名不同物, 公开包做"装配", internal 包做"埋点", 必须明确区分, 否则会让读者搞混 `telemetry.New` 与 `telemetry.StartInvokeAgentSpan` 的边界。

## 3. 核心类型与接口

| 名称 | 定义位置 | 签名 / 形态 | 设计意图 |
| --- | --- | --- | --- |
| `Providers` | `telemetry/telemetry.go:31` | `struct { TracerProvider, LoggerProvider, genAICaptureMessageContent }` | 包装 `*sdktrace.TracerProvider` 和 `*sdklog.LoggerProvider`, 对外屏蔽 OTel 内部细节并提供 `Shutdown` / `SetGlobalOtelProviders` 生命周期方法 |
| `Option` | `telemetry/config.go:61` | `interface { apply(*config) error }` | functional options 模式, 配置项, 每个 `WithXxx` 返回实现该接口的 `optionFunc` (`config.go:65`) |
| `config` | `telemetry/config.go:25` | 私有 struct, 持有 `oTelToCloud` / `gcpResourceProject` / `gcpQuotaProject` / `googleCredentials` / `resource` / `spanProcessors` / `logProcessors` / `tracerProvider` / `loggerProvider` / `genAICaptureMessageContent` | 仅在 `configure` 流程内构造, 不可被外部访问, 保证不可变风格 |
| `New(ctx, opts...)` | `telemetry/telemetry.go:118` | `func New(ctx context.Context, opts ...Option) (*Providers, error)` | 包级入口, 把 Option 列表合成为 `*config`, 再调 `newInternal` 创建 Provider |
| `SetGlobalOtelProviders()` | `telemetry/telemetry.go:56` | `func (t *Providers) SetGlobalOtelProviders()` | 把 `Providers` 暴露的 OTel Provider 写入 OTel 全局注册表, 并通过 `internal.SetGenAICaptureMessageContent` 同步给埋点层 |
| `Shutdown(ctx)` | `telemetry/telemetry.go:40` | `func (t *Providers) Shutdown(ctx context.Context) error` | 顺序关闭 TracerProvider 和 LoggerProvider, 用 `errors.Join` 合并错误 |

> 内部 instrumentation 包的关键 API: `StartInvokeAgentSpan` (`internal/telemetry/telemetry.go:67`), `StartGenerateContentSpan` (`internal/telemetry/telemetry.go:99`), `StartExecuteToolSpan` (`internal/telemetry/telemetry.go:148`), `TraceAgentResult` / `TraceGenerateContentResult` / `TraceToolResult` / `TraceMergedToolCallsResult`, `LogRequest` / `LogResponse` (`internal/telemetry/logger.go:56`、`logger.go:69`), 通用 `StartTrace` / `WrapYield`. 这些是供 `agent` / `internal/llminternal` 调用的低层埋点, 不属于"装配"模块, 但应作为"相关 API"在文档里指引读者跳读。

## 4. 关键数据结构

| 名称 | 位置 | 字段含义 |
| --- | --- | --- |
| `Providers` | `telemetry/telemetry.go:31` | `genAICaptureMessageContent` 私有 bool, 标记是否记录 LLM 消息原文; `TracerProvider` / `LoggerProvider` 是真正可注入到 OTel 全局的 SDK 实例, 为 nil 时 `SetGlobalOtelProviders` 不会覆盖 (避免污染用户自带的 Provider) |
| `config` | `telemetry/config.go:25` | 12 个字段承载全部用户可调参数; 重点关注 `oTelToCloud` (是否启 GCP)、`googleCredentials` (覆盖 ADC)、`tracerProvider` / `loggerProvider` (整 Provider 注入, 见 `initTracerProvider`/`initLoggerProvider` 的短路逻辑) |
| `StartGenerateContentSpanParams` | `internal/telemetry/telemetry.go:91` | `ModelName` + `InvocationID`, span 启动参数 |
| `TraceGenerateContentResultParams` | `internal/telemetry/telemetry.go:110` | `Response *model.LLMResponse` / `EventID` / `Error`, 用于在 span 结束时落 token 用量 |
| `TraceToolResultParams` | `internal/telemetry/telemetry.go:157` | 工具调用的描述、响应 event、错误 |
| `inMemoryLogExporter` | `telemetry/telemetry_test.go:435` | 测试内 mock, 实现 `sdklog.Exporter` 接口, 收集到 `records` 字段用于断言 |

## 5. 关键流程

### 5.1 装配入口: `telemetry.New`

入口: `telemetry/telemetry.go:118` `New`
→ 调 `configure(ctx, opts...)` (`setup_otel.go:34`)
→ `configFromOpts(opts...)` (`setup_otel.go:74`) 解析 options, 同步从 `OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT` env 读默认
→ 若 `oTelToCloud=true` (`setup_otel.go:40`), 通过 `google.FindDefaultCredentials` 拉 ADC, 再用 `resolveGcpQuotaProject` / `resolveGcpResourceProject` 推算 quota / resource project
→ `resolveResource` (`setup_otel.go:140`) 合并: `resource.Default()` (env 推断) → GCP 探测器 (`gcp.NewDetector`) → 用户传入的 `cfg.resource`, 后者覆盖前者
→ `configureExporters` (`setup_otel.go:173`): 若 `OTEL_EXPORTER_OTLP_ENDPOINT` / `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` 命中, 创建 `otlptracehttp.New`; 若 `oTelToCloud`, 追加 `newGcpSpanExporter` (指向 `https://telemetry.googleapis.com/v1/traces` 并带 `x-goog-user-project` 头). Logs 同理
→ 把环境/配置产生的 processor 追加到 `cfg.spanProcessors` / `cfg.logProcessors`
→ `newInternal(cfg)` (`setup_otel.go:87`) → `initTracerProvider` / `initLoggerProvider` (`setup_otel.go:211` / `:229`): 若用户提供整 Provider (`cfg.tracerProvider != nil`) 直接用, 否则按 processor 数量决定是否真创建, 数量为 0 返回 nil (不会空建)
出口: 返回 `*Providers`, 同时把 `genAICaptureMessageContent` 状态保留在 struct 上等待 `SetGlobalOtelProviders` 同步给埋点层

### 5.2 全局注册: `SetGlobalOtelProviders`

入口: `telemetry/telemetry.go:56`
→ 先调 `internal.SetGenAICaptureMessageContent` (`internal/telemetry/logger.go:36`) 把 bool 写入 `atomic.Bool`
→ 若 `TracerProvider != nil`, 调 `otel.SetTracerProvider`
→ 若 `LoggerProvider != nil`, 调 `logglobal.SetLoggerProvider`
出口: 之后 OTel 全局 API (`otel.Tracer`、`global.GetLoggerProvider`) 与 `internal/telemetry` 的 `tracer` / `otelLogger` 都使用新的 Provider

### 5.3 优雅关闭: `Providers.Shutdown`

入口: `telemetry/telemetry.go:40`
→ 顺序关闭 TracerProvider, 错误用 `errors.Join` 累积
→ 再关 LoggerProvider, 错误继续 `errors.Join`
→ 返回合并错误, 调用者用 `defer` + timeout ctx 兜底
注: `internal/telemetry` 没有资源需要关, 它的 `tracer` / `otelLogger` 是包级变量, 跟随全局 Provider 失效即失

### 5.4 (辅助流程) GCP 项目解析: `resolveProject`

入口: `setup_otel.go:118`
→ 顺序: `cfg` 显式值 > `google.Credentials.ProjectID` > `GOOGLE_CLOUD_PROJECT` env
→ 全空且 `requireProject=true` 时返回错误, 提示 "telemetry.googleapis.com requires setting the X project"
→ 用 `strings.TrimSpace` 处理空白字符串作为未配置
被 `resolveGcpQuotaProject` (`setup_otel.go:105`) 和 `resolveGcpResourceProject` (`setup_otel.go:114`) 共用, 但 quota 在 `oTelToCloud=false` 时不强求, resource 同理 (`resolveGcpQuotaProject` 走 `oTelToCloud=true` 才会入 `requireProject`)

### 5.5 业务埋点 (内部包, 给 doc 写作者参考)

入口: `agent/agent.go:164` `StartInvokeAgentSpan` → 在 `base_flow.go:811` 创建 `generate_content` span → 在 `base_flow.go:818` 调 `LogRequest` 输出 `gen_ai.system.message` + `gen_ai.user.message` 事件 → 拿到响应后 `TraceGenerateContentResult` (`base_flow.go:828`) + `LogResponse` (`base_flow.go:847`) 输出 `gen_ai.choice` → 工具执行走 `StartExecuteToolSpan` (`base_flow.go:1034`) / `StartTrace` 合并调用 (`base_flow.go:1018`)
出口: 所有 span 在 defer 处 `End()`, 日志事件通过 OTel Logger 异步发出

## 6. 扩展点

| 扩展类型 | 入口 | 说明 |
| --- | --- | --- |
| 自定义 SpanProcessor | `WithSpanProcessors(p ...sdktrace.SpanProcessor)` (`config.go:112`) | 追加到 OTel 默认列表, 与 `BatchSpanProcessor`(OTLP) 并存, 用户可注入自己的 exporter |
| 自定义 LogRecordProcessor | `WithLogRecordProcessors(p ...sdklog.Processor)` (`config.go:120`) | 同上, 用于日志 |
| 整体替换 TracerProvider | `WithTracerProvider(tp *sdktrace.TracerProvider)` (`config.go:128`) | `initTracerProvider` (`setup_otel.go:211`) 短路直接返回, 用户配置中的 spanProcessors 全部被忽略 |
| 整体替换 LoggerProvider | `WithLoggerProvider(lp *sdklog.LoggerProvider)` (`config.go:136`) | 同上 |
| 注入自定义 OTel Resource | `WithResource(r *resource.Resource)` (`config.go:96`) | `resolveResource` 阶段会与 `resource.Default()` / GCP 探测器合并, 用户提供的最后覆盖 |
| 凭证覆盖 | `WithGoogleCredentials(c *google.Credentials)` (`config.go:104`) | 跳过 `google.FindDefaultCredentials`, 测试场景常用 |
| 是否记录消息原文 | `WithGenAICaptureMessageContent(capture bool)` (`config.go:144`) | 通过 `SetGlobalOtelProviders` 写入 `atomic.Bool` (`internal/telemetry/logger.go:33`), 埋点层在 `LogRequest` / `LogResponse` 里读取, 关闭时返回 `<elided>` (`logger.go:45`) |
| 是否导出到 GCP | `WithOtelToCloud(value bool)` (`config.go:72`) | 同时影响 ADC 加载、项目解析、GCP 探测器、Cloud Trace exporter |

> 文档可以提示用户: "如果你需要 Meter, 暂时没有, 见 `setup_otel.go:91` 的 TODO(#479)"。

## 7. 错误处理

- 没有自定义 error 类型, 全部用 `fmt.Errorf("...: %w", err)` 包装。
- 关键错误来源 (按发生顺序):
  - `configFromOpts` 应用 option 失败 (`setup_otel.go:81`)
  - `oTelToCloud=true` 时 ADC 拉取失败 (`setup_otel.go:45`)
  - quota / resource project 解析失败 (`setup_otel.go:50`、`setup_otel.go:55`), 错误提示用户看 `telemetry.config`
  - `resolveResource` 中任意 `resource.New` / `resource.Merge` 失败 (`setup_otel.go:155` / `:160` / `:165`)
  - `configureExporters` 中 OTLP HTTP / GCP exporter 创建失败 (`setup_otel.go:183` / `:192` / `:201`)
- `Providers.Shutdown` 不会 panic, 多个错误用 `errors.Join` (`telemetry/telemetry.go:44`、`telemetry/telemetry.go:49`)。
- 常见失败模式:
  1. 用户启了 `WithOtelToCloud(true)`, 但没设项目也找不到 ADC → `resolveGcpResourceProject` / `resolveGcpQuotaProject` 报错
  2. `OTEL_EXPORTER_OTLP_ENDPOINT` 设置但 endpoint 不可达 → exporter 创建不会立即失败, 真正失败发生在批处理 flush 阶段
  3. 用户同时传 `WithTracerProvider(tp)` 与 `WithSpanProcessors(...)`, 后者被静默忽略 (`setup_otel.go:213`), 文档需要明确写出此行为避免用户疑惑

## 8. 并发与性能

- 没有 goroutine, 没有锁。
- 唯一一处并发相关: `internal/telemetry/logger.go:33` 用 `atomic.Bool` 维护 `genAICaptureMessageContent`, 在 `LogRequest` / `LogResponse` 中并发读取, 写只在 `SetGlobalOtelProviders` 期间发生。
- `Providers` struct 本身无锁, 假设用户遵循"启动时配置、运行时只读" 的模式。
- 性能瓶颈不在装配阶段, 而在使用阶段:
  - 默认 SpanProcessor / LogProcessor 都是 `BatchSpanProcessor` / `BatchProcessor` (`setup_otel.go:185`、`setup_otel.go:194`、`setup_otel.go:204`), 异步批处理
  - 每次 `LogRequest` 会 JSON 序列化 LLM 请求 (`internal/telemetry/logger.go:180`), 大请求注意开销
  - `contentToJSONLikeValue` 双跳 json.Marshal + json.Unmarshal (`internal/telemetry/logger.go:186`), 看似冗余, 但因为要拿到 `map[string]any` 走 `toLogValue` (`internal/telemetry/converters.go:34`), 所以保留
- 调优点: `genAICaptureMessageContent=false` 时直接返回 `<elided>` 字符串, 跳过 JSON 序列化 (`logger.go:150`、`logger.go:172`), 是文档可以提示的"开/关"开关

## 9. 依赖与被依赖

### 9.1 本模块导入 (用 grep)

`telemetry/telemetry.go` 与 `telemetry/setup_otel.go` 的 import 列表 (按字母):

- `context` (Go std)
- `errors` (Go std)
- `fmt`, `os`, `strings` (Go std)
- `internal "google.golang.org/adk/internal/telemetry"` (`telemetry/telemetry.go:22`)
- `golang.org/x/oauth2`, `golang.org/x/oauth2/google` (`setup_otel.go:30`、`config.go:22`)
- `go.opentelemetry.io/otel` (全局 API, `telemetry/telemetry.go:24`)
- `logglobal "go.opentelemetry.io/otel/log/global"` (`telemetry/telemetry.go:25`)
- `sdklog "go.opentelemetry.io/otel/sdk/log"` (`telemetry/telemetry.go:26`、`config.go:19`、`setup_otel.go:27`)
- `sdktrace "go.opentelemetry.io/otel/sdk/trace"` (`telemetry/telemetry.go:27`、`config.go:21`、`setup_otel.go:29`)
- `go.opentelemetry.io/otel/attribute` (`setup_otel.go:24`)
- `go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp` (`setup_otel.go:25`)
- `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp` (`setup_otel.go:26`)
- `go.opentelemetry.io/otel/sdk/resource` (`setup_otel.go:28`、`config.go:20`)
- `go.opentelemetry.io/contrib/detectors/gcp` (`setup_otel.go:23`)

### 9.2 哪些模块导入本模块 (公开 telemetry)

- `examples/telemetry/main.go:33`
- `examples/tools/multipletools/main.go:31`
- `cmd/launcher/launcher.go:28`
- `cmd/launcher/internal/telemetry/telemetry.go:22`
- `cmd/launcher/web/api/api.go:31`

### 9.3 哪些模块导入 internal/telemetry (埋点层)

- `telemetry/telemetry.go:22` (本模块)
- `agent/agent.go:28` (Agent 调度埋点)
- `internal/llminternal/base_flow.go:39` (LLM 调用 + 工具调用埋点)
- `server/adkrest/internal/services/debugtelemetry.go:33` (debug 端点)

> 阅读理解: 公开 `telemetry` 是"出口", 内部 `internal/telemetry` 是"入口", 两者组合起来形成完整闭环 — 文档可以画一张"装配 → 注入全局 → ADK 内部埋点" 的图。

## 10. 测试与可观察性

### 10.1 测试文件

- 公开包测试: `telemetry/telemetry_test.go` (555 行)
  - `TestTelemetrySmoke` (`:37`): 端到端冒烟, 用 `tracetest.NewInMemoryExporter` + 自定义 `inMemoryLogExporter` 验证 span 与日志带正确的 resource attribute (`gcp.project_id`、`service.name`、`service.version`)
  - `TestTelemetryCustomProvider` (`:128`): 验证 `WithTracerProvider` 会整体替换, 同时传入的 `WithSpanProcessors` 被忽略 (`unusedExporter` 必须为 0)
  - `TestTelemetryCustomLoggerProvider` (`:176`): 同上, 针对 LoggerProvider
  - `TestResolveResourceProject` (`:239`): table-driven 覆盖 7 个 case, 包括 options > credentials > env > 错误 顺序, 并验证空白字符串视作未配置
  - `TestResolveQuotaProject` (`:333`): 同上, 覆盖 8 个 case, 多了 "otelToCloud disabled 时不强制" 的场景
  - `TestConfigureExporters` (`:453`): 覆盖 6 种环境变量 + `oTelToCloud` 组合, 用 processor 数量做断言
  - `extractResourceAttributes` (`:225`): 工具方法, 从 `*resource.Resource` 抽取三个属性
  - `inMemoryLogExporter` (`:435`): 测试内 mock, 实现 `sdklog.Exporter`
- 内部包测试: `internal/telemetry/telemetry_test.go` (555 行) 与 `internal/telemetry/logger_test.go` (待查) + `converters_test.go`
- 集成示例: `examples/telemetry/main.go` 是一个可运行的演示

### 10.2 Telemetry 埋点

- 本模块不直接产生业务 span, 它生产 OTel Provider, 由其它模块注入埋点
- 与本模块直接相关的"埋点"是它对环境的读取:
  - `OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT` env (`setup_otel.go:76`) → 控制消息体是否被脱敏
  - `OTEL_EXPORTER_OTLP_ENDPOINT` / `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` / `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT` env (`setup_otel.go:177`、`setup_otel.go:179`、`setup_otel.go:197`) → 控制 OTLP 导出目标
  - `GOOGLE_CLOUD_PROJECT` env (`setup_otel.go:128`) → quota / resource 项目的兜底
  - `OTEL_SERVICE_NAME` / `OTEL_RESOURCE_ATTRIBUTES` env → 通过 `resource.Default()` 透传 (`setup_otel.go:141`)

## 11. 文档写作提示

### 11.1 必须写

1. 公开包 vs internal 包的关系, 用一段话 + 一张分层图说清"装配" 与 "埋点" 的边界, 这是最容易被混淆的地方
2. `New` 的最小可运行示例, 包含 `Shutdown` + `SetGlobalOtelProviders` 的标准模式, 可以直接复用 godoc 里的示例 (`telemetry/telemetry.go:73-115`)
3. 全部 `WithXxx` 选项, 重点强调:
   - `WithOtelToCloud` 与 GCP 凭据的关系
   - `WithTracerProvider` / `WithLoggerProvider` 是"整体替换" 而非"追加", 配合 `WithSpanProcessors` 时后者会被忽略
   - `WithGenAICaptureMessageContent` 的隐私影响 (会打印明文)
4. 环境变量清单, 包括 OTEL 系列 + `GOOGLE_CLOUD_PROJECT`
5. 错误排查: 典型 "telemetry.googleapis.com requires setting the X project" 错误, 引导用户检查 ADC 和项目 ID
6. Meter Provider 暂未实现 (TODO #479), 文档中必须说明, 否则用户会困惑

### 11.2 可以省略 / 简短

- `optionFunc` 内部类型 — 实现细节, 提一句"functional options" 即可
- `safeSerialize` 等内部工具函数, 属于 internal 包, 文档可不展开
- 测试细节 (table case 数量) — 不需要枚举, 写"覆盖 7 种项目解析场景" 即可

### 11.3 潜在的坑

- `WithTracerProvider` 整体替换后, 用户传的 `WithSpanProcessors` 静默失效 — 必须在文档中以 WARNING 形式标注
- `genAICaptureMessageContent` 通过 `SetGlobalOtelProviders` 时机同步, 如果用户手动管理 Provider 而不调用 `SetGlobalOtelProviders`, 埋点层 `LogRequest` 会拿到 `false` 默认值, 消息体被脱敏
- GCP 导出器 URL 硬编码为 `https://telemetry.googleapis.com/v1/traces` (`setup_otel.go:251`), 不可改, 但 quota project 通过 `x-goog-user-project` 头解决 ADC 用户凭据问题 (`setup_otel.go:254`)
- `configureExporters` 注释明确说明 "Golang OTel exporter to CloudLogging is not yet available" (`setup_otel.go:207`), 日志到 Cloud Logging 暂不支持
- `internal/telemetry` 的包级 `tracer` / `otelLogger` 在包初始化时通过 `otel.GetTracerProvider()` 抓取 (`internal/telemetry/telemetry.go:54`、`logger.go:47`), 因此必须先 `SetGlobalOtelProviders` 再触发业务代码, 否则拿到的是 OTel 默认 noop provider; 文档需要明确启动顺序
- `internal/telemetry` 头部明确写了 "may change", 文档应提醒用户该包不受 Go module 兼容承诺保护

### 11.4 建议章节

1. Overview (装配 + 埋点分层)
2. Quick Start (完整 main 示例)
3. Options Reference (10 个 `WithXxx` 表格)
4. Environment Variables (OTel 标准 + Google 专属)
5. GCP Integration (Cloud Trace + 凭据 + 项目解析规则)
6. Shutdown & Lifecycle
7. Troubleshooting (常见 4-5 个错误场景)
8. Related: `internal/telemetry` (指针性段落, 不展开)
