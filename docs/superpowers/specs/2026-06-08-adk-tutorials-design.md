# 子项目 12：ADK 入门与上手指南 —— 设计规格

**日期**：2026-06-08
**项目**：`/home/wu/oneone/adk`（`google.golang.org/adk`）
**目标产物**：`/home/wu/oneone/adk/docs/tutorials/` 目录下的 31 个 Markdown 文件（中文），含 29 个教程 + 1 个 prerequisites + 1 个 README
**关联交付物**：可选用 Go 编写的 2 个 LLM 适配器（OpenAI 兼容 + Anthropic）

---

## 1. 背景与目标

### 1.1 为什么做这件事

`/home/wu/oneone/adk/examples/` 下有 15 个子目录、24 个可运行示例，但 `examples/README.md` 仅有 29 行（只讲了 launcher 概念），`examples/bidi/README.md` 也只是简短说明。新读者面对 `examples/quickstart/main.go` 这样的代码无从下手——不知道为什么要用 `full.NewLauncher()`、不知道 `GOOGLE_API_KEY` 怎么获取、不知道如何在业务系统里嵌入 ADK。

本规格解决这个问题：产出一套从环境准备到 LLM 供应商定制的完整教程，**让零 LLM 经验的 Go 开发者 1 小时内跑通第一个 agent**。

### 1.2 文档目标读者

按优先级：

1. **Go 开发者，零 LLM 经验** —— 懂 Go，不懂 Agent / LLM。需要先解释核心概念再上代码。
2. **Go 开发者，已有 LLM 基础**（用过 LangChain / OpenAI API）—— 已懂概念，重点讲 ADK 独有的 API 设计、抽象与其它框架的差异。
3. **偏产品/后端，Go 中等水平** —— 重点是"跑通第一个 agent"和"如何接入业务系统"，不深入 Go 高级特性。

文档采用"分层 + 严格线性入门"组织以服务多类读者：入门层（5 教程）严格线性，进阶层和专题层每个独立但有"前置教程"提示。

### 1.3 与已有交付物、子项目的关系

- **`docs/architecture/`**（子项目 0）—— 本教程的姐妹文档。教程"用"架构文档的解释，但侧重"动手"而非"理解"
- **`examples/`** —— 教程指向 examples/ 中的可运行示例，**不复制代码、不修改源码**
- **后续子项目（1-11 模块深读）** —— 教程末尾的"延伸阅读"指向具体的架构文档章节
- **本文档不写"完善 godoc"**（属于子项目 13+）

---

## 2. 输出结构（文件树）

```
docs/tutorials/
├── README.md                       # 总入口 + 6 大主题索引 + 依赖图
├── 00-prerequisites.md             # Go 环境、API key、Vertex AI 凭证
│
├── 01-getting-started/             # 入门层（严格线性 5 步）
│   ├── 01-hello-world.md
│   ├── 02-first-tool.md
│   ├── 03-persistent-session.md
│   ├── 04-multi-agents.md
│   └── 05-run-as-server.md
│
├── 02-tools/                       # 工具系统（7 教程）
│   ├── 01-functiontool.md
│   ├── 02-agent-as-tool.md
│   ├── 03-mcp-tools.md
│   ├── 04-skill-tools.md
│   ├── 05-confirmation.md
│   ├── 06-load-artifacts.md
│   └── 07-load-memory.md
│
├── 03-agents/                      # 多种 agent 模式（5 教程）
│   ├── 01-workflow-sequential.md
│   ├── 02-workflow-parallel.md
│   ├── 03-workflow-loop.md
│   ├── 04-remote-agent.md
│   └── 05-llm-auditor.md
│
├── 04-deployment/                  # 部署形态（5 教程）
│   ├── 01-rest-server.md
│   ├── 02-a2a-server.md
│   ├── 03-web-ui.md
│   ├── 04-vertexai-agent-engine.md
│   └── 05-bidi-streaming.md
│
├── 05-llm-providers/               # LLM 供应商（5 教程，方案 D）
│   ├── 01-gemini.md
│   ├── 02-apigee-gateway.md
│   ├── 03-openai-compatible.md
│   ├── 04-anthropic.md
│   └── 05-custom-llm-adapter.md
│
└── 06-observability/               # 可观测性（2 教程）
    ├── 01-telemetry.md
    └── 02-debug-endpoint.md
```

