// Package eval 负责执行金标评测、保存逐题结果并计算四个指标。
package eval

import (
	"errors"
	"fmt"
	"log"
	"sync"

	"knowledge-qa/generate"
	"knowledge-qa/store"
)

// ErrConcurrent 表示已有一批评测正在执行。
var ErrConcurrent = errors.New("已有一批评测正在执行，请稍后重试")

// Answer 是内部问答链路提供给评测器的结果；RetrievedChunkIDs 即使拒答也保留 Top 5。
type Answer struct {
	Text              string
	Refused           bool
	Retried           bool
	RetrievedChunkIDs []int64
	CitationLimit     int
}

// AskFunc 由 HTTP 层注入，直接复用 /api/ask 的内部函数，不走 HTTP 自调用。
type AskFunc func(question string) (Answer, error)

// Counts 给出本批题目、回答与人工标注数量，避免只看比率产生误解。
type Counts struct {
	QuestionCount     int `json:"question_count"`
	AnswerableCount   int `json:"answerable_count"`
	FalseRefusalCount int `json:"false_refusal_count"`
	AnsweredCount     int `json:"answered_count"`
	MarkedCount       int `json:"marked_count"`
	PendingCount      int `json:"pending_count"`
	UsableCount       int `json:"usable_count"`
}

// Report 是评测响应。按确认的口径，无人工标注时 UsableRate 为 null。
type Report struct {
	HitRate      float64         `json:"hit_rate"`
	CitationRate float64         `json:"citation_rate"`
	UsableRate   *float64        `json:"usable_rate"`
	RefusalRate  float64         `json:"refusal_rate"`
	Details      []store.EvalRun `json:"detail"`
	Counts       Counts          `json:"counts"`
}

// MarkResult 在标注成功后返回当前所有已标注回答的可用率。
type MarkResult struct {
	OK          bool     `json:"ok"`
	UsableRate  *float64 `json:"usable_rate"`
	UsableCount int      `json:"usable_count"`
	MarkedCount int      `json:"marked_count"`
}

// Evaluator 执行评测；HTTP 层还会把并发冲突映射为 409。
type Evaluator struct {
	store *store.Store
	ask   AskFunc
	mu    sync.Mutex
}

// New 创建评测器。
func New(st *store.Store, ask AskFunc) *Evaluator {
	return &Evaluator{store: st, ask: ask}
}

// Run 逐题走完整问答链路，整批结果一次事务落库。usable 始终为 NULL。
func (e *Evaluator) Run() (Report, error) {
	if !e.mu.TryLock() {
		return Report{}, ErrConcurrent
	}
	defer e.mu.Unlock()

	questions, err := e.store.AllEvalQuestions()
	if err != nil {
		return Report{}, fmt.Errorf("读取金标题失败：%w", err)
	}
	if len(questions) == 0 {
		return Report{}, errors.New("eval_questions 为空，请先运行 scripts/seed_eval.go")
	}

	runs := make([]store.EvalRun, 0, len(questions))
	for i, q := range questions {
		log.Printf("[eval] 开始第 %d/%d 题（question_id=%d）：%s", i+1, len(questions), q.ID, q.Question)
		answer, err := e.ask(q.Question)
		if err != nil {
			return Report{}, fmt.Errorf("第 %d 题（question_id=%d）问答失败：%w", i+1, q.ID, err)
		}
		if answer.RetrievedChunkIDs == nil {
			answer.RetrievedChunkIDs = []int64{}
		}
		run := store.EvalRun{
			Question:          q.Question,
			QuestionID:        q.ID,
			GoldAnswer:        q.GoldAnswer,
			ShouldRefuse:      q.ShouldRefuse,
			GoldChunkID:       q.GoldChunkID,
			RetrievedChunkIDs: answer.RetrievedChunkIDs,
			Answer:            answer.Text,
			Refused:           answer.Refused,
			Usable:            nil,
		}
		if !q.ShouldRefuse {
			hit := containsChunk(answer.RetrievedChunkIDs, q.GoldChunkID)
			run.Hit = &hit
		}
		// 只有实际非拒答回答才计算引用；应拒答却作答的题也纳入分母。
		if !answer.Refused {
			ok, _ := generate.ValidateCitations(answer.Text, answer.CitationLimit)
			run.CitationOK = &ok
		}
		runs = append(runs, run)
		log.Printf("[eval] 第 %d 题问答完成：refused=%v hit=%v citation_ok=%v", i+1, answer.Refused, boolText(run.Hit), boolText(run.CitationOK))
	}

	saved, err := e.store.SaveEvalRuns(runs)
	if err != nil {
		return Report{}, fmt.Errorf("保存本批评测结果失败：%w", err)
	}
	report := Summarize(saved)
	LogReport(report)
	return report, nil
}

