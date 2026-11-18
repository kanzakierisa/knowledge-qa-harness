package retrieve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"knowledge-qa/store"
)

func TestRerankClientStandardAndDashScopeProtocols(t *testing.T) {
	t.Run("standard /rerank protocol", func(t *testing.T) {
		var seenAuth, seenPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seenAuth = r.Header.Get("Authorization")
			seenPath = r.URL.Path
			var req standardRerankRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode req: %v", err)
			}
			if req.Model != "bge-reranker-v2-m3" || len(req.Documents) != 3 {
				t.Fatalf("unexpected req: %+v", req)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(standardRerankResponse{
				Results: []RerankResult{
					{Index: 2, RelevanceScore: 0.91},
					{Index: 0, RelevanceScore: 0.42},
					{Index: 1, RelevanceScore: 0.15},
				},
			})
		}))
		defer srv.Close()

		client := NewRerankClient(srv.URL, "test-key", "bge-reranker-v2-m3")
		got, err := client.Rerank("query", []string{"d0", "d1", "d2"}, 3)
		if err != nil {
			t.Fatalf("Rerank error: %v", err)
		}
		if seenAuth != "Bearer test-key" || seenPath != "/rerank" {
			t.Fatalf("auth=%q path=%q", seenAuth, seenPath)
		}
		if len(got) != 3 || got[0].Index != 2 || got[0].RelevanceScore != 0.91 {
			t.Fatalf("unexpected results: %+v", got)
		}
	})

	t.Run("dashscope text-rerank protocol", func(t *testing.T) {
		var seenPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seenPath = r.URL.Path
			var req dashScopeRerankRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode req: %v", err)
			}
			if req.Model != "gte-rerank" || req.Input.Query != "q" || len(req.Input.Documents) != 2 || req.Parameters.TopN != 2 {
				t.Fatalf("unexpected dashscope req: %+v", req)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"output":{"results":[{"index":1,"relevance_score":0.88},{"index":0,"relevance_score":0.31}]}}`))
		}))
		defer srv.Close()

		client := NewRerankClient(srv.URL+"/services/rerank/text-rerank/text-rerank", "dash-key", "gte-rerank")
		got, err := client.Rerank("q", []string{"a", "b"}, 2)
		if err != nil {
			t.Fatalf("DashScope Rerank error: %v", err)
		}
		if !strings.HasSuffix(seenPath, "/text-rerank") || len(got) != 2 || got[0].Index != 1 {
			t.Fatalf("path=%q got=%+v", seenPath, got)
		}
	})
}

func TestRerankAndSelectPreservesFinalAndFallsBack(t *testing.T) {
	scored := []MergedChunk{
		{Chunk: store.Chunk{ID: 1, HeadingPath: "H1", Content: "Doc 1"}, Final: 0.72},
		{Chunk: store.Chunk{ID: 2, HeadingPath: "H2", Content: "Doc 2"}, Final: 0.61},
		{Chunk: store.Chunk{ID: 3, HeadingPath: "H3", Content: "Doc 3"}, Final: 0.55},
	}

	// nil reranker 自动回落纯融合顺序
	fallbackNil := RerankAndSelect("query", scored, nil, TopN, DefaultCoverageMinScore)
	if len(fallbackNil) != 3 || fallbackNil[0].Chunk.ID != 1 {
		t.Fatalf("nil reranker fallback mismatch: %+v", fallbackNil)
	}

	// 服务端报错时自动回落纯融合顺序
	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer errSrv.Close()
	errClient := NewRerankClient(errSrv.URL, "bad", "bge-reranker-v2-m3")
	fallbackErr := RerankAndSelect("query", scored, errClient, TopN, DefaultCoverageMinScore)
	if len(fallbackErr) != 3 || fallbackErr[0].Chunk.ID != 1 {
		t.Fatalf("error reranker fallback mismatch: %+v", fallbackErr)
	}

	// 成功重排时采用方案 A：按重排得分重排，同时保留原始 Final 分数
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(standardRerankResponse{
			Results: []RerankResult{
				{Index: 2, RelevanceScore: 0.94},
				{Index: 0, RelevanceScore: 0.60},
				{Index: 1, RelevanceScore: 0.20},
			},
		})
	}))
	defer okSrv.Close()
	okClient := NewRerankClient(okSrv.URL, "k", "bge-reranker-v2-m3")
	reranked := RerankAndSelect("query", scored, okClient, TopN, DefaultCoverageMinScore)
	if len(reranked) != 3 || reranked[0].Chunk.ID != 3 || reranked[0].Final != 0.55 || reranked[0].RerankScore != 0.94 {
		t.Fatalf("plan A rerank mismatch: %+v", reranked)
	}
}

type stubReranker struct {
	fn func(query string, docs []string, topN int) ([]RerankResult, error)
}

func (s stubReranker) Rerank(query string, documents []string, topN int) ([]RerankResult, error) {
	return s.fn(query, documents, topN)
}

func TestRerankAndSelectConvexBlendAndNarrowSpan(t *testing.T) {
	// 1. 凸组合保护高融合分语义命中不被微弱字面 Rerank 逆转
	// chunk 10: Final=0.82, rerank=0.68 -> normR=(0.68-0.10)/(0.74-0.10)=0.90625 -> blend=0.8631
	// chunk 11: Final=0.42, rerank=0.74 -> normR=1.0 -> blend=0.7100
	// chunk 12: Final=0.35, rerank=0.10 -> normR=0.0 -> blend=0.1750
	scored := []MergedChunk{
		{Chunk: store.Chunk{ID: 10, Content: "A"}, Final: 0.82},
		{Chunk: store.Chunk{ID: 11, Content: "B"}, Final: 0.42},
		{Chunk: store.Chunk{ID: 12, Content: "C"}, Final: 0.35},
	}
	blended := RerankAndSelect("query", scored, stubReranker{
		fn: func(_ string, _ []string, _ int) ([]RerankResult, error) {
			return []RerankResult{
				{Index: 1, RelevanceScore: 0.74},
				{Index: 0, RelevanceScore: 0.68},
				{Index: 2, RelevanceScore: 0.10},
			}, nil
		},
	}, TopN, DefaultCoverageMinScore)
	if len(blended) != 3 || blended[0].Chunk.ID != 10 || blended[0].Final != 0.82 {
		t.Fatalf("convex blend should preserve strong Final semantic match at #1: %+v", blended)
	}

	// 2. maxR == minR 与极窄分差保护：不得除零产生 NaN，并按原 Final 顺序稳定排序
	flat := RerankAndSelect("query", scored, stubReranker{
		fn: func(_ string, _ []string, _ int) ([]RerankResult, error) {
			return []RerankResult{
				{Index: 2, RelevanceScore: 0.5000000},
				{Index: 1, RelevanceScore: 0.5000002},
				{Index: 0, RelevanceScore: 0.5000000},
			}, nil
		},
	}, TopN, DefaultCoverageMinScore)
	if len(flat) != 3 || flat[0].Chunk.ID != 10 || flat[1].Chunk.ID != 11 || flat[2].Chunk.ID != 12 {
		t.Fatalf("narrow-span protection should fall back to Final order: %+v", flat)
	}

	// 3. 候选窗口扩大到 Top 25：第 22 名高相关切片可进入 Top 5，且第 26 名不会被送进 Reranker
	wide := make([]MergedChunk, 26)
	for i := range wide {
		wide[i] = MergedChunk{Chunk: store.Chunk{ID: int64(i + 1), Content: "doc"}, Final: 0.70 - float64(i)*0.01}
	}
	var seenDocs int
	wideOut := RerankAndSelect("query", wide, stubReranker{
		fn: func(_ string, docs []string, topN int) ([]RerankResult, error) {
			seenDocs = len(docs)
			res := make([]RerankResult, len(docs))
			for i := range docs {
				score := 0.2
				if i == 21 { // 原始第 22 名（ID=22, Final=0.49）重排分最高
					score = 0.99
				}
				res[i] = RerankResult{Index: i, RelevanceScore: score}
			}
			return res, nil
		},
	}, TopN, DefaultCoverageMinScore)
	if seenDocs != RerankCandidateLimit {
		t.Fatalf("expected %d candidates passed to reranker, got %d", RerankCandidateLimit, seenDocs)
	}
	// ID=22: 0.5*0.49 + 0.5*1.0 = 0.745 > ID=1: 0.5*0.70 + 0.5*0.0 = 0.35
	if len(wideOut) != TopN || wideOut[0].Chunk.ID != 22 || wideOut[0].Final != 0.49 {
		t.Fatalf("expected rank-22 item promoted to #1 with original Final=0.49 preserved, got %+v", wideOut)
	}
}
