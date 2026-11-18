package eval

import (
	"errors"
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

func TestSummarizeWithSetCategories(t *testing.T) {
	runs := []store.EvalRun{
		// near_miss_refusal: 全为应拒答且拒答正确 -> HitRate/CitationRate/UsableRate 为 nil，RefusalRate 为 1
		{Category: "near_miss_refusal", ShouldRefuse: true, Refused: true},
		{Category: "near_miss_refusal", ShouldRefuse: true, Refused: true},
		// cross_para_compare: 1 题命中且可用，1 题未命中且误拒
		{Category: "cross_para_compare", Hit: boolPtr(true), CitationOK: boolPtr(true), Refused: false, Usable: boolPtr(true)},
		{Category: "cross_para_compare", Hit: boolPtr(false), Refused: true},
		// 无 category 不进入 Categories
		{Category: "", ShouldRefuse: true, Refused: true},
	}

	report := SummarizeWithSet(runs, "holdout")
	if report.QuestionSet != "holdout" {
		t.Fatalf("expected question_set holdout, got %q", report.QuestionSet)
	}
	if len(report.Categories) != 2 {
		t.Fatalf("expected 2 non-empty categories, got %+v", report.Categories)
	}

	nm := report.Categories["near_miss_refusal"]
	if nm.QuestionCount != 2 || nm.ShouldRefuseCount != 2 || nm.RefusalCorrectCount != 2 {
		t.Fatalf("unexpected near_miss_refusal counts: %+v", nm)
	}
	if nm.HitRate != nil || nm.CitationRate != nil || nm.UsableRate != nil {
		t.Fatalf("expected nil HitRate/CitationRate/UsableRate for pure refusal category, got %+v", nm)
	}
	if nm.RefusalRate == nil || *nm.RefusalRate != 1.0 {
		t.Fatalf("expected refusal_rate=1.0, got %+v", nm.RefusalRate)
	}

	cp := report.Categories["cross_para_compare"]
	if cp.QuestionCount != 2 || cp.AnswerableCount != 2 || cp.FalseRefusalCount != 1 || cp.AnsweredCount != 1 {
		t.Fatalf("unexpected cross_para_compare counts: %+v", cp)
	}
	if cp.HitRate == nil || *cp.HitRate != 0.5 || cp.CitationRate == nil || *cp.CitationRate != 1.0 || cp.UsableRate == nil || *cp.UsableRate != 1.0 || cp.RefusalRate != nil {
		t.Fatalf("unexpected cross_para_compare rates: %+v", cp)
	}
}

func TestRunSetRejectsInvalidSet(t *testing.T) {
	ev := NewWithProject(nil, func(_, _ string) (Answer, error) {
		t.Fatal("ask function should not be called on invalid set")
		return Answer{}, nil
	})
	if _, err := ev.RunSet("invalid-set"); !errors.Is(err, store.ErrInvalidEvalSet) {
		t.Fatalf("expected ErrInvalidEvalSet, got %v", err)
	}
}
