// merge.go 把向量检索与关键词检索两路结果融合排序（阶段 3）。
//
// 融合规则：
//   - 向量余弦相似度归一化到 0~1：embedding 余弦本身在 [-1,1]，负值视为完全不相关截断为 0，
//     保留绝对量纲（不按最高分归一），否则拒答阈值会失去意义。
//   - BM25 分数归一化到 0~1：除以本次最高分（同时设软下限 KeywordNormFloor 防止极弱匹配被放大为 1.0），
//     并对"多词问句只孤立命中 1 个常见/噪声词"的情形做抑制，避免中文 2-gram 跨词噪声（如"做法"）抬高无关问句分数。
//   - final = VectorWeight × vector + KeywordWeight × keyword。
//   - 取 Top 5（自阶段 3 起由 3 改为 5，prompt 随之带 5 段资料）。
package retrieve

import (
	"encoding/json"
	"math"
	"sort"
	"strings"

	"knowledge-qa/store"
)

// 可调常量：两路权重与返回条数（阶段 6 迭代 3 将权重由 0.6/0.4 调为 0.75/0.25）。
const (
	VectorWeight  = 0.75 // 向量路权重（语义为主）
	KeywordWeight = 0.25 // 关键词路权重（精确术语补强）
	TopN          = 5    // 融合后取的段数
)

// KeywordNormFloor 是 BM25 归一化的分母软下限（阶段 6 迭代 2 由 10.0 调至 45.0）：
//   - 高匹配度多术语精确查询（maxBM25 通常在 25~55）保持显著关键词贡献；
//   - 低绝对分且仅靠中文通用 2-gram 碰词的语义改写查询（如 maxBM25≈10~16）不再被虚假放大为满分 1.0，
//     避免通用词噪声把纯向量第 1 名的目标切片挤出 Top 5（如 q_id=32）；
//   - 单个稀有精确词（如 fallthrough / WithValue / QueryMap，IDF≈5.19）仍保留非零关键词信号排到首位；
//   - 单纯命中 1 个常见词（如 gin，IDF≈1.3）分母被软下限托住，不会被虚假抬成满分 1.0。
//
// 同时配合多词问句孤词噪声抑制：当查询有 ≥3 个词且全场最高仅命中 1 词、BM25Raw < 8.0 时，
// 多为中文 2-gram 跨词噪声（如"怎么做法式红酒炖牛肉"中的"做法"）或无关句碰词，关键词路记 0。
const (
	KeywordNormFloor     = 45.0
	SingleTermNoiseLimit = 8.0
)

// 文档治理权重倍率常量（阶段 10；便于阶段 11 控制变量调优）：
//   - 权威层级：正式规范 spec(1.00) > 方案评审 rfc(0.96) > 会议纪要 note(0.92)
//   - 生命周期：现行有效 active(1.00) > 已废弃 deprecated(0.78)
//     确保现行规范优先于过期文档，同时当新文档缺失、仅旧文档可答时仍能召回并触发废弃警示。
const (
	DocLevelWeightSpec = 1.00
	DocLevelWeightRFC  = 0.96
	DocLevelWeightNote = 0.92

	DocStatusWeightActive     = 1.00
	DocStatusWeightDeprecated = 0.78
)

// MergedChunk 是一段的融合结果，附带每路明细供 debug 输出。
type MergedChunk struct {
	Chunk        store.Chunk
	VectorScore  float64  // 余弦原始分（截断到 0~1）
	BM25Raw      float64  // BM25 原始分
	KeywordScore float64  // BM25 归一化分
	LevelFactor  float64  // 文档层级倍率（spec/rfc/note）
	StatusFactor float64  // 文档状态倍率（active/deprecated）
	Final        float64  // 融合分（已乘治理倍率，仍保持在 0~1）
	MatchedTerms []string // 命中的关键词
}

// MergeOptions 配置多项目硬隔离与检索行为。
type MergeOptions struct {
	// ProjectSlug 非空且不为 "auto"/"all" 时，仅保留目标项目与 "shared" 公共域切片参与打分。
	ProjectSlug string
}

