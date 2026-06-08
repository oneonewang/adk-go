# model 模块阅读笔记

## 1. 一句话定位
`model` 是 ADK 中"模型后端抽象层"：通过 `LLM` 接口统一封装不同 LLM 提供方（Gemini API、Vertex AI、Apigee 代理），向上层（agent/runner/tool/plugin）屏蔽具体的 SDK 与 HTTP 细节，仅暴露请求/响应/流式迭代的稳定协议。

## 2. 子包/子目录结构

| 子包 | 路径 | 作用（1 行） |
|------|------|------------|
| `model`（根） | `model/llm.go` | 定义 `LLM` 接口、`LLMRequest`、`LLMResponse` 等中立数据结构 |
| `model/gemini` | `model/gemini/gemini.go` | `LLM` 的 Google Gemini API / Vertex AI 实现，基于 `google.golang.org/genai` SDK |
| `model/apigee` | `model/apigee/apigee.go` | 通过 Apigee 代理调用 Gemini 的实现，解析 `apigee/...` 形式的模型名并委托给 `gemini` 子包 |

注意：根包没有 `doc.go`，包级注释直接写在 `llm.go:15`。

## 3. 核心类型与接口

### 3.1 `LLM` 接口
- 位置：`model/llm.go:26-29`
- 签名：
  ```go
  type LLM interface {
      Name() string
      GenerateContent(ctx context.Context, req *LLMRequest, stream bool) iter.Seq2[*LLMResponse, error]
  }
  ```
- 设计意图：极简——只暴露"模型名 + 生成内容（含流式）"两个能力；用 Go 1.23 起的 `iter.Seq2` 把同步/流式统一成"惰性迭代器"，上层用 `for ... range` 即可消费，无需区分 channel / callback。

### 3.2 `LLMRequest` 结构体
- 位置：`model/llm.go:32-38`
- 字段：`Model`、`Contents []*genai.Content`、`Config *genai.GenerateContentConfig`、`Tools map[string]any`（`json:"-"` 显式排除序列化）
- 设计意图：直接复用 `google.golang.org/genai` 的 `Content` / `GenerateContentConfig` 作为底层 wire-format，避免重复定义 DTO；`Tools` 单独成字段是因为其类型与 genai 内部 tool 表示不一一对应；`Model` 可被 `BeforeModelCallback` 改写（参见 `gemini.go:120-125` 的 `modelName`）。

### 3.3 `LLMResponse` 结构体
- 位置：`model/llm.go:42-68`
- 关键字段：`Content`、`CitationMetadata`、`GroundingMetadata`、`UsageMetadata`、`LogprobsResult`、`Partial`（流式中段标记）、`TurnComplete`（候选是否有非空 `FinishReason`）、`Interrupted`（bidi 流被用户中断）、`SessionResumptionHandle`、`ErrorCode`/`ErrorMessage`、`FinishReason`、`AvgLogprobs`
- 设计意图：用 `Partial` + `TurnComplete` 区分流式 chunk；用 `ErrorCode` / `ErrorMessage` 描述业务级错误（`SAFETY`/`RECITATION`/prompt block），让上层不必总返回 `error`——便于"错误也是响应"的下游处理。

### 3.4 `apigeeModel` 结构体
- 位置：`model/apigee/apigee.go:45-48`
- 字段：`delegate model.LLM`（实际工作由 `gemini.NewModel` 返回的实例承担）、`name string`（原始 `apigee/...` 字符串）
- 设计意图：纯粹的代理/适配器模式——自身不直接调用 HTTP，只负责"解析模型名 + 装配 HTTPOptions + 委派"。

### 3.5 `apigee.Config` 结构体
- 位置：`model/apigee/apigee.go:51-56`
- 字段：`ModelName`、`ProxyURL`、`CustomHeaders http.Header`、`HTTPClient *http.Client`
- 设计意图：Functional Options 模式的承载结构，避免日后扩展破坏调用方。

### 3.6 `apigee.Option` 闭包
- 位置：`model/apigee/apigee.go:59-80`
- 三个具体 Option：`WithProxyURL`、`WithCustomHeaders`、`WithHTTPClient`（明确标注"测试专用"）
- 设计意图：让"默认配置走环境变量、显式传入覆盖默认"成为标准模式。

