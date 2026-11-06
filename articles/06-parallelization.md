# Parallelization：并发独立子任务

> 这是《Building Effective Agents》精读系列第 6 篇。原文：<https://www.anthropic.com/research/building-effective-agents>
>
> 上一篇：[Routing：先分类，再交给专门的提示词](05-routing.md)

## 先说结论

Parallelization 把任务拆成多个可以同时执行的 LLM 调用，再把结果汇总。Anthropic 原文区分了两种用法：

- **Sectioning**：把任务拆成互不依赖的部分，并发执行，最后拼接或综合；
- **Voting**：同一个任务跑多次，用多数票或评审模型选出更可靠的结果。

前者用并发换延迟，后者用重复换稳定性。两者都不改变“代码控制流程”这个事实，所以仍然属于 Workflow。

## Sectioning：拆成互不依赖的部分

```text
          -> LLM 分析 A ->
输入 -----> LLM 分析 B -> 汇总 LLM -> 输出
          -> LLM 分析 C ->
```

关键是**子任务之间没有依赖**。A 的结果不需要等 B，B 也不需要知道 C 做了什么。只有这样，并发才真的能缩短总时间。

[`examples/parallelization`](../examples/parallelization) 分析一款面向大学生的 AI 学习助手，从三个角度同时展开：

```go
tasks := []struct {
	Name   string
	Prompt string
}{
	{"用户价值", "从目标用户价值角度分析：它解决什么问题？谁最需要它？"},
	{"技术风险", "从技术实现角度分析：主要技术风险和落地难点是什么？"},
	{"商业可行性", "从商业角度分析：成本、付费意愿和可持续性如何？"},
}
```

三个任务共享同一个产品主题，但彼此不依赖。它们可以同时发给模型，最后再把三份分析交给一个“产品负责人”综合成建议。

### 用 goroutine 并发执行

```go
results := make([]analysis, len(tasks))
var wg sync.WaitGroup

for i, task := range tasks {
	wg.Add(1)
	go func(i int, name, prompt string) {
		defer wg.Done()

		resp, err := client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     model,
			MaxTokens: 1024,
			System:    []anthropic.TextBlockParam{{Text: "你是一名产品分析顾问。回答要具体，避免空话。"}},
			Messages: []anthropic.MessageParam{
				anthropic.NewUserMessage(anthropic.NewTextBlock(fmt.Sprintf(
					"产品：%s\n\n%s\n\n用 3 条要点回答，每条约 30 字。",
					*topic, prompt,
				))),
			},
		})

		result := analysis{Name: name, Err: err}
		if resp != nil {
			result.Text = textOf(resp)
		}
		results[i] = result
	}(i, task.Name, task.Prompt)
}

wg.Wait()
```

这段代码有几个细节值得说：

**1. `i` 必须作为参数传进去。**

旧版本的 Go 里，循环变量是复用的，goroutine 直接捕获 `i` 会读到错误的值。Go 1.22 之后每次迭代都有新的变量，但显式传参仍然更清楚，也避免团队里有人用旧版本编译时踩坑。

**2. 每个 goroutine 写自己的下标。**

`results[i]` 是预先分配好的切片，每个下标只被一个 goroutine 写，所以不需要额外的锁。最后按 `results` 的顺序输出，结果顺序和任务顺序一致，不受调度影响。

**3. 共享的 `client` 可以并发使用。**

SDK 的 HTTP client 通常可以安全地并发调用。真正需要小心的是你自己的共享状态，比如计数器、缓存、数据库连接池。

**4. 统一用 `wg.Wait()` 收尾。**

不要在 goroutine 里调用 `log.Fatal`，那会直接退出进程，跳过其他正在进行的调用。把错误收集到结果里，等 `wg.Wait()` 之后再统一处理：

```go
for _, result := range results {
	if result.Err != nil {
		log.Fatalf("%s 分析失败: %v", result.Name, result.Err)
	}
	fmt.Printf("## %s\n%s\n\n", result.Name, result.Text)
}
```

### 汇总：把多份分析变成一份建议

并发只解决了“分别做”，最后还需要一步综合：

```go
final, err := client.Messages.New(ctx, anthropic.MessageNewParams{
	Model:     model,
	MaxTokens: 1024,
	System:    []anthropic.TextBlockParam{{Text: "你是一名产品负责人。"}},
	Messages: []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(fmt.Sprintf(
			"产品：%s\n\n以下是三份独立分析：\n%s\n请综合成 3 条下一步建议，每条一句话。",
			*topic, combined.String(),
		))),
	},
})
```

汇总提示词要明确：

- 输入是几份独立分析；
- 输出要什么形式（3 条建议、每条一句话）；
- 冲突时怎么处理（保留分歧、选一个、还是标注风险）。

如果只是把三份文本拼起来，其实不需要再调用一次模型；既然调用了，就要让它做**综合**，而不是重复。

运行 `go run ./examples/parallelization/claude` 会先看到三份分析，再看到综合建议：

```text
## 用户价值
- ...
- ...

## 技术风险
- ...

## 商业可行性
- ...

综合建议：
1. ...
2. ...
3. ...
```

总耗时大致等于最慢的那个调用，而不是三个调用之和。这是 sectioning 最直接的收益。

