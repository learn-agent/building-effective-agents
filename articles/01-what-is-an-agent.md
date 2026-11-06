# 什么是 Agent，什么不是？

> 这是《Building Effective Agents》精读系列第 1 篇。原文：<https://www.anthropic.com/research/building-effective-agents>
>
> 本文只讨论 Anthropic 在文章里使用的架构定义，不讨论营销语境下被泛化的“Agent”。

## 先说结论

Agent 不是“用了 LLM 的产品”，也不是“会调用工具的程序”，更不是“比 workflow 更高级的东西”。

Anthropic 把很多种系统统称为 **agentic systems**，但在这之下做了一个很关键的架构区分：

> Workflows are systems where LLMs and tools are orchestrated through predefined code paths.
>
> Agents are systems where LLMs dynamically direct their own processes and tool usage, maintaining control over how they accomplish tasks.

翻成中文：

- **Workflow**：LLM 和工具由**预先写好的代码路径**编排。
- **Agent**：LLM **自己决定**执行过程和工具使用，掌握“怎么完成任务”的控制权。

一句话：**看控制流在谁手里。**

## 先看一次真实的 HTTP 往返

在继续讲概念之前，先看一次最小的工具调用。Claude 不会自己执行 `get_weather`；它只会在响应里告诉你“我要调用这个工具”。真正执行工具的是你的程序。

整个往返长这样：

```text
用户问题
  -> POST /v1/messages（带上 tools）
  <- stop_reason: "tool_use"，content 里有 tool_use
  -> 你的程序执行工具
  -> POST /v1/messages（带上 assistant 的 tool_use + user 的 tool_result）
  <- stop_reason: "end_turn"，content 里是最终文本
```

### 第一次请求：模型要求调用工具

```bash
export ANTHROPIC_BASE_URL="${ANTHROPIC_BASE_URL:-https://api.anthropic.com}"
export ANTHROPIC_MODEL="${ANTHROPIC_MODEL:-claude-haiku-5-5}"

curl "${ANTHROPIC_BASE_URL%/}/v1/messages" \
  -H "x-api-key: $ANTHROPIC_API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "content-type: application/json" \
  -d @- <<JSON
{
  "model": "$ANTHROPIC_MODEL",
  "max_tokens": 1024,
  "tools": [
    {
      "name": "get_weather",
      "description": "查询指定城市的当前天气",
      "input_schema": {
        "type": "object",
        "properties": {
          "city": {"type": "string", "description": "城市名，例如北京"}
        },
        "required": ["city"]
      }
    }
  ],
  "messages": [
    {"role": "user", "content": "北京今天天气如何？"}
  ]
}
JSON
```

`ANTHROPIC_MODEL` 默认是 `claude-haiku-5-5`。如果使用中转/代理，把它换成代理支持的模型名，并把 `ANTHROPIC_BASE_URL` 指向代理地址。`ANTHROPIC_BASE_URL` 填到域名即可，curl/SDK 会自己拼 `/v1/messages`。Claude 返回的响应大致如下：

```json
{
  "id": "msg_01...",
  "type": "message",
  "role": "assistant",
  "model": "claude-haiku-5-5",
  "content": [
    {
      "type": "tool_use",
      "id": "toolu_01...",
      "name": "get_weather",
      "input": {"city": "北京"}
    }
  ],
  "stop_reason": "tool_use",
  "stop_sequence": null,
  "usage": {"input_tokens": 12, "output_tokens": 18}
}
```

这里最重要的是三件事：

- `stop_reason` 是 `"tool_use"`，说明模型还没给出最终答案，它在等工具结果。
- `content` 是数组；找到 `type` 为 `"tool_use"` 的块，里面有工具名 `name`、参数 `input` 和这次调用的 `id`。
- `tool_use.id` 是关联 ID，回传 `tool_result` 时必须带上。

`content` 是数组，因为一次响应可能包含多个块：普通文本、thinking、一个或多个 `tool_use`。代码里按 `type` 遍历即可。工具本身由你的代码执行。假设查到的结果是 `晴，25°C`。

### 真实调用返回：多了一个 thinking 块

上面的响应为了说明结构做了精简。比如我用 DeepSeek 调用后，返回的结构如下：

