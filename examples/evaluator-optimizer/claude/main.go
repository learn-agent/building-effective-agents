// Evaluator-optimizer with the Anthropic SDK: generate, evaluate, revise.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const requirements = `要求：
1. 不超过 80 个汉字
2. 明确目标用户
3. 包含一个具体收益
4. 只输出正文，不要标题和解释`

func main() {
	log.SetFlags(0)

	brief := flag.String("brief", "为一款面向程序员的降噪耳机写一段产品介绍", "写作任务")
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
	if m := os.Getenv("LLM_MODEL"); m != "" {
		model = anthropic.Model(m)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var draft, feedback string

	for round := 1; round <= 3; round++ {
		var prompt string
		if draft == "" {
			prompt = fmt.Sprintf("任务：%s\n\n%s", *brief, requirements)
		} else {
			prompt = fmt.Sprintf(
				"任务：%s\n\n%s\n\n上一版草稿：\n%s\n\n评审反馈：\n%s\n\n请根据反馈修改，只输出新草稿。",
				*brief, requirements, draft, feedback,
			)
		}

		generated, err := client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     model,
			MaxTokens: 1024,
			System:    []anthropic.TextBlockParam{{Text: "你是一名产品文案。严格遵守字数与格式要求。"}},
			Messages: []anthropic.MessageParam{
				anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
			},
		})
		if err != nil {
			log.Fatal(err)
		}
		draft = strings.TrimSpace(textOf(generated))

		evaluated, err := client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     model,
			MaxTokens: 512,
			System:    []anthropic.TextBlockParam{{Text: "你是一名严格的文案评审。"}},
			Messages: []anthropic.MessageParam{
				anthropic.NewUserMessage(anthropic.NewTextBlock(fmt.Sprintf(
					`任务：%s

%s

待评审草稿：
%s

检查草稿是否满足全部要求。
第一行只写 PASS 或 REVISE，第二行开始写具体、可执行的修改意见。`,
					*brief, requirements, draft,
				))),
			},
		})
		if err != nil {
			log.Fatal(err)
		}

		pass, feedback := parseVerdict(textOf(evaluated))

		// 字数检查是确定性的，不依赖评审模型。
		if utf8.RuneCountInString(draft) > 80 {
			pass = false
			if feedback == "" {
				feedback = "超过 80 字，请压缩。"
			}
		}

		fmt.Printf("第 %d 轮\n草稿：%s\n评审：pass=%v\n反馈：%s\n\n",
			round, draft, pass, feedback)

		if pass {
			fmt.Println("最终稿：")
			fmt.Println(draft)
			return
		}
	}

	fmt.Println("达到最大轮数，输出最后一版草稿：")
	fmt.Println(draft)
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

func parseVerdict(s string) (bool, string) {
	lines := strings.SplitN(strings.TrimSpace(s), "\n", 2)
	verdict := strings.ToUpper(strings.TrimSpace(lines[0]))
	verdict = strings.Trim(verdict, "`'\"。.!！:：")

	feedback := ""
	if len(lines) > 1 {
		feedback = strings.TrimSpace(lines[1])
	}
	return verdict == "PASS", feedback
}
