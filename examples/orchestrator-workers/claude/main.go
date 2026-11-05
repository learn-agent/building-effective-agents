// Orchestrator-workers with the Anthropic SDK: plan, execute in parallel, synthesize.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

type plannedTask struct {
	Title       string `json:"title"`
	Instruction string `json:"instruction"`
}

type workerResult struct {
	Title string
	Text  string
	Err   error
}

func main() {
	log.SetFlags(0)

	goal := flag.String("goal", "为一场面向开发者的 AI 编程比赛制定宣传计划", "总目标")
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

	planResp, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     model,
		MaxTokens: 1024,
		System:    []anthropic.TextBlockParam{{Text: "你是一名任务规划器。只做拆解，不执行子任务。"}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(fmt.Sprintf(
				`把下面的目标拆成 3 个可以独立执行的子任务。
只返回 JSON 数组，不要 Markdown，不要解释。每个元素的格式：
{"title":"短标题","instruction":"给执行者的具体指令"}

目标：%s`,
				*goal,
			))),
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	var tasks []plannedTask
	if err := json.Unmarshal([]byte(cleanJSON(textOf(planResp))), &tasks); err != nil {
		log.Fatalf("解析计划: %v\n原始返回:\n%s", err, textOf(planResp))
	}
	if len(tasks) == 0 {
		log.Fatalf("计划为空\n原始返回:\n%s", textOf(planResp))
	}
	if len(tasks) > 4 {
		tasks = tasks[:4]
	}

	fmt.Println("编排器拆出的子任务：")
	for i, task := range tasks {
		fmt.Printf("  %d. %s — %s\n", i+1, task.Title, task.Instruction)
	}
	fmt.Println()

	results := make([]workerResult, len(tasks))
	var wg sync.WaitGroup

	for i, task := range tasks {
		wg.Add(1)
		go func(i int, task plannedTask) {
			defer wg.Done()

			resp, err := client.Messages.New(ctx, anthropic.MessageNewParams{
				Model:     model,
				MaxTokens: 1024,
				System:    []anthropic.TextBlockParam{{Text: "你是一名执行者。只完成分到的子任务，输出可直接并入最终方案的内容。"}},
				Messages: []anthropic.MessageParam{
					anthropic.NewUserMessage(anthropic.NewTextBlock(fmt.Sprintf(
						"总目标：%s\n\n子任务：%s\n\n%s",
						*goal, task.Title, task.Instruction,
					))),
				},
			})

			result := workerResult{Title: task.Title, Err: err}
			if resp != nil {
				result.Text = textOf(resp)
			}
			results[i] = result
		}(i, task)
	}

	wg.Wait()

	var combined strings.Builder
	for _, result := range results {
		if result.Err != nil {
			log.Fatalf("%s 执行失败: %v", result.Title, result.Err)
		}
		fmt.Printf("## %s\n%s\n\n", result.Title, result.Text)
		fmt.Fprintf(&combined, "## %s\n%s\n\n", result.Title, result.Text)
	}

	final, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     model,
		MaxTokens: 2048,
		System:    []anthropic.TextBlockParam{{Text: "你是一名主编。整合结果时要保留关键细节，删除重复内容。"}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(fmt.Sprintf(
				"总目标：%s\n\n以下是各个子任务的产出：\n%s\n请整合成一份简洁的最终方案。",
				*goal, combined.String(),
			))),
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("最终方案：")
	fmt.Println(textOf(final))
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

func cleanJSON(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}

	start := strings.IndexAny(s, "{[")
	end := strings.LastIndexAny(s, "}]")
	if start >= 0 && end > start {
		s = s[start : end+1]
	}
	return strings.TrimSpace(s)
}
