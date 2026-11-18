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
