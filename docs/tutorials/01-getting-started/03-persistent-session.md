# 持久化会话：多轮对话的存储

## 你将学到
- 什么是 Session（会话）
- `session.Service` 接口的核心方法
- 如何用 `session/inmemory` 实现跨多轮对话保存上下文
- `Runner.Run` 与 session 的协作方式
- Event 流的概念

## 前置条件
- [x] 已完成 [00-prerequisites.md](../00-prerequisites.md)
- [x] 已完成 [01-hello-world.md](./01-hello-world.md)
- [x] 已完成 [02-first-tool.md](./02-first-tool.md)
- [x] 已设置 `GOOGLE_API_KEY`

## 核心概念

**Session（会话）**：一个用户与 Agent 的多轮交互记录。Session 由 `session.Service` 管理（[session/service.go:25](../../../session/service.go)），核心方法有 `Create/Get/AppendEvent/List/Close`。

**Event（事件）**：Session 中的每一条记录。包含消息（用户输入、模型回复、工具结果）与 `EventActions`（状态变更、artifact 引用等）。`AppendEvent` 把 Event 原子写入 Session。

**Runner**：把 Agent 与 Session 串起来的执行入口。`Runner.Run(ctx, userID, sessionID, msg, opts)` 自动 Get/Create Session、调用 Agent、把 Event 写回 Session。

**sessionID**：会话的唯一标识。同一个 sessionID 跨多次 `Runner.Run` 调用复用，Agent 能"记住"之前对话。

## 完整代码

```go
// examples/persistent_session/main.go
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	sessioninmemory "google.golang.org/adk/session/inmemory"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/geminitool"
	"google.golang.org/genai"
)

func main() {
	ctx := context.Background()

	model, err := gemini.NewModel(ctx, "gemini-3.1-flash-lite", &genai.ClientConfig{
		APIKey: os.Getenv("GOOGLE_API_KEY"),
	})
	if err != nil {
		log.Fatalf("Failed to create model: %v", err)
	}

	a, err := llmagent.New(llmagent.Config{
		Name:        "chat_assistant",
		Model:       model,
		Description: "A helpful chat assistant.",
		Instruction: "You are a helpful assistant. Remember the conversation context.",
		Tools:       []tool.Tool{geminitool.GoogleSearch{}},
	})
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	sessSvc := sessioninmemory.NewService()
	r, err := runner.New(runner.Config{
		AppName:        "tutorial",
		Agent:          a,
		SessionService: sessSvc,
	})
	if err != nil {
		log.Fatalf("Failed to create runner: %v", err)
	}

	userID := "u1"
	sessionID := "s1"

	fmt.Println("Chat started. Type 'quit' to exit.")
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}
		input := scanner.Text()
		if input == "quit" {
			break
		}
		for event, err := range r.Run(ctx, userID, sessionID, genai.NewPartFromText(input), agent.RunOptions{}) {
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				break
			}
			if event.Partial {
				continue
			}
			for _, p := range event.LLMResponse.Content().Parts {
				fmt.Print(p.Text)
			}
		}
		fmt.Println()
	}
}
```

## 代码逐段讲解

### 1. 创建 Session Service

```go
sessSvc := sessioninmemory.NewService()
```

[session/inmemory/inmemory.go:43](../../../session/inmemory/inmemory.go) 返回一个进程内 Session Service。同进程多次 `Runner.Run` 共享内存中的 Session；进程退出后清空（生产环境应换 [session/database](../../../session/database/) 或 [session/vertexai](../../../session/vertexai/)，见 [F4 长会话与 Session 持久化](../../architecture/01-core-flows.md#f4长会话与-session-持久化)）。

### 2. 配置 Runner

```go
r, _ := runner.New(runner.Config{
	AppName:        "tutorial",
	Agent:          a,
	SessionService: sessSvc,
})
```

[runner/runner.go:131](../../../runner/runner.go) 的 `Runner` 是执行入口。`AppName` 用于命名空间隔离；不同 App 的 Session 不互相干扰。

### 3. 复用 sessionID 跨多轮

```go
for ... {
    for event, err := range r.Run(ctx, userID, sessionID, genai.NewPartFromText(input), agent.RunOptions{}) {
```

每次循环调用 `r.Run` 都用同一个 `sessionID`。Runner 内部会 Get 已有的 Session，把新 Event 追加，事件流包含历史上下文，LLM 看到完整历史。

### 4. 消费事件流

```go
for event, err := range r.Run(...) {
    if event.Partial { continue }
    for _, p := range event.LLMResponse.Content().Parts {
        fmt.Print(p.Text)
    }
}
```

`r.Run` 返回 channel，每条 Event 代表 Agent 生命周期中的一个事件。`event.Partial` 是流式中间结果（拼到一行末尾）；非 partial 的 Event 包含最终文本。`Content().Parts` 是消息的多模态片段（文本、图片等），本教程只取 Text。

## 准备与运行

### 步骤 1：保存代码

把上面"完整代码"保存为 `examples/persistent_session/main.go`。

### 步骤 2：运行

```bash
cd /path/to/adk-go
go run ./examples/persistent_session
```

### 步骤 3：测试多轮

```
Chat started. Type 'quit' to exit.
> Hi, I'm Alice.
Hello Alice! How can I help you today?
> What's my name?
Your name is Alice.
> quit
```

第二轮问题"我叫什么"能正确回答，说明 Session 持久化生效。

## 常见错误

- **`session not found`** —— sessionID 不存在且 Runner 配置未自动 Create；解决：使用 `runner.Config.SessionService` 自动创建
- **进程退出后记忆丢失** —— 用了 `inmemory`；生产应换 `database` 或 `vertexai` 后端
- **事件流没有输出** —— 漏写 `if event.Partial { continue }`，流式中间事件导致重复输出
- **`r.Run` channel 没 close** —— 用 `for range` 而非 `for` 循环，确保 Runner 关闭 channel
- **`event.LLMResponse` 为 nil** —— 这是中间事件（不是 LLM 输出的事件），应跳过

## 关键 API 小结

| API | 位置 | 作用 |
|---|---|---|
| `session.Service` | `session/service.go:25` | Session 服务接口 |
| `session/inmemory.NewService` | `session/inmemory/inmemory.go:43` | 内存实现（开发用） |
| `runner.New` | `runner/runner.go:131` | 创建 Runner |
| `runner.Runner.Run` | `runner/runner.go:328` | 执行入口（返回事件 channel） |
| `session.Session` | `session/session.go:32` | Session 数据结构 |
| `session.Event` | `session/session.go` | 单条事件 |

## 延伸阅读
- [架构文档：F4 长会话与 Session 持久化](../../architecture/01-core-flows.md#f4长会话与-session-持久化)
- [架构文档：05-session 模块](../../architecture/03-modules/05-session.md)
- [架构文档：04-runner 模块](../../architecture/03-modules/04-runner.md)
- [examples/quickstart/main.go](../../../examples/quickstart/main.go)（无 Session 的最简版本）
- 子项目深读占位：5-session 详细文档
