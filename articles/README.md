# 《Building Effective Agents》精读系列

按 Anthropic 原文的顺序，把 Agent 相关概念一步步拆开。每篇配一个可以直接运行的 Go 示例。

原文：<https://www.anthropic.com/research/building-effective-agents>

## 目录

1. [什么是 Agent，什么不是？](01-what-is-an-agent.md)
   - 从一次真实的工具调用往返讲起，区分 Workflow 和 Agent，并说明“有工具”“用了框架”为什么不等于 Agent。
2. [Building Block：Augmented LLM](02-augmented-llm.md)
   - 检索、工具、记忆分别在哪一层增强模型，以及为什么增强本身不等于 Agent。
3. [Workflow 总览：什么时候需要 workflow](03-workflow-overview.md)
   - 五种模式的对比和选择标准，以及它们如何组合。
4. [Prompt Chaining：把任务拆成可检查的步骤](04-prompt-chaining.md)
   - 固定顺序、程序化 gate、把结构化结果交给下一步。
5. [Routing：先分类，再交给专门的提示词](05-routing.md)
   - 分类器、路由表、输出归一化和兜底路径。
6. [Parallelization：并发独立子任务](06-parallelization.md)
   - sectioning 与 voting，goroutine、并发上限、部分失败。
7. [Orchestrator-Workers：让模型拆任务，让代码管执行](07-orchestrator-workers.md)
   - 动态计划、JSON 解析、worker 并发和最终汇总。
8. [Evaluator-Optimizer：生成、评审、再修改](08-evaluator-optimizer.md)
   - 明确标准、结构化评审、确定性检查和最大轮数。
9. [Agent：自主循环与停止条件](09-agent-loop.md)
   - 完整工具循环、环境反馈、停止条件、可观测性和生产护栏。

## 阅读建议

- 第一次接触这几个概念：按顺序从第 1 篇读起。
- 已经了解 Workflow 和 Agent 的区别：可以直接跳到第 4 篇看具体模式。
- 准备写代码：每篇末尾都链到对应的 `claude/` 和 `openai/` 示例。
- 只想知道怎么选：先看[第 3 篇的对比表](03-workflow-overview.md#五种模式怎么选)和[第 9 篇的决策总结](09-agent-loop.md#什么时候不该用-agent)。

## 示例代码

示例在 [`examples/`](../examples) 下，每个模式都有 `claude/` 和 `openai/` 两个自包含版本：

| 模式 | 文章 | Claude | OpenAI |
| --- | --- | --- | --- |
| Prompt chaining | [第 4 篇](04-prompt-chaining.md) | [`claude`](../examples/prompt-chaining/claude) | [`openai`](../examples/prompt-chaining/openai) |
| Routing | [第 5 篇](05-routing.md) | [`claude`](../examples/routing/claude) | [`openai`](../examples/routing/openai) |
| Parallelization | [第 6 篇](06-parallelization.md) | [`claude`](../examples/parallelization/claude) | [`openai`](../examples/parallelization/openai) |
| Orchestrator-workers | [第 7 篇](07-orchestrator-workers.md) | [`claude`](../examples/orchestrator-workers/claude) | [`openai`](../examples/orchestrator-workers/openai) |
| Evaluator-optimizer | [第 8 篇](08-evaluator-optimizer.md) | [`claude`](../examples/evaluator-optimizer/claude) | [`openai`](../examples/evaluator-optimizer/openai) |
| Autonomous agent | [第 9 篇](09-agent-loop.md) | [`claude`](../examples/agent/claude) | [`openai`](../examples/agent/openai) |

运行示例会调用真实 API 并产生费用。凭证和环境变量见根目录 [README](../README.md#准备)。
