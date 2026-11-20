package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"knowledge-qa/config"
	"knowledge-qa/generate"
	"knowledge-qa/ingest"
	"knowledge-qa/retrieve"
)

var errModelNotConfigured = errors.New("尚未配置模型 API，请先在网页端完成模型配置")

type runtimeSetupMeta struct {
	Configured       bool
	ChatBaseURL      string
	ChatModel        string
	EmbedBaseURL     string
	EmbedModel       string
	HasEmbedOverride bool
	VLMModel         string
	VLMBaseURL       string
	RerankBaseURL    string
	RerankModel      string
	ConvertEngine    string
	MissingModelVars []string
}

// InitSetupFromConfig 根据启动配置初始化运行时客户端与脱敏状态元数据（绝不保留/下发任何密钥明文）。
func (h *Handler) InitSetupFromConfig(cfg *config.Config, setupEnabled bool, missingModelVars []string) {
	h.clientMu.Lock()
	defer h.clientMu.Unlock()

	h.SetupEnabled = setupEnabled
	if cfg == nil {
		return
	}
	configured := len(missingModelVars) == 0 &&
		strings.TrimSpace(cfg.Chat.BaseURL) != "" &&
		strings.TrimSpace(cfg.Chat.APIKey) != "" &&
		strings.TrimSpace(cfg.Chat.Model) != "" &&
		strings.TrimSpace(cfg.Embed.Model) != ""

	if configured {
		h.Embedder = ingest.NewEmbedder(cfg.Embed.BaseURL, cfg.Embed.APIKey, cfg.Embed.Model)
		h.Chat = generate.NewChatClient(cfg.Chat.BaseURL, cfg.Chat.APIKey, cfg.Chat.Model)
		var vlm ingest.VLMTranscriber
		if cfg.VLM.Model != "" {
			vlm = ingest.NewVLMClient(cfg.VLM.BaseURL, cfg.VLM.APIKey, cfg.VLM.Model)
		}
		h.Converter = ingest.NewConverter(ingest.ConvertOptions{Engine: cfg.ConvertEngine, VLM: vlm})
		if cfg.Rerank.BaseURL != "" {
			h.Reranker = retrieve.NewRerankClient(cfg.Rerank.BaseURL, cfg.Rerank.APIKey, cfg.Rerank.Model)
		} else {
			h.Reranker = nil
		}
	}

	eng := cfg.ConvertEngine
	if eng == "" {
		eng = "native"
	}
	h.setupMeta = runtimeSetupMeta{
		Configured:       configured,
		ChatBaseURL:      cfg.Chat.BaseURL,
		ChatModel:        cfg.Chat.Model,
		EmbedBaseURL:     cfg.Embed.BaseURL,
		EmbedModel:       cfg.Embed.Model,
		HasEmbedOverride: cfg.Embed.BaseURL != "" && cfg.Embed.BaseURL != cfg.Chat.BaseURL,
		VLMModel:         cfg.VLM.Model,
		VLMBaseURL:       cfg.VLM.BaseURL,
		RerankBaseURL:    cfg.Rerank.BaseURL,
		RerankModel:      cfg.Rerank.Model,
		ConvertEngine:    eng,
		MissingModelVars: append([]string(nil), missingModelVars...),
	}
}

func (h *Handler) getClients() (*ingest.Embedder, *generate.ChatClient, ingest.Converter, retrieve.Reranker) {
	h.clientMu.RLock()
	defer h.clientMu.RUnlock()
	conv := h.Converter
	if conv == nil {
		conv = ingest.NewConverterFromEnv()
	}
	return h.Embedder, h.Chat, conv, h.Reranker
}

type setupStatusResponse struct {
	SetupEnabled     bool     `json:"setup_enabled"`
	Configured       bool     `json:"configured"`
	Missing          []string `json:"missing,omitempty"`
	ChatBaseURL      string   `json:"chat_base_url,omitempty"`
	ChatModel        string   `json:"chat_model,omitempty"`
	EmbedBaseURL     string   `json:"embed_base_url,omitempty"`
	EmbedModel       string   `json:"embed_model,omitempty"`
	HasEmbedOverride bool     `json:"has_embed_override"`
	VLMModel         string   `json:"vlm_model,omitempty"`
	VLMBaseURL       string   `json:"vlm_base_url,omitempty"`
	RerankBaseURL    string   `json:"rerank_base_url,omitempty"`
	RerankModel      string   `json:"rerank_model,omitempty"`
	ConvertEngine    string   `json:"convert_engine"`
}

// GetSetupStatus 返回当前模型配置就绪状态（只返回脱敏元数据，严禁下发任何 API Key）。
func (h *Handler) GetSetupStatus(c *gin.Context) {
	h.clientMu.RLock()
	meta := h.setupMeta
	configured := meta.Configured || (h.Embedder != nil && h.Chat != nil)
	setupEnabled := h.SetupEnabled
	h.clientMu.RUnlock()

	eng := meta.ConvertEngine
	if eng == "" {
		eng = "native"
	}
	c.JSON(http.StatusOK, setupStatusResponse{
		SetupEnabled:     setupEnabled,
		Configured:       configured,
		Missing:          meta.MissingModelVars,
		ChatBaseURL:      meta.ChatBaseURL,
		ChatModel:        meta.ChatModel,
		EmbedBaseURL:     meta.EmbedBaseURL,
		EmbedModel:       meta.EmbedModel,
		HasEmbedOverride: meta.HasEmbedOverride,
		VLMModel:         meta.VLMModel,
		VLMBaseURL:       meta.VLMBaseURL,
		RerankBaseURL:    meta.RerankBaseURL,
		RerankModel:      meta.RerankModel,
		ConvertEngine:    eng,
	})
}