**合计**：1 README + 1 prerequisites + 29 教程 = **31 个 Markdown 文件**

### 2.1 命名约定

- 顶层目录：`docs/tutorials/`
- 主题目录：`NN-<topic-kebab>/`（两位数字 + 连字符主题名）
- 教程文件：`<主题目录>/NN-<topic>.md`（目录内两位数字 + 连字符主题名）
- 跨目录引用：相对路径
- 引用架构文档：`../../architecture/00-overview.md`
- 引用 examples/：`../../../examples/quickstart/main.go`

### 2.2 文档规模估计

| 文件类别 | 数量 | 平均行数 | 合计 |
|---|---|---|---|
| README.md | 1 | 100-150 | ~120 |
| 00-prerequisites.md | 1 | 200-300 | ~250 |
| 入门层教程 | 5 | 200-300 | ~1,250 |
| 02-tools/ 教程 | 7 | 200-400 | ~2,100 |
| 03-agents/ 教程 | 5 | 200-400 | ~1,500 |
| 04-deployment/ 教程 | 5 | 250-450 | ~2,000 |
| 05-llm-providers/ 教程 | 5 | 250-400 | ~1,750 |
| 06-observability/ 教程 | 2 | 200-300 | ~500 |
| **合计** | **31** | | **~9,500 行** |

加上 2 个新写的 LLM 适配器（OpenAI/Anthropic 各约 120 行 Go），总计约 9,700 行。

---

## 3. 教程统一模板

每个教程按以下结构（平均 200-400 行）：

```markdown
# <教程名>

## 你将学到
[3-5 条要点，明确学完后能做什么]

## 前置条件
- [ ] 已完成 [教程 N](../path/to/prev.md)（链接）
- [ ] 已设置 [环境/凭证]
- [ ] [其他具体条件]

## 核心概念
[1-2 段：先讲清 1-2 个新概念]
[如："什么是 Tool" / "什么是 Session 持久化"]

## 完整代码
[展示 examples/<dir>/main.go 完整内容 + file:line 引用]
[代码块前后说明该示例在 examples/ 哪个目录、关键文件]

## 代码逐段讲解
### 1. <段名>
[代码片段 + 3-5 行解释]
[每个新 API 标注 file:line]

### 2. <段名>
[同上]

### 3. ...

## 准备与运行
### 步骤 1：获取凭证
[如适用：GOOGLE_API_KEY / GCP 服务账号]

### 步骤 2：设置环境变量
\`\`\`bash
export GOOGLE_API_KEY=...
\`\`\`

### 步骤 3：运行
\`\`\`bash
cd examples/<dir>
go run . console      # 或 a2a / restapi / webui
\`\`\`

### 步骤 4：测试输入
[给 1-2 个示例输入，预期输出]

## 常见错误
[3-5 个典型错误 + 解决方法]
- `xxx: yyy` —— 原因：...，解决：...

## 关键 API 小结
[2-3 行表格：本教程涉及的所有 API 一览]

## 延伸阅读
- [架构文档对应章节](../../architecture/01-core-flows.md#f1)
- [相关子项目深读占位]
- [ADK 官方文档链接]
```

---

## 4. 各目录内容大纲

### 4.1 `00-prerequisites.md` —— 前置条件（约 200-300 行）

```markdown
# 前置条件

## 1. 环境要求
- Go 1.25+ 
- 操作系统：macOS / Linux / Windows
- 网络：能访问 Google API

## 2. 安装 Go
[3 平台的安装指引]

## 3. 获取 Google API Key
- 步骤 1：访问 Google AI Studio 创建 API key
- 步骤 2：设置环境变量

## 4. （可选）Vertex AI 凭证
- 适用场景：使用 model/apigee、vertexai 示例、agentengine 部署

## 5. 克隆 ADK 仓库

## 6. 验证安装
\`\`\`bash
go version
go run ./examples/quickstart help
\`\`\`
```

