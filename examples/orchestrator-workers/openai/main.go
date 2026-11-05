// Orchestrator-workers with the OpenAI SDK: plan, execute in parallel, synthesize.
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

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	planResp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:               model,
		MaxCompletionTokens: openai.Int(1024),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("你是一名任务规划器。只做拆解，不执行子任务。"),
			openai.UserMessage(fmt.Sprintf(
				`把下面的目标拆成 3 个可以独立执行的子任务。
只返回 JSON 数组，不要 Markdown，不要解释。每个元素的格式：
{"title":"短标题","instruction":"给执行者的具体指令"}

目标：%s`,
				*goal,
			)),
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	if len(planResp.Choices) == 0 {
		log.Fatal("规划器返回空 choices")
	}
	planText := planResp.Choices[0].Message.Content

	var tasks []plannedTask
	if err := json.Unmarshal([]byte(cleanJSON(planText)), &tasks); err != nil {
		log.Fatalf("解析计划: %v\n原始返回:\n%s", err, planText)
	}
	if len(tasks) == 0 {
		log.Fatalf("计划为空\n原始返回:\n%s", planText)
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

			resp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
				Model:               model,
				MaxCompletionTokens: openai.Int(1024),
				Messages: []openai.ChatCompletionMessageParamUnion{
					openai.SystemMessage("你是一名执行者。只完成分到的子任务，输出可直接并入最终方案的内容。"),
					openai.UserMessage(fmt.Sprintf(
						"总目标：%s\n\n子任务：%s\n\n%s",
						*goal, task.Title, task.Instruction,
					)),
				},
			})

			result := workerResult{Title: task.Title, Err: err}
			if err == nil {
				if len(resp.Choices) == 0 {
					result.Err = fmt.Errorf("返回空 choices")
				} else {
					result.Text = resp.Choices[0].Message.Content
				}
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

	final, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:               model,
		MaxCompletionTokens: openai.Int(2048),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("你是一名主编。整合结果时要保留关键细节，删除重复内容。"),
			openai.UserMessage(fmt.Sprintf(
				"总目标：%s\n\n以下是各个子任务的产出：\n%s\n请整合成一份简洁的最终方案。",
				*goal, combined.String(),
			)),
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("最终方案：")
	fmt.Println(final.Choices[0].Message.Content)
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
