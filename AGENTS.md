# AGENTS.md

## 项目定位

本仓库是 Anthropic《Building effective agents》的学习笔记与 Go 示例：
https://www.anthropic.com/research/building-effective-agents

示例只使用官方 Anthropic Claude SDK 和 OpenAI SDK。不要引入 LangChain/LangGraph、Eino、AutoGen 等 agent 框架，也不要把示例扩展成通用框架。

## 代码结构

- Go module：`github.com/learn-agent/building-effective-agents`，`go.mod` 要求 Go 1.26，已在 Go 1.27.3 验证。不要仅为使用新工具链就提高 `go` 指令，保留对 1.26 的兼容。
- 每个模式都有两个独立版本：`examples/<pattern>/claude/main.go` 和 `examples/<pattern>/openai/main.go`。
- 两个版本都刻意保持自包含，直接创建 SDK client、直接发请求。**不要**把 provider 调用抽到 `internal` 包或公共 helper；重复代码是为了让每段代码能直接贴进博客。
- Claude 版本使用 Anthropic Messages API；OpenAI 版本使用 Chat Completions API。
- 小工具放在各自文件里：prompt chaining 的数量检查、orchestrator 的 `cleanJSON`、evaluator 的 `PASS`/`REVISE` 解析、agent 的工具执行。
- Agent 的 Claude 版本用 `resp.ToParam()` 保留完整 assistant content（可能包含 thinking 等额外块），不要改成只回传 `tool_use`；中转端点的返回结构可能比官方示例更丰富。
- `articles/` 是按原文顺序编号的中文博客草稿。文章里的代码片段尽量与 `examples/` 对应，并保持自包含；不要为了复用把片段改成跨文件调用。

## 常用命令

```bash
go fmt ./...
go build ./...
go vet ./...
go test ./...
go run ./examples/<pattern>/claude -h
go run ./examples/<pattern>/openai -h
go run ./examples/prompt-chaining/claude
```

`go run` 会调用真实 API 并产生费用；`go test ./...` 必须保持离线、免费且可重复。

## 凭证与 provider

- Claude 示例读取 `ANTHROPIC_API_KEY`，模型和 Base URL 分别用 `ANTHROPIC_MODEL`、`ANTHROPIC_BASE_URL`。
- OpenAI 示例读取 `OPENAI_API_KEY`，模型和 Base URL 分别用 `OPENAI_MODEL`、`OPENAI_BASE_URL`。
- 不再使用 `LLM_PROVIDER` 或共享的 `LLM_MODEL`：运行哪个目录就使用哪个 SDK 和对应环境变量。
- 默认模型/Base URL 只是 fallback；使用中转/代理时不应改代码，只改环境变量。
- 仓库没有 `.env` 自动加载，运行前需 export 或 `source .env`。`.env` 已被 gitignore，永远不要硬编码或提交密钥。

## 测试与内容约定

- 默认测试不得调用真实 API。目前 `go test ./...` 主要验证所有示例可编译。
- 新增示例时同时提供 `claude/` 和 `openai/` 两个自包含版本，并在 README 示例表中补一行。
- 示例对应文章中的模式：prompt chaining、routing、parallelization、orchestrator-workers、evaluator-optimizer，以及 autonomous agent loop。
- 单个示例优先保持短小、直接；不要为了“整洁”提前制造抽象层。