```json
{
  "id": "66c12d11-d765-43fb-be85-115b5e87875c",
  "type": "message",
  "role": "assistant",
  "model": "deepseek-flash[1m]",
  "content": [
    {
      "type": "thinking",
      "thinking": "We need answer requires weather tool. Call get_weather city 北京.",
      "signature": "66c12d11-d765-43fb-be85-115b5e87875c"
    },
    {
      "type": "tool_use",
      "id": "call_00_mdlaKp0eg90ADkPs4pT26794",
      "name": "get_weather",
      "input": {
        "city": "北京"
      }
    }
  ],
  "stop_reason": "tool_use",
  "stop_sequence": null,
  "usage": {
    "input_tokens": 303,
    "cache_creation_input_tokens": 0,
    "cache_read_input_tokens": 0,
    "output_tokens": 54,
    "service_tier": "standard"
  }
}
```

这个真实响应多了几个值得注意的点：

- `tool_use` 不在 `content[0]`，而是在 `content[1]`。所以代码不能写死索引，必须遍历 `content` 并按 `type` 判断。
- `model` 可能是替代模型名，`usage` 里也可能多出缓存、服务层级等字段；这些不影响消息结构。
- 如果第一次响应包含 `thinking` 块，第二次请求里的 assistant `content` 要原样带上它们，不要只传 `tool_use`。

### 第二次请求：把工具结果交回模型

```bash
export ANTHROPIC_BASE_URL="${ANTHROPIC_BASE_URL:-https://api.anthropic.com}"
export ANTHROPIC_MODEL="${ANTHROPIC_MODEL:-claude-haiku-5-5}"

curl "${ANTHROPIC_BASE_URL%/}/v1/messages" \
  -H "x-api-key: $ANTHROPIC_API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "content-type: application/json" \
  -d @- <<JSON
{
  "model": "$ANTHROPIC_MODEL",
  "max_tokens": 1024,
  "tools": [
    {
      "name": "get_weather",
      "description": "查询指定城市的当前天气",
      "input_schema": {
        "type": "object",
        "properties": {
          "city": {"type": "string", "description": "城市名，例如北京"}
        },
        "required": ["city"]
      }
    }
  ],
  "messages": [
    {"role": "user", "content": "北京今天天气如何？"},
    {
      "role": "assistant",
      "content": [
        {
          "type": "tool_use",
          "id": "toolu_01...",
          "name": "get_weather",
          "input": {"city": "北京"}
        }
      ]
    },
    {
      "role": "user",
      "content": [
        {
          "type": "tool_result",
          "tool_use_id": "toolu_01...",
          "content": "晴，25°C"
        }
      ]
    }
  ]
}
JSON
```

这个示例沿用了精简版，所以 assistant 的 `content` 里只有 `tool_use`。实际调用如果返回了 `thinking` 块，要把 `thinking` 也一起放回 assistant 的 `content` 数组。

这次响应会变成最终答案。用 DeepSeek 时，实际返回如下：

```json
{
  "id": "0df5e53f-314f-4a76-a34c-54dbfb10829c",
  "type": "message",
  "role": "assistant",
  "model": "deepseek-flash[1m]",
  "content": [
    {
      "type": "thinking",
      "thinking": "",
      "signature": "0df5e53f-314f-4a76-a34c-54dbfb10829c"
    },
    {
      "type": "text",
      "text": "北京今天天气晴朗，当前气温约 **25°C**。适合外出，注意防晒和补水。"
    }
  ],
  "stop_reason": "end_turn",
  "stop_sequence": null,
  "usage": {
    "input_tokens": 246,
    "cache_creation_input_tokens": 0,
    "cache_read_input_tokens": 128,
    "output_tokens": 23,
    "service_tier": "standard"
  }
}
```

`stop_reason` 变成 `"end_turn"`，`content` 里是 `thinking` + `text`，没有新的 `tool_use`，循环就可以停止。

注意最终文本同样不一定在 `content[0]`：这里前面还有一个空的 `thinking` 块。所以 Go 里的 `textOf` 不是直接取第一个元素，而是遍历所有 `type == "text"` 的块再拼接。

如果模型一次返回多个 `tool_use`，就把多个 `tool_result` 放在同一条 `user` 消息的 `content` 数组里，每个结果的 `tool_use_id` 对应一个工具调用。

### 这跟 Go 代码怎么对应

在 Go SDK 里：

