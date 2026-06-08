# 第一个工具：让 Agent 调用函数

## 你将学到
- 什么是 Tool（工具）
- 如何用 `functiontool.New` 把普通 Go 函数包装成 Agent 可调用的工具
- 如何让 Agent 在 `GoogleSearch` 与自定义函数之间自动选择
- 工具的输入输出契约（struct tags）

## 前置条件
- [x] 已完成 [00-prerequisites.md](../00-prerequisites.md)
- [x] 已完成 [01-hello-world.md](./01-hello-world.md)
- [x] 已设置 `GOOGLE_API_KEY`

## 核心概念

**Tool（工具）**：Agent 可以"调用的函数"。`tool.Tool` 是 ADK 里的核心接口（[tool/tool.go:38](../../../tool/tool.go)）。每个 Tool 都有名字、描述（让 LLM 知道何时调用）、参数 schema、实际执行函数。

**FunctionTool 装饰器**：`tool/functiontool.New(cfg, handler)` 让你把任意 `func(ctx, Input) (Output, error)` 包装成 Tool（[tool/functiontool/function.go:75](../../../tool/functiontool/function.go)）。它从 Go struct 的 JSON tag 自动推断参数 schema。

**多工具协同**：一个 Agent 可拥有多个 Tool。LLM 根据 Tool 描述与用户问题决定调用哪个。本教程演示 `GoogleSearch`（内建工具）与自定义 `poem` 函数并存。

## 完整代码

完整源码在 [examples/tools/multipletools/main.go](../../../examples/tools/multipletools/main.go)（约 120 行）。要点：

```go
// examples/tools/multipletools/main.go
package main

import (
	"context"
	"log"
	"os"
	"strings"

	"google.golang.org/genai"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/cmd/launcher/full"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/agenttool"
	"google.golang.org/adk/tool/functiontool"
	"google.golang.org/adk/tool/geminitool"
)

func main() {
	ctx := context.Background()

	model, err := gemini.NewModel(ctx, "gemini-3.1-flash-lite", &genai.ClientConfig{
		APIKey: os.Getenv("GOOGLE_API_KEY"),
	})
	if err != nil {
		log.Fatalf("Failed to create model: %v", err)
	}

	// 子 Agent 1：只做 Google 搜索
	searchAgent, _ := llmagent.New(llmagent.Config{
		Name:        "search_agent",
		Model:       model,
		Description: "Does google search.",
		Instruction: "You're a specialist in Google Search.",
		Tools:       []tool.Tool{geminitool.GoogleSearch{}},
	})

	// 自定义工具：返回一个指定行数的诗
	type Input struct {
		LineCount int `json:"lineCount"`
	}
	type Output struct {
		Poem string `json:"poem"`
	}
	handler := func(ctx agent.ToolContext, input Input) (Output, error) {
		return Output{Poem: strings.Repeat("A line of a poem,", input.LineCount) + "\n"}, nil
	}
	poemTool, _ := functiontool.New(functiontool.Config{
		Name:        "poem",
		Description: "Returns poem",
	}, handler)

	// 子 Agent 2：只生成诗
	poemAgent, _ := llmagent.New(llmagent.Config{
		Name:        "poem_agent",
		Model:       model,
		Description: "returns poem",
		Instruction: "You return poems.",
		Tools:       []tool.Tool{poemTool},
	})

	// 根 Agent：把两个子 Agent 暴露为 tool
	a, _ := llmagent.New(llmagent.Config{
		Name:        "root_agent",
		Model:       model,
		Description: "You can do a google search and generate poems.",
		Instruction: "Answer questions about weather based on google search unless asked for a poem, for a poem generate it with a tool.",
		Tools: []tool.Tool{
			agenttool.New(searchAgent, nil),
			agenttool.New(poemAgent, nil),
		},
	})

	config := &launcher.Config{AgentLoader: agent.NewSingleLoader(a)}
	l := full.NewLauncher()
	if err = l.Execute(ctx, config, os.Args[1:]); err != nil {
		log.Fatalf("Run failed: %v", err, l.CommandLineSyntax())
	}
}
```

## 代码逐段讲解

### 1. 定义输入输出 struct

