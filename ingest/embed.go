package ingest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Embedder 调 OpenAI 兼容的 embedding API。
type Embedder struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

// NewEmbedder 创建 Embedder；baseURL 末尾的 / 会被去掉。
func NewEmbedder(baseURL, apiKey, model string) *Embedder {
	return &Embedder{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

type embedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Embed 把一段文本变成向量。
func (e *Embedder) Embed(text string) ([]float64, error) {
	reqBody, _ := json.Marshal(embedRequest{Model: e.model, Input: text})
	req, err := http.NewRequest("POST", e.baseURL+"/embeddings", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding 请求失败：%w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding API 返回 %d：%s", resp.StatusCode, string(raw))
	}
	var er embedResponse
	if err := json.Unmarshal(raw, &er); err != nil {
		return nil, fmt.Errorf("embedding 响应解析失败：%w", err)
	}
	if er.Error != nil {
		return nil, fmt.Errorf("embedding API 错误：%s", er.Error.Message)
	}
	if len(er.Data) == 0 {
		return nil, fmt.Errorf("embedding API 返回空数据")
	}
	return er.Data[0].Embedding, nil
}
