# Orchestrator-Workers：让模型拆任务，让代码管执行

> 这是《Building Effective Agents》精读系列第 7 篇。原文：<https://www.anthropic.com/research/building-effective-agents>
>
> 上一篇：[Parallelization：并发独立子任务](06-parallelization.md)

## 先说结论

Orchestrator-workers 在普通并发之上加了一个**动态规划**步骤：

```text
                    -> worker 1 ->
总目标 -> 编排器 LLM -> worker 2 -> 汇总 LLM -> 最终结果
                    -> worker 3 ->
```

编排器根据总目标现场拆出子任务，代码把子任务分发给多个 worker 并发执行，最后再汇总。

它和 Parallelization 的区别只有一句话：**子任务是不是提前写死的。**

- Parallelization：写代码时就知道要分析“用户价值、技术风险、商业可行性”；
- Orchestrator-workers：写代码时不知道，得先让模型看目标，才能决定拆成什么。

也正因为拆解是动态的，它比固定并发更灵活，但也更难预测、更难控制成本。

## 为什么还需要代码

容易产生一个误解：既然编排器是模型，那整个流程是不是就交给模型了？

不是。这个模式里，模型只负责**产出计划**。计划怎么执行、并发多少、失败怎么办、最多拆几个任务，仍然由代码决定。

```text
模型：把目标拆成这些子任务
代码：解析计划、校验、限制数量、并发执行、收集结果
模型：把结果综合成最终方案
```

控制流在代码手里，所以 Anthropic 把它归为 Workflow，而不是 Agent。

## 示例：为编程比赛制定宣传计划

[`examples/orchestrator-workers`](../examples/orchestrator-workers) 的目标是“为一场面向开发者的 AI 编程比赛制定宣传计划”。这个目标足够开放，事先很难确定应该拆成“渠道、内容、时间表”还是“选手招募、评委邀请、赞助商”。所以先让编排器拆：

```go
planResp, err := client.Messages.New(ctx, anthropic.MessageNewParams{
	Model:     model,
	MaxTokens: 1024,
	System:    []anthropic.TextBlockParam{{Text: "你是一名任务规划器。只做拆解，不执行子任务。"}},
	Messages: []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(fmt.Sprintf(
			`把下面的目标拆成 3 个可以独立执行的子任务。
只返回 JSON 数组，不要 Markdown，不要解释。每个元素的格式：
{"title":"短标题","instruction":"给执行者的具体指令"}

目标：%s`,
			*goal,
		))),
	},
})
```

提示词里有三个关键约束：

1. **只做拆解，不执行。** 否则编排器可能顺手把活干了，worker 再重复一遍。
2. **固定为 3 个。** 示例为了控制成本和输出长度，明确要求 3 个；代码里还会再限制上限。
3. **只返回 JSON。** 让计划成为可解析的数据，而不是一段自然语言。

## 解析计划：不要相信模型一定输出纯 JSON

即使提示词写了“只返回 JSON”，模型仍可能加上 Markdown 代码块、前后解释，或者在 JSON 里用尾逗号。示例用一个小的 `cleanJSON` 做清洗：

```go
func cleanJSON(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}

	start := strings.IndexAny(s, "{[")
	end := strings.LastIndexAny(s, "}]")
	if start >= 0 && end > start {
		s = s[start : end+1]
	}
	return strings.TrimSpace(s)
}
```

它处理两种情况：

- 去掉 ` ```json ... ``` ` 这样的代码块包装；
- 如果 JSON 前后还有解释文字，只截取第一个 `{` 或 `[` 到最后一个 `}` 或 `]` 之间的内容。

然后解析并校验：

```go
var tasks []plannedTask
if err := json.Unmarshal([]byte(cleanJSON(textOf(planResp))), &tasks); err != nil {
	log.Fatalf("解析计划: %v\n原始返回:\n%s", err, textOf(planResp))
}
if len(tasks) == 0 {
	log.Fatalf("计划为空\n原始返回:\n%s", textOf(planResp))
}
if len(tasks) > 4 {
	tasks = tasks[:4]
}
```

这里做了三道防线：

- **解析失败就停止**，并打印原始返回，方便定位是提示词问题还是模型输出格式问题；
- **空计划就停止**，否则后面会得到空结果；
- **超过 4 个就截断**，防止模型一次拆出十几个任务，把成本放大。

`cleanJSON` 只是演示，不是通用 JSON 解析器。生产环境更稳妥的做法是：

- 用 provider 的结构化输出 / JSON schema 功能约束格式；
- 给每个字段做校验，比如 `title` 和 `instruction` 都不能为空；
- 对计划做去重和依赖检查；
- 解析失败时重试一次，而不是直接失败。

## 执行：worker 并发干活

计划解析完，执行方式和 Parallelization 很像：

```go
results := make([]workerResult, len(tasks))
var wg sync.WaitGroup

for i, task := range tasks {
	wg.Add(1)
	go func(i int, task plannedTask) {
		defer wg.Done()

		resp, err := client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     model,
			MaxTokens: 1024,
			System:    []anthropic.TextBlockParam{{Text: "你是一名执行者。只完成分到的子任务，输出可直接并入最终方案的内容。"}},
			Messages: []anthropic.MessageParam{
				anthropic.NewUserMessage(anthropic.NewTextBlock(fmt.Sprintf(
					"总目标：%s\n\n子任务：%s\n\n%s",
					*goal, task.Title, task.Instruction,
				))),
			},
		})

		result := workerResult{Title: task.Title, Err: err}
		if resp != nil {
			result.Text = textOf(resp)
		}
		results[i] = result
	}(i, task)
}

wg.Wait()
```

