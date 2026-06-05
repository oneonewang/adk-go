# 子项目 0：ADK 架构与设计文档 —— 设计规格

**日期**：2026-06-05
**项目**：`/home/wu/oneone/adk`（`google.golang.org/adk`）
**目标产物**：`/home/wu/oneone/adk/docs/architecture/` 目录下的多文件 Markdown 架构文档（中文 + Mermaid 图表 + Go 源码引用）

---

## 1. 背景与目标

### 1.1 为什么做这件事

ADK 是 Google 开源的 Agent Development Kit 框架，Go 编写，规模较大（11 个顶层模块、100+ 个 .go 文件）。现有的顶层文档只有 `README.md`（53 行）和 `CONTRIBUTING.md`（142 行），对"整体怎么组织、各模块怎么协作、扩展点在哪"几乎没有描述。本项目填补这一空白。

### 1.2 文档目标读者

按优先级：

1. **架构师 / Tech Lead** —— 评估技术选型、决定是否在自家系统采用 ADK。要看关键设计决策的 trade-off、依赖边界、扩展性与锁定风险。
2. **已有 ADK 使用经验、要二次开发或扩展的工程师** —— 已会用，要弄清内部机制以便接入 plugin、自定义 agent / tool / model、定制 session 后端等。
3. **ADK 新贡献者** —— 想快速理解项目怎么组织的，刚 clone 下来还不熟悉。

文档采用"分层组织"以服务多类读者：先 30 秒摘要，再分章节深入；后两类读者可从对应章节切入。

### 1.3 与后续子项目的关系

本规格是 12 个子项目（架构 + 11 模块深读 + internal 附录）中的"**子项目 0**"。后续子项目 1-11 将对每个模块做"独立、完整、可发布的深读文档"。本规格的 `03-modules/` 子文件是那些深读文档的"前菜 / 占位 / 索引"，未来可被替换或与子项目深读文档双向链接。

---

## 2. 输出结构（文件树）

```
docs/architecture/
├── README.md                  # 30 秒摘要 + 三条阅读路径（新人 / 工程师 / 架构师）
├── 00-overview.md             # 顶层架构：模块依赖图、整体数据流、核心抽象
├── 01-core-flows.md           # 端到端核心流程（5 个）
├── 02-extension-points.md     # 扩展点：plugin / 自定义 agent/tool/model/session/server
├── 03-modules/                # 11 个模块详情（子目录）
│   ├── 01-agent.md            #  含 llmagent / remoteagent / workflowagents
│   ├── 02-model.md            #  含 apigee / gemini
│   ├── 03-tool.md             #  含 11 个子工具
│   ├── 04-runner.md
│   ├── 05-session.md          #  含 database / vertexai
│   ├── 06-artifact.md         #  含 gcsartifact
│   ├── 07-memory.md           #  含 vertexai
│   ├── 08-plugin.md           #  含 functioncallmodifier / loggingplugin / retryandreflect
│   ├── 09-telemetry.md
│   ├── 10-server.md           #  含 adka2a / adkrest / agentengine
│   └── 11-internal.md         #  按子包分组，浅深
└── 04-appendix.md             # 术语表、参考链接、阅读建议、文档维护说明
```

### 2.1 命名约定

