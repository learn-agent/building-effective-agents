# 《Building Effective Agents》精读系列

目标是按 Anthropic 原文的顺序，把 Agent 相关概念一步步拆开：

1. [什么是 Agent，什么不是？](01-what-is-an-agent.md)
2. Building Block：Augmented LLM
3. Workflow 总览：什么时候需要 workflow
4. Prompt Chaining
5. Routing
6. Parallelization
7. Orchestrator-Workers
8. Evaluator-Optimizer
9. Agent：自主循环

Workflow 部分可以合成一篇长文，也可以按模式拆成多篇。这里先按“一种模式一篇”规划，方便每篇都配一个可以直接运行的 Go 示例。

原文：<https://www.anthropic.com/research/building-effective-agents>

示例代码在 [`examples/`](../examples) 下，每个模式都有 `claude/` 和 `openai/` 两个自包含版本。
