// Package eval 负责执行金标评测、保存逐题结果并计算四个指标。
package eval

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"knowledge-qa/generate"
	"knowledge-qa/store"
)

// ErrConcurrent 表示已有一批评测正在执行。
var (
	ErrConcurrent   = errors.New("已有一批评测正在执行，请稍后重试")
	ErrEmptyEvalSet = errors.New("目标评测集为空，请先导入对应金标题")
)

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

// AskWithProjectFunc 支持将题目绑定的 project_slug 传入问答链路（阶段 11 多项目盲测）。
type AskWithProjectFunc func(question, projectSlug string) (Answer, error)

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

// CategorySummary 统计单个对抗评测分类的细粒度指标（如 near_miss_refusal、cross_para_compare）。
type CategorySummary struct {
	QuestionCount       int      `json:"question_count"`
	AnswerableCount     int      `json:"answerable_count"`
	ShouldRefuseCount   int      `json:"should_refuse_count"`
	AnsweredCount       int      `json:"answered_count"`
	FalseRefusalCount   int      `json:"false_refusal_count"`
	HitCount            int      `json:"hit_count"`
	HitRate             *float64 `json:"hit_rate"`
	CitationCount       int      `json:"citation_count"`
	CitationRate        *float64 `json:"citation_rate"`
	RefusalCorrectCount int      `json:"refusal_correct_count"`
	RefusalRate         *float64 `json:"refusal_rate"`
	MarkedCount         int      `json:"marked_count"`
	UsableCount         int      `json:"usable_count"`
	UsableRate          *float64 `json:"usable_rate"`
}

// Report 是评测响应。按确认的口径，无人工标注时 UsableRate 为 null。
type Report struct {
	QuestionSet  string                     `json:"question_set,omitempty"`
	HitRate      float64                    `json:"hit_rate"`
	CitationRate float64                    `json:"citation_rate"`
	UsableRate   *float64                   `json:"usable_rate"`
	RefusalRate  float64                    `json:"refusal_rate"`
	Details      []store.EvalRun            `json:"detail"`
	Counts       Counts                     `json:"counts"`
	Categories   map[string]CategorySummary `json:"categories,omitempty"`
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
	store          *store.Store
	askWithProject AskWithProjectFunc
	mu             sync.Mutex
}

// New 创建评测器（保持 V1 签名兼容）。
func New(st *store.Store, ask AskFunc) *Evaluator {
	return &Evaluator{
		store: st,
		askWithProject: func(question, _ string) (Answer, error) {
			return ask(question)
		},
	}
}

// NewWithProject 创建支持 project_slug 隔离的 V2 评测器。
func NewWithProject(st *store.Store, ask AskWithProjectFunc) *Evaluator {
	return &Evaluator{store: st, askWithProject: ask}
}

// Run 默认执行 base52 评测集，保持 V1 向后兼容。
func (e *Evaluator) Run() (Report, error) {
	return e.RunSet("base52")
}

// RunSet 按指定分组（base52 / holdout / feedback / all）逐题走完整问答链路并事务落库。
func (e *Evaluator) RunSet(set string) (Report, error) {
	if !e.mu.TryLock() {
		return Report{}, ErrConcurrent
	}
	defer e.mu.Unlock()

	normSet := strings.ToLower(strings.TrimSpace(set))
	if normSet == "" {
		normSet = "base52"
	}

	questions, err := e.store.EvalQuestionsBySet(normSet)
	if err != nil {
		return Report{}, err
	}
	if len(questions) == 0 {
		return Report{}, fmt.Errorf("%w（set=%s）", ErrEmptyEvalSet, normSet)
	}

	batchID := fmt.Sprintf("%s-%d", normSet, time.Now().UnixNano())
	runs := make([]store.EvalRun, 0, len(questions))
	for i, q := range questions {
		log.Printf("[eval:%s] 开始第 %d/%d 题（question_id=%d, project=%q, cat=%q）：%s",
			normSet, i+1, len(questions), q.ID, q.ProjectSlug, q.Category, q.Question)
		answer, err := e.askWithProject(q.Question, q.ProjectSlug)
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
			ProjectSlug:       q.ProjectSlug,
			QuestionSet:       q.QuestionSet,
			Category:          q.Category,
			BatchID:           batchID,
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
		log.Printf("[eval:%s] 第 %d 题问答完成：refused=%v hit=%v citation_ok=%v",
			normSet, i+1, answer.Refused, boolText(run.Hit), boolText(run.CitationOK))
	}

	saved, err := e.store.SaveEvalRuns(runs)
	if err != nil {
		return Report{}, fmt.Errorf("保存本批评测结果失败：%w", err)
	}
	report := SummarizeWithSet(saved, normSet)
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
	return SummarizeWithSet(runs, "")
}