// Mark 只保存人工判断，并仅统计该 runID 所属批次的可用率。
func (e *Evaluator) Mark(runID int64, usable bool) (MarkResult, error) {
	if err := e.store.MarkEvalRun(runID, usable); err != nil {
		return MarkResult{}, err
	}
	marked, useful, err := e.store.EvalUsableSummaryForRun(runID)
	if err != nil {
		return MarkResult{}, err
	}
	result := MarkResult{OK: true, UsableCount: useful, MarkedCount: marked}
	if marked > 0 {
		rate := float64(useful) / float64(marked)
		result.UsableRate = &rate
	}
	return result, nil
}

func containsChunk(ids []int64, gold *int64) bool {
	if gold == nil {
		return false
	}
	for _, id := range ids {
		if id == *gold {
			return true
		}
	}
	return false
}

// Summarize 按同一批 run 记录计算四个指标，可在标注后离线重算，无需再调模型。
func Summarize(runs []store.EvalRun) Report {
	var c Counts
	hit, hitTotal, citation, citationTotal, refusal, refusalTotal := 0, 0, 0, 0, 0, 0
	for _, run := range runs {
		c.QuestionCount++
		if !run.ShouldRefuse {
			c.AnswerableCount++
			if run.Refused {
				c.FalseRefusalCount++
			}
			hitTotal++
			if run.Hit != nil && *run.Hit {
				hit++
			}
		}
		if run.CitationOK != nil {
			citationTotal++
			if *run.CitationOK {
				citation++
			}
		}
		if run.ShouldRefuse {
			refusalTotal++
			if run.Refused {
				refusal++
			}
		}
		if !run.Refused {
			c.AnsweredCount++
			if run.Usable != nil {
				c.MarkedCount++
				if *run.Usable {
					c.UsableCount++
				}
			}
		}
	}
	c.PendingCount = c.AnsweredCount - c.MarkedCount
	return Report{
		HitRate:      ratio(hit, hitTotal),
		CitationRate: ratio(citation, citationTotal),
		UsableRate:   optionalRatio(c.UsableCount, c.MarkedCount),
		RefusalRate:  ratio(refusal, refusalTotal),
		Details:      runs,
		Counts:       c,
	}
}

func ratio(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func optionalRatio(numerator, denominator int) *float64 {
	if denominator == 0 {
		return nil
	}
	rate := ratio(numerator, denominator)
	return &rate
}

func boolText(v *bool) string {
	if v == nil {
		return "NULL"
	}
	return fmt.Sprint(*v)
}

// LogReport 打印对齐的文本表格，供服务日志和离线报告工具共用。
func LogReport(r Report) {
	hit, citation, refusal := 0, 0, 0
	for _, run := range r.Details {
		if run.Hit != nil && *run.Hit {
			hit++
		}
		if run.CitationOK != nil && *run.CitationOK {
			citation++
		}
		if run.ShouldRefuse && run.Refused {
			refusal++
		}
	}
	hitTotal, refusalTotal := 0, 0
	for _, run := range r.Details {
		if !run.ShouldRefuse {
			hitTotal++
		}
		if run.ShouldRefuse {
			refusalTotal++
		}
	}
	usable := "NULL"
	if r.UsableRate != nil {
		usable = fmt.Sprintf("%.2f%%", *r.UsableRate*100)
	}
	log.Printf("\n==================== 本次评测指标 ====================\n"+
		"指标          比率          计数\n"+
		"检索命中率    %6.2f%%      %d/%d\n"+
		"引用准确率    %6.2f%%      %d/%d\n"+
		"答案可用率    %10s      %d/%d（已标注）\n"+
		"拒答正确率    %6.2f%%      %d/%d\n"+
		"补充计数：可作答题 %d 条，其中误拒 %d 条；实际非拒答 %d 条（待人工标注 %d 条）\n"+
		"======================================================",
		r.HitRate*100, hit, hitTotal,
		r.CitationRate*100, citation, r.Counts.AnsweredCount,
		usable, r.Counts.UsableCount, r.Counts.MarkedCount,
		r.RefusalRate*100, refusal, refusalTotal,
		r.Counts.AnswerableCount, r.Counts.FalseRefusalCount, r.Counts.AnsweredCount, r.Counts.PendingCount)
}
