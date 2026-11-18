// Package api 提供 HTTP 接口与单页 Web 界面路由。
package api

import (
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"knowledge-qa/generate"
	"knowledge-qa/ingest"
	"knowledge-qa/retrieve"
	"knowledge-qa/static"
	"knowledge-qa/store"
)

// refuseThreshold 融合检索 Top1 融合分低于该值直接拒答（不调 LLM）。
// 初始 0.35，属可调常量：调高更保守（更容易拒答），调低更激进。
const refuseThreshold = 0.35

// Handler 聚合问答链路与资料库治理所需依赖。
type Handler struct {
	Store     *store.Store
	Embedder  *ingest.Embedder
	Chat      *generate.ChatClient
	Converter ingest.Converter // 可选：nil 时按环境变量初始化默认转换器
	evalMu    sync.Mutex       // 同一时刻只允许一批评测，冲突请求返回 409
}

// NewRouter 装配路由。
func NewRouter(h *Handler) *gin.Engine {
	r := gin.Default()
	r.GET("/", h.Index)
	r.HEAD("/", h.Index)
	r.POST("/api/ask", h.Ask)
	r.GET("/api/chunks", h.Chunks)
	r.GET("/api/projects", h.ListProjects)
	r.POST("/api/projects", h.CreateProject)
	r.GET("/api/documents", h.ListDocuments)
	r.POST("/api/documents/upload", h.UploadDocument)
	r.PATCH("/api/documents/:id", h.PatchDocument)
	r.DELETE("/api/documents/:id", h.DeleteDocument)
	evalHandler := newEvalHTTPHandler(h)
	r.POST("/api/eval/run", evalHandler.Run)
	r.POST("/api/eval/mark", evalHandler.Mark)
	return r
}

// Index 返回内嵌的阶段 7 单页 Web 界面。
func (h *Handler) Index(c *gin.Context) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	if c.Request.Method == http.MethodHead {
		c.Status(http.StatusOK)
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", static.IndexHTML)
}

type askRequest struct {
	Question string `json:"question" binding:"required"`
	Debug    bool   `json:"debug"` // 可选：true 时返回每路分数明细，默认关闭
}

type citation struct {
	N       int    `json:"n"`
	ChunkID int64  `json:"chunk_id"`
	Heading string `json:"heading"`
	Content string `json:"content"`
}

// debugChunk 是 debug 模式下单段的分数明细。
type debugChunk struct {
	N            int      `json:"n"`
	ChunkID      int64    `json:"chunk_id"`
	Heading      string   `json:"heading"`
	VectorScore  float64  `json:"vector_score"`  // 向量余弦原始分（0~1）
	BM25Raw      float64  `json:"bm25_raw"`      // BM25 原始分
	KeywordScore float64  `json:"keyword_score"` // BM25 归一化分
	Final        float64  `json:"final"`         // 融合分
	MatchedTerms []string `json:"matched_terms"` // 命中的关键词
}

type askResponse struct {
	Answer    string       `json:"answer"`
	Citations []citation   `json:"citations"`
	Refused   bool         `json:"refused"`
	Retried   bool         `json:"retried"` // 阶段 4：引用校验失败后是否重试过
	Debug     []debugChunk `json:"debug,omitempty"`
}

// Ask 校验 HTTP 入参后调用内部问答函数；评测也复用该内部函数。
func (h *Handler) Ask(c *gin.Context) {
	var req askRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Question == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "question 不能为空"})
		return
	}
	resp, _, err := h.answerQuestion(req.Question, req.Debug)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// answerQuestion 走完整链路：问题 embedding → 混合检索 Top 5 → 阈值拒答 →
