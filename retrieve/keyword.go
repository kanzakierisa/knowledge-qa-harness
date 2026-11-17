// keyword.go 实现带饱和与长度归一的 BM25 关键词打分，与向量检索互补（阶段 3）。
//
// 切词规则：
//   - 中文（CJK）：按 2-gram 滑窗切片，如"路由参数" → 路由、由参、参数。
//   - 英文/代码标识符：按非字母数字字符切分，如 "c.Query" → c、query；统一转小写。
//
// 打分公式（在 Stage 3 简化版基础上启用标准 BM25 的 TF 饱和与长度归一化修正）：
//
//	score = Σ(对query中每个词) IDF(词) × TF_bm25(词, 段落)
//	IDF = ln(总段数 / 含该词段数)，一个词在所有段都出现时 IDF=0，不起作用。
//	TF_bm25 = tf × (k1 + 1) / (tf + k1 × (1 - b + b × docLen / avgDocLen))，k1=1.2, b=0.75。
//
// 同时索引 heading_path 与 content（与 embedding 入库口径一致），
// 防止纯短标识符（如 "c"）在长代码块中靠无上限重复刷分挤掉标题高度相关的段落。
// 词频信息在检索时现场计算（段数 ≤500，性能够），不落库。
package retrieve

import (
	"math"
	"strings"
	"unicode"

	"knowledge-qa/store"
)

const (
	bm25K1 = 1.2  // 词频饱和参数：限制单个高频词的边际收益
	bm25B  = 0.75 // 文档长度归一化系数
)

// tokenize 把文本切成检索词：中文走 2-gram，英文/标识符走非字母数字切分。
func tokenize(s string) []string {
	var terms []string
	var ident []rune // 连续字母数字缓冲（英文/代码标识符）
	var cjk []rune   // 连续中文缓冲

	flushIdent := func() {
		if len(ident) > 0 {
			terms = append(terms, strings.ToLower(string(ident)))
			ident = ident[:0]
		}
	}
	flushCJK := func() {
		// 每 2 个字一个滑窗片段；不足 2 字时不产生词
		for i := 0; i+2 <= len(cjk); i++ {
			terms = append(terms, string(cjk[i:i+2]))
		}
		cjk = cjk[:0]
	}

	for _, r := range s {
		switch {
		case unicode.Is(unicode.Han, r):
			flushIdent()
			cjk = append(cjk, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			flushCJK()
			ident = append(ident, r)
		default:
			flushIdent()
			flushCJK()
		}
	}
	flushIdent()
	flushCJK()
	return terms
}

// KeywordResult 是一段的关键词打分结果。
type KeywordResult struct {
	Raw     float64  // BM25 原始分（未归一化）
	Matched []string // query 中实际命中该段的词（去重），供 debug 展示
}

// uniqueQueryTerms 返回去重后的查询词切片，同时供 merge 计算查询覆盖度。
func uniqueQueryTerms(query string) []string {
	qTerms := tokenize(query)
	seen := map[string]bool{}
	uniq := make([]string, 0, len(qTerms))
	for _, t := range qTerms {
		if !seen[t] {
			seen[t] = true
			uniq = append(uniq, t)
		}
	}
	return uniq
}

// BM25 对全部切片计算关键词分数，返回与 chunks 等长的结果切片。
func BM25(query string, chunks []store.Chunk) []KeywordResult {
	out := make([]KeywordResult, len(chunks))
	if len(chunks) == 0 {
		return out
	}

	uniqQuery := uniqueQueryTerms(query)

	// 每段切词（索引标题路径 + 正文）并统计词频与段长
	termFreqs := make([]map[string]int, len(chunks))
	docLens := make([]int, len(chunks))
	df := map[string]int{} // 每个词出现在多少段里（文档频率）
	totalLen := 0
	for i, c := range chunks {
		tf := map[string]int{}
		tokens := tokenize(c.HeadingPath + "\n" + c.Content)
		docLens[i] = len(tokens)
		totalLen += len(tokens)
		for _, t := range tokens {
			tf[t]++
		}
		termFreqs[i] = tf
		for t := range tf {
			df[t]++
		}
	}

	avgDocLen := float64(totalLen) / float64(len(chunks))
	if avgDocLen <= 0 {
		avgDocLen = 1
	}
	n := float64(len(chunks))

	for i := range chunks {
		var score float64
		matchedSet := map[string]bool{}
		lenNorm := 1 - bm25B + bm25B*(float64(docLens[i])/avgDocLen)
		for _, t := range uniqQuery {
			tf := termFreqs[i][t]
			if tf == 0 {
				continue
			}
			idf := math.Log(n / float64(df[t])) // df≤n，故 idf≥0
			tfSat := (float64(tf) * (bm25K1 + 1)) / (float64(tf) + bm25K1*lenNorm)
			score += idf * tfSat
			matchedSet[t] = true
		}
		matched := make([]string, 0, len(matchedSet))
		for t := range matchedSet {
			matched = append(matched, t)
		}
		out[i] = KeywordResult{Raw: score, Matched: matched}
	}
	return out
}
