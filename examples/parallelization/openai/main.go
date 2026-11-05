// Parallelization with the OpenAI SDK: run independent analyses concurrently.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
)

type analysis struct {
	Name string
	Text string
	Err  error
}

func main() {
	log.SetFlags(0)

	topic := flag.String("topic", "一款面向大学生的 AI 学习助手", "要分析的产品")
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
	if m := os.Getenv("LLM_MODEL"); m != "" {
		model = shared.ChatModel(m)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	tasks := []struct {
		Name   string
		Prompt string
	}{
		{"用户价值", "从目标用户价值角度分析：它解决什么问题？谁最需要它？"},
		{"技术风险", "从技术实现角度分析：主要技术风险和落地难点是什么？"},
		{"商业可行性", "从商业角度分析：成本、付费意愿和可持续性如何？"},
	}

	results := make([]analysis, len(tasks))
	var wg sync.WaitGroup

	for i, task := range tasks {
		wg.Add(1)
		go func(i int, name, prompt string) {
			defer wg.Done()

			resp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
				Model:               model,
				MaxCompletionTokens: openai.Int(1024),
				Messages: []openai.ChatCompletionMessageParamUnion{
					openai.SystemMessage("你是一名产品分析顾问。回答要具体，避免空话。"),
					openai.UserMessage(fmt.Sprintf(
						"产品：%s\n\n%s\n\n用 3 条要点回答，每条约 30 字。",
						*topic, prompt,
					)),
				},
			})

			result := analysis{Name: name, Err: err}
			if err == nil {
				if len(resp.Choices) == 0 {
					result.Err = fmt.Errorf("返回空 choices")
				} else {
					result.Text = resp.Choices[0].Message.Content
				}
			}
			results[i] = result
		}(i, task.Name, task.Prompt)
	}

	wg.Wait()

	var combined strings.Builder
	for _, result := range results {
		if result.Err != nil {
			log.Fatalf("%s 分析失败: %v", result.Name, result.Err)
		}
		fmt.Printf("## %s\n%s\n\n", result.Name, result.Text)
		fmt.Fprintf(&combined, "## %s\n%s\n\n", result.Name, result.Text)
	}

	final, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:               model,
		MaxCompletionTokens: openai.Int(1024),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("你是一名产品负责人。"),
			openai.UserMessage(fmt.Sprintf(
				"产品：%s\n\n以下是三份独立分析：\n%s\n请综合成 3 条下一步建议，每条一句话。",
				*topic, combined.String(),
			)),
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("综合建议：")
	fmt.Println(final.Choices[0].Message.Content)
}