## Voting：同一个任务跑多次

Voting 的形状不同：

```text
          -> LLM 尝试 1 ->
同一输入 --> LLM 尝试 2 -> 汇总/投票 -> 输出
          -> LLM 尝试 3 ->
```

它不是把任务拆开，而是**对同一个任务做多次独立尝试**，再选一个更可靠的结果。常见做法：

- **多数票**：分类、审核、是非判断，取出现次数最多的答案；
- **评审模型**：把多个候选交给另一个模型，让它选最好的；
- **按规则选**：比如选最短的、选通过测试的、选引用最多的。

一个投票的骨架：

```go
const runs = 5

votes := make([]string, runs)
var wg sync.WaitGroup

for i := 0; i < runs; i++ {
	wg.Add(1)
	go func(i int) {
		defer wg.Done()
		votes[i] = classify(ctx, client, model, input)
	}(i)
}
wg.Wait()

result := majority(votes) // majority 是示意函数：按出现次数取最多的答案
```

Voting 适合：

- 单次调用有随机性，错误率不能接受；
- 有明确的“正确答案”可以投票；
- 成本允许你多跑几次。

它不适合开放式创作。五首诗里选一首“最好的”，并不一定比写一首更好的诗更容易。

## 并发不是越多越好

并发能降延迟，但也会带来新的问题：

### 速率限制

大多数 API 都有 RPM/TPM 限制。一次发出几十个请求，很容易触发 429。生产环境通常需要：

- 用信号量限制并发数；
- 对 429 做指数退避重试；
- 把任务排队，而不是无限开 goroutine。

一个简单的并发上限：

```go
sem := make(chan struct{}, 4) // 最多 4 个并发

for i := range tasks {
	wg.Add(1)
	go func(i int) {
		defer wg.Done()

		select {
		case sem <- struct{}{}:
			defer func() { <-sem }()
		case <-ctx.Done():
			results[i] = analysis{Err: ctx.Err()}
			return
		}

		// runTask 代表对这个子任务的完整处理。
		results[i] = runTask(ctx, tasks[i])
	}(i)
}
```

### 超时和取消

所有并发调用最好共享同一个 `ctx`。一旦整体超时，或者用户取消请求，应该让还没完成的调用一起退出，而不是继续烧钱。

```go
ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
defer cancel()
```

### 部分失败

一个子任务失败，不代表整个任务都要失败。可以：

- 全部成功才继续（示例采用的方式，简单直接）；
- 允许部分失败，在汇总时标注缺失；
- 对失败的子任务单独重试；
- 降级到更简单的模型或更短的提示词。

选择哪种，取决于这个子任务对最终结果是不是必需的。

### 成本

并发不会减少 token 成本，只是把时间叠在一起。三个分析就是三次完整调用，加上一次汇总，一共四次。算成本时要按调用次数算，不是按“用户请求数”算。

## OpenAI 版本

[`examples/parallelization/openai`](../examples/parallelization/openai) 的结构完全相同，只是每个 goroutine 里换成 Chat Completions：

```go
resp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
	Model:               model,
	MaxCompletionTokens: openai.Int(1024),
	Messages: []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("你是一名产品分析顾问。回答要具体，避免空话。"),
		openai.UserMessage(fmt.Sprintf(
			"产品：%s\n\n%s\n\n用 3 条要点回答，每条约 30 字。",
			*topic, prompt,
		)),
	},
})
```

读取结果时记得检查 `len(resp.Choices) == 0`，否则 `resp.Choices[0]` 会 panic。Claude 版本里 `textOf` 遍历 content 块，也是同样的防御思路。

## 和 Orchestrator-workers 的区别

两者都并发，但拆解方式不同：

| | Parallelization | Orchestrator-workers |
| --- | --- | --- |
| 子任务从哪来 | 代码提前写死 | 编排器模型动态生成 |
| 子任务数量 | 固定 | 随输入变化 |
| 适合 | 分析角度已知 | 无法提前知道要拆什么 |
| 控制流 | 代码 | 代码（但拆解内容由模型决定） |

一个简单的判断：如果你能在写代码时列出所有子任务，用 Parallelization；如果列不出来，才需要 Orchestrator-workers。

## 运行

```bash
go run ./examples/parallelization/claude
go run ./examples/parallelization/openai

# 换产品
go run ./examples/parallelization/claude -topic "一款面向自由职业者的记账工具"
```

## 本文小结

- Parallelization 用并发执行互不依赖的子任务，最后汇总。
- Sectioning 拆任务，Voting 用重复换可靠性，两者解决的问题不同。
- 每个 goroutine 写自己的结果下标，共享 client，统一 `wg.Wait()`。
- 并发要配上限、超时、取消和失败策略，否则会把延迟问题变成稳定性问题。
- 子任务由代码写死，所以它仍然是 Workflow。

下一篇是 **Orchestrator-workers**：当子任务无法提前列出时，如何让模型先做规划，再分发给 worker 执行。

---

- 系列目录：[README.md](README.md)
- 上一篇：[Routing：先分类，再交给专门的提示词](05-routing.md)
- 下一篇：[Orchestrator-Workers：让模型拆任务，让代码管执行](07-orchestrator-workers.md)
- 示例：[`examples/parallelization`](../examples/parallelization)
