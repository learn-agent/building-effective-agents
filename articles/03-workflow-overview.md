# Workflow 总览：什么时候需要 workflow

> 这是《Building Effective Agents》精读系列第 3 篇。原文：<https://www.anthropic.com/research/building-effective-agents>
>
> 上一篇：[Building Block：Augmented LLM](02-augmented-llm.md)

## 先说结论

Workflow 是**用代码预先编排 LLM 和工具**的系统。模型可以很聪明，但“下一步做什么”由代码决定。

Anthropic 在原文里列了五种常见模式：

| 模式 | 一句话 | 适合的问题 |
| --- | --- | --- |
| Prompt chaining | 把任务拆成固定顺序的步骤 | 步骤清晰、可以逐步检查 |
| Routing | 先分类，再交给专门的处理路径 | 输入类型明确、不同类需要不同提示词 |
| Parallelization | 多个独立调用并发，最后汇总 | 子任务互不依赖，或需要多次投票 |
| Orchestrator-workers | 由模型动态拆解子任务，再分发给 worker | 子任务无法提前写死 |
| Evaluator-optimizer | 一个模型生成，另一个评审，循环改进 | 有明确评价标准、迭代能带来提升 |

它们不是五个必须依次升级的台阶，而是五种可以组合的结构。真正要回答的问题只有一个：**这个任务的控制流，能不能提前写死？**

能，就用 Workflow；不能，再考虑 Agent。

## 为什么不是一上来就做 Agent

Agent 把控制权交给模型，换来处理开放问题的能力，同时也带来更高的成本、更长的延迟和更难预测的行为。Workflow 则相反：

- **可预测。** 同样的输入走同样的路径，方便测试和回归。
- **可观察。** 每一步的输入输出都能单独打日志、加断言。
- **可控制成本。** 调用次数基本固定，不会因为模型“想多做几步”而失控。
- **容易定位失败。** 哪一步错了，就修那一步的提示词或代码。

原文的建议很直接：**先找最简单的方案，只有在确实需要时才增加复杂度。** 很多看起来需要 Agent 的任务，拆成两三个固定步骤就能完成，而且效果更稳定。

## 一个 Workflow 由什么组成

不管哪种模式，基本单元都是上一篇讲的 Augmented LLM。一个 Workflow 通常包含三样东西：

1. **LLM 调用**：每次调用有明确的输入、输出和提示词。
2. **代码逻辑**：决定调用顺序、分支、并发、循环和停止条件。
3. **显式状态**：上一步的输出通过变量或消息传给下一步，而不是依赖模型“记住”。

第三点经常被忽略。Workflow 里没有隐式的共享记忆，每一步需要什么，就要在提示词里明确给它。这也意味着你可以随时检查、修改、重放中间结果。

一个最小的两步链条：

```go
// callLLM 代表任意一次增强后的模型调用，重点是外面的编排逻辑。
// 第一步：生成候选
first := callLLM("写 3 条口号")
slogans := parseLines(first)

// 代码检查：不满足条件就停止，不把问题交给下一步
if len(slogans) != 3 {
	return fmt.Errorf("期望 3 条口号，实际 %d 条", len(slogans))
}

// 第二步：把通过检查的结果交给下一段提示词
second := callLLM("从这些口号里选一条并写文案：\n" + strings.Join(slogans, "\n"))
fmt.Println(second)
```

这里没有 Agent，也没有工具循环。但 `callLLM` 背后完全可以是带检索、带工具定义的增强调用。**Workflow 描述的是编排方式，不是模型能力。**

## 五种模式

### 1. Prompt chaining：固定顺序，逐步检查

```text
输入 -> LLM 步骤 1 -> 检查 -> LLM 步骤 2 -> 检查 -> 输出
```

把任务拆成固定的几步，每一步只做一件事。中间可以插入程序化检查，比如数量、格式、长度、JSON 能否解析。

适合：

- 任务可以自然分解成先后步骤；
- 每一步都能定义清楚的输入输出；
- 你愿意用更多调用换更高的准确率。

典型例子：先写提纲，检查提纲是否覆盖关键点，再写正文，最后翻译。每一步都基于上一步的结果，模型不需要一次性处理所有约束。

对应示例：[`examples/prompt-chaining`](../examples/prompt-chaining)

### 2. Routing：先分类，再分流

```text
                    -> 账单提示词 ->
输入 -> 分类器 LLM  -> 技术提示词 -> 输出
                    -> 账户提示词 ->
```

先让模型判断输入属于哪一类，再由代码选择对应的提示词、模型或工具集。分类器只负责分类，后面的处理路径由代码决定。

适合：

- 类别清晰、互斥；
- 不同类别需要不同的系统提示词、知识库或工具；
- 可以用小模型做分类，把大模型留给难的分支。

典型例子：客服工单分流、把简单问题路由到便宜模型、按语言路由到不同翻译提示词。

对应示例：[`examples/routing`](../examples/routing)

### 3. Parallelization：并发独立子任务

```text
          -> LLM 分析 A ->
输入 -----> LLM 分析 B -> 汇总 LLM -> 输出
          -> LLM 分析 C ->
```

有两种用法：

