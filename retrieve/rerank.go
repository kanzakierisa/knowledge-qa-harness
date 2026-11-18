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

const (
	// RerankCandidateLimit 开启可选重排时传入 Cross-Encoder 的候选窗口大小（阶段 11 R1 扩窗至 Top 25）。
	RerankCandidateLimit = 25
	// RerankBlendWeight 凸组合中归一化重排分的权重，与 (1 - RerankBlendWeight) 的原融合分 Final 线性加权。
	RerankBlendWeight = 0.5
	rerankScoreEps    = 1e-6
)

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

// RerankAndSelect 对候选 Top 25 执行可选 Cross-Encoder 重排（R1 方案）：
// 严格保留原始 Final 分用于 0.35 拒答阈值与 0.08 跨项目歧义判定，仅使用本地凸组合分数
// blend = 0.5*Final + 0.5*normRerankScore 重排候选窗口，随后经 ApplyMultiEntityCoverage 保底输出 Top k；
// 若 reranker 为 nil 或调用失败则自动回落纯融合顺序 + 多标识符覆盖保底。
func RerankAndSelect(question string, scored []MergedChunk, reranker Reranker, k int, minCoverageScore float64) []MergedChunk {
	if k <= 0 || len(scored) == 0 {
		return nil
	}
	if reranker == nil || len(scored) <= 1 {
		return ApplyMultiEntityCoverage(question, scored, k, minCoverageScore)
	}

	candLimit := RerankCandidateLimit
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

	rawScores := make([]float64, candLimit)
	hasScore := make([]bool, candLimit)
	minR, maxR := 0.0, 0.0
	anyScored := false
	for _, r := range results {
		if r.Index < 0 || r.Index >= candLimit || hasScore[r.Index] {
			continue
		}
		hasScore[r.Index] = true
		rawScores[r.Index] = r.RelevanceScore
		window[r.Index].RerankScore = r.RelevanceScore
		if !anyScored {
			minR, maxR = r.RelevanceScore, r.RelevanceScore
			anyScored = true
		} else {
			if r.RelevanceScore < minR {
				minR = r.RelevanceScore
			}
			if r.RelevanceScore > maxR {
				maxR = r.RelevanceScore
			}
		}
	}
	if !anyScored {
		return ApplyMultiEntityCoverage(question, scored, k, minCoverageScore)
	}

	type blendedEntry struct {
		chunk MergedChunk
		blend float64
	}
	entries := make([]blendedEntry, candLimit)
	span := maxR - minR
	for i := 0; i < candLimit; i++ {
		raw := rawScores[i]
		if !hasScore[i] {
			raw = minR
		}
		normR := 0.5
		if span > rerankScoreEps {
			normR = (raw - minR) / span
			if normR < 0 {
				normR = 0
			} else if normR > 1 {
				normR = 1
			}
		}
		entries[i] = blendedEntry{
			chunk: window[i],
			blend: (1.0-RerankBlendWeight)*window[i].Final + RerankBlendWeight*normR,
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].blend > entries[j].blend
	})

	reordered := make([]MergedChunk, 0, len(scored))
	for _, e := range entries {
		reordered = append(reordered, e.chunk)
	}
	if len(scored) > candLimit {
		reordered = append(reordered, scored[candLimit:]...)
	}

	log.Printf("[rerank] Top1 凸组合重排前后：orig_id=%d(final=%.4f) -> top_id=%d(final=%.4f, rerank=%.4f, blend=%.4f)",
		scored[0].Chunk.ID, scored[0].Final, entries[0].chunk.Chunk.ID, entries[0].chunk.Final, entries[0].chunk.RerankScore, entries[0].blend)

	return ApplyMultiEntityCoverage(question, reordered, k, minCoverageScore)
}
