// 切片统计工具：用切片器 v2 对 corpus/ 全部 .md 切片，
// 输出总段数、平均长度、最长/最短段、按文档分布，写入 corpus/STATS.md。
// 纯本地计算，不需要 MySQL 和 LLM API。
// 运行方式：在项目根目录执行 go run ./scripts/stats
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"knowledge-qa/ingest"
)

func main() {
	files, err := filepath.Glob("corpus/*.md")
	if err != nil || len(files) == 0 {
		log.Fatal("corpus/ 下没有找到 .md 文件")
	}
	sort.Strings(files)

	type docStat struct {
		name  string
		count int
		chars int
	}
	var stats []docStat
	totalChunks, totalChars := 0, 0
	maxLen, minLen := -1, -1
	var maxPiece, minPiece ingest.ChunkPiece
	var maxFile, minFile string

	for _, f := range files {
		base := filepath.Base(f)
		// STATS.md 是本工具生成的报告，不算语料
		if strings.EqualFold(base, "STATS.md") {
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			log.Fatalf("读取 %s 失败：%v", f, err)
		}
		pieces := ingest.SplitMarkdownV2(string(raw))
		ds := docStat{name: base}
		for _, p := range pieces {
			n := utf8.RuneCountInString(p.Content)
			ds.count++
			ds.chars += n
			totalChunks++
			totalChars += n
			if n > maxLen {
				maxLen, maxPiece, maxFile = n, p, base
			}
			if minLen < 0 || n < minLen {
				minLen, minPiece, minFile = n, p, base
			}
		}
		stats = append(stats, ds)
	}

	// 组装 STATS.md
	var sb strings.Builder
	sb.WriteString("# 语料切片统计（切片器 v2）\n\n")
	fmt.Fprintf(&sb, "- 文档数：%d\n", len(stats))
	fmt.Fprintf(&sb, "- 总段数：%d\n", totalChunks)
	avg := 0
	if totalChunks > 0 {
		avg = totalChars / totalChunks
	}
	fmt.Fprintf(&sb, "- 平均段长：%d 字符\n", avg)
	fmt.Fprintf(&sb, "- 最长段：%d 字符（%s · %s）\n", maxLen, maxFile, maxPiece.HeadingPath)
	fmt.Fprintf(&sb, "- 最短段：%d 字符（%s · %s）\n", minLen, minFile, minPiece.HeadingPath)
	sb.WriteString("\n## 按文档分布\n\n")
	sb.WriteString("| 文档 | 段数 | 总字符数 | 平均段长 |\n|---|---|---|---|\n")
	for _, ds := range stats {
		a := 0
		if ds.count > 0 {
			a = ds.chars / ds.count
		}
		fmt.Fprintf(&sb, "| %s | %d | %d | %d |\n", ds.name, ds.count, ds.chars, a)
	}
	// 附最长段预览，便于人工检查是否切得合理
	preview := maxPiece.Content
	if utf8.RuneCountInString(preview) > 200 {
		r := []rune(preview)
		preview = string(r[:200]) + "..."
	}
	sb.WriteString("\n## 最长段预览（前 200 字符）\n\n```\n" + preview + "\n```\n")

	if err := os.WriteFile("corpus/STATS.md", []byte(sb.String()), 0o644); err != nil {
		log.Fatal("写 STATS.md 失败：", err)
	}
	fmt.Printf("STATS.md 已生成：总段数 %d，平均 %d 字符，最长 %d，最短 %d\n",
		totalChunks, avg, maxLen, minLen)
}
