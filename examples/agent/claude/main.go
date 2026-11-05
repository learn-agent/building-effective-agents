// Autonomous agent loop with the Anthropic SDK.
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

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
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

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		log.Fatal("请设置 ANTHROPIC_API_KEY")
	}

	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if base := os.Getenv("ANTHROPIC_BASE_URL"); base != "" {
		opts = append(opts, option.WithBaseURL(base))
	}
	client := anthropic.NewClient(opts...)

	model := anthropic.ModelClaudeHaiku5_5
	if m := os.Getenv("ANTHROPIC_MODEL"); m != "" {
		model = anthropic.Model(m)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	tools := []anthropic.ToolUnionParam{
		{
			OfTool: &anthropic.ToolParam{
				Name:        "get_weather",
				Description: anthropic.String("查询中国城市当前的模拟天气。"),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: map[string]any{
						"city": map[string]any{
							"type":        "string",
							"description": "城市名，例如北京、上海、深圳",
						},
					},
					Required: []string{"city"},
				},
			},
		},
	}

	system := `你是一个会使用工具的助手。
需要外部信息时必须调用工具，不要编造工具结果。
拿到工具结果后继续推理；信息足够时，直接用中文给出最终答案。`

	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(*question)),
	}

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

		if text := textOf(resp); strings.TrimSpace(text) != "" {
			fmt.Printf("第 %d 步 · 助手：%s\n", step, text)
		}

		// 把助手的文本和 tool_use 块原样加入对话。
		messages = append(messages, resp.ToParam())

		var toolResults []anthropic.ContentBlockParamUnion
		for _, block := range resp.Content {
			if block.Type != "tool_use" {
				continue
			}

			fmt.Printf("第 %d 步 · 工具调用：%s(%s)\n", step, block.Name, block.Input)

			result, err := getWeather(block.Input)
			isError := err != nil
			if isError {
				result = "错误：" + err.Error()
			}
			fmt.Printf("第 %d 步 · 工具结果：%s\n", step, result)

			toolResults = append(toolResults, anthropic.NewToolResultBlock(block.ID, result, isError))
		}

		if len(toolResults) == 0 {
			fmt.Println("\n最终答案：")
			fmt.Println(textOf(resp))
			return
		}

		// Anthropic 把 tool_result 放在 user turn 里返回给模型。
		messages = append(messages, anthropic.NewUserMessage(toolResults...))
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

func textOf(msg *anthropic.Message) string {
	var b strings.Builder
	for _, block := range msg.Content {
		if block.Type == "text" {
			b.WriteString(block.Text)
		}
	}
	return b.String()
}