### 4.2 `01-getting-started/` 入门层（5 教程，严格线性）

| 教程 | 对应 examples/ | 关键概念 |
|---|---|---|
| 01-hello-world | `quickstart/` | 最小 Agent、launcher、console 模式 |
| 02-first-tool | `tools/multipletools/` | Tool 接口、Tools 列表、FunctionTool |
| 03-persistent-session | （基于 runner + session/inmemory） | Session、Event、appendEvent |
| 04-multi-agents | `workflowagents/sequential/` | SubAgents、workflow agents |
| 05-run-as-server | `rest/` | REST API、HTTP server、SSE |

### 4.3 `02-tools/` 工具系统（7 教程）

| 教程 | 对应 examples/ | 关键概念 |
|---|---|---|
| 01-functiontool | `tools/multipletools/` | functiontool 装饰器、struct/map 入参 |
| 02-agent-as-tool | `agenttool` 路径在源码 | agenttool 把 agent 暴露为 tool |
| 03-mcp-tools | `mcp/` | MCPClient、Transport、lazy 连接 |
| 04-skill-tools | `skills/` | skill source 4 种实现、SKILL.md |
| 05-confirmation | `toolconfirmation/` | HITL、WithConfirmation 装饰器 |
| 06-load-artifacts | `tools/loadartifacts/` | artifact service、load_artifacts_tool |
| 07-load-memory | `tools/loadmemory/` 与 `preloadmemory` | memory service、检索与预加载 |

### 4.4 `03-agents/` agent 模式（5 教程）

| 教程 | 对应 examples/ | 关键概念 |
|---|---|---|
| 01-workflow-sequential | `workflowagents/sequential/` | SequentialAgent、OutputKey |
| 02-workflow-parallel | `workflowagents/parallel/` | ParallelAgent、ack backpressure |
| 03-workflow-loop | `workflowagents/loop/` | LoopAgent、MaxIterations |
| 04-remote-agent | `a2a/main.go` 客户端视角 | RemoteAgent、partial 聚合 |
| 05-llm-auditor | `web/agents/llmauditor.go` | 复杂 LLM-as-judge 模式 |

### 4.5 `04-deployment/` 部署形态（5 教程）

| 教程 | 对应 examples/ | 关键概念 |
|---|---|---|
| 01-rest-server | `rest/` | controllers/runtime、REST 路由 |
| 02-a2a-server | `a2a/main.go` 服务端 | Executor.Execute、adka2a/v2 |
| 03-web-ui | `web/` | webui launcher、React 前端 |
| 04-vertexai-agent-engine | `agentengine/` 与 `vertexai/vertexengine/` | MethodHandler、class_method 路由 |
| 05-bidi-streaming | `bidi/main.go` | 双向流、SSE/WebSocket |

### 4.6 `05-llm-providers/` LLM 供应商（5 教程，方案 D）

| 教程 | 关键概念 |
|---|---|
| 01-gemini | `model/gemini.NewModel`、APIKey、ModelName |
| 02-apigee-gateway | `model/apigee.NewModel`、网关配置、多后端路由 |
| 03-openai-compatible | **新写** `model/openaiadapter` 适配器示例，接 DeepSeek/Moonshot/Ollama |
| 04-anthropic | **新写** `model/anthropicadapter` 适配器示例，接 Claude |
| 05-custom-llm-adapter | 通用指南：实现 `model.LLM` 接口的 2 个方法，处理 tool calling、流式输出 |

> 教程 03/04 需要提供可工作的 Go 适配器代码（每个约 80-150 行）。可放在 `examples/openaiadapter/main.go` 与 `examples/anthropicadapter/main.go`。
>
> 教程 05 是接口讲解 + 极简骨架（不写完整实现）。