### 3.7 `apigee.modelInfo`（私有）
- 位置：`model/apigee/apigee.go:39-43`
- 字段：`modelID`、`apiVersion`、`isVertexAI`
- 设计意图：把字符串 `apigee/...` 解析出的三段信息汇总，下游分发给 `gemini.NewModel`。

### 3.8 `gemini.geminiModel` 结构体
- 位置：`model/gemini/gemini.go:36-40`
- 字段：`client *genai.Client`、`name string`（构造时指定的模型名）、`versionHeaderValue string`（`google-adk/<ver> gl-go/<goVer>`）
- 设计意图：把"统计/遥测用版本头"在构造期就生成一次（`gemini.go:73-74`），避免每请求拼接。

### 3.9 `mergeHeadersInterceptor`
- 位置：`model/gemini/gemini.go:175-190`
- 设计意图：`http.RoundTripper` 装饰器，强制把 `x-goog-api-client` 与 `user-agent` 这两个重复 header 合并成单值（`strings.Join`）——`genai` SDK 与 ADK 同时设置时不会重复。

## 4. 关键数据结构

| 类型 | 位置 | 字段含义要点 |
|------|------|------------|
| `LLMRequest` | `model/llm.go:32` | `Model` 可被 callback 覆写；`Contents`/`Config` 直接复用 genai 类型；`Tools` 走 `json:"-"` 不被序列化 |
| `LLMResponse` | `model/llm.go:42` | `Partial`/`TurnComplete` 是流式控制位；`ErrorCode/ErrorMessage` 表达业务级失败；`SessionResumptionHandle` 服务于 bidi/live |
| `apigeeModel` | `model/apigee/apigee.go:45` | 仅持 `delegate` + `name` 字符串 |
| `apigee.Config` | `model/apigee/apigee.go:51` | `HTTPClient` 字段明确为测试桩 |
| `apigee.modelInfo` | `model/apigee/apigee.go:39` | 解析结果三元组 |
| `gemini.geminiModel` | `model/gemini/gemini.go:36` | `versionHeaderValue` 构造期固化 |
| `mergeHeadersInterceptor` | `model/gemini/gemini.go:175` | `base http.RoundTripper` 为可空（为 nil 时走 `http.DefaultTransport`） |

## 5. 关键流程

### 5.1 创建 Gemini 模型
- 入口：`gemini.NewModel(ctx, modelName, cfg)`（`model/gemini/gemini.go:49`）
- 关键步骤：
  1. 深拷贝 `cfg` 及 `cfg.HTTPClient`，**不污染调用方**（`gemini.go:50-59`）
  2. `genai.NewClient` 创建底层客户端
  3. 把 `http.Client.Transport` 包成 `mergeHeadersInterceptor`（`gemini.go:66-70`）
  4. 用 `internal/version.Version` 拼 `versionHeaderValue` 一次（`gemini.go:73-74`）
- 出口：返回 `*geminiModel`，隐式实现 `model.LLM`（通过 `var _ googlellm.GoogleLLM = &geminiModel{}` 编译期断言，见 `gemini.go:203`）

### 5.2 同步生成内容
- 入口：`geminiModel.GenerateContent(ctx, req, stream=false)`（`gemini.go:88`）
- 关键步骤：
  1. `maybeAppendUserContent` 兜底：内容为空时补一条 "Handle the requests as specified in the System Instruction."；最后一条非 user 时补一条 "Continue processing..." 提示模型继续输出（`gemini.go:163-171`）
  2. 兜底初始化 `req.Config.HTTPOptions.Headers`（`gemini.go:90-99`）
  3. `addHeaders` 写入遥测头（`gemini.go:112-115`）
  4. 走 `generate` → `m.client.Models.GenerateContent` → `converters.Genai2LLMResponse` 转译（`gemini.go:128-138`）
- 出口：包装成"只 yield 一次"的 `iter.Seq2`

### 5.3 流式生成内容
- 入口：`geminiModel.GenerateContent(ctx, req, stream=true)`（`gemini.go:101-103`）
- 关键步骤：
  1. 创建 `streamingResponseAggregator`（内部包 `internal/llminternal`）
  2. 迭代 `m.client.Models.GenerateContentStream`，逐 chunk 喂给 `aggregator.ProcessResponse`（`gemini.go:141-160`）
  3. 流结束后调用 `aggregator.Close()` 产出汇总响应（`stream_aggregator.go:304-329`）
  4. 消费者随时 `return`，`for ... range` 终止时退出