- **Sectioning**：把任务拆成互不依赖的部分，并发执行，最后拼接或综合。
- **Voting**：同一个任务跑多次，用多数票或评审模型选出更可靠的结果。

适合：

- 子任务之间没有依赖；
- 需要降低总延迟；
- 需要多角度分析，或需要多次采样来提高可靠性。

典型例子：同时做安全审查、性能审查、可读性审查；对内容审核任务跑多次投票。

对应示例：[`examples/parallelization`](../examples/parallelization)

### 4. Orchestrator-workers：动态拆解，再分发

```text
                 -> worker 1 ->
输入 -> 编排器 LLM -> worker 2 -> 汇总 LLM -> 输出
                 -> worker 3 ->
```

编排器根据总目标**动态**拆出子任务，代码把子任务并发交给 worker，最后再汇总。它和 Parallelization 的关键区别是：**子任务不是提前写死的**，而是编排器现场生成的。

适合：

- 无法提前知道需要哪些子任务；
- 子任务数量随输入变化；
- 每个子任务可以独立执行，最后能合并。

典型例子：跨多个文件的代码修改、复杂研究任务、一次生成多章节的内容。

对应示例：[`examples/orchestrator-workers`](../examples/orchestrator-workers)

### 5. Evaluator-optimizer：生成与评审分离

```text
          -> 生成器 LLM -> 草稿 -> 评审器 LLM -> 通过？ -> 输出
                ^                                  |
                └────────── 不通过，带反馈重写 ──────┘
```

一个模型负责生成，另一个模型负责按明确标准评审。不通过就把具体反馈交给生成器重写，最多循环 N 轮。

适合：

- 有清晰、可写下来的评价标准；
- 第一版通常不够好，但反馈能指导改进；
- 多轮迭代的收益大于额外成本。

典型例子：文学翻译、复杂查询、对格式和字数有严格要求的文案。

对应示例：[`examples/evaluator-optimizer`](../examples/evaluator-optimizer)

## 五种模式怎么选

先看任务形态，再看控制流：

| 问题形态 | 优先考虑 | 原因 |
| --- | --- | --- |
| 有明确的先后步骤 | Prompt chaining | 顺序固定，检查点清晰 |
| 输入天然分成几类 | Routing | 每类可以用专门的提示词 |
| 子任务互相独立、可以同时做 | Parallelization | 降延迟，或多角度汇总 |
| 子任务要看了输入才知道 | Orchestrator-workers | 拆解本身需要模型参与 |
| 有明确标准，需要反复打磨 | Evaluator-optimizer | 生成和评审分离，反馈可执行 |
| 步数无法预测，需要边做边看 | Agent | Workflow 写死不了路径 |

这张表不是互斥的。真实系统经常组合使用：

- Routing 的每个分支内部可以是 Prompt chaining；
- Parallelization 的每个分支可以是一个 Evaluator-optimizer；
- Orchestrator-workers 的 worker 可以调用工具，甚至内部再跑一个小 Agent。

组合时仍然要守住一个原则：**每一层都要能说清楚，为什么这里需要模型决策，而不是代码分支。**

## 什么时候 Workflow 不够用

出现下面这些信号时，说明固定路径开始吃力：

- 需要多少步取决于中间结果，写代码时无法枚举；
- 每一步要调用哪个工具，取决于上一步返回了什么；
- 任务经常需要“回头再做一次”，而回头的条件无法提前定义；
- 失败后需要模型自己换一种策略，而不是走你预设的 fallback。

这些正是 Agent 的适用场景。但注意，Agent 不是“没有编排”，而是把编排逻辑从代码移到了模型的循环里。第 9 篇会详细讲这个循环怎么写、怎么加护栏。

## 运行示例

五个模式都有 Claude 和 OpenAI 两个自包含版本。先看参数：

```bash
go run ./examples/prompt-chaining/claude -h
go run ./examples/routing/claude -h
go run ./examples/parallelization/claude -h
go run ./examples/orchestrator-workers/claude -h
go run ./examples/evaluator-optimizer/claude -h
```

运行时会调用真实 API 并产生费用。Claude 版本读取 `ANTHROPIC_API_KEY`，OpenAI 版本读取 `OPENAI_API_KEY`；模型和 Base URL 分别用 `ANTHROPIC_MODEL`、`ANTHROPIC_BASE_URL` 和 `OPENAI_MODEL`、`OPENAI_BASE_URL` 覆盖。

## 本文小结

- Workflow 的本质是**代码掌握控制流**，模型负责每个节点内的理解和生成。
- 五种模式解决的是不同的编排问题，不是能力等级。
- Prompt chaining 换准确率，Routing 换分工，Parallelization 换延迟和多角度，Orchestrator-workers 换动态拆解，Evaluator-optimizer 换迭代质量。
- 能用固定路径解决，就不要把控制权交给模型。
- 只有当路径真的无法提前写死时，才进入 Agent。

接下来的五篇会逐一拆开这五种模式，每篇对应一个可以直接运行的 Go 示例。下一篇从最简单的 **Prompt Chaining** 开始。

---

- 系列目录：[README.md](README.md)
- 上一篇：[Building Block：Augmented LLM](02-augmented-llm.md)
- 下一篇：[Prompt Chaining：把任务拆成可检查的步骤](04-prompt-chaining.md)
