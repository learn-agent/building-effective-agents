# Evaluator-Optimizer：生成、评审、再修改

> 这是《Building Effective Agents》精读系列第 8 篇。原文：<https://www.anthropic.com/research/building-effective-agents>
>
> 上一篇：[Orchestrator-Workers：让模型拆任务，让代码管执行](07-orchestrator-workers.md)

## 先说结论

Evaluator-optimizer 把“写”和“评”拆成两个角色：

```text
          -> 生成器 LLM -> 草稿 -> 评审器 LLM -> 通过？ -> 输出
                ^                                  |
                └────────── 不通过，带反馈重写 ──────┘
```

一个模型生成草稿，另一个模型按明确标准评审。不通过就把具体反馈交回生成器修改，循环到通过或达到最大轮数。

它适合两个条件同时成立的任务：

1. **有清晰、可写下来的评价标准**；
2. **多轮迭代确实能带来提升**，而不只是把同样的话换个说法。

如果标准说不清，或者第一版通常已经够用，这个循环只会增加成本和延迟。

## 为什么把生成和评审分开

一个模型当然可以“写完再自己检查”。但把角色分开有几个实际好处：

- **提示词可以分别优化。** 生成器专注写，评审器专注按标准挑问题，不用在一个提示词里塞两套目标。
- **评价标准更明确。** 评审器拿到的是检查清单，而不是“你觉得好不好”。
- **更容易替换和评估。** 你可以换更强的评审模型，或者用规则、测试、人工来替代评审器。
- **避免“生成即自我肯定”。** 同一个上下文里，模型容易觉得自己刚写的就是对的。换一个角色、换一段提示词，能拉开一点距离。

严格说，用同一个模型也可以，只要两次调用的系统提示词和任务不同。示例就是这么做的：同一个模型，分别扮演“产品文案”和“严格的文案评审”。

## 示例：有硬性要求的产品介绍

[`examples/evaluator-optimizer`](../examples/evaluator-optimizer) 的任务是为一款面向程序员的降噪耳机写产品介绍，要求写死在常量里：

```go
const requirements = `要求：
1. 不超过 80 个汉字
2. 明确目标用户
3. 包含一个具体收益
4. 只输出正文，不要标题和解释`
```

这四条要求有两个特点：

- **可检查**：字数、目标用户、收益、格式，都能判断是否满足；
- **有取舍**：字数限制会迫使内容压缩，评审才有东西可提。

如果要求是“写得有感染力”，评审就会变成主观发挥，反馈也很难执行。

## 循环：代码控制轮数和停止

```go
var draft, feedback string

for round := 1; round <= 3; round++ {
	var prompt string
	if draft == "" {
		prompt = fmt.Sprintf("任务：%s\n\n%s", *brief, requirements)
	} else {
		prompt = fmt.Sprintf(
			"任务：%s\n\n%s\n\n上一版草稿：\n%s\n\n评审反馈：\n%s\n\n请根据反馈修改，只输出新草稿。",
			*brief, requirements, draft, feedback,
		)
	}

	generated, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     model,
		MaxTokens: 1024,
		System:    []anthropic.TextBlockParam{{Text: "你是一名产品文案。严格遵守字数与格式要求。"}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	draft = strings.TrimSpace(textOf(generated))

	// ... 评审 ...
}
```

几个关键点：

- **第一轮不带反馈**，只给任务和要求；
- **后续轮次同时带上一版草稿和反馈**，让生成器知道要改什么；
- **明确要求“只输出新草稿”**，避免它把修改说明也写进正文；
- **最多 3 轮**，由代码写死，不由模型决定要不要继续。

这个 `for` 循环就是 Evaluator-optimizer 和 Agent 的分界线：循环条件、轮数、什么时候停止，全在代码里。

## 评审：输出结构要固定

评审器的提示词要求第一行只写 `PASS` 或 `REVISE`，第二行开始写具体意见：

```go
evaluated, err := client.Messages.New(ctx, anthropic.MessageNewParams{
	Model:     model,
	MaxTokens: 512,
	System:    []anthropic.TextBlockParam{{Text: "你是一名严格的文案评审。"}},
	Messages: []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(fmt.Sprintf(
			`任务：%s

%s

待评审草稿：
%s

检查草稿是否满足全部要求。
第一行只写 PASS 或 REVISE，第二行开始写具体、可执行的修改意见。`,
			*brief, requirements, draft,
		))),
	},
})
```

为什么要求固定格式？因为代码要据此决定是否继续：

```go
func parseVerdict(s string) (bool, string) {
	lines := strings.SplitN(strings.TrimSpace(s), "\n", 2)
	verdict := strings.ToUpper(strings.TrimSpace(lines[0]))
	verdict = strings.Trim(verdict, "`'\"。.!！:：")

	feedback := ""
	if len(lines) > 1 {
		feedback = strings.TrimSpace(lines[1])
	}
	return verdict == "PASS", feedback
}
```

`parseVerdict` 只做三件事：

- 取第一行作为判定；
- 去掉大小写、标点、引号等噪声；
- 剩下的部分作为反馈。

如果第一行不是 `PASS`，就一律当成不通过。这是一种保守策略：宁可多改一轮，也不要把有问题的草稿当成通过。

## 代码检查：不要把所有判断都交给评审器

评审器也是模型，也会数错字数、看漏要求。示例在模型评审之外，加了一个确定性的检查：

```go
pass, feedback := parseVerdict(textOf(evaluated))

