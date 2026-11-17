package eval

import (
	"testing"

	"knowledge-qa/store"
)

func boolPtr(v bool) *bool { return &v }

func TestSummarizeMetricDefinitions(t *testing.T) {
	runs := []store.EvalRun{
		{Hit: boolPtr(true), CitationOK: boolPtr(true), Refused: false, Usable: boolPtr(true)},
		{Hit: boolPtr(false), Refused: true},
		{Hit: boolPtr(true), CitationOK: boolPtr(false), Refused: false, Usable: boolPtr(false)},
		{ShouldRefuse: true, Refused: true},
		{ShouldRefuse: true, Refused: false, CitationOK: boolPtr(true), Usable: boolPtr(true)},
	}
	r := Summarize(runs)
	if r.HitRate != 2.0/3.0 {
		t.Fatalf("检索命中率应按 3 道非拒答题计算，得到 %v", r.HitRate)
	}
	if r.CitationRate != 2.0/3.0 {
		t.Fatalf("引用准确率应按 3 个实际回答计算，得到 %v", r.CitationRate)
	}
	if r.RefusalRate != 0.5 {
		t.Fatalf("拒答正确率应按 2 道应拒答题计算，得到 %v", r.RefusalRate)
	}
	if r.UsableRate == nil || *r.UsableRate != 2.0/3.0 {
		t.Fatalf("答案可用率应按 3 条已标注回答计算，得到 %v", r.UsableRate)
	}
	if r.Counts.AnswerableCount != 3 || r.Counts.FalseRefusalCount != 1 || r.Counts.AnsweredCount != 3 || r.Counts.MarkedCount != 3 || r.Counts.PendingCount != 0 {
		t.Fatalf("回答与标注计数错误：%+v", r.Counts)
	}
}

func TestSummarizeUsableRateIsNullBeforeMarking(t *testing.T) {
	r := Summarize([]store.EvalRun{{Hit: boolPtr(true), CitationOK: boolPtr(true)}})
	if r.UsableRate != nil {
		t.Fatalf("未人工标注时可用率必须为 null，得到 %v", *r.UsableRate)
	}
	if r.Counts.PendingCount != 1 || r.Counts.MarkedCount != 0 {
		t.Fatalf("待标注计数错误：%+v", r.Counts)
	}
}

func TestSummarizePartialMarksUseMarkedDenominator(t *testing.T) {
	r := Summarize([]store.EvalRun{
		{CitationOK: boolPtr(true), Usable: boolPtr(true)},
		{CitationOK: boolPtr(true), Usable: boolPtr(false)},
		{CitationOK: boolPtr(true), Usable: nil},
	})
	if r.UsableRate == nil || *r.UsableRate != 0.5 {
		t.Fatalf("部分标注应按 2 条已标注回答计算，得到 %v", r.UsableRate)
	}
	if r.Counts.PendingCount != 1 || r.Counts.AnsweredCount != 3 {
		t.Fatalf("部分标注计数错误：%+v", r.Counts)
	}
}

func TestContainsChunk(t *testing.T) {
	gold := int64(42)
	if !containsChunk([]int64{1, 42}, &gold) {
		t.Fatal("Top 5 中包含 gold_chunk_id 时应命中")
	}
	if containsChunk([]int64{1, 2}, &gold) || containsChunk([]int64{42}, nil) {
		t.Fatal("缺少 gold 或 gold 为空时不得命中")
	}
}
