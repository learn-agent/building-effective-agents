// Autonomous agent loop with the OpenAI SDK.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
)

const maxSteps = 8

var weather = map[string]string{
	"北京": "晴，25°C",
	"上海": "多云，29°C",
	"深圳": "阵雨，31°C",
}

func main() {
	log.SetFlags(0)

	question := flag.String("q", "北京和上海今天天气如何？哪个更热？", "给 agent 的任务")
	flag.Parse()

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("请设置 OPENAI_API_KEY")
	}

	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if base := os.Getenv("OPENAI_BASE_URL"); base != "" {
		opts = append(opts, option.WithBaseURL(base))
	}
	client := openai.NewClient(opts...)

	model := openai.ChatModelGPT5Mini
	if m := os.Getenv("OPENAI_MODEL"); m != "" {
		model = shared.ChatModel(m)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	tools := []openai.ChatCompletionToolUnionParam{
		openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        "get_weather",
			Description: openai.String("查询中国城市当前的模拟天气。"),
			Parameters: shared.FunctionParameters(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{
						"type":        "string",
						"description": "城市名，例如北京、上海、深圳",
					},
				},
				"required": []string{"city"},
			}),
		}),
	}

	system := `你是一个会使用工具的助手。
需要外部信息时必须调用工具，不要编造工具结果。
拿到工具结果后继续推理；信息足够时，直接用中文给出最终答案。`

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(system),
		openai.UserMessage(*question),
	}

	for step := 1; step <= maxSteps; step++ {
		resp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
			Model:               model,
			MaxCompletionTokens: openai.Int(2048),
			Messages:            messages,
			Tools:               tools,
		})
		if err != nil {
			log.Fatal(err)
		}
		if len(resp.Choices) == 0 {
			log.Fatal("返回空 choices")
		}

		choice := resp.Choices[0]
		if text := strings.TrimSpace(choice.Message.Content); text != "" {
			fmt.Printf("第 %d 步 · 助手：%s\n", step, text)
		}

		// 把助手的文本和 tool_calls 原样加入对话。
		messages = append(messages, choice.Message.ToParam())

		if len(choice.Message.ToolCalls) == 0 {
			fmt.Println("\n最终答案：")
			fmt.Println(choice.Message.Content)
			return
		}

		for _, call := range choice.Message.ToolCalls {
			fmt.Printf("第 %d 步 · 工具调用：%s(%s)\n", step, call.Function.Name, call.Function.Arguments)

			result, err := getWeather(json.RawMessage(call.Function.Arguments))
			if err != nil {
				result = "错误：" + err.Error()
			}
			fmt.Printf("第 %d 步 · 工具结果：%s\n", step, result)

			messages = append(messages, openai.ToolMessage(result, call.ID))
		}
	}

	log.Fatalf("超过最大步数 %d，agent 未给出最终答案", maxSteps)
}

func getWeather(input json.RawMessage) (string, error) {
	var args struct {
		City string `json:"city"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("解析参数: %w", err)
	}

	city := strings.TrimSuffix(strings.TrimSpace(args.City), "市")
	if city == "" {
		return "", fmt.Errorf("city 不能为空")
	}

	value, ok := weather[city]
	if !ok {
		return "", fmt.Errorf("没有 %s 的模拟天气数据（可用：北京、上海、深圳）", city)
	}
	return value, nil
}
