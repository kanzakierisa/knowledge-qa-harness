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

// MergedChunk 是一段的融合结果，附带每路明细供 debug 输出。
type MergedChunk struct {
	Chunk        store.Chunk
	VectorScore  float64  // 余弦原始分（截断到 0~1）
	BM25Raw      float64  // BM25 原始分
	KeywordScore float64  // BM25 归一化分
	Final        float64  // 融合分
	MatchedTerms []string // 命中的关键词
}

// Merge 对全部切片同时算向量分与关键词分，融合排序后返回 Top k（k 传 TopN）。
func Merge(question string, questionVec []float64, chunks []store.Chunk, k int) []MergedChunk {
	kw := BM25(question, chunks)
	qTermCount := len(uniqueQueryTerms(question))

	merged := make([]MergedChunk, 0, len(chunks))
	maxBM25 := 0.0
	maxMatched := 0
	for i, c := range chunks {
		var vec []float64
		if err := json.Unmarshal([]byte(c.Embedding), &vec); err != nil {
			continue // 向量解析失败的切片跳过
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
		merged[i].Final = VectorWeight*merged[i].VectorScore + KeywordWeight*merged[i].KeywordScore
	}

	sort.Slice(merged, func(i, j int) bool { return merged[i].Final > merged[j].Final })
	if len(merged) > k {
		merged = merged[:k]
	}
	return merged
}
