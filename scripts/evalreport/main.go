// 读取指定一批的评测记录，人工标注后重算指标；不会再次调用模型或产生新记录。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"

	"knowledge-qa/config"
	"knowledge-qa/eval"
	"knowledge-qa/store"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	idsText := flag.String("run-ids", "", "本批 run_id 列表，以逗号分隔")
	input := flag.String("input", "", "/api/eval/run 保存的 JSON 文件；与 -run-ids 二选一")
	format := flag.String("format", "json", "输出 json 或 markdown")
	flag.Parse()
	if flag.NArg() != 0 || (*idsText == "") == (*input == "") {
		return fmt.Errorf("用法：go run ./scripts/evalreport -input eval/first-run.json [-format markdown]，或 -run-ids 1,2,3")
	}
	if *format != "json" && *format != "markdown" {
		return fmt.Errorf("format 只能是 json 或 markdown")
	}
	var ids []int64
	if *input != "" {
		raw, err := os.ReadFile(*input)
		if err != nil {
			return err
		}
		var original eval.Report
		if err := json.Unmarshal(raw, &original); err != nil {
			return err
		}
		for _, detail := range original.Details {
			ids = append(ids, detail.ID)
		}
	} else {
		for _, value := range strings.Split(*idsText, ",") {
			id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			if err != nil || id <= 0 {
				return fmt.Errorf("run_id 必须是正整数：%q", value)
			}
			ids = append(ids, id)
		}
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	st, err := store.New(cfg.MySQLDSN)
	if err != nil {
		return err
	}
	defer st.Close()
	runs, err := st.LoadEvalRuns(ids)
	if err != nil {
		return err
	}
	set := ""
	if len(runs) > 0 {
		set = runs[0].QuestionSet
		for _, r := range runs[1:] {
			if r.QuestionSet != set {
				set = "all"
				break
			}
		}
	}
	report := eval.SummarizeWithSet(runs, set)
	eval.LogReport(report)
	if *format == "markdown" {
		printMarkdown(report)
		return nil
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func cell(value string) string {
	value = strings.ReplaceAll(value, "|", "&#124;")
	value = strings.ReplaceAll(value, "\r", "")
	return strings.ReplaceAll(value, "\n", "<br>")
}

func state(value *bool) string {
	if value == nil {
		return "NULL"
	}
	return strconv.FormatBool(*value)
}

func rateText(rate *float64) string {
	if rate == nil {
		return "NULL"
	}
	return fmt.Sprintf("%.2f%%", *rate*100)
}

func printMarkdown(report eval.Report) {
	fmt.Println("# 本批评测结果")
	fmt.Println("\n| 指标 | 比例 |")
	fmt.Println("|---|---|")
	fmt.Printf("| 检索命中率 | %.2f%% |\n| 引用准确率 | %.2f%% |\n| 答案可用率 | %s |\n| 拒答正确率 | %.2f%% |\n",
		report.HitRate*100, report.CitationRate*100, rateText(report.UsableRate), report.RefusalRate*100)
	fmt.Printf("\n共 %d 题（可作答 %d 题，误拒 %d 题）；系统非拒答 %d 条；人工已标注 %d 条、待标注 %d 条。答案可用率按已标注回答计算，未标注时为 NULL。\n",
		report.Counts.QuestionCount, report.Counts.AnswerableCount, report.Counts.FalseRefusalCount, report.Counts.AnsweredCount, report.Counts.MarkedCount, report.Counts.PendingCount)
	if len(report.Categories) > 0 {
		fmt.Println("\n## 分类小计")
		fmt.Println("\n| 分类 | 题数 | 检索命中率 | 引用准确率 | 答案可用率 | 拒答正确率 | 误拒数 |")
		fmt.Println("|---|---|---|---|---|---|---|")
		keys := make([]string, 0, len(report.Categories))
		for k := range report.Categories {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			cs := report.Categories[k]
			fmt.Printf("| %s | %d | %s (%d/%d) | %s | %s (%d/%d) | %s (%d/%d) | %d |\n",
				k, cs.QuestionCount,
				rateText(cs.HitRate), cs.HitCount, cs.AnswerableCount,
				rateText(cs.CitationRate),
				rateText(cs.UsableRate), cs.UsableCount, cs.MarkedCount,
				rateText(cs.RefusalRate), cs.RefusalCorrectCount, cs.ShouldRefuseCount,
				cs.FalseRefusalCount)
		}
	}
	fmt.Println("\n| run_id | 问题 | 应拒答 | gold_chunk_id | 检索 Top 5 | 实际拒答 | hit | citation_ok | usable | 回答 |")
	fmt.Println("|---|---|---|---|---|---|---|---|---|---|")
	for _, row := range report.Details {
		gold := "NULL"
		if row.GoldChunkID != nil {
			gold = strconv.FormatInt(*row.GoldChunkID, 10)
		}
		ids, _ := json.Marshal(row.RetrievedChunkIDs)
		fmt.Printf("| %d | %s | %t | %s | %s | %t | %s | %s | %s | %s |\n", row.ID, cell(row.Question),
			row.ShouldRefuse, gold, ids, row.Refused, state(row.Hit), state(row.CitationOK), state(row.Usable), cell(row.Answer))
	}
}