- 出口：每 chunk 一个 `*model.LLMResponse`（`Partial=true`），最后再 yield 一个聚合版（`Partial=false`，携带完整 `Content`、usage、citation 等）

### 5.4 创建 Apigee 模型
- 入口：`apigee.NewModel(ctx, modelName, opts...)`（`model/apigee/apigee.go:84`）
- 关键步骤：
  1. 应用所有 `Option` 闭包到 `Config`（`apigee.go:88-90`）
  2. 校验 `modelName` 必须以 `apigee/` 开头（`apigee.go:92-94`）
  3. `parseModelName` 解析出 `modelInfo`（`apigee.go:134-175`）：支持 `apigee/<id>` / `apigee/<apiVersion>/<id>` / `apigee/vertex_ai/<id>` / `apigee/gemini/<apiVersion>/<id>` 等 5 种形态
  4. 解析 `proxyURL`：优先用 `Config`，回落到 `APIGEE_PROXY_URL` 环境变量（`apigee.go:177-182`）
  5. Vertex AI 模式下强校验 `GOOGLE_CLOUD_PROJECT` / `GOOGLE_CLOUD_LOCATION`（`apigee.go:219-230`）
  6. 构造 `genai.ClientConfig` 并 `gemini.NewModel` 创建底层 client，把返回的 `geminiModel` 存为 `delegate`（`apigee.go:114-117`）
- 出口：`*apigeeModel`（`apigee.go:119-122`）

### 5.5 Apigee GenerateContent 委派
- 入口：`apigeeModel.GenerateContent(...)`（`model/apigee/apigee.go:130-132`）
- 关键步骤：直接 `m.delegate.GenerateContent(ctx, req, stream)` 透传
- 出口：与 Gemini 实现完全相同的迭代器

## 6. 扩展点

- **`model.LLM` 接口本身**：任何想接入新模型（OpenAI、Anthropic、自研等）只需实现 `Name()` + `GenerateContent(...)` 两个方法。
- **`googlellm.GoogleLLM` 嵌入式接口**（`internal/llminternal/googlellm/variant.go:39-41`）：通过类型断言提供 `GetGoogleLLMVariant()`，让 `agent/llmagent` 区分 Vertex AI / Gemini API 走不同输出 schema 路径（`NeedsOutputSchemaProcessor`，`variant.go:71-76`）。
- **`apigee.Option` 模式**：新增配置只需追加一个 `WithXxx` 闭包，不破坏既有调用方。
- **`apigee.parseModelName`**：是字符串协议层，新增命名空间只需扩展 `components` 长度分支。
- **`genai.ClientConfig`**：透传给 `gemini.NewModel`，理论上可挂载任何 `genai` 客户端选项（如 `APIKey`、`Backend`、`HTTPOptions`）。

## 7. 错误处理

- **未定义独立的错误类型**——`LLM` 接口用 `error` 透传 genai SDK 错误；`LLMResponse.ErrorCode`/`ErrorMessage` 描述**业务级**失败（如 `SAFETY`、`RECITATION`、prompt block），`FinishReason` 也会同步写入。
- **典型失败模式**（来自源码 + 测试）：
  1. 模型名不以 `apigee/` 开头 → `apigee.NewModel` 返回 `fmt.Errorf("invalid model string: %s", cfg.ModelName)`（`apigee.go:92-94, 136-137, 140-141, 171-172`）
  2. `apigee` 模式下未设置 `APIGEE_PROXY_URL` → `apigee.go:101-103`
  3. Vertex AI 模式下缺 `GOOGLE_CLOUD_PROJECT` / `GOOGLE_CLOUD_LOCATION` → `apigee.go:222-227`
  4. `generate` 收到 `len(resp.Candidates) == 0` → `fmt.Errorf("empty response")`（`gemini.go:133-136`，注释带"shouldn't happen?"）
  5. genai 错误包裹为 `fmt.Errorf("failed to call model: %w", err)`（`gemini.go:130-132`）
- `converters.Genai2LLMResponse`（`internal/llminternal/converters/converters.go:23-73`）在三类输入下分别产出"内容响应 / 错误响应 / 空 content 但有 usage 的兜底响应"——针对 Vertex AI 上 gemini-3 早期空 entry 做了特殊处理（注释见 `converters.go:60-67`）。

