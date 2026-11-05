// Prompt chaining with the Anthropic SDK.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

func main() {
	log.SetFlags(0)

	topic := flag.String("topic", "独立书店的会员计划", "要生成文案的主题")
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

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// 第一步：生成三条候选口号。
	first, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     model,
		MaxTokens: 2048,
		System:    []anthropic.TextBlockParam{{Text: "你是一名简洁的文案策划。"}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(fmt.Sprintf(
				"为「%s」写 3 条不同的宣传口号。每条单独一行，不要编号，不要解释。",
				*topic,
			))),
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	// 程序化检查：只有恰好三条非空口号才继续。
	slogans := nonEmptyLines(textOf(first))
	if len(slogans) != 3 {
		log.Fatalf("程序化检查失败：期望 3 条口号，实际 %d 条\n原始返回:\n%s", len(slogans), textOf(first))
	}
	fmt.Println("步骤 1：生成 3 条口号（已通过数量检查）")
	for i, slogan := range slogans {
		fmt.Printf("  %d. %s\n", i+1, slogan)
	}

	// 第二步：把通过检查的结果交给下一段提示词。
	second, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     model,
		MaxTokens: 2048,
		System:    []anthropic.TextBlockParam{{Text: "你是一名品牌文案。"}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(fmt.Sprintf(
				"主题：%s\n\n候选口号：\n%s\n\n选出最合适的一条，并写一段不超过 80 字的宣传文案。先写口号，再写文案。",
				*topic, strings.Join(slogans, "\n"),
			))),
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("\n步骤 2：基于口号生成宣传文案")
	fmt.Println(textOf(second))
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
