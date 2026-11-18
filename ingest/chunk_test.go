package ingest

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSplitMarkdownV1AndV2Preserved(t *testing.T) {
	md := "# 标题1\n\n第一段正文内容足够长用于测试是否被保留或者拆分，这里写满一百多个字符，确保 v1 和 v2 切片器都按既有规则输出而不会受 v3 新增代码影响。继续补齐字符数量到超过一百个字符。\n\n```go\n# 代码注释不是标题\nfmt.Println(\"ok\")\n```\n"
	v1 := SplitMarkdown(md)
	if len(v1) != 2 {
		t.Fatalf("v1 should still treat # inside code block as heading (got %d pieces)", len(v1))
	}
	v2 := SplitMarkdownV2(md)
	if len(v2) != 1 || v2[0].HeadingPath != "标题1" {
		t.Fatalf("v2 should protect # inside code block (got %+v)", v2)
	}
}

func TestSplitMarkdownV3TableUnder800StaysWhole(t *testing.T) {
	md := "# 活动方案\n\n## 排期表\n\n| 时间 | 环节 | 负责人 |\n| --- | --- | --- |\n| 10:00 | 开幕 | 张三 |\n| 14:00 | 颁奖 | 李四 |\n"
	pieces := SplitMarkdownV3(md)
	if len(pieces) != 1 {
		t.Fatalf("expected 1 piece for small table, got %d: %+v", len(pieces), pieces)
	}
	if pieces[0].HeadingPath != "活动方案 > 排期表" {
		t.Fatalf("unexpected heading_path: %q", pieces[0].HeadingPath)
	}
	if !strings.Contains(pieces[0].Content, "| 14:00 | 颁奖 | 李四 |") {
		t.Fatalf("table content missing row: %q", pieces[0].Content)
	}
}

func TestSplitMarkdownV3LargeTablePreservesHeaderOnEveryChunk(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("# 活动运营\n\n## 详细排期\n\n")
	sb.WriteString("| 日期 | 场馆 | 活动名称 | 负责人 |\n")
	sb.WriteString("| --- | --- | --- | --- |\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&sb, "| 2026-11-%02d | 主会场%02d | 创作者沙龙与内容消费专场第%02d期活动 | 运营同学%02d |\n",
			(i%28)+1, i, i, i)
	}
	// 末尾只留一条短章节测试 split-table 不会误合并进后续小节
	sb.WriteString("\n## 备注\n\n这是一段超过一百个字符的独立备注说明，用来验证前面超长表格切出来的最后一个子切片不会被错误地并入备注章节里去，从而保证表格切片的标题路径始终严格一致。")

	pieces := SplitMarkdownV3(sb.String())
	if len(pieces) < 3 {
		t.Fatalf("expected oversized table to split into multiple chunks + note chunk, got %d", len(pieces))
	}

	tableChunks := 0
	for i, p := range pieces {
		if p.HeadingPath == "活动运营 > 详细排期" {
			tableChunks++
			lines := strings.Split(strings.TrimSpace(p.Content), "\n")
			if len(lines) < 3 {
				t.Fatalf("table chunk %d has too few lines: %q", i, p.Content)
			}
			if lines[0] != "| 日期 | 场馆 | 活动名称 | 负责人 |" || lines[1] != "| --- | --- | --- | --- |" {
				t.Fatalf("table chunk %d missing repeated header: %q", i, p.Content)
			}
			if rc := utf8.RuneCountInString(p.Content); rc > maxChunkChars {
				t.Fatalf("table chunk %d exceeded %d runes: got %d", i, maxChunkChars, rc)
			}
		}
	}
	if tableChunks < 2 {
		t.Fatalf("expected at least 2 split table chunks, got %d", tableChunks)
	}
	last := pieces[len(pieces)-1]
	if last.HeadingPath != "活动运营 > 备注" || strings.Contains(last.Content, "| 日期 |") {
		t.Fatalf("trailing note chunk should remain isolated, got %+v", last)
	}
}

func TestSplitMarkdownV3HeaderlessProseWithOverlap(t *testing.T) {
	var sb strings.Builder
	for i := 1; i <= 45; i++ {
		fmt.Fprintf(&sb, "第%02d条会议纪要明确了内容消费产品团队在十一月的活动上线节奏、审核标准与跨部门协作接口人。", i)
	}
	pieces := SplitMarkdownV3(sb.String())
	if len(pieces) < 2 {
		t.Fatalf("expected long headerless txt to split into multiple chunks, got %d", len(pieces))
	}
	for i, p := range pieces {
		if p.HeadingPath != "" {
			t.Fatalf("headerless chunk %d expected empty heading_path, got %q", i, p.HeadingPath)
		}
		if rc := utf8.RuneCountInString(p.Content); rc > maxChunkChars {
			t.Fatalf("chunk %d exceeded %d runes: got %d", i, maxChunkChars, rc)
		}
		if i > 0 {
			prevTail := tailRunes(pieces[i-1].Content, v3OverlapChars)
			if !strings.HasPrefix(p.Content, prevTail) {
				t.Fatalf("chunk %d missing %d-char overlap from chunk %d.\nwant prefix: %q\ngot start: %q",
					i, v3OverlapChars, i-1, prevTail, string([]rune(p.Content)[:min(100, utf8.RuneCountInString(p.Content))]))
			}
		}
	}
}

func TestSplitMarkdownV3ProtectsCodeBlocks(t *testing.T) {
	codeBody := strings.Repeat("fmt.Println(\"hello world\")\n", 40)
	md := "# 代码规范\n\n## 示例\n\n前置说明文本。\n\n```go\n# 伪标题\n" + codeBody + "```\n"
	pieces := SplitMarkdownV3(md)
	foundCode := false
	for _, p := range pieces {
		if strings.Contains(p.Content, "```go") {
			foundCode = true
			if !strings.HasSuffix(strings.TrimSpace(p.Content), "```") {
				t.Fatalf("code fence was cut in the middle: %q", p.Content)
			}
		}
	}
	if !foundCode {
		t.Fatal("expected code block chunk")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
