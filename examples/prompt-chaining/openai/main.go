// Prompt chaining with the OpenAI SDK.
package main

import (
	"context"
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

func main() {
	log.SetFlags(0)

	topic := flag.String("topic", "独立书店的会员计划", "要生成文案的主题")
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

	// 第一步：生成三条候选口号。
	first, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:               model,
		MaxCompletionTokens: openai.Int(2048),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("你是一名简洁的文案策划。"),
			openai.UserMessage(fmt.Sprintf(
				"为「%s」写 3 条不同的宣传口号。每条单独一行，不要编号，不要解释。",
				*topic,
			)),
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	// 程序化检查：只有恰好三条非空口号才继续。
	firstText := first.Choices[0].Message.Content
	slogans := nonEmptyLines(firstText)
	if len(slogans) != 3 {
		log.Fatalf("程序化检查失败：期望 3 条口号，实际 %d 条\n原始返回:\n%s", len(slogans), firstText)
	}
	fmt.Println("步骤 1：生成 3 条口号（已通过数量检查）")
	for i, slogan := range slogans {
		fmt.Printf("  %d. %s\n", i+1, slogan)
	}

	// 第二步：把通过检查的结果交给下一段提示词。
	second, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:               model,
		MaxCompletionTokens: openai.Int(2048),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("你是一名品牌文案。"),
			openai.UserMessage(fmt.Sprintf(
				"主题：%s\n\n候选口号：\n%s\n\n选出最合适的一条，并写一段不超过 80 字的宣传文案。先写口号，再写文案。",
				*topic, strings.Join(slogans, "\n"),
			)),
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("\n步骤 2：基于口号生成宣传文案")
	fmt.Println(second.Choices[0].Message.Content)
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimLeft(line, "-*0123456789.、) ")
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}
