package generate

import (
	"reflect"
	"strings"
	"testing"

	"knowledge-qa/retrieve"
	"knowledge-qa/store"
)

func TestBuildPromptV1AndV2MetadataHeaders(t *testing.T) {
	v1Top := []retrieve.MergedChunk{
		{Chunk: store.Chunk{ID: 1, Content: "Group 创建路由组。"}},
	}
	v1Prompt := BuildPrompt("路由组", v1Top)
	if !strings.Contains(v1Prompt, "[1] Group 创建路由组。") {
		t.Fatalf("V1 切片不应附加 V2 标头，实际：\n%s", v1Prompt)
	}
	if strings.Contains(v1Prompt, "deprecated（已废弃）") {
		t.Fatalf("纯 V1 切片不应出现废弃提示规则")
	}

	repID := int64(45)
	v2Top := []retrieve.MergedChunk{
		{Chunk: store.Chunk{
			ID:          1,
			ProjectSlug: "project-a",
			DocLevel:    "spec",
			DocStatus:   "active",
			Content:     "新版端口为 8091。",
		}},
		{Chunk: store.Chunk{
			ID:              2,
			ProjectSlug:     "project-b",
			DocLevel:        "note",
			DocStatus:       "deprecated",
			ReplacedByDocID: &repID,
			ReplacedByTitle: "新版规范 v2",
			Content:         "旧版端口为 8080。",
		}},
	}
	v2Prompt := BuildPrompt("端口是多少", v2Top)
	for _, want := range []string{
		"[1]（项目: project-a | 类型: spec | 状态: active） 新版端口为 8091。",
		"[2]（项目: project-b | 类型: note | 状态: deprecated（已废弃），替代文档: 新版规范 v2） 旧版端口为 8080。",
		"5. 请注意各段标头中的文档类型",
	} {
		if !strings.Contains(v2Prompt, want) {
			t.Errorf("V2 prompt 缺少 %q，实际：\n%s", want, v2Prompt)
		}
	}

	// 验证 V2 标头不会影响阶段 4 句级引用校验
	if ok, reason := ValidateCitations("新版端口为 8091 [1]。", len(v2Top)); !ok {
		t.Fatalf("句级引用校验应通过，原因：%s", reason)
	}
}

func TestExtractCitedNumbers(t *testing.T) {
	ans := "根据规范，先读取配置 `arr[0]` [3]，再监听端口 [1]。\n```go\nx := items[9]\n```\n详见 [3] 与 [2]。"
	got := ExtractCitedNumbers(ans)
	want := []int{1, 2, 3}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ExtractCitedNumbers got %v, want %v", got, want)
	}
	if gotEmpty := ExtractCitedNumbers("`nums[1]` 无正文引用"); len(gotEmpty) != 0 {
		t.Fatalf("expected empty cited numbers when only code span has brackets, got %v", gotEmpty)
	}
}
