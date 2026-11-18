package retrieve

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"
)

// RerankResult 表示单篇文档的重排索引与相关性得分。
type RerankResult struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
}

// Reranker 抽象 Cross-Encoder 重排能力，便于单测注入替身与线上按环境变量开关接入。
type Reranker interface {
	Rerank(query string, documents []string, topN int) ([]RerankResult, error)
}

// RerankClient 支持标准 /rerank（Jina / Cohere / SiliconFlow / vLLM）与阿里云 DashScope text-rerank 协议。
type RerankClient struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

// NewRerankClient 创建重排客户端；若 baseURL 为空则返回 nil（未配置则跳过重排）。
func NewRerankClient(baseURL, apiKey, model string) *RerankClient {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return nil
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = "gte-rerank"
	}
	return &RerankClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  strings.TrimSpace(apiKey),
		model:   model,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *RerankClient) isDashScope() bool {
	return strings.Contains(strings.ToLower(c.baseURL), "dashscope.aliyuncs.com") ||
		strings.Contains(strings.ToLower(c.baseURL), "text-rerank")
}

func (c *RerankClient) endpoint() string {
	lower := strings.ToLower(c.baseURL)
	if c.isDashScope() {
		if strings.HasSuffix(lower, "/text-rerank") {
			return c.baseURL
		}
		return c.baseURL + "/services/rerank/text-rerank/text-rerank"
	}
	if strings.HasSuffix(lower, "/rerank") {
		return c.baseURL
	}
	return c.baseURL + "/rerank"
}

type standardRerankRequest struct {
	Model           string   `json:"model"`
	Query           string   `json:"query"`
	Documents       []string `json:"documents"`
	TopN            int      `json:"top_n,omitempty"`
	ReturnDocuments bool     `json:"return_documents"`
}

type standardRerankResponse struct {
	Results []RerankResult `json:"results"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type dashScopeRerankRequest struct {
	Model string `json:"model"`
	Input struct {
		Query     string   `json:"query"`
		Documents []string `json:"documents"`
	} `json:"input"`
	Parameters struct {
		TopN            int  `json:"top_n,omitempty"`
		ReturnDocuments bool `json:"return_documents"`
	} `json:"parameters"`
}

type dashScopeRerankResponse struct {
	Output *struct {
		Results []RerankResult `json:"results"`
	} `json:"output"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Rerank 调用 Cross-Encoder 重排 API 返回按相关性降序排列的结果。
func (c *RerankClient) Rerank(query string, documents []string, topN int) ([]RerankResult, error) {
	if c == nil || c.baseURL == "" || len(documents) == 0 {
		return nil, nil
	}
	if topN <= 0 || topN > len(documents) {
		topN = len(documents)
	}

	var payload any
	if c.isDashScope() {
		var dr dashScopeRerankRequest
		dr.Model = c.model
		dr.Input.Query = query
		dr.Input.Documents = documents
		dr.Parameters.TopN = topN
		dr.Parameters.ReturnDocuments = false
		payload = dr
	} else {
		payload = standardRerankRequest{
			Model:           c.model,
			Query:           query,
			Documents:       documents,
			TopN:            topN,
			ReturnDocuments: false,
		}
	}

	rawReq, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, c.endpoint(), bytes.NewReader(rawReq))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Rerank 请求失败：%w", err)
	}
	defer resp.Body.Close()
	rawResp, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Rerank API 返回 %d：%s", resp.StatusCode, string(rawResp))
	}

	var results []RerankResult
	if c.isDashScope() {
		var dr dashScopeRerankResponse
		if err := json.Unmarshal(rawResp, &dr); err != nil {
			return nil, fmt.Errorf("DashScope Rerank 响应解析失败：%w", err)
		}
		if dr.Code != "" {
			return nil, fmt.Errorf("DashScope Rerank 错误（%s）：%s", dr.Code, dr.Message)
		}
		if dr.Output != nil {
			results = dr.Output.Results
		}
	} else {
		var sr standardRerankResponse
		if err := json.Unmarshal(rawResp, &sr); err != nil {
			return nil, fmt.Errorf("Rerank 响应解析失败：%w", err)
		}
		if sr.Error != nil && sr.Error.Message != "" {
			return nil, fmt.Errorf("Rerank API 错误：%s", sr.Error.Message)
		}
		results = sr.Results
	}

	sort.SliceStable(results, func(i, j int) bool {
		return results[i].RelevanceScore > results[j].RelevanceScore
	})
	return results, nil
}

// RerankAndSelect 对候选 Top 15 执行可选 Cross-Encoder 重排（方案 A：保留原 Final 分用于阈值与歧义判定，
// 仅用重排相关性对候选重排并填充 RerankScore），随后执行多标识符覆盖保底并截取 Top k；
// 若 reranker 为 nil 或调用失败则自动回落纯融合顺序 + 多标识符覆盖保底。
func RerankAndSelect(question string, scored []MergedChunk, reranker Reranker, k int, minCoverageScore float64) []MergedChunk {
	if k <= 0 || len(scored) == 0 {
		return nil
	}
	if reranker == nil || len(scored) <= 1 {
		return ApplyMultiEntityCoverage(question, scored, k, minCoverageScore)
	}

	candLimit := MultiEntityCandidateLimit
	if len(scored) < candLimit {
		candLimit = len(scored)
	}
	window := make([]MergedChunk, candLimit)
	copy(window, scored[:candLimit])

	docs := make([]string, candLimit)
	for i, sc := range window {
		if sc.Chunk.HeadingPath != "" {
			docs[i] = sc.Chunk.HeadingPath + "\n" + sc.Chunk.Content
		} else {
			docs[i] = sc.Chunk.Content
		}
	}

	results, err := reranker.Rerank(question, docs, candLimit)
	if err != nil {
		log.Printf("[rerank] 重排调用失败（自动回落纯融合排序）：%v", err)
		return ApplyMultiEntityCoverage(question, scored, k, minCoverageScore)
	}
	if len(results) == 0 {
		return ApplyMultiEntityCoverage(question, scored, k, minCoverageScore)
	}

	reordered := make([]MergedChunk, 0, candLimit)
	seen := make([]bool, candLimit)
	for _, r := range results {
		if r.Index < 0 || r.Index >= candLimit || seen[r.Index] {
			continue
		}
		seen[r.Index] = true
		item := window[r.Index]
		item.RerankScore = r.RelevanceScore
		reordered = append(reordered, item)
	}
	for i, item := range window {
		if !seen[i] {
			reordered = append(reordered, item)
		}
	}
	if len(scored) > candLimit {
		reordered = append(reordered, scored[candLimit:]...)
	}

	log.Printf("[rerank] Top1 重排前后：orig_id=%d(final=%.4f) -> rerank_id=%d(final=%.4f, rerank=%.4f)",
		scored[0].Chunk.ID, scored[0].Final, reordered[0].Chunk.ID, reordered[0].Final, reordered[0].RerankScore)

	return ApplyMultiEntityCoverage(question, reordered, k, minCoverageScore)
}