- 文件名采用两位数字前缀 + 连字符命名（`00-overview.md`、`01-core-flows.md`），保证目录排序与"先整体后细节"的阅读顺序一致。
- `03-modules/` 子文件用两位数字编号，与子项目 1-11 拆解一一对应。
- 所有 Mermaid 图用 ```` ```mermaid ```` 代码块包裹，便于在 GitHub / VS Code 直接渲染。
- 引用 Go 源文件使用 `` `path/to/file.go:line` `` 形式，可点击。

### 2.2 文档规模估计

| 文件 | 估计页数 |
|---|---|
| `README.md` | 1-2 页（~300-500 行） |
| `00-overview.md` | 3-4 页 |
| `01-core-flows.md` | 5-7 页（5 个流程，每个 1-1.5 页） |
| `02-extension-points.md` | 3-4 页 |
| `03-modules/01-agent.md` ~ `05-session.md`（Phase 1 核心 5 个） | 每个 3-5 页 |
| `03-modules/06-artifact.md` ~ `10-server.md`（Phase 2+3 六个） | 每个 2-3 页 |
| `03-modules/11-internal.md` | 1-2 页（附录式） |
| `04-appendix.md` | 1-2 页 |
| **合计** | **约 30-45 页 Markdown** |

这是一个"中等深度的技术手册"。

---

## 3. 各文件内容大纲

### 3.1 README.md —— 入口与阅读路径

**目标**：让读者在 30 秒内知道"ADK 是什么、我该读哪一章"。

```markdown
# ADK 架构与设计文档

## 一句话定位
[ADK 是 Google 开源的 Agent Development Kit 框架，……]

## 30 秒速览
[一段 3-5 句的简介：项目目标、核心能力、典型使用场景]

## 模块鸟瞰（一图）
[Mermaid graph TB: 11 个顶层模块及其依赖关系]

## 三条阅读路径
### 路径 A：我是新贡献者
1. 00-overview.md（必读）
2. 01-core-flows.md 中 F1 单轮对话、F2 工具调用
3. 03-modules/01-agent.md

### 路径 B：我要扩展 / 二次开发
1. 00-overview.md
2. 02-extension-points.md（**必读**）
3. 01-core-flows.md 中相关的流程
4. 03-modules/ 中对应模块

### 路径 C：我在做技术选型评估
1. 00-overview.md 中"设计目标"与"非目标"小节
2. 01-core-flows.md（了解运行时行为）
3. 02-extension-points.md（了解扩展性与锁定风险）
4. 04-appendix.md 中"对比参考"小节（若有）

## 文档地图
[目录树]

## 维护说明
- 本文档基于 commit <SHA>，未来更新请同步修改 README 顶部的版本号
- 每章的"延伸阅读"指向子项目深读文档（未来补充）
```

### 3.2 00-overview.md —— 顶层架构

**目标**：让读者建立"ADK 整体怎么组织"的心智模型。

```markdown
# 顶层架构

## 1. 项目目标与非目标
- 目标：让开发者以最小成本构建可生产化的 AI Agent
- 非目标：……（避免读者期望错位）

## 2. 模块全景图
[Mermaid graph TB: 11 个顶层模块的依赖图]
[Mermaid graph LR: 关键子模块（如 llmagent / workflowagents / gemini / mcptoolset 等）]

## 3. 核心抽象一览
四个最关键的接口/类型（每个 1-2 段）：
- agent.Agent —— "Agent 是什么"
- runner.Runner —— "执行入口是什么"
- tool.Tool —— "工具是什么"
- session.Session / session.Service —— "会话怎么存"

## 4. 端到端数据流（高层版）
[Mermaid sequenceDiagram: 用户 → CLI/server → Runner → Agent → Model/Tool/Session]
本节给"鸟瞰式"时序图；详细流程在 01-core-flows.md。

## 5. 一段代码看完所有抽象
[选 examples/ 中最短的 1 个示例（10-30 行），逐行注释每个抽象的角色]
[代码块带 path:line 引用]

## 6. 依赖与包边界
[Mermaid graph: 顶层模块的导入关系]
[关键约束：哪些模块禁止导入哪些模块（架构"规矩"）]

## 7. 并发模型
[一段话：哪些操作是并发的、哪些是串行的、哪些用 goroutine、是否有全局状态]

## 8. 错误处理与可观测性总览
- 错误类型分层（参数错误 vs 内部错误 vs 外部依赖错误）
- telemetry 的角色（埋点、日志、追踪）