// LLM 生成与引用校验。除 HTTP 响应外还返回 Top 5，供评测记录真实检索结果。
func (h *Handler) answerQuestion(question string, debug bool) (askResponse, []retrieve.MergedChunk, error) {
	// 1. 问题向量化
	qVec, err := h.Embedder.Embed(question)
	if err != nil {
		return askResponse{}, nil, err
	}

	// 2. 全量读切片，混合检索 Top 5（融合权重与软下限由 retrieve 包常量控制）
	chunks, err := h.Store.AllChunks()
	if err != nil {
		return askResponse{}, nil, err
	}
	top := retrieve.Merge(question, qVec, chunks, retrieve.TopN)

	// 3. Top1 融合分低于阈值 → 直接拒答（仍把 Top 5 交给评测记录）
	if len(top) == 0 || top[0].Final < refuseThreshold {
		log.Printf("[ask] 阈值拒答（Top1 融合分 %.4f < %.2f），未调用 LLM：%q",
			top1Final(top), refuseThreshold, question)
		resp := askResponse{
			Answer:    generate.RefuseAnswer,
			Citations: []citation{},
			Refused:   true,
		}
		if debug {
			resp.Debug = buildDebug(top)
		}
		return resp, top, nil
	}

	// 4. 拼 prompt 调 LLM + 引用后校验（失败重试一次，仍失败按拒答处理）
	answer, retried, ok, err := h.Chat.GenerateAnswer(question, top)
	if err != nil {
		return askResponse{}, top, err
	}

	// 5. 生成侧拒答保险：模型自己说拒答 或 引用校验重试仍失败
	if generate.IsRefusal(answer) || !ok {
		log.Printf("[ask] 生成侧拒答（模型自拒答=%v, 引用校验通过=%v, 重试过=%v）：%q",
			generate.IsRefusal(answer), ok, retried, question)
		resp := askResponse{
			Answer:    generate.RefuseAnswer,
			Citations: []citation{},
			Refused:   true,
			Retried:   retried,
		}
		if debug {
			resp.Debug = buildDebug(top)
		}
		return resp, top, nil
	}

	// 6. 返回答案 + 引用编号→原文段落映射
	if retried {
		log.Printf("[ask] 引用校验重试后通过：%q", question)
	}
	cits := make([]citation, 0, len(top))
	for i, sc := range top {
		cits = append(cits, citation{
			N:       i + 1,
			ChunkID: sc.Chunk.ID,
			Heading: sc.Chunk.HeadingPath,
			Content: sc.Chunk.Content,
		})
	}
	resp := askResponse{
		Answer:    answer,
		Citations: cits,
		Refused:   false,
		Retried:   retried,
	}
	if debug {
		resp.Debug = buildDebug(top)
	}
	return resp, top, nil
}

func buildDebug(top []retrieve.MergedChunk) []debugChunk {
	out := make([]debugChunk, 0, len(top))
	for i, sc := range top {
		matched := sc.MatchedTerms
		if matched == nil {
			matched = []string{}
		}
		out = append(out, debugChunk{
			N:            i + 1,
			ChunkID:      sc.Chunk.ID,
			Heading:      sc.Chunk.HeadingPath,
			VectorScore:  round4(sc.VectorScore),
			BM25Raw:      round4(sc.BM25Raw),
			KeywordScore: round4(sc.KeywordScore),
			Final:        round4(sc.Final),
			MatchedTerms: matched,
		})
	}
	return out
}

// round4 保留 4 位小数，让 debug 输出干净些。
func round4(f float64) float64 { return float64(int(f*10000+0.5)) / 10000 }

// top1Final 取 Top1 融合分，用于日志；空结果返回 0。
func top1Final(top []retrieve.MergedChunk) float64 {
	if len(top) == 0 {
		return 0
	}
	return top[0].Final
}

// Chunks 健康检查：返回库里的段数（可选 ?project_slug=... 过滤）。
func (h *Handler) Chunks(c *gin.Context) {
	projectSlug := strings.TrimSpace(c.Query("project_slug"))
	var (
		n   int
		err error
	)
	if projectSlug != "" && projectSlug != "auto" && projectSlug != "all" {
		n, err = h.Store.CountChunksByProject(projectSlug)
	} else {
		n, err = h.Store.CountChunks()
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"total": n})
}