- `resp.ToParam()` 会把完整的 assistant content（包括 thinking 和 tool_use）原样放进下一轮对话；
- `anthropic.NewToolResultBlock(block.ID, result, isError)` 就是构造 `tool_result`；
- 把 tool result 作为 `user` 消息追加进去，再调用一次 `Messages.New`。

Agent 的循环本质上就是重复这三步。OpenAI SDK 的结构类似，只是字段名不同：工具调用在 `tool_calls` 里，工具结果用 `role: "tool"` 和 `tool_call_id` 回传。OpenAI 版本对应的环境变量是 `OPENAI_BASE_URL` 和 `OPENAI_MODEL`。

## 一张表看懂区别

| 系统 | 下一步由谁决定 | 执行路径 | 典型例子 |
| --- | --- | --- | --- |
| 单次 LLM 调用 | 人/代码 | 固定 | 总结、分类、改写 |
| Workflow | 代码 | 预定义，可有分支 | prompt chaining、routing、parallelization、orchestrator-workers、evaluator-optimizer |
| Agent | 模型 | 动态，步数不固定 | 工具调用循环、coding agent、computer use |

这张表是架构分类，不是能力排名。Agent 不一定比 Workflow 更聪明，只是把更多控制权交给了模型，同时也把更多不确定性带了进来。

## 什么不是 Agent

下面这些系统都可能是很好的 LLM 应用，但在 Anthropic 的定义里，它们不是 Agent。

### 1. 单次 LLM 调用

把一段文本交给模型，拿回一个结果：

```text
用户输入 -> LLM -> 输出
```

没有工具循环，没有环境反馈，也没有“下一步做什么”的决策。这是最基本的 LLM 调用。

### 2. RAG

检索增强生成通常是：

```text
用户问题 -> 检索 -> 拼接上下文 -> LLM -> 回答
```

检索是 LLM 的增强能力，但整个流程由代码固定。除非模型能够自己决定“要不要再检索一次、换什么查询、下一步调用什么工具”，否则它仍然是一个固定管线。

### 3. Prompt chaining

Prompt chaining 把任务拆成固定步骤，每一步处理上一步的输出：

```text
写提纲 -> 检查提纲 -> 写正文 -> 翻译
```

中间可以有程序化检查，但步骤和顺序是代码写死的。它属于 Workflow。

### 4. Routing

Routing 先让模型分类，再由代码把请求送到不同的处理路径：

```text
用户问题 -> 分类 -> 账单 / 技术 / 账户 分支
```

模型只参与了“分类”这一步，真正的控制流仍然在代码里。它属于 Workflow。

### 5. Parallelization

Parallelization 把任务拆成几个独立子任务并发执行，再汇总结果：

```text
          -> 分析 A ->
用户问题  -> 分析 B -> 汇总
          -> 分析 C ->
```

拆分方式、并发数量、汇总方式都是预先定义的。它属于 Workflow。

### 6. Orchestrator-workers

这个模式最容易和 Agent 混淆：编排器本身也是 LLM，它会根据任务动态拆出子任务。

但 Anthropic 仍然把它归为 Workflow。原因是：拆完之后，谁负责执行、怎么执行、最后怎么汇总，仍然由代码控制。模型的“动态”只发生在规划阶段，没有掌握整个执行循环。

### 7. Evaluator-optimizer

生成器写草稿，评审器给反馈，代码控制循环：

```text
生成 -> 评审 -> 不通过就再生成 -> 最多 N 轮
```

循环条件、最大轮数、什么时候停止，都是代码决定的。它属于 Workflow。

### 8. “用了工具”不等于 Agent

一个程序里出现 tool call，不代表它就是 Agent。关键问题是：

> 是代码决定“现在该调用哪个工具”，还是模型根据当前环境反馈决定？

如果是前者，即使有工具调用，它仍然是 Workflow 或普通自动化。

### 9. “用了 Agent 框架”也不等于 Agent

框架只是实现手段。用不用 LangChain、Eino、Claude Agent SDK，不改变系统的架构分类。一个固定流程即使用最复杂的框架来写，也不会自动变成 Agent。

## 什么才算 Agent

Anthropic 对 Agent 的描述里有几个关键词：

- 从用户的命令或交互开始；
- 任务明确后，模型**独立规划和执行**；
- 每一步都要从环境获得 **ground truth**，比如工具调用结果、代码执行结果；
- 可以在检查点暂停，等待人类反馈，或在遇到阻塞时向人类求助；
- 任务完成时终止，也常用最大迭代次数等停止条件来控制风险；
- 实现通常很直接：**LLM 在循环中使用工具，并根据环境反馈继续。**

