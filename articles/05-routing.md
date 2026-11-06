# Routing：先分类，再交给专门的提示词

> 这是《Building Effective Agents》精读系列第 5 篇。原文：<https://www.anthropic.com/research/building-effective-agents>
>
> 上一篇：[Prompt Chaining：把任务拆成可检查的步骤](04-prompt-chaining.md)

## 先说结论

Routing 先判断输入属于哪一类，再由代码把请求送到对应的处理路径。模型只参与“分类”这一步，真正的分支逻辑仍然在代码里。

```text
                    -> 账单提示词  ->
用户问题 -> 分类器 -> 技术提示词  -> 回答
                    -> 账户提示词  ->
```

它解决的是**不同输入需要不同处理方式**的问题。与其写一个什么都能答、但什么都不精的巨大提示词，不如把场景拆开，每一类用一套专门的系统提示词、知识库或模型。

前提是：**类别清晰、互斥，而且分类可以做得足够准。**

## 什么时候值得路由

Anthropic 原文给的两个典型场景：

1. **客服分流。** 账单、技术、账户问题需要不同的处理流程和话术。
2. **按难度选模型。** 简单问题交给便宜的小模型，难题才交给大模型，控制成本和延迟。

除此之外，还有这些常见用法：

- 按语言路由到不同的翻译提示词；
- 按内容类型路由到摘要、抽取、问答等不同管线；
- 按风险等级路由到自动处理或人工审核；
- 按意图选择不同的工具集。

共同点是：**每一类都有明显不同的最优处理方式。** 如果所有类别的处理方式差不多，路由只会多一次调用。

## 示例：客服工单分流

[`examples/routing`](../examples/routing) 定义了三条路由，外加一个兜底：

```go
type route struct {
	Name        string
	Description string
	System      string
}

routes := []route{
	{
		Name:        "billing",
		Description: "账单、发票、退款、付款问题",
		System:      "你是账单支持专员。回答要具体，涉及金额或发票时提醒用户核对信息。",
	},
	{
		Name:        "technical",
		Description: "报错、集成、API、性能问题",
		System:      "你是技术支持工程师。优先给出可执行的排查步骤，不要泛泛而谈。",
	},
	{
		Name:        "account",
		Description: "登录、密码、账户、权限问题",
		System:      "你是账户支持专员。回答要说明操作路径，并提醒必要的安全注意事项。",
	},
}
```

每条路由包含三样东西：

- `Name`：代码使用的稳定标识；
- `Description`：给分类器看的判断依据；
- `System`：选中后使用的专门提示词。

注意 `Name` 和 `Description` 是分开的。`Name` 要稳定，方便代码判断；`Description` 要写清楚边界，方便模型分类。不要让模型输出一段解释，再由你用正则去猜。

## 第一步：让模型只做分类

分类提示词由路由表动态拼出来，这样新增路由时不用改两处：

```go
var menu strings.Builder
menu.WriteString("把用户问题分类为以下之一：\n")
for _, r := range routes {
	fmt.Fprintf(&menu, "- %s: %s\n", r.Name, r.Description)
}
menu.WriteString("\n只输出分类名，不要解释。")

classified, err := client.Messages.New(ctx, anthropic.MessageNewParams{
	Model:     model,
	MaxTokens: 100,
	System:    []anthropic.TextBlockParam{{Text: "你是一名意图分类器。"}},
	Messages: []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(fmt.Sprintf(
			"%s\n用户问题：%s", menu.String(), *question,
		))),
	},
})
```

这里有几个刻意的设计：

- `MaxTokens` 很小（100）。分类不需要长篇大论，限制输出也能减少跑偏。
- 系统提示词只说“你是意图分类器”，不掺杂业务知识。分类和处理是两件事。
- 明确要求“只输出分类名，不要解释”。输出越短，后续解析越稳定。

即便如此，也不能假设模型一定只输出一个干净的词。所以还需要一步归一化。

## 第二步：校验分类结果