### 4.7 `06-observability/` 可观测性（2 教程）

| 教程 | 对应 examples/ | 关键概念 |
|---|---|---|
| 01-telemetry | `telemetry/` | OpenTelemetry、Span、Exporter |
| 02-debug-endpoint | （基于 `adkrest/controllers/debug`） | debug controller、agent graph |

### 4.8 `README.md` 顶层入口

```markdown
# ADK 入门与上手指南

## 适用读者
- Go 开发者，零 LLM 经验
- Go 开发者，已有 LLM 基础
- 偏产品/后端，Go 中等水平

## 推荐学习路径（首次）
1. 阅读 prerequisites
2. 按顺序完成 01-getting-started/ 全部 5 个教程
3. 按需跳读其他目录

## 6 大主题

[目录树 + 每目录 1 行说明]

## 教程依赖图

[Mermaid graph: 教程之间的前置关系]

## 常见问题
[指向 FAQ 或最常见的 3-5 个问题答案]

## 维护说明
- 基于 commit <SHA> 锁定
- 与 docs/architecture/ 互引
```

---

## 5. 跨切面规范

### 5.1 链接与互引

| 引用类型 | 形式 | 示例 |
|---|---|---|
| 引用同目录另一教程 | 相对路径 | `[前一教程](./01-hello-world.md)` |
| 引用跨目录教程 | `../` 相对路径 | `[工具教程](../02-tools/01-functiontool.md)` |
| 引用架构文档 | `../../architecture/...` | `[架构文档 F1](../../architecture/01-core-flows.md#f1)` |
| 引用 examples/ 源码 | 仓库根相对 | `[examples/quickstart/main.go](../../../examples/quickstart/main.go)` |
| 引用 Go 源文件 | `path:line` 形式 | `agent/agent.go:43` |

### 5.2 教程代码自洽性

由于严格线性（每教程从 0 开始复制前一个的代码继续扩展），教程代码必须：
- 第 1 个教程有完整 `package main` 与 `import`
- 后续教程基于前一个教程的代码，开头说明"在 [前一教程] 基础上，增加以下内容"
- 不依赖教程外的私有代码

但**教程最终指向 examples/ 中的可运行版本**（更完整）。

### 5.3 通用"准备与运行"四段式

每个教程都按以下顺序写运行指引：

1. **获取凭证**（如适用）
2. **设置环境变量**（`export ...`）
3. **运行命令**（`go run .`）
4. **测试输入**（预期输出片段）

### 5.4 通用"常见错误"覆盖

每个教程至少包含以下 3 类错误：

1. **环境/凭证类**（API key 缺失、项目未启用 API）
2. **编译类**（`go.mod` 未拉依赖、版本不匹配）
3. **运行时类**（工具超时、模型拒绝、session 写入失败）

### 5.5 截图与图示

- **不**做真实截图（环境依赖太重）
- 用 Mermaid 图代替（架构图、状态机、时序图）
- 流程图必须"画图指引"段

### 5.6 锁定与漂移

- 教程基于 commit `d06992e2b1ec2c9b95c6070e0fd12d50a43e4c99`（与架构文档同）
- examples/ 源码如变更，应同步修改教程
- 04-deployment 类教程可能随 server 协议变更漂移较快，README 标注"高漂移风险"

---

## 6. 验证方式

| 验证项 | 范围 | 方法 |
|---|---|---|
| Markdown 语法 | 全部 31 个文件 | 人工目检（不引入 lint 工具） |
| 内部链接 | 全部 | grep 抽链接 + 文件存在性检查 |
| 教程间依赖 | 全部 | 验证每个教程"前置条件"指向真实存在的上一教程 |
| 代码引用 | 全部 | 验证 `file:line` 引用真实 |
| `go build` 编译 | examples/ 引用部分 | `cd examples/<dir> && go build` |
| `go vet` 检查 | examples/ 引用部分 | `go vet ./...` |
| 实际 `go run` | 仅 01-getting-started/ 入门层 5 个的 `help` 子命令 | 在 sandbox 中跑通 |
| API 一致性 | 全部 | 与 `docs/architecture/02-extension-points.md` 对比 |