把这几点压缩成一个实用判断：

1. **模型选择下一步动作或工具**，而不是代码写死顺序；
2. **模型根据工具结果调整计划**，而不是拿到结果后仍走固定分支；
3. **有明确的环境反馈**，模型不是在真空里“反思”；
4. **有停止条件**，包括任务完成、最大步数、预算或人工中断。

缺了控制权和环境反馈，就很难说是文章语境里的 Agent。

## 用 Go 看控制流的区别

先看 Workflow。下面的路径完全由代码决定：

```go
// Workflow：步骤和顺序是写死的
outline := callLLM("先写提纲")
if !valid(outline) {
    return
}
draft := callLLM("根据提纲写正文：\n" + outline)
fmt.Println(draft)
```

再看 Agent 的循环。下面这段来自 [`examples/agent/claude/main.go`](../examples/agent/claude/main.go)，使用 Anthropic SDK 直接调用：

```go
for step := 1; step <= maxSteps; step++ {
    resp, err := client.Messages.New(ctx, anthropic.MessageNewParams{
        Model:     model,
        MaxTokens: 2048,
        System:    []anthropic.TextBlockParam{{Text: system}},
        Messages:  messages,
        Tools:     tools,
    })
    if err != nil {
        log.Fatal(err)
    }

    // 把助手的文本和 tool_use 块加入对话
    messages = append(messages, resp.ToParam())

    var toolResults []anthropic.ContentBlockParamUnion
    for _, block := range resp.Content {
        if block.Type != "tool_use" {
            continue
        }

        result, err := getWeather(block.Input)
        isError := err != nil
        if isError {
            result = "错误：" + err.Error()
        }
        toolResults = append(toolResults,
            anthropic.NewToolResultBlock(block.ID, result, isError))
    }

    // 没有工具调用，说明模型认为可以给出最终答案
    if len(toolResults) == 0 {
        break
    }

    // 把工具结果交回模型，继续下一轮
    messages = append(messages, anthropic.NewUserMessage(toolResults...))
}
```

两段代码的差别不在“有没有 for 循环”，而在于：

> Workflow 的 `for`/`if` 条件由代码预先定义；Agent 的下一步由模型根据环境反馈决定。

OpenAI SDK 的版本在 [`examples/agent/openai/main.go`](../examples/agent/openai/main.go)，循环结构相同，只是消息和工具调用的类型不同。

## 什么时候该用 Agent

Anthropic 的建议很明确：**先找最简单的方案，只有在确实需要时才增加复杂度。**

Workflow 适合：

- 任务可以拆成已知的固定步骤；
- 需要可预测、可测试、可复现；
- 对延迟和成本比较敏感；
- 失败路径可以提前枚举。

Agent 适合：

- 无法事先确定需要多少步；
- 步骤依赖执行过程中得到的信息；
- 需要模型根据环境反馈不断调整计划；
- 有清晰的成功标准或验证手段，比如测试、编译器、工具返回值；
- 你愿意为更高的延迟和成本，换取处理开放问题的能力。

Agent 的代价也很真实：

- 更多模型调用，成本和延迟更高；
- 错误可能逐步累积；
- 行为更难预测，调试更困难；
- 需要沙箱、工具白名单、最大步数、预算限制、人工检查点和完整日志。

所以不要因为“Agent 听起来更先进”就上 Agent。很多场景把 prompt 写好、加一点检索、或者用固定 Workflow，效果已经足够。

## 本文小结

- **Agentic system** 是统称，包含 Workflow 和 Agent。
- **Workflow** 的控制流在代码里；**Agent** 的控制流在模型手里。
- 有工具调用、用了框架、模型参与分类，都不自动等于 Agent。
- Agent 的核心是：模型在循环中决定下一步，使用工具，获取环境反馈，并在停止条件满足时结束。
- Agent 不是默认答案。先问：这个任务真的无法预先拆成固定步骤吗？

下一篇会讲 Agent 的基础积木：**Augmented LLM**，也就是带有检索、工具和记忆的 LLM，以及它在 Go 里怎么直接调用。

---

- 系列目录：[README.md](README.md)
- 下一篇：[Building Block：Augmented LLM](02-augmented-llm.md)