```go
type Input struct {
	LineCount int `json:"lineCount"`
}
type Output struct {
	Poem string `json:"poem"`
}
```

`functiontool` 用 JSON tag 作为参数 schema（[tool/functiontool/function.go:103-110](../../../tool/functiontool/function.go)）。LLM 会按 schema 传参。命名遵循 camelCase（与 JSON 习惯一致）。

### 2. 写 handler

```go
handler := func(ctx agent.ToolContext, input Input) (Output, error) {
	return Output{Poem: strings.Repeat("A line of a poem,", input.LineCount) + "\n"}, nil
}
```

签名必须是 `func(ctx agent.ToolContext, input Input) (Output, error)`，否则 `functiontool.New` 编译失败。`agent.ToolContext` 拿到 session/artifact 访问能力（本教程不用）。

### 3. 注册为 Tool

```go
poemTool, _ := functiontool.New(functiontool.Config{
	Name:        "poem",
	Description: "Returns poem",  // 这句话决定 LLM 何时调用
}, handler)
```

**Description 字段至关重要**：LLM 根据描述判断"何时调用此 tool"。写得越清晰，调用越准。

### 4. 用 agenttool 包装子 Agent

```go
agenttool.New(searchAgent, nil)
```

[tool/agenttool/agent_tool.go](../../../tool/agenttool/agent_tool.go) 把整个 Agent 暴露为一个 tool。LLM 调用时实际触发子 Agent 的完整运行。本例中 `searchAgent` 自带 `GoogleSearch` 工具，因此调用 `search_agent` tool 等于触发搜索。

### 5. 根 Agent 拥有多 Tool

```go
a, _ := llmagent.New(llmagent.Config{
	Tools: []tool.Tool{
		agenttool.New(searchAgent, nil),
		agenttool.New(poemAgent, nil),
	},
})
```

根 Agent 不直接拥有 `GoogleSearch` 或 `poemTool`（因 genai 限制，多类型 Tool 不能共存），而是通过 `agenttool` 间接拥有。

## 准备与运行

### 步骤 1：确认 API key

```bash
echo $GOOGLE_API_KEY
```

### 步骤 2：运行

```bash
cd /path/to/adk-go
go run ./examples/tools/multipletools console
```

### 步骤 3：测试输入

```
User: What's the weather in Beijing?
[agent 调用 search_agent 子 agent，触发 GoogleSearch，返回天气]

User: Write a 3-line poem.
[agent 调用 poem_agent 子 agent，触发 poemTool，生成 3 行诗]
```

## 常见错误

- **`functiontool.New: handler signature invalid`** —— handler 签名必须严格匹配 `func(ctx, Input) (Output, error)`，参数与返回值类型任意但顺序不能换
- **JSON tag 缺失** —— 没有 `json:"xxx"` tag 的字段不会出现在 schema 中，LLM 无法传参
- **Description 太模糊** —— 如 `"Does stuff"`，LLM 不知何时调用，会忽略此 tool
- **`GoogleSearch` + 自定义 tool 混用冲突** —— 需走 `agenttool` 包装为子 agent，不能在同一 agent 的 Tools 列表里直接混合
- **多输入参数** —— struct 字段全部以 JSON 形式传给 handler；如果参数互斥，需要分多个 tool

## 关键 API 小结

| API | 位置 | 作用 |
|---|---|---|
| `tool.Tool` | `tool/tool.go:38` | 工具接口 |
| `functiontool.New` | `tool/functiontool/function.go:75` | 函数 → tool 装饰器 |
| `functiontool.Config` | `tool/functiontool/function.go:88` | name + description 配置 |
| `agenttool.New` | `tool/agenttool/agent_tool.go` | agent → tool 包装器 |
| `geminitool.GoogleSearch` | `tool/geminitool/` | 内建 Google Search 工具 |

## 延伸阅读
- [架构文档：tool 工具契约](../../architecture/03-modules/03-tool.md)
- [架构文档：写一个自定义 Tool](../../architecture/02-extension-points.md#3-写一个自定义-tool)
- [架构文档：F2 工具调用流程](../../architecture/01-core-flows.md#f2工具调用)
- [examples/tools/multipletools/main.go](../../../examples/tools/multipletools/main.go)