```go
func normalizeRoute(text string, routes []route) string {
	text = strings.ToLower(strings.TrimSpace(text))
	text = strings.Trim(text, "`'\"。.!！?？ \n\t")
	for _, r := range routes {
		if text == r.Name || strings.Contains(text, r.Name) {
			return r.Name
		}
	}
	return "general"
}
```

它处理了这些情况：

- 前后空白；
- 大小写；
- 模型偶尔加上的标点、引号、反引号；
- 输出里除了分类名还带了一点解释。

如果什么都不匹配，就返回 `general`，走兜底路径：

```go
selected := route{
	Name:   "general",
	System: "你是通用客服助手。先确认用户的问题，再给出简洁、可执行的回答。",
}
for _, r := range routes {
	if r.Name == routeName {
		selected = r
		break
	}
}
```

**兜底不是可选项。** 分类器会出错，模型会输出没见过的词，路由表也会在迭代中变化。没有兜底，一个解析失败就会让整个请求失败。

## 第三步：用选中的提示词处理

```go
answer, err := client.Messages.New(ctx, anthropic.MessageNewParams{
	Model:     model,
	MaxTokens: 1024,
	System:    []anthropic.TextBlockParam{{Text: selected.System}},
	Messages: []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(
			*question + "\n\n请用中文回答，最多 120 字。")),
	},
})
```

到了这一步，模型已经不再需要知道“有哪些路由”。它只看到自己这一类角色的系统提示词和用户问题。**关注点被隔离了。**

运行 `go run ./examples/routing/claude`（默认问题是发票抬头写错）会看到：

```text
路由结果：billing

用户问题：我的发票抬头写错了，怎么修改？

回答：
请登录账户中心，在「发票管理」中找到对应发票，提交重开申请...
```

如果把问题换成“API 调用一直返回 401”，路由结果应该变成 `technical`，系统提示词也会随之切换。

## 设计路由时的几个问题

### 类别真的互斥吗

如果一个问题同时涉及“登录失败”和“被重复扣款”，它该走 `account` 还是 `billing`？分类器只能选一个，但用户的真实需求可能是两个。

处理方式有几种：

- 允许模型输出多个类别，代码依次或并发处理；
- 增加一个 `mixed` 类别，走更通用的路径；
- 先做一次澄清，再分类。

路由表越复杂，越要先想清楚这些交叉情况。

### 分类器要不要用同一个模型

不一定。分类通常比处理简单，可以用更小、更快的模型。这样总成本可能反而更低：

```text
小模型分类 -> 简单问题小模型回答
           -> 难题大模型回答
```

但要注意：分类错误会把请求送到错误的路径，后面的模型再强也救不回来。省下的钱要和错误率一起算。

### 分类失败怎么办

除了兜底路由，还可以：

- 重试一次分类，并在提示词里强调只能输出枚举值；
- 用 JSON schema 或结构化输出约束格式；
- 记录分类结果和最终处理结果，定期抽查混淆矩阵；
- 对低置信度的输入直接转人工。

不要把“模型说自己是 billing”当成绝对事实。分类也是一个需要验证的模型输出。

### 路由和工具选择有什么区别

Routing 是**代码**根据分类结果选择处理路径；工具选择是**模型**在响应里决定调用哪个工具。

- 路由：代码写死 `if route == "billing" { use billing prompt }`；
- 工具选择：模型返回 `tool_use`，你执行对应工具。

前者属于 Workflow，后者通常出现在 Agent 循环里。两者可以组合：路由到某个分支后，该分支内部再让模型使用工具。

## OpenAI 版本

[`examples/routing/openai`](../examples/routing/openai) 的路由表、归一化函数和兜底逻辑完全相同，只是调用方式变成 Chat Completions：

```go
classified, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
	Model:               model,
	MaxCompletionTokens: openai.Int(100),
	Messages: []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("你是一名意图分类器。"),
		openai.UserMessage(fmt.Sprintf("%s\n用户问题：%s", menu.String(), *question)),
	},
})
```

读取分类结果用 `classified.Choices[0].Message.Content`。后续处理调用也只需要替换 SDK 参数，`selected.System` 和用户问题的拼法不变。

## 运行

```bash
go run ./examples/routing/claude
go run ./examples/routing/openai

# 换一个问题
go run ./examples/routing/claude -q "调用 API 时一直返回 401，怎么排查？"
```

## 本文小结

- Routing 先分类，再由代码选择专门的处理路径。
- 它适合类别清晰、不同类别需要不同提示词或模型的场景。
- 分类输出必须校验，并且一定要有兜底路径。
- 分类器和处理器可以分开选模型，但要一起评估总成本和错误率。
- 控制流仍在代码里，所以 Routing 是 Workflow。

下一篇是 **Parallelization**：当多个子任务互不依赖时，如何用并发换延迟，以及什么时候该用多次投票而不是一次调用。

---

- 系列目录：[README.md](README.md)
- 上一篇：[Prompt Chaining：把任务拆成可检查的步骤](04-prompt-chaining.md)
- 下一篇：[Parallelization：并发独立子任务](06-parallelization.md)
- 示例：[`examples/routing`](../examples/routing)
