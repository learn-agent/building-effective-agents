// Routing with the OpenAI SDK: classify first, then use a specialized prompt.
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

type route struct {
	Name        string
	Description string
	System      string
}

func main() {
	log.SetFlags(0)

	question := flag.String("q", "我的发票抬头写错了，怎么修改？", "用户问题")
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

	routes := []route{
		{
			Name:        "billing",
			Description: "账单、发票、退款、付款问题",
			System:      "你是账单支持专员。回答要具体，涉及金额或发票时提醒用户核对信息。",
		},
		{
			Name:        "technical",
			Description: "报错、集成、API、性能问题",
			System:      "你是技术支持工程师。优先给出可执行的排查步骤，不要泛泛而谈。",
		},
		{
			Name:        "account",
			Description: "登录、密码、账户、权限问题",
			System:      "你是账户支持专员。回答要说明操作路径，并提醒必要的安全注意事项。",
		},
	}

	var menu strings.Builder
	menu.WriteString("把用户问题分类为以下之一：\n")
	for _, r := range routes {
		fmt.Fprintf(&menu, "- %s: %s\n", r.Name, r.Description)
	}
	menu.WriteString("\n只输出分类名，不要解释。")

	classified, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:               model,
		MaxCompletionTokens: openai.Int(100),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("你是一名意图分类器。"),
			openai.UserMessage(fmt.Sprintf("%s\n用户问题：%s", menu.String(), *question)),
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	routeName := normalizeRoute(classified.Choices[0].Message.Content, routes)
	selected := route{
		Name:   "general",
		System: "你是通用客服助手。先确认用户的问题，再给出简洁、可执行的回答。",
	}
	for _, r := range routes {
		if r.Name == routeName {
			selected = r
			break
		}
	}

	fmt.Printf("路由结果：%s\n\n", selected.Name)
	fmt.Printf("用户问题：%s\n\n", *question)

	answer, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:               model,
		MaxCompletionTokens: openai.Int(1024),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(selected.System),
			openai.UserMessage(*question + "\n\n请用中文回答，最多 120 字。"),
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("回答：")
	fmt.Println(answer.Choices[0].Message.Content)
}

func normalizeRoute(text string, routes []route) string {
	text = strings.ToLower(strings.TrimSpace(text))
	text = strings.Trim(text, "`'\"。.!！?？ \n\t")
	for _, r := range routes {
		if text == r.Name || strings.Contains(text, r.Name) {
			return r.Name
		}
	}
	return "general"
}
