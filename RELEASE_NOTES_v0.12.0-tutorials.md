# ADK 文档发布说明 · v0.12.0-tutorials

**发布日期**：2026-06-09
**关联 commit**：`3982e94` (main HEAD)
**Tag**：`v0.12.0-tutorials` (annotated)
**Release branch**：`release/v0.12.0-tutorials`

---

## 本次发布内容

本次发布包含两个子项目的文档成果，合并后形成完整的 ADK 中文文档体系：

| 子项目 | 目录 | 文件数 | 行数 |
|---|---|---|---|
| 子项目 0 · 架构与设计文档 | `docs/architecture/` | 16 | 5,514 |
| 子项目 12 · 入门与上手指南 | `docs/tutorials/` | 31 | 9,692 |
| **合计** | | **47** | **15,206** |

附加交付：4 个新 Go LLM 示例 + 2 个设计规格 + 2 个实施计划。

## 一、新增文档（docs/）

### `docs/architecture/`（子项目 0）

- `README.md` — 入口 + 三条阅读路径
- `00-overview.md` — 顶层架构（模块依赖图、整体数据流、核心抽象）
- `01-core-flows.md` — 5 个端到端流程（F1-F5）
- `02-extension-points.md` — 8 个扩展面（agent / tool / model / session / artifact / memory / plugin / server）
- `04-appendix.md` — 术语表（67 条）+ 关键文件索引（80+ 个 .go 文件）
- `03-modules/01-agent.md` — `03-modules/02-model.md` — ... `03-modules/11-internal.md` — 11 个模块详情

### `docs/tutorials/`（子项目 12）

- `README.md` — 入口 + Mermaid 依赖图（30 节点）
- `00-prerequisites.md` — Go 环境、API key、Vertex AI 凭证
- `01-getting-started/01-...05-...md` — 5 个严格线性入门教程
- `02-tools/01-...07-...md` — 7 个工具系统教程
- `03-agents/01-...05-...md` — 5 个多 agent 模式教程
- `04-deployment/01-...05-...md` — 5 个部署形态教程
- `05-llm-providers/01-...05-...md` — 5 个 LLM 供应商教程
- `06-observability/01-...02-...md` — 2 个可观测性教程

## 二、新增 Go 示例（examples/）

| 示例 | 用途 | 来源 |
|---|---|---|
| `examples/openaiadapter/main.go` | 适配 OpenAI 兼容 API（DeepSeek / Moonshot / Ollama 等） | 本次新增 |
| `examples/anthropicadapter/main.go` | 适配 Anthropic Claude API | 本次新增 |
| `examples/persistent_session/main.go` | 多轮对话 + in-memory session 持久化 | 本次新增 |
| `examples/debug_endpoint/main.go` | adkrest.Server + OpenTelemetry 接入 | 本次新增 |

所有 4 个新示例通过 `go build ./examples/<dir>/...` 验证。

## 三、新增规格与计划

- `docs/superpowers/specs/2026-06-05-adk-architecture-design.md` — 架构文档设计规格（418 行）
- `docs/superpowers/specs/2026-06-08-adk-tutorials-design.md` — 教程设计规格（451 行）
- `docs/superpowers/plans/2026-06-05-adk-architecture.md` — 架构文档实施计划（1357 行）
- `docs/superpowers/plans/2026-06-08-adk-tutorials.md` — 教程实施计划（981 行）

## 四、质量指标

| 指标 | 数值 | 说明 |
|---|---|---|
| Mermaid 图 | 36 | tutorials 100% 围栏/类型/语法合规 |
| `file:line` 引用 | 832 | tutorials 97.6% 命中真实源文件 |
| 交叉链接 | 248 | tutorials 100% 有效（73 原始断裂已全部修复） |
| 占位符残留 | 0 | tutorials 仅有受控"已知缺口"标记 |
| `go build` | 全过 | 16 个 examples/ 子目录 + 4 新 adapter 编译通过 |

## 五、与上游关系

- 上游：`https://github.com/google/adk-go`（提交时本地 `origin` 指向 `oneonewang/adk-go` 派生仓库）
- 当前 main 相对 origin/main：**41 提交 ahead，19 提交 behind**
- 19 个 behind commits 是上游 `google/adk-go` 在我们开发期间的新提交
- 合并策略建议：
  1. `git fetch origin` 拉取最新
  2. `git rebase origin/main`（推荐）或 `git merge origin/main` 解决冲突
  3. 解决冲突后 `git push origin main`

## 六、发布操作

发布 tag 已在本地创建：

```bash
$ git tag -l
v0.12.0-tutorials
```

Release 分支已创建：

```bash
$ git branch -l
* main
  release/v0.12.0-tutorials
```

Patch 文件已生成（1.28 MB，41 commits）：

```
/tmp/release-v0.12.0-tutorials.patch
```

## 七、推送前手动操作（用户授权后执行）

> ⚠️ 推送直接到 `origin/main` 是破坏性操作，需用户显式确认。

如果用户授权推送，建议：

```bash
# 选项 A：直接推 main（覆盖远端）
git push origin main --force-with-lease

# 选项 B：推送 release 分支 + 开 PR
git push origin release/v0.12.0-tutorials
# 然后在 GitHub 上从 release/v0.12.0-tutorials 开 PR 到 main

# 选项 C：rebase 后推送
git fetch origin
git rebase origin/main
git push origin main --force-with-lease

# 推送 tag
git push origin v0.12.0-tutorials
```

## 八、已知遗留项

- 4/5 入门教程的 `go run <example> help` 子命令需 `GOOGLE_API_KEY`，已文档化在 `docs/tutorials/README.md` 已知问题小节
- `examples/skills` 目录有二进制名冲突（与 `skills/` 子包同名），建议用 `go run ./examples/skills` 替代
- 上游 `origin/main` 19 个新 commit 未合并到本 main，发布前需先 rebase

## 九、后续可做工作

- **子项目 1-11** —— 11 个模块的"独立深读文档"（每个 200-500 页级）
- **完善 godoc** —— `docs/architecture/04-appendix.md A.2` 标注的"待 godoc"文件
- **CI/CD 集成** —— `go build ./examples/...` + Markdown 链接检查 + Mermaid 渲染验证

---

**发布人**：吴小建（via Claude Code · MiniMax-M3）
**发布方式**：本地 tag + release branch + patch 文件（待用户授权推送）