type applySetupRequest struct {
	LLMBaseURL    string `json:"llm_base_url"`
	LLMAPIKey     string `json:"llm_api_key"`
	LLMChatModel  string `json:"llm_chat_model"`
	LLMEmbedModel string `json:"llm_embed_model"`
	EmbedBaseURL  string `json:"embed_base_url"`
	EmbedAPIKey   string `json:"embed_api_key"`
	LLMVLMModel   string `json:"llm_vlm_model"`
	VLMBaseURL    string `json:"vlm_base_url"`
	VLMAPIKey     string `json:"vlm_api_key"`
	RerankBaseURL string `json:"rerank_base_url"`
	RerankAPIKey  string `json:"rerank_api_key"`
	RerankModel   string `json:"rerank_model"`
	ConvertEngine string `json:"convert_engine"`
}

func isValidHTTPURL(u string) bool {
	low := strings.ToLower(strings.TrimSpace(u))
	return strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://")
}

// ApplySetup 仅在 --setup-ui / ALLOW_WEB_SETUP=true 开启时接收模型配置并热更新内存运行态（绝不落盘）。
func (h *Handler) ApplySetup(c *gin.Context) {
	if !h.SetupEnabled {
		c.JSON(http.StatusForbidden, gin.H{"error": "当前未开启 --setup-ui / ALLOW_WEB_SETUP，禁止通过接口修改运行时配置"})
		return
	}
	var req applySetupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求 JSON 格式无效"})
		return
	}

	chatBaseURL := strings.TrimRight(strings.TrimSpace(req.LLMBaseURL), "/")
	chatAPIKey := strings.TrimSpace(req.LLMAPIKey)
	chatModel := strings.TrimSpace(req.LLMChatModel)
	embedModel := strings.TrimSpace(req.LLMEmbedModel)

	if chatBaseURL == "" || chatAPIKey == "" || chatModel == "" || embedModel == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "LLM_BASE_URL、LLM_API_KEY、LLM_CHAT_MODEL、LLM_EMBED_MODEL 均不能为空"})
		return
	}
	if !isValidHTTPURL(chatBaseURL) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "LLM_BASE_URL 必须以 http:// 或 https:// 开头"})
		return
	}

	embedBaseURL := strings.TrimRight(strings.TrimSpace(req.EmbedBaseURL), "/")
	if embedBaseURL == "" {
		embedBaseURL = chatBaseURL
	} else if !isValidHTTPURL(embedBaseURL) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "EMBED_BASE_URL 必须以 http:// 或 https:// 开头"})
		return
	}
	embedAPIKey := strings.TrimSpace(req.EmbedAPIKey)
	if embedAPIKey == "" {
		embedAPIKey = chatAPIKey
	}

	vlmModel := strings.TrimSpace(req.LLMVLMModel)
	vlmBaseURL := strings.TrimRight(strings.TrimSpace(req.VLMBaseURL), "/")
	if vlmBaseURL == "" {
		vlmBaseURL = chatBaseURL
	} else if !isValidHTTPURL(vlmBaseURL) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "VLM_BASE_URL 必须以 http:// 或 https:// 开头"})
		return
	}
	vlmAPIKey := strings.TrimSpace(req.VLMAPIKey)
	if vlmAPIKey == "" {
		vlmAPIKey = chatAPIKey
	}

	rerankBaseURL := strings.TrimRight(strings.TrimSpace(req.RerankBaseURL), "/")
	if rerankBaseURL != "" && !isValidHTTPURL(rerankBaseURL) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "RERANK_BASE_URL 必须以 http:// 或 https:// 开头"})
		return
	}
	rerankAPIKey := strings.TrimSpace(req.RerankAPIKey)
	if rerankAPIKey == "" {
		rerankAPIKey = embedAPIKey
	}
	rerankModel := strings.TrimSpace(req.RerankModel)
	if rerankModel == "" && rerankBaseURL != "" {
		rerankModel = "gte-rerank-v2"
	}

	convertEngine := strings.ToLower(strings.TrimSpace(req.ConvertEngine))
	if convertEngine == "" {
		convertEngine = "native"
	}
	if convertEngine != "native" && convertEngine != "markitdown" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "CONVERT_ENGINE 仅支持 native 或 markitdown"})
		return
	}

	var vlm ingest.VLMTranscriber
	if vlmModel != "" {
		vlm = ingest.NewVLMClient(vlmBaseURL, vlmAPIKey, vlmModel)
	}
	var reranker retrieve.Reranker
	if rerankBaseURL != "" {
		reranker = retrieve.NewRerankClient(rerankBaseURL, rerankAPIKey, rerankModel)
	}

	h.clientMu.Lock()
	h.Embedder = ingest.NewEmbedder(embedBaseURL, embedAPIKey, embedModel)
	h.Chat = generate.NewChatClient(chatBaseURL, chatAPIKey, chatModel)
	h.Converter = ingest.NewConverter(ingest.ConvertOptions{Engine: convertEngine, VLM: vlm})
	h.Reranker = reranker
	h.setupMeta = runtimeSetupMeta{
		Configured:       true,
		ChatBaseURL:      chatBaseURL,
		ChatModel:        chatModel,
		EmbedBaseURL:     embedBaseURL,
		EmbedModel:       embedModel,
		HasEmbedOverride: embedBaseURL != chatBaseURL,
		VLMModel:         vlmModel,
		VLMBaseURL:       vlmBaseURL,
		RerankBaseURL:    rerankBaseURL,
		RerankModel:      rerankModel,
		ConvertEngine:    convertEngine,
		MissingModelVars: nil,
	}
	h.clientMu.Unlock()

	h.GetSetupStatus(c)
}