> 由于 sandbox 无 GOOGLE_API_KEY，**实际 go run 只跑无 API key 也能编译的命令**（如 `go run . help`）。需要 API key 的命令仅验证"编译通过"。

---

## 7. 验收标准

1. **完整性**：31 个目标文件全部存在，无空文件或纯占位文件
2. **可读性**：通过 README + 依赖图，1 小时内能让"零 LLM 经验 Go 开发者"跑通第一个 agent
3. **可运行**：每个教程的 examples/ 引用 `go build` 通过；入门层 5 个 `go run . help` 实际可执行
4. **可验证**：所有跨文档链接、教程间链接有效；`file:line` 引用准确
5. **教学性**：每个教程遵循"概念 → 完整代码 → 逐段讲解 → 准备与运行 → 常见错误"结构
6. **多 LLM 覆盖**：05-llm-providers/ 5 个教程覆盖 gemini + apigee + openai-compatible + anthropic + 自定义
7. **维护友好**：README 含锁定 commit、依赖图、维护说明
8. **与架构文档一致**：教程引用的 API 与 `docs/architecture/` 描述一致

---

## 8. 工作计划

### 8.1 写作流程

1. **Phase A：基础设施**
   - 写 `00-prerequisites.md`
   - 写顶层 `README.md`（含 Mermaid 依赖图）
   - 创建目录结构（`mkdir -p` 6 个子目录）

2. **Phase B：入门层**（5 个子代理并行）
   - 01-getting-started/01-05

3. **Phase C：5 个专题层并行**（5 个工作流）
   - 02-tools/（7 教程）
   - 03-agents/（5 教程）
   - 04-deployment/（5 教程）
   - 05-llm-providers/（5 教程，含 2 个新写的 adapter 示例）
   - 06-observability/（2 教程）

4. **Phase D：验证**
   - 跨教程链接 + 依赖图校验
   - examples/ 引用编译验证
   - 入门层 5 个实跑
   - 最终审查 + commit

### 8.2 工作量估计

- **基础设施**：~30 分钟
- **29 个教程 × ~250 行 ≈ 7,250 行 Markdown** + 2 个新 adapter 适配器（每个 ~120 行 Go）
- **预计**：与子项目 0 类似规模（30+ 子代理，~1.5-2.5M token，~60-90 分钟墙钟）

### 8.3 关键风险

| 风险 | 应对 |
|---|---|
| 教程代码与源码漂移 | 严格基于 commit 锁定；如发现漂移，记录到 README "已知漂移"小节 |
| 新写 2 个 adapter 适配器工作量 | 只做"够用"版本（OpenAI/Anthropic 各 ~120 行），不强求 streaming/tool calling 完美 |
| 无 API key 无法真跑 | 实跑仅 5 个入门教程的 `help` 命令；其余仅编译验证 |
| 跨目录链接维护 | 集中用相对路径 + 在 README 一次性生成依赖图 |

---

## 9. 范围之外

明确**不做**的事项，避免范围蔓延：

- 不写子项目 1-11（11 个模块深读文档）的具体内容
- 不写"完善 godoc"（属于子项目 13+）
- 不做实际运行示例（仅做 `go build` 验证；`go run . help` 入门层 5 个）
- 不修改 examples/ 源码
- 不写国际化版本（仅中文）
- 教程代码不强求与 examples/ 完全一致——可以更精简以教学

---

## 10. 参考

- 头脑风暴技能：`superpowers:brainstorming`
- 后续过渡技能：`superpowers:writing-plans`（将本规格转成可执行计划）
- 姐妹文档：`docs/architecture/`（子项目 0）
- examples/ 目录：24 个 .go 文件
- 锁定 commit：`d06992e2b1ec2c9b95c6070e0fd12d50a43e4c99`
