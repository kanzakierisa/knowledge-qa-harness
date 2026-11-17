// Package retrieve 负责向量相似度检索（本阶段只有向量一路，关键词检索是阶段 3 的事）。
package retrieve

import (
	"encoding/json"
	"math"
	"sort"

	"knowledge-qa/store"
)

// ScoredChunk 是带相似度分数的切片。
type ScoredChunk struct {
	Chunk store.Chunk
	Score float64
}

// cosine 计算两个向量的余弦相似度。
func cosine(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// TopK 用问题向量对全部切片算余弦相似度，返回分数最高的 K 个（降序）。
func TopK(questionVec []float64, chunks []store.Chunk, k int) []ScoredChunk {
	scored := make([]ScoredChunk, 0, len(chunks))
	for _, c := range chunks {
		var vec []float64
		if err := json.Unmarshal([]byte(c.Embedding), &vec); err != nil {
			continue // 向量解析失败的切片跳过
		}
		scored = append(scored, ScoredChunk{Chunk: c, Score: cosine(questionVec, vec)})
	}
	sort.Slice(scored, func(i, j int) bool { return scored[i].Score > scored[j].Score })
	if len(scored) > k {
		scored = scored[:k]
	}
	return scored
}
