# Prompt Chaining：把任务拆成可检查的步骤

> 这是《Building Effective Agents》精读系列第 4 篇。原文：<https://www.anthropic.com/research/building-effective-agents>
>
> 上一篇：[Workflow 总览：什么时候需要 workflow](03-workflow-overview.md)

## 先说结论

Prompt chaining 把一个大任务拆成固定顺序的多个 LLM 调用，每一步处理上一步的输出。它用**更多的调用次数**换**更高的可控性和准确率**。

它适合这样的任务：

- 可以自然拆成先后步骤；
- 每一步都能定义清楚的输入和输出；
- 中间可以插入程序化检查，不满足条件就停止或重试。

如果任务无法拆开，或者一次调用已经足够好，就不要为了“看起来像 workflow”而拆。

## 结构

```text
用户输入
  -> LLM 步骤 1
  -> 程序化检查（通过？）
  -> LLM 步骤 2
  -> 程序化检查（通过？）
  -> 输出
```

这里的关键不是“有多个步骤”，而是**步骤之间由代码控制**。代码知道下一步是什么，也知道什么情况下不该继续。

Anthropic 原文把这种模式描述为：把任务分解成一系列步骤，每个 LLM 调用处理前一个的输出，中间的“gate”确保流程还在正确的轨道上。它用延迟换准确率，因为把复杂任务拆成更简单的子任务，通常能让每次调用更容易做对。

## 示例：从口号到宣传文案

[`examples/prompt-chaining`](../examples/prompt-chaining) 做了两件事：

1. 为「独立书店的会员计划」生成 3 条宣传口号；
2. **由代码检查确实有 3 条**，再把它们交给第二步，选一条并写宣传文案。

为什么需要第二步？因为“生成 3 条口号”和“基于口号写一段有感染力的文案”是两种不同的写作任务。拆开之后，每一步的提示词都可以很聚焦，第二步还能拿到经过筛选的候选，而不是让模型在一次性回答里边想口号边写文案。

### 第一步：生成候选口号

```go
first, err := client.Messages.New(ctx, anthropic.MessageNewParams{
	Model:     model,
	MaxTokens: 2048,
	System:    []anthropic.TextBlockParam{{Text: "你是一名简洁的文案策划。"}},
	Messages: []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(fmt.Sprintf(
			"为「%s」写 3 条不同的宣传口号。每条单独一行，不要编号，不要解释。",
			*topic,
		))),
	},
})
if err != nil {
	log.Fatal(err)
}
```

提示词里明确要求“每条单独一行，不要编号，不要解释”，是为了让下一步的解析足够简单。这不是在迁就模型，而是在**设计一个稳定的步骤接口**。

### 中间的检查：代码说了算

```go
slogans := nonEmptyLines(textOf(first))
if len(slogans) != 3 {
	log.Fatalf("程序化检查失败：期望 3 条口号，实际 %d 条\n原始返回:\n%s",
		len(slogans), textOf(first))
}
```

`nonEmptyLines` 做的是最朴素的清洗：

```go
func nonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimLeft(line, "-*0123456789.、) ")
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}
```

它去掉空行、列表符号和编号，然后统计剩下的行数。

这个检查看起来很简单，但它承担了很重要的职责：**确认第一步真的完成了。** 如果模型只返回两条，或者把三条写成了一整段，继续往下走只会把错误放大。这里直接失败，至少你能马上看到问题出在哪一步。

也可以不直接失败，而是重试：

```go
for attempt := 1; attempt <= 2; attempt++ {
	first := generateSlogans(topic)
	slogans := nonEmptyLines(textOf(first))
	if len(slogans) == 3 {
		break
	}
	if attempt == 2 {
		log.Fatalf("两次尝试都没有得到 3 条口号")
	}
}
```

是失败还是重试，取决于任务的重要程度和成本预算。重点是：**这个决定由代码做，不由模型自己说“我完成了”。**

### 第二步：基于候选写文案

```go
second, err := client.Messages.New(ctx, anthropic.MessageNewParams{
	Model:     model,
	MaxTokens: 2048,
	System:    []anthropic.TextBlockParam{{Text: "你是一名品牌文案。"}},
	Messages: []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(fmt.Sprintf(
			"主题：%s\n\n候选口号：\n%s\n\n选出最合适的一条，并写一段不超过 80 字的宣传文案。先写口号，再写文案。",
			*topic, strings.Join(slogans, "\n"),
		))),
	},
})
if err != nil {
	log.Fatal(err)
}
```