// SummarizeWithSet 按同一批 run 记录计算总指标及按 category 分组的小计。
func SummarizeWithSet(runs []store.EvalRun, set string) Report {
	var c Counts
	hit, hitTotal, citation, citationTotal, refusal, refusalTotal := 0, 0, 0, 0, 0, 0
	type catAcc struct {
		summary       CategorySummary
		citationTotal int
	}
	byCat := make(map[string]*catAcc)

	for _, run := range runs {
		c.QuestionCount++
		cat := strings.TrimSpace(run.Category)
		var acc *catAcc
		if cat != "" {
			acc = byCat[cat]
			if acc == nil {
				acc = &catAcc{}
				byCat[cat] = acc
			}
			acc.summary.QuestionCount++
		}

		if !run.ShouldRefuse {
			c.AnswerableCount++
			if run.Refused {
				c.FalseRefusalCount++
			}
			hitTotal++
			if run.Hit != nil && *run.Hit {
				hit++
			}
			if acc != nil {
				acc.summary.AnswerableCount++
				if run.Refused {
					acc.summary.FalseRefusalCount++
				}
				if run.Hit != nil && *run.Hit {
					acc.summary.HitCount++
				}
			}
		}
		if run.CitationOK != nil {
			citationTotal++
			if *run.CitationOK {
				citation++
			}
			if acc != nil {
				acc.citationTotal++
				if *run.CitationOK {
					acc.summary.CitationCount++
				}
			}
		}
		if run.ShouldRefuse {
			refusalTotal++
			if run.Refused {
				refusal++
			}
			if acc != nil {
				acc.summary.ShouldRefuseCount++
				if run.Refused {
					acc.summary.RefusalCorrectCount++
				}
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
			if acc != nil {
				acc.summary.AnsweredCount++
				if run.Usable != nil {
					acc.summary.MarkedCount++
					if *run.Usable {
						acc.summary.UsableCount++
					}
				}
			}
		}
	}
	c.PendingCount = c.AnsweredCount - c.MarkedCount

	var categories map[string]CategorySummary
	if len(byCat) > 0 {
		categories = make(map[string]CategorySummary, len(byCat))
		for k, acc := range byCat {
			s := acc.summary
			s.HitRate = optionalRatio(s.HitCount, s.AnswerableCount)
			s.CitationRate = optionalRatio(s.CitationCount, acc.citationTotal)
			s.RefusalRate = optionalRatio(s.RefusalCorrectCount, s.ShouldRefuseCount)
			s.UsableRate = optionalRatio(s.UsableCount, s.MarkedCount)
			categories[k] = s
		}
	}

	return Report{
		QuestionSet:  set,
		HitRate:      ratio(hit, hitTotal),
		CitationRate: ratio(citation, citationTotal),
		UsableRate:   optionalRatio(c.UsableCount, c.MarkedCount),
		RefusalRate:  ratio(refusal, refusalTotal),
		Details:      runs,
		Counts:       c,
		Categories:   categories,
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

	if len(r.Categories) > 0 {
		keys := make([]string, 0, len(r.Categories))
		for k := range r.Categories {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var catLines strings.Builder
		catLines.WriteString("\n-------------------- 分类小计 --------------------\n")
		for _, k := range keys {
			cs := r.Categories[k]
			fmt.Fprintf(&catLines, "%-22s 总计=%2d | 命中=%s (%d/%d) | 拒答=%s (%d/%d) | 误拒=%d | 可用=%s (%d/%d)\n",
				k, cs.QuestionCount,
				optPct(cs.HitRate), cs.HitCount, cs.AnswerableCount,
				optPct(cs.RefusalRate), cs.RefusalCorrectCount, cs.ShouldRefuseCount,
				cs.FalseRefusalCount,
				optPct(cs.UsableRate), cs.UsableCount, cs.MarkedCount)
		}
		catLines.WriteString("--------------------------------------------------")
		log.Print(catLines.String())
	}
}

func optPct(rate *float64) string {
	if rate == nil {
		return "N/A"
	}
	return fmt.Sprintf("%6.2f%%", *rate*100)
}
