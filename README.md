# Building Effective Agents · Go 示例

Anthropic 文章 [Building effective agents](https://www.anthropic.com/research/building-effective-agents) 的 Go 示例。

每个模式都提供 Claude 和 OpenAI 两个版本。代码刻意不做公共封装：每个 `main.go` 都是自包含的，直接从创建 SDK client 开始，方便单独复制到博客文章里。

## 文章系列

- [什么是 Agent，什么不是？](articles/01-what-is-an-agent.md)
- [系列目录与写作计划](articles/README.md)

## 示例目录

| 模式 | Claude SDK | OpenAI SDK | 演示内容 |
| --- | --- | --- | --- |
| Prompt chaining | [`claude`](examples/prompt-chaining/claude) | [`openai`](examples/prompt-chaining/openai) | 生成三条口号，程序化检查数量后，再生成宣传文案 |
| Routing | [`claude`](examples/routing/claude) | [`openai`](examples/routing/openai) | 先分类问题，再交给账单/技术/账户专用提示词 |
| Parallelization | [`claude`](examples/parallelization/claude) | [`openai`](examples/parallelization/openai) | 三个独立分析并发执行，最后综合成建议 |
| Orchestrator-workers | [`claude`](examples/orchestrator-workers/claude) | [`openai`](examples/orchestrator-workers/openai) | 编排器拆解任务，多个 worker 并发执行，最后汇总 |
| Evaluator-optimizer | [`claude`](examples/evaluator-optimizer/claude) | [`openai`](examples/evaluator-optimizer/openai) | 生成草稿 → 评审 → 按反馈修改，最多三轮 |
| Autonomous agent | [`claude`](examples/agent/claude) | [`openai`](examples/agent/openai) | 模型自主调用天气工具，循环直到给出答案 |

两个版本使用同一套提示词和流程，区别只在 SDK 调用方式：Claude 使用 Messages API，OpenAI 使用 Chat Completions API。

## 准备

需要 Go 1.26+（已在 Go 1.27.3 验证）。`go.mod` 保持 `go 1.26.0`，以兼容略旧的工具链。

```bash
export ANTHROPIC_API_KEY=...
# 或者
export OPENAI_API_KEY=...
```

- Claude：`ANTHROPIC_API_KEY`，可选 `ANTHROPIC_BASE_URL`、`ANTHROPIC_MODEL`
- OpenAI：`OPENAI_API_KEY`，可选 `OPENAI_BASE_URL`、`OPENAI_MODEL`

使用中转/代理时，只改环境变量即可：

```bash
export ANTHROPIC_BASE_URL=https://your-proxy.example.com
export ANTHROPIC_MODEL=your-model-name

# 或者
export OPENAI_BASE_URL=https://your-proxy.example.com/v1
export OPENAI_MODEL=your-model-name
```

Base URL 的具体格式以代理文档为准。一般来说，Anthropic 填到域名即可（SDK 会拼 `/v1/messages`），OpenAI 通常包含 `/v1`（SDK 会拼 `/chat/completions`）。

仓库不会自动加载 `.env`。需要时可以这样导入：

```bash
set -a
source .env
set +a
```

## 运行

```bash
# Claude
go run ./examples/prompt-chaining/claude

# OpenAI
go run ./examples/prompt-chaining/openai

# 查看参数
go run ./examples/agent/claude -h
```

常用参数：

| 示例 | 参数 | 默认值 |
| --- | --- | --- |
| `prompt-chaining` | `-topic` | 独立书店的会员计划 |
| `routing` | `-q` | 发票抬头问题 |
| `parallelization` | `-topic` | 面向大学生的 AI 学习助手 |
| `orchestrator-workers` | `-goal` | AI 编程比赛宣传计划 |
| `evaluator-optimizer` | `-brief` | 程序员降噪耳机产品介绍 |
| `agent` | `-q` | 查询北京和上海天气并比较 |

运行示例会调用真实 API 并产生费用。

## 代码结构

```text
examples/<pattern>/claude/main.go   Anthropic SDK 版本
examples/<pattern>/openai/main.go   OpenAI SDK 版本
```

没有共享的 `internal` 包。示例之间的重复代码是刻意保留的：抽成公共层会降低博客片段的可读性和可复制性。Orchestrator 的 JSON 清洗、evaluator 的 `PASS`/`REVISE` 解析等小工具也都放在各自文件里。

## 验证

```bash
go build ./...
go test ./...
go vet ./...
```

`go test ./...` 目前只做编译检查，不访问外部 API。示例为了可读性省略了生产级的重试、限流、持久化和可观测性。