第二步拿到的不是第一步的原始响应，而是**经过解析和检查的结构化结果**。这带来两个好处：

- 第二步的输入格式稳定，不会因为模型多写了一句解释就乱掉；
- 你可以在中间做过滤、排序、去重，甚至换成人工挑选的结果。

运行后大概是这个形状：

```text
步骤 1：生成 3 条口号（已通过数量检查）
  1. 把好书，带回家
  2. 每一本，都值得被遇见
  3. 会员的每一次翻页，都有回响

步骤 2：基于口号生成宣传文案
口号：每一本，都值得被遇见
文案：独立书店会员计划，为你留住那些差点错过的书。...
```

具体文字会随模型变化，但**步骤边界和检查点是固定的**。

## 为什么把检查放在代码里

你完全可以让模型自己检查：

```text
请写 3 条口号，然后确认是否正好 3 条。
```

这通常也能工作，但不如代码可靠：

- 模型可能数错，尤其是编号、标点混在一起时；
- 模型可能“确认通过”但实际并不满足条件；
- 你无法在检查失败时自动重试或告警，因为失败信号也是自然语言。

程序化检查则确定、可测试、可重放。能用代码判断的事情，尽量不要交给模型判断。

这也解释了 Prompt chaining 和 Agent 的区别：这里的“gate”是代码写死的，模型没有权力决定“要不要进入下一步”。

## 适合与不适合

**适合：**

- 写作：提纲 → 初稿 → 按标准修改；
- 翻译：直译 → 校对 → 润色；
- 结构化生成：先列要点 → 检查覆盖度 → 生成 JSON；
- 需要把不同约束分开处理的场景，比如“先保证不超字数，再保证语气”。

**不适合：**

- 步骤之间高度依赖，无法定义中间产物；
- 每一步都需要看下一步的结果才能决定怎么做；
- 一次调用已经足够好，拆开只会增加延迟和成本；
- 中间检查无法自动化，只能靠人反复看。

Prompt chaining 的代价很直接：N 步就是至少 N 次模型调用，延迟和成本都会上升。拆之前先问：**这一步拆开，真的能让它更容易做对吗？**

## 和 Agent 的边界

即使链条很长，只要步骤和顺序是代码写死的，它就是 Workflow。

如果模型开始决定“要不要回到上一步”“要不要多生成几条”“下一步调用哪个工具”，控制权就转移了。这时你需要的不再是更长的链条，而是一个带停止条件的循环，也就是第 9 篇的 Agent。

## OpenAI 版本

[`examples/prompt-chaining/openai`](../examples/prompt-chaining/openai) 的流程完全一样，区别只在 SDK 调用：

```go
first, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
	Model:               model,
	MaxCompletionTokens: openai.Int(2048),
	Messages: []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("你是一名简洁的文案策划。"),
		openai.UserMessage(fmt.Sprintf(
			"为「%s」写 3 条不同的宣传口号。每条单独一行，不要编号，不要解释。",
			*topic,
		)),
	},
})
```

读取文本的位置变成了 `first.Choices[0].Message.Content`。检查逻辑、第二步提示词、`nonEmptyLines` 都是同一套。

## 运行

```bash
go run ./examples/prompt-chaining/claude
go run ./examples/prompt-chaining/openai

# 换主题
go run ./examples/prompt-chaining/claude -topic "社区咖啡馆的周末读书会"
```

需要设置 `ANTHROPIC_API_KEY` 或 `OPENAI_API_KEY`。运行会调用真实 API 并产生费用。

## 本文小结

- Prompt chaining 用固定顺序的多次调用，把复杂任务拆成更容易做对的子任务。
- 中间的程序化检查是核心：它确认上一步真的完成，避免错误向下传播。
- 检查通过后再把结构化结果交给下一步，而不是直接传原始文本。
- 代价是延迟和成本；拆之前先确认拆分确实带来收益。
- 步骤由代码写死，所以它始终是 Workflow。

下一篇是 **Routing**：当输入天然分成几类时，如何先分类，再交给不同的专门提示词。

---

- 系列目录：[README.md](README.md)
- 上一篇：[Workflow 总览：什么时候需要 workflow](03-workflow-overview.md)
- 下一篇：[Routing：先分类，再交给专门的提示词](05-routing.md)
- 示例：[`examples/prompt-chaining`](../examples/prompt-chaining)