## 9. 设计模式与架构风格
[点名 ADK 用了哪些常见模式：组合优于继承、接口隔离、依赖注入、回调链、策略模式（tool/model/session 后端可替换）、观察者（plugin 钩子）]
```

每个 Mermaid 图后都跟 1 段 2-4 行的解读，告诉读者"看这个图要关注什么"。

### 3.3 01-core-flows.md —— 5 个端到端流程

每个流程统一结构：

```markdown
## F<n>.<name>

### 场景与触发
[一段话描述：什么场景、入口在哪]

### 时序图
[Mermaid sequenceDiagram,包含所有相关模块]
[图后 2-3 行解读：重点关注哪几个 hop]

### 关键步骤详解
1. 步骤 1 —— 涉及的文件:函数
2. 步骤 2 —— …

### 状态变化
[该流程中 session、artifact、memory、telemetry 的状态变化]

### 错误路径
[典型失败模式：超时、工具执行失败、模型拒绝、session 写入失败等]

### 延伸阅读
[指向 03-modules/ 中相关模块]
```

| 编号 | 流程 | 入口 | 核心路径 | 关键文件 |
|---|---|---|---|---|
| F1 | 单轮对话 | `runner.Runner.Run` | Runner → Agent → Model → 输出聚合 → 写 Session | `runner/runner.go`、`agent/llmagent`、`model/llm.go` |
| F2 | 工具调用 | F1 中 Model 返回 `tool_calls` | Model → Runner 解析 → Tool.Run → 结果回灌 Model → 终态 | `tool/tool.go`、各 `tool/<sub>/<sub>.go` |
| F3 | 多 Agent 协作 | 父 Agent 配置 `sub_agents` 或 `agenttool` | 父 Agent 决策 → 委派子 Agent → 子 Agent 完成循环 → 结果回灌 | `agent/agent.go`、`agent/workflowagents/*`、`tool/agenttool/*` |
| F4 | 长会话与 Session 持久化 | 多轮输入 / 从 SessionID 恢复 | Session.Service.Get/Append/Close → Backend 存储 → 历史截断与送入 Model | `session/service.go`、`session/session.go`、`session/inmemory.go`、`session/database/*`、`session/vertexai/*` |
| F5 | Live 双向流 | `agent/live.go` 的 `RunLive` 系列 | WebSocket / SSE 长连接 → 流式输入输出 → 中断与接管 | `agent/live.go`、`runner/runner.go` 中 Live 相关入口、`server/adkrest`、`server/adka2a` |

F3 特别要点：workflowagents（Sequential / Parallel / Loop）如何编排子 Agent；Agent-as-Tool 与真正子 Agent 的语义差异；嵌套深度与会话共享。

F4 特别要点：事件流（Event）与 Session 状态的关系；上下文窗口管理（截断、压缩、summary）；多 Backend 差异与切换。

F5 特别要点：与 F1（请求-响应）的本质差异；中断（用户打断）如何处理；server 如何把 Live 暴露为 HTTP / A2A 协议。

### 3.4 02-extension-points.md —— 扩展点

**目标**：让读者清楚"我想自定义 X，应该改 / 实现哪里"。

```markdown
# 扩展点

## 1. 总览：可扩展面
[Mermaid graph: 8 个扩展面（agent / tool / model / session / artifact / memory / plugin / server）]
[每个面用 1 行说明"扩展什么、为什么"]

## 2. 写一个自定义 Agent
- 实现 agent.Agent 接口
- 典型场景：自定义决策逻辑、外部状态机
- 代码骨架 + 路径引用

## 3. 写一个自定义 Tool
- 实现 tool.Tool 接口
- 3 种推荐实现路径：直接实现、functiontool 包装、agenttool 包装
- tool.Context 的能力与限制

## 4. 接入自定义 Model
- 实现 model.LLM 接口
- 已有 model/apigee / model/gemini 作为参考实现
- 流式输出、tool calling、structured output 的协议适配

## 5. 接入自定义 Session Backend
- 实现 session.Service 接口
- 已有 session/inmemory / database / vertexai 作为参考
- 关键约束：Append 原子性、事件顺序、并发安全

## 6. 接入自定义 Artifact / Memory Backend
- 与 Session 类似，更轻量

## 7. 写一个 Plugin
- plugin.Plugin 接口的钩子（按 before/after 模型整理）
- 已有的 3 个参考实现：loggingplugin / functioncallmodifier / retryandreflect
- plugin_manager 的注册与生命周期

## 8. 暴露为自定义 Server
- 已有 server/adkrest / adka2a / agentengine
- 如何选择 / 改造 / 新增协议

## 9. 扩展的"边界"与禁忌
- 哪些是不应该被覆盖的（如 internal 子包）
- 升级时的兼容性策略
```

### 3.5 03-modules/ —— 模块详情统一模板

每个模块详情文件采用以下统一结构（约 2-5 页）：

```markdown
# <模块名> 模块

## 1. 定位与边界
- 一句话定位
- 包含哪些子包、各自负责什么
- 在整体架构中的位置（依赖谁、被谁依赖）
- 哪些代码属于"公共契约"、哪些是"内部实现"

## 2. 核心接口与类型
[代码片段：每个核心接口/类型的定义、文件位置、关键方法]
[每个接口后 1-2 段说明"它代表什么抽象"]

## 3. 关键数据结构
[表格或代码块：关键 struct 字段含义]
[状态机、生命周期相关的类型重点讲]

## 4. 关键流程
[本模块内部 2-4 个最常见流程，每个配 Mermaid 图]
[不重复 01-core-flows.md 中的端到端流程；这里讲模块内部细节]

## 5. 扩展点
[与 02-extension-points.md 中对应小节交叉引用]
[本模块特有的扩展方式]

## 6. 错误处理
[本模块定义的错误类型]
[典型失败模式与处理建议]

## 7. 并发与性能考量
[是否有 goroutine、锁、全局状态]
[已知性能瓶颈或调优点]

## 8. 依赖与被依赖
[Mermaid graph: 本模块的导入与被导入关系]
[关键上游/下游]

## 9. 测试与可观察性
[本模块的核心测试文件位置]
[telemetry 埋点位置]
[集成测试入口]

## 10. 延伸阅读
[指向 01-core-flows.md 中相关流程]
[指向 02-extension-points.md 中相关小节]
[指向未来子项目深读文档占位]
```

**深度差异化**：
- Phase 1 的 5 个核心模块（agent / model / tool / runner / session）按完整模板写。
- Phase 2 + 3 的 6 个模块（artifact / memory / plugin / telemetry / server / internal）适当精简：可省略第 4 节"关键流程"中部分冗余内容，第 7-9 节合并或简化。

### 3.6 04-appendix.md —— 附录

```markdown
# 附录

## A.1 术语表
[中英对照 30-50 个关键术语]
[如：Agent / Runner / Session / Event / Tool / Plugin / Sub-Agent / Live ……]

## A.2 关键文件索引
[按字母序列出最重要的 ~30-50 个 .go 文件，每个 1 行说明]

## A.3 与外部生态的对比（可选，未来补充）
[LangChain / LlamaIndex / CrewAI / AutoGen 等的差异点 —— 仅当有可信来源时]

## A.4 进一步阅读
- ADK 官方文档链接
- 相关 RFC / 设计文档链接（若有）
- 关键 issue / PR 链接

## A.5 文档维护说明
- 如何更新本文档（与代码同步的约定）
- 谁负责审阅
- 版本号策略
- 已知缺口 / TODO
```

---

## 4. 关键技术决策

| 决策 | 选择 | 理由 |
|---|---|---|
| 文档语言 | 简体中文（代码标识符、命令、文件路径保留原文） | 用户全程中文交流 |
| 图表 | 全部 Mermaid | 文本格式、GitHub/VS Code 渲染友好、易维护 |
| 文件组织 | 目录 + 顶层 README 索引 | 兼顾通读与跳读 |
| 覆盖深度 | 11 个顶层模块全深读 | 用户选择"深而全" |
| 源码阅读 | 全量阅读 100+ 个 .go 文件 | 用户选择"深而全"模式 C |
| 端到端流程 | 集中放 01-core-flows.md，模块内不重复 | 避免跨模块流程被切断 |
| 错误处理 | 每个模块详情都写"错误路径"小节 | 跨模块流程也单列"错误路径" |
| 测试覆盖 | 文档中标记测试文件位置与 telemetry 埋点 | 帮读者快速验证理解 |

---

## 5. 验收标准

判断"子项目 0 交付物"合格的标准：

1. **完整性**：12 个目标文件全部存在，无空文件或纯占位文件。
2. **准确性**：每个 Mermaid 图、关键代码引用、数据流描述在源码中存在且当前 commit 有效（基于 commit SHA 锁定）。
3. **可读性**：
   - 30 秒速览（README 摘录）能让未读过代码的人说出"ADK 是什么"
   - 路径 A / B / C 三条阅读路径在 1 小时内能让对应角色完成目标
4. **可验证**：每章的"延伸阅读"链接有效；所有 `file:line` 引用能跳转到对应代码
5. **图表质量**：所有 Mermaid 图渲染正确，复杂图配有文字解读
6. **不重复**：跨模块流程集中在 01-core-flows.md；模块内部细节在 03-modules/。无大段内容重复。
7. **维护友好**：附录含"如何更新本文档"小节，作者或他人后续可基于本文档增量更新

---

## 6. 工作计划

### 6.1 写作流程

1. 一次性读取所有目标源码（Phase 1 核心 5 模块 + Phase 2+3 其余 6 模块 + internal 子包关键文件）
2. 边读边在 `docs/architecture/` 写入各文件
3. 全部写完后做一次自检：图渲染、引用准确、路径有效

### 6.2 工作量估计

- **源码阅读**：~100 个 .go 文件，重点精读 ~40 个，其余浏览。预计耗时占整体 30-40%
- **文档写作**：~30-45 页 Markdown。预计耗时占整体 50-60%
- **自检与修订**：~10%

### 6.3 关键风险与应对

| 风险 | 应对 |
|---|---|
| 源码量大（100+ 文件），一次性读不完 | 分批读取，每批聚焦一个模块；写完一个模块再读下一个 |
| internal 子包与上层模块可能存在循环依赖 | 写作时优先按"自顶向下"叙述，internal 子包放最后做附录 |
| Live / A2A / AgentEngine 等较新功能可能源码注释不全 | 必要时通过 `examples/` 中相关示例推断行为，并在文档中标"基于示例推断" |
| 跨模块流程涉及多文件，难以追踪 | 以 `runner.Runner.Run` 为主线，借助 grep / 跳转定位 |
| 文档写完后与代码可能漂移 | 在 README 与附录明确"基于 commit SHA 锁定"，并写明更新流程 |

---

## 7. 范围之外

明确**不做**的事项，避免范围蔓延：

- 不写子项目 1-11（11 个模块深读）的具体内容（它们是后续独立子项目）
- 不写"上手指南 / 入门教程"（属于子项目 12+）
- 不写"完善现有 godoc / README / CONTRIBUTING"（属于子项目 12+）
- 不做代码修改（只产出文档）
- 不做实际运行示例（只读源码与 examples/ 目录）

---

## 8. 参考

- 头脑风暴技能：`superpowers:brainstorming`
- 后续过渡技能：`superpowers:writing-plans`（将本规格转成可执行计划）
- 上游项目：`https://github.com/google/adk-go`（如该仓库公开可访问；否则基于本地 `/home/wu/oneone/adk`）
