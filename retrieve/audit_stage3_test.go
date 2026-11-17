package retrieve

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"knowledge-qa/ingest"
	"knowledge-qa/store"
)

func loadAuditCorpusChunks(t *testing.T) []store.Chunk {
	t.Helper()
	files, err := filepath.Glob("../corpus/*.md")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	var chunks []store.Chunk
	for _, file := range files {
		if filepath.Base(file) == "STATS.md" {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, piece := range ingest.SplitMarkdownV2(string(data)) {
			chunks = append(chunks, store.Chunk{
				ID:          int64(len(chunks) + 1),
				HeadingPath: piece.HeadingPath,
				Content:     piece.Content,
				Embedding:   "[0,1]",
			})
		}
	}
	return chunks
}

func TestAuditStage3RealCorpusKeywordGate(t *testing.T) {
	chunks := loadAuditCorpusChunks(t)
	t.Logf("real corpus chunks=%d; isolate keyword path with orthogonal vectors", len(chunks))

	// 1. 无关/噪声词提问：仅靠孤词命中（如 cookie / gin / 做法）不应在零向量分下直接过 0.35 阈值
	for _, query := range []string{
		"巧克力 cookie 饼干怎么烤",
		"如何调制 gin 琴酒鸡尾酒",
		"怎么做法式红酒炖牛肉？",
		"今天天气怎么样？",
		"Gin 作者最喜欢的颜色是什么？",
		"Go 语言作者早餐爱吃什么？",
	} {
		top := Merge(query, []float64{1, 0}, chunks, TopN)
		if len(top) == 0 {
			t.Fatal("no retrieved chunks")
		}
		r := top[0]
		t.Logf("noise q=%q heading=%q bm25=%.4f kw=%.4f final=%.4f terms=%v gate_pass=%v",
			query, r.Chunk.HeadingPath, r.BM25Raw, r.KeywordScore, r.Final, r.MatchedTerms, r.Final >= 0.35)
		if r.Final >= 0.35 {
			t.Errorf("noise query %q passed threshold purely via keyword: final=%.4f terms=%v", query, r.Final, r.MatchedTerms)
		}
	}

	// 2. 真实带多个术语的提问：Top1 必须准确命中目标段落
	exactCases := []struct {
		query       string
		wantHeading string
	}{
		{"c.Query 和 c.Param 的区别是什么", "Gin 路由参数 > 与查询参数的区别"},
		{"sync.WaitGroup 的 Done 什么时候调用", "Go 并发：goroutine > 等待 goroutine 结束：sync.WaitGroup"},
		{"Gin 里怎么获取 URL 查询参数？", "Gin 查询参数 > 读取查询参数"},
	}
	for _, tc := range exactCases {
		top := Merge(tc.query, []float64{1, 0}, chunks, TopN)
		for i, r := range top {
			t.Logf("exact q=%q [%d] heading=%q bm25=%.4f kw=%.4f final=%.4f terms=%v",
				tc.query, i+1, r.Chunk.HeadingPath, r.BM25Raw, r.KeywordScore, r.Final, r.MatchedTerms)
		}
	}

	// 3. 稀有单个精确术语：不能被死板下限清零，keyword_score 与排序必须保留有效信号
	for _, tc := range []struct {
		query       string
		wantHeading string
	}{
		{"fallthrough", "Go 控制流：for、if、switch > switch 语句"},
		{"WithValue", "Go context 包 > 传值"},
		{"QueryMap", "Gin 查询参数 > 读取多个值"},
	} {
		top := Merge(tc.query, []float64{1, 0}, chunks, TopN)
		r := top[0]
		t.Logf("sparse q=%q top1=%q bm25=%.4f kw=%.4f final=%.4f terms=%v",
			tc.query, r.Chunk.HeadingPath, r.BM25Raw, r.KeywordScore, r.Final, r.MatchedTerms)
		if r.Chunk.HeadingPath != tc.wantHeading {
			t.Errorf("sparse query %q top1=%q, want %q", tc.query, r.Chunk.HeadingPath, tc.wantHeading)
		}
		if r.KeywordScore <= 0 {
			t.Errorf("sparse query %q keyword_score=%.4f, must stay > 0", tc.query, r.KeywordScore)
		}
	}
}