// 字数检查是确定性的，不依赖评审模型。
if utf8.RuneCountInString(draft) > 80 {
	pass = false
	if feedback == "" {
		feedback = "超过 80 字，请压缩。"
	}
}
```

注意这里用的是 `utf8.RuneCountInString`，不是 `len`。`len` 统计的是字节数，一个汉字通常占 3 个字节，用 `len` 会把 80 字错误地算成 240。Go 里判断“字符数”要用 rune 数。

这个检查体现了一个原则：**能确定性验证的条件，优先用代码验证。** 模型负责主观判断（目标用户是否明确、收益是否具体），代码负责客观约束（字数、格式、是否包含禁用词）。

类似地，还可以加：

- 正则检查是否包含标题；
- 检查是否出现“最”“第一”等广告法风险词；
- 检查目标用户关键词是否出现；
- 用测试用例验证代码生成任务。

## 反馈要具体、可执行

评审环节最常见的失败是反馈太泛：

```text
REVISE
可以更有吸引力一些。
```

生成器拿到这种反馈，只能随机改一改，下一轮评审可能还是同样的意见。示例在提示词里明确要求“具体、可执行”，但还可以更进一步，要求反馈指出：

- 哪一条要求没满足；
- 原文哪里有问题；
- 建议怎么改。

例如：

```text
REVISE
第 2 条未满足：没有明确目标用户。建议把“适合长时间工作的人”改成
“适合每天戴耳机写代码 6 小时以上的程序员”。
另外全文 96 字，超过 80 字，请删除重复的形容词。
```

这样的反馈才能让下一轮真正收敛。

## 什么时候停止

示例有三种结束方式：

1. **评审通过**：输出当前草稿并返回；
2. **达到最大轮数**：输出最后一版草稿，并说明已经达到上限；
3. **调用出错**：直接失败。

达到最大轮数时，不要假装成功。示例打印“达到最大轮数，输出最后一版草稿”，让调用方知道这不是通过评审的结果。

生产环境还可以：

- 记录每一轮的草稿、判定和反馈，便于分析循环是否在改进；
- 如果连续两轮反馈几乎一样，提前停止，避免空转；
- 如果分数在下降，回退到历史最佳版本；
- 把最终稿交给人工复核，尤其是对外发布的内容。

## 常见问题

### 评审器太宽松

表现：第一轮就 `PASS`，但人工一看就不合格。

处理：

- 把要求写得更具体，最好一条一条对应检查项；
- 要求评审器逐条列出“满足/不满足”，再给总判定；
- 用更强的模型做评审，或者用多个评审器投票；
- 加入确定性检查兜底。

### 评审器太严格

表现：每一轮都 `REVISE`，但意见互相矛盾，草稿越改越差。

处理：

- 明确“通过”的最低标准，不要让评审追求完美；
- 限制反馈条数，比如每轮最多 3 条；
- 要求反馈按重要性排序；
- 设最大轮数，接受“足够好”而不是“最好”。

### 生成器没有真正采纳反馈

表现：下一版和上一版几乎一样，或者改了无关紧要的地方。

处理：

- 在生成提示词里要求逐条回应反馈；
- 把反馈拆成结构化字段，比如 `issue`、`suggestion`；
- 在代码里比较两版差异，如果没有变化就停止或换策略。

### 成本和延迟

每一轮至少两次调用（生成 + 评审）。3 轮就是 6 次。如果任务本身很简单，这个成本可能不划算。

先问：**第一版真的经常不合格吗？多轮真的在提升质量吗？** 如果没有数据支持，先不要加循环。

## 和 Agent 的边界

Evaluator-optimizer 有一个循环，看起来也像 Agent。但它的循环是**固定结构**：

- 每轮做什么是代码写死的：生成 → 评审 → 再生成；
- 最多几轮是代码写死的；
- 评审结果只影响“要不要再来一轮”，不影响“下一步做什么类型的动作”；
- 模型不能决定去调用新工具、查新资料或改变任务目标。

如果让模型自己决定“这轮该查资料还是重写”“要不要再找另一个评审”“什么时候算完成”，那就进入了 Agent 的领域。

## OpenAI 版本

[`examples/evaluator-optimizer/openai`](../examples/evaluator-optimizer/openai) 的循环、`requirements`、`parseVerdict` 和字数检查完全一样，只是调用换成：

```go
generated, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
	Model:               model,
	MaxCompletionTokens: openai.Int(1024),
	Messages: []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("你是一名产品文案。严格遵守字数与格式要求。"),
		openai.UserMessage(prompt),
	},
})
draft = strings.TrimSpace(generated.Choices[0].Message.Content)
```

评审调用同理，读取 `evaluated.Choices[0].Message.Content` 后交给 `parseVerdict`。

## 运行

```bash
go run ./examples/evaluator-optimizer/claude
go run ./examples/evaluator-optimizer/openai

# 换任务
go run ./examples/evaluator-optimizer/claude -brief "为一款面向夜班护士的助眠 App 写一段介绍"
```

输出会显示每一轮的草稿、`pass` 和反馈，最后给出通过评审的版本或最后一版草稿。

## 本文小结

- Evaluator-optimizer 用生成器和评审器两个角色，循环打磨同一份产物。
- 适合有明确评价标准、且迭代确实能提升质量的任务。
- 评审输出要结构化，反馈要具体、可执行。
- 能用代码验证的条件，不要只靠评审模型。
- 轮数和停止条件由代码控制，所以它仍然是 Workflow。

下一篇是这个系列的最后一篇：**Agent 的自主循环**。把前面所有积木放进一个由模型决定下一步的 `for` 循环里，并讨论停止条件、工具错误和生产环境的护栏。

---

- 系列目录：[README.md](README.md)
- 上一篇：[Orchestrator-Workers：让模型拆任务，让代码管执行](07-orchestrator-workers.md)
- 下一篇：[Agent：自主循环与停止条件](09-agent-loop.md)
- 示例：[`examples/evaluator-optimizer`](../examples/evaluator-optimizer)