## 8. 并发与性能

- **无 goroutine / 无锁**——`LLM` 实现都是被动响应迭代器，消费方控制节奏。
- **流式聚合器 `streamingResponseAggregator` 持有可变状态**（`stream_aggregator.go:33-50`）：`currentTextBuffer`、`currentFunctionArgs` 等——但每个 `geminiModel.generateStream` 调用都 `NewStreamingResponseAggregator` 新建一次，**不是**共享对象（`gemini.go:142`），因此可安全并发。
- **版本头一次性计算**：`gemini.go:73-74` 把 `google-adk/<ver> gl-go/<goVer>` 拼好后存进 `versionHeaderValue`，避免每次请求的 `fmt.Sprintf` 开销。
- **HTTP 客户端深拷贝**：`gemini.NewModel` 主动 `cfgCopy := *cfg; clientCopy := *cfg.HTTPClient`（`gemini.go:53-57`）防止调用方与 ADK 互相污染 `Transport` 字段。
- **可优化点**：apigee 模型每次都新建 `genai.Client`；若同一进程需要多个 apigee 模型，未提供 client 复用 API。

## 9. 依赖与被依赖

### 9.1 本模块导入

| 包 | 导入内容 |
|---|---|
| `model`（根） | 标准库 `context` / `iter`，外部 `google.golang.org/genai` |
| `model/gemini` | `google.golang.org/genai`；`internal/llminternal`、`internal/llminternal/converters`、`internal/llminternal/googlellm`、`internal/version`、`model` |
| `model/apigee` | `google.golang.org/genai`；`model`、`model/gemini` |

### 9.2 本模块被哪些模块依赖（grep `adk/model` 排除自身与 `_test.go`，约 59 个非测试文件）

主要消费者（按主题分类）：

- **核心框架**：`agent/agent.go`、`agent/llmagent/llmagent.go`、`runner/runner.go`、`tool/tool.go`、`internal/llminternal/*`（`base_flow.go`、`agent.go`、`contents_processor.go`、各类 `_processor.go` 共 10+ 文件）
- **可观察性 / 插件**：`internal/telemetry/telemetry.go`、`internal/telemetry/logger.go`、`plugin/loggingplugin`、`plugin/functioncallmodifier`、`internal/configurable/conformance/...`
- **会话 / 记忆**：`session/session.go`、`session/database/storage_session.go`、`session/vertexai/vertexai_client.go`、`session/session_test/service_suite.go`
- **工具 / 传输**：`tool/agenttool`、`tool/geminitool`、`tool/functiontool`、`tool/loadmemorytool`、`tool/preloadmemorytool`、`tool/loadartifactstool`、`tool/mcptoolset`、`tool/exampletool`、`tool/skilltoolset`、`server/adkrest/...`、`server/adka2a/v2/...`
- **测试 / 内部工具**：`internal/testutil/test_agent_runner.go`（含 `MockModel`）、`internal/utils/utils.go`
- **示例（examples/）**：约 20+ main.go

`model/gemini` 直接被 import 的位置（约 20 处），全部位于 `examples/` 与少量 agent 测试（`agent/workflowagents/parallelagent/agent_test.go`、`agent/remoteagent/v2/a2a_e2e_test.go`）。
`model/apigee` 在仓库内部**没有**任何非测试 import 记录（截至 d06992e2）。

## 10. 测试与可观察性

### 10.1 测试文件位置

| 文件 | 测试对象 | 手法 |
|------|----------|------|
| `model/llm_test.go` | `converters.Genai2LLMResponse` | 9 个表驱动用例覆盖 logprobs / citation / 错误码 / 无候选 / 部分内容等场景（`llm_test.go:65-208`） |
| `model/gemini/gemini_test.go` | `gemini.NewModel` 行为 | `httprr` 回放（4 个 `.httprr` 文件在 `testdata/`）+ `headerInterceptor`/`roundTripFunc` 桩（`gemini_test.go:329-342`）。覆盖：基本生成、流式生成、Vertex 开关下的遥测头、request-time `Model` 字段覆盖构造时 `name`、`NewModel` 不污染入参 `http.Client` |
| `model/apigee/apigee_test.go` | `apigee.NewModel` + 委派 | 用 `roundTripFunc` 替换 `http.Client.Transport`（`apigee_test.go:35-47`）。覆盖 5 种合法 + 5 种非法 model 名解析、custom headers、缺失 proxy URL、Vertex 缺 project/location、完整 `GenerateContent` 端到端 |