每个 worker 都能看到两样东西：

- **总目标**：避免它只盯着自己的小任务，产出和整体方向不一致；
- **子任务标题和具体指令**：告诉它这次要做什么、边界在哪。

系统提示词要求“只完成分到的子任务”，是为了减少重复劳动。如果每个 worker 都写一份完整方案，汇总时会有大量重复内容。

## 汇总：从多份草稿到一份方案

```go
final, err := client.Messages.New(ctx, anthropic.MessageNewParams{
	Model:     model,
	MaxTokens: 2048,
	System:    []anthropic.TextBlockParam{{Text: "你是一名主编。整合结果时要保留关键细节，删除重复内容。"}},
	Messages: []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(fmt.Sprintf(
			"总目标：%s\n\n以下是各个子任务的产出：\n%s\n请整合成一份简洁的最终方案。",
			*goal, combined.String(),
		))),
	},
})
```

汇总不是简单拼接。主编要做的是：

- 合并重复内容；
- 解决子任务之间的冲突；
- 补上子任务之间的衔接；
- 按总目标重新组织顺序。

如果只是把几段文本用 `##` 拼起来，那最后一步可以省掉；既然调用了模型，就要给它明确的编辑职责。

## 动态拆解带来的新问题

### 计划质量决定上限

worker 再强，也只能执行拿到的指令。如果编排器把任务拆歪了，比如漏掉关键环节、子任务之间大量重叠，最终结果就会有问题。

可以做的：

- 在规划提示词里给出拆解原则，比如“互不重叠、合起来覆盖总目标”；
- 让另一个模型评审计划，再执行（这本身又可以用 Evaluator-optimizer）；
- 保留计划，便于事后分析哪一步出了问题。

### 成本不可预测

Parallelization 的调用次数是固定的：3 个分析 + 1 个汇总 = 4 次。Orchestrator-workers 的次数取决于计划：

```text
1 次规划 + N 次 worker + 1 次汇总
```

所以代码里一定要有上限。示例把任务数限制在 4 个，加上规划和汇总，最多 6 次调用。生产环境还应该加上 token 预算和超时。

### 子任务可能不独立

编排器被要求拆成“可以独立执行”的子任务，但模型不一定做得到。如果两个子任务都需要先读同一份数据，或者一个的输出是另一个的输入，并发就会出问题。

两个处理方向：

- **把依赖显式化**：让计划里包含 `depends_on` 字段，代码按拓扑顺序执行；
- **接受串行**：如果依赖很多，也许这个任务不适合并发拆解，改用带状态的循环更合适。

### 失败处理

示例采用“任一 worker 失败就整体失败”的策略：

```go
if result.Err != nil {
	log.Fatalf("%s 执行失败: %v", result.Title, result.Err)
}
```

这对演示足够了，但生产环境通常要更细：

- 单个 worker 失败时重试；
- 允许部分失败，在汇总时说明缺失；
- 对关键子任务失败直接终止，对非关键子任务降级；
- 把失败的任务和错误一起交给汇总模型，让它决定怎么补。

## 和 Agent 的边界

Orchestrator-workers 里，模型参与了“拆任务”，看起来很像 Agent。但它的动态性只发生在**规划阶段**：

- 拆完之后，worker 怎么执行、并发多少、最多几个，都是代码定的；
- worker 不会在执行过程中回头修改计划；
- 没有“根据环境反馈决定下一步”的循环。

如果让编排器在执行过程中根据 worker 的结果不断调整计划、追加任务、决定何时停止，那就开始接近 Agent 了。区别仍然在控制权，而不在“有没有用模型做规划”。

## OpenAI 版本

[`examples/orchestrator-workers/openai`](../examples/orchestrator-workers/openai) 的规划、解析、并发、汇总结构完全一致，只是调用换成 Chat Completions：

```go
planResp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
	Model:               model,
	MaxCompletionTokens: openai.Int(1024),
	Messages: []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("你是一名任务规划器。只做拆解，不执行子任务。"),
		openai.UserMessage(fmt.Sprintf(`...`, *goal)),
	},
})
```

计划文本从 `planResp.Choices[0].Message.Content` 取，`cleanJSON` 和 `plannedTask` 结构体可以原样复用。

## 运行

```bash
go run ./examples/orchestrator-workers/claude
go run ./examples/orchestrator-workers/openai

# 换目标
go run ./examples/orchestrator-workers/claude -goal "为一款开源数据库设计开发者关系计划"
```

输出大致是：

```text
编排器拆出的子任务：
  1. 渠道策略 — ...
  2. 内容规划 — ...
  3. 时间表 — ...

## 渠道策略
...

## 内容规划
...

## 时间表
...

最终方案：
...
```

## 本文小结

- Orchestrator-workers 让模型动态拆解子任务，代码负责执行和汇总。
- 它适合子任务无法提前写死的开放任务；能提前列出子任务时，用 Parallelization 更简单。
- 计划必须是可解析、可校验的数据，并且要有数量上限。
- worker 并发执行时要注意顺序、错误、超时和成本。
- 动态性只发生在规划阶段，所以它仍然是 Workflow。

下一篇是 **Evaluator-Optimizer**：当“好不好”有明确标准时，如何让一个模型生成、另一个模型评审，循环打磨。

---

- 系列目录：[README.md](README.md)
- 上一篇：[Parallelization：并发独立子任务](06-parallelization.md)
- 下一篇：[Evaluator-Optimizer：生成、评审、再修改](08-evaluator-optimizer.md)
- 示例：[`examples/orchestrator-workers`](../examples/orchestrator-workers)