// DocLevelFactor 返回文档权威层级对应的排序倍率。
func DocLevelFactor(level string) float64 {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "", "spec":
		return DocLevelWeightSpec
	case "rfc":
		return DocLevelWeightRFC
	case "note":
		return DocLevelWeightNote
	default:
		return DocLevelWeightSpec
	}
}

// DocStatusFactor 返回文档生效状态对应的排序倍率。
func DocStatusFactor(status string) float64 {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "active":
		return DocStatusWeightActive
	case "deprecated":
		return DocStatusWeightDeprecated
	default:
		return DocStatusWeightActive
	}
}

// FilterChunksByProject 按项目硬隔离规则过滤切片：
// 当 projectSlug 为空、"auto" 或 "all" 时不过滤；否则只保留目标项目及 "shared" 公共域切片。
func FilterChunksByProject(chunks []store.Chunk, projectSlug string) []store.Chunk {
	slug := strings.TrimSpace(projectSlug)
	if slug == "" || slug == "auto" || slug == "all" {
		return chunks
	}
	filtered := make([]store.Chunk, 0, len(chunks))
	for _, c := range chunks {
		if c.ProjectSlug == slug || c.ProjectSlug == "shared" {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

// Merge 对全部切片同时算向量分与关键词分，融合排序后返回 Top k（k 传 TopN）。
func Merge(question string, questionVec []float64, chunks []store.Chunk, k int) []MergedChunk {
	return MergeWithOptions(question, questionVec, chunks, k, MergeOptions{})
}

// MergeWithOptions 先按项目硬隔离过滤，再做向量+BM25融合与治理权重排序，返回 Top k。
func MergeWithOptions(question string, questionVec []float64, chunks []store.Chunk, k int, opts MergeOptions) []MergedChunk {
	scored := ScoreWithOptions(question, questionVec, chunks, opts)
	if k >= 0 && len(scored) > k {
		scored = scored[:k]
	}
	return scored
}

// ScoreWithOptions 返回过滤并融合排序后的全部候选切片，供需要跨项目歧义检测的调用方查看 Top N 以上候选。
func ScoreWithOptions(question string, questionVec []float64, chunks []store.Chunk, opts MergeOptions) []MergedChunk {
	scoped := FilterChunksByProject(chunks, opts.ProjectSlug)
	if len(scoped) == 0 {
		return nil
	}

	kw := BM25(question, scoped)
	qTermCount := len(uniqueQueryTerms(question))

	merged := make([]MergedChunk, 0, len(scoped))
	maxBM25 := 0.0
	maxMatched := 0
	for i, c := range scoped {
		vec := c.Vec
		if len(vec) == 0 {
			if err := json.Unmarshal([]byte(c.Embedding), &vec); err != nil {
				continue // 向量解析失败的切片跳过
			}
		}
		vs := cosine(questionVec, vec)
		if vs < 0 {
			vs = 0 // 负相关截断为 0，归一到 0~1
		}
		if kw[i].Raw > maxBM25 {
			maxBM25 = kw[i].Raw
		}
		if len(kw[i].Matched) > maxMatched {
			maxMatched = len(kw[i].Matched)
		}
		merged = append(merged, MergedChunk{
			Chunk:        c,
			VectorScore:  vs,
			BM25Raw:      kw[i].Raw,
			LevelFactor:  DocLevelFactor(c.DocLevel),
			StatusFactor: DocStatusFactor(c.DocStatus),
			MatchedTerms: kw[i].Matched,
		})
	}

	// 多词问句（≥3 词）若任意段落最多只命中 1 个词且 BM25 低于噪声阈值，视为偶然碰词噪声
	suppressNoise := qTermCount >= 3 && maxMatched <= 1 && maxBM25 < SingleTermNoiseLimit
	if !suppressNoise && maxBM25 > 0 {
		denom := math.Max(maxBM25, KeywordNormFloor)
		for i := range merged {
			merged[i].KeywordScore = merged[i].BM25Raw / denom
		}
	}
	for i := range merged {
		base := VectorWeight*merged[i].VectorScore + KeywordWeight*merged[i].KeywordScore
		merged[i].Final = base * merged[i].LevelFactor * merged[i].StatusFactor
	}

	sort.SliceStable(merged, func(i, j int) bool { return merged[i].Final > merged[j].Final })
	return merged
}