### 10.2 Telemetry 埋点

- `model` 模块本身**不直接**写 telemetry span。
- 遥测头由 `gemini.geminiModel.addHeaders` 在每次请求时写入（`gemini.go:112-115`），`x-goog-api-client` 与 `user-agent` 均带 `google-adk/<version> gl-go/<goVer>`。
- 上层调用方（`internal/telemetry/telemetry.go:99-137`）创建 `generate_content <modelName>` span，记录 `FinishReason`、input/output token 计数、cache read 计数、reasoning token 计数。

## 11. 文档写作提示

### 11.1 必须写清楚

1. **`LLM` 接口的极简契约**：两个方法 + 返回类型 `iter.Seq2[*LLMResponse, error]`，强调"同步与流式统一"的设计取舍（不是 channel、不是 callback）。
2. **`LLMResponse` 的"双通道错误"模型**：业务级失败 → `ErrorCode`/`ErrorMessage`；协议级失败 → `error` 返回值。配合 `converters.Genai2LLMResponse` 一起讲。
3. **`gemini.NewModel` 不会修改入参**（深拷贝 `cfg` 与 `http.Client`）——值得作为"安全使用"小贴士显式说明。
4. **apigee 模型名 DSL**：`apigee/<id>` / `apigee/<v>/<id>` / `apigee/vertex_ai/<id>` / `apigee/gemini/<v>/<id>` / `apigee/vertex_ai/<v>/<id>`。配合 `parseModelName` 的 12 个测试用例。
5. **Vertex 模式的副作用**：`apigee.NewModel` 会读 `GOOGLE_GENAI_USE_VERTEXAI`（默认 false），且 Vertex 模式下强制要求 `GOOGLE_CLOUD_PROJECT` + `GOOGLE_CLOUD_LOCATION`。
6. **遥测头的存在**：每次 Gemini 请求都会带 `google-adk/<version>` 头。

### 11.2 可以省略

- 私有 `modelInfo` 内部表示细节（只需在"apigee 解析"段提一句）。
- `streamingResponseAggregator` 的字段完整列表（实现细节，外部只关心"流式会把 partial + 聚合版都 yield 出来"）。
- `mergeHeadersInterceptor` 的具体拼接逻辑（可一句"避免重复 header 出现多次"带过）。

### 11.3 潜在的坑

1. **`LLMResponse.Partial` 与 `TurnComplete` 的语义不对称**：`Partial` 仅流式用；`TurnComplete` 标记"该候选已给出 `FinishReason`"。写文档时要避免混用。
2. **`LLMRequest.Model` 字段的"双来源"**：构造 `gemini.NewModel` 时传的 `modelName` 是默认值；运行期 `BeforeModelCallback` 可改写 `req.Model` 走 `gemini.go:120-125` 的 `modelName()`。这是个静默的运行时覆盖点。
3. **`LLMRequest.Tools` 是 `map[string]any` 且 `json:"-"`**：表示 ADK 内部用，不参与序列化/反序列化——若需自己 mock 一个 `LLM`，此字段可忽略。
4. **apigee 模型名错配**（比如写 `gemini/...` 而非 `apigee/gemini/...`）会被 `parseModelName` 拒绝；提醒用户使用前看测试用例 `apigee_test.go:50-56`。
5. **`apigee.WithHTTPClient` 注释明确"测试专用"**——生产代码里传入可能破坏 proxy 行为。
6. **仓库内部目前无任何 `apigee` 生产调用点**（仅测试），意味着 apigee 子包是"为外部用户准备"的扩展接入点；写文档时需诚实标注使用率。

### 11.4 建议的章节结构（给后续 doc 写作者参考）

1. 概览（`model` 在 ADK 中的角色）
2. `LLM` 接口契约
3. 数据结构 `LLMRequest` / `LLMResponse` 字段表
4. `gemini` 子包：构造、调用、流式聚合
5. `apigee` 子包：模型名解析、HTTPOptions 装配、委派
6. 扩展指南：如何实现自己的 `LLM`
7. 与 telemetry / 错误处理 / 测试的协作
