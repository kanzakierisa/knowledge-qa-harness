// Package api 提供 HTTP 接口与单页 Web 界面路由。
package api

import (
	"errors"
	"fmt"
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

const (
	// refuseThreshold 融合检索 Top1 融合分低于该值直接拒答（不调 LLM）。
	// 初始 0.35，属可调常量：调高更保守（更容易拒答），调低更激进。
	refuseThreshold = 0.35

	// 跨项目歧义拦截阈值（阶段 10）：看 Top 8 候选中非 shared 项目，
	// 若至少 2 个项目达标且第 2 名与全场 Top1 分差 <= 0.08，则判定为歧义。
	ambiguityTopCandidates   = 8
	ambiguityCompetitionDiff = 0.08
	ambiguityFloatEpsilon    = 1e-9
)

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
	Question    string `json:"question" binding:"required"`
	Debug       bool   `json:"debug"`        // 可选：true 时返回每路分数明细，默认关闭
	ProjectSlug string `json:"project_slug"` // 可选："" 或 "auto" 为自动路由，否则强隔离为 target + shared
	Compare     bool   `json:"compare"`      // 可选：true 且命中跨项目歧义时对候选项目逐一生成对照回答
}

type citation struct {
	N           int    `json:"n"`
	ChunkID     int64  `json:"chunk_id"`
	Heading     string `json:"heading"`
	Content     string `json:"content"`
	ProjectSlug string `json:"project_slug,omitempty"`
	ProjectName string `json:"project_name,omitempty"`
	DocTitle    string `json:"doc_title,omitempty"`
	DocStatus   string `json:"doc_status,omitempty"`
	DocLevel    string `json:"doc_level,omitempty"`
	Owner       string `json:"owner,omitempty"`
}

// debugChunk 是 debug 模式下单段的分数明细。
type debugChunk struct {
	N            int      `json:"n"`
	ChunkID      int64    `json:"chunk_id"`
	Heading      string   `json:"heading"`
	ProjectSlug  string   `json:"project_slug,omitempty"`
	DocLevel     string   `json:"doc_level,omitempty"`
	DocStatus    string   `json:"doc_status,omitempty"`
	VectorScore  float64  `json:"vector_score"`  // 向量余弦原始分（0~1）
	BM25Raw      float64  `json:"bm25_raw"`      // BM25 原始分
	KeywordScore float64  `json:"keyword_score"` // BM25 归一化分
	Final        float64  `json:"final"`         // 融合分
	MatchedTerms []string `json:"matched_terms"` // 命中的关键词
}

type ambiguityCandidate struct {
	ProjectSlug string  `json:"project_slug"`
	ProjectName string  `json:"project_name"`
	TopScore    float64 `json:"top_score"`
	TopHeading  string  `json:"top_heading"`
}

type ambiguityInfo struct {
	Detected   bool                 `json:"detected"`
	Candidates []ambiguityCandidate `json:"candidates"`
}

type askWarning struct {
	Type            string `json:"type"`
	ChunkN          int    `json:"chunk_n"`
	DocumentTitle   string `json:"document_title"`
	Owner           string `json:"owner"`
	ReplacedByDocID *int64 `json:"replaced_by_doc_id,omitempty"`
	ReplacedByTitle string `json:"replaced_by_title,omitempty"`
	Message         string `json:"message"`
}

type comparisonResult struct {
	ProjectSlug string       `json:"project_slug"`
	ProjectName string       `json:"project_name"`
	Answer      string       `json:"answer"`
	Citations   []citation   `json:"citations"`
	Refused     bool         `json:"refused"`
	Retried     bool         `json:"retried"`
	Warnings    []askWarning `json:"warnings,omitempty"`
	Debug       []debugChunk `json:"debug,omitempty"`
}

type askResponse struct {
	Answer          string             `json:"answer"`
	Citations       []citation         `json:"citations"`
	Refused         bool               `json:"refused"`
	Retried         bool               `json:"retried"` // 阶段 4：引用校验失败后是否重试过
	ProjectResolved string             `json:"project_resolved,omitempty"`
	Ambiguity       *ambiguityInfo     `json:"ambiguity,omitempty"`
	Warnings        []askWarning       `json:"warnings,omitempty"`
	Comparisons     []comparisonResult `json:"comparisons,omitempty"`
	Debug           []debugChunk       `json:"debug,omitempty"`
}

type askOptions struct {
	Debug       bool
	ProjectSlug string
	Compare     bool
}

// Ask 校验 HTTP 入参后调用内部问答函数；评测也复用该内部函数。
func (h *Handler) Ask(c *gin.Context) {
	var req askRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Question) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "question 不能为空"})
		return
	}
	resp, _, err := h.answerQuestionWithOptions(req.Question, askOptions{
		Debug:       req.Debug,
		ProjectSlug: req.ProjectSlug,
		Compare:     req.Compare,
	})
	if err != nil {
		if errors.Is(err, store.ErrProjectNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// answerQuestion 保持 V1 内部签名兼容（供 eval 等复用，默认自动路由）。
func (h *Handler) answerQuestion(question string, debug bool) (askResponse, []retrieve.MergedChunk, error) {
	return h.answerQuestionWithOptions(question, askOptions{Debug: debug})
}

// answerQuestionWithOptions 走完整多项目问答链路：
// 项目隔离校验 → 读切片与问题向量化 → 混合检索与治理加权 → 跨项目歧义拦截 → 阈值拒答 →
// LLM 生成与句级引用校验 → 废弃文档警告组装。
func (h *Handler) answerQuestionWithOptions(question string, opts askOptions) (askResponse, []retrieve.MergedChunk, error) {
	reqSlug := strings.TrimSpace(opts.ProjectSlug)
	isAuto := reqSlug == "" || reqSlug == "auto" || reqSlug == "all"

	if !isAuto {
		if _, err := h.Store.GetProjectBySlug(reqSlug); err != nil {
			if errors.Is(err, store.ErrProjectNotFound) {
				return askResponse{}, nil, err
			}
			if !store.IsV1SchemaMissingError(err) {
				return askResponse{}, nil, err
			}
		}
	}

	chunks, err := h.Store.AllChunksWithMeta()
	if err != nil {
		return askResponse{}, nil, err
	}

	if !isAuto {
		scoped := retrieve.FilterChunksByProject(chunks, reqSlug)
		if len(scoped) == 0 {
			log.Printf("[ask] 项目 %q（含 shared）暂无切片，未调用 LLM：%q", reqSlug, question)
			return askResponse{
				Answer:          "当前项目（含公共规范库）暂无资料，文档中没有依据",
				Citations:       []citation{},
				Refused:         true,
				ProjectResolved: reqSlug,
			}, nil, nil
		}
	} else if len(chunks) == 0 {
		return askResponse{
			Answer:    generate.RefuseAnswer,
			Citations: []citation{},
			Refused:   true,
		}, nil, nil
	}

	qVec, err := h.Embedder.Embed(question)
	if err != nil {
		return askResponse{}, nil, err
	}

	if !isAuto {
		top := retrieve.MergeWithOptions(question, qVec, chunks, retrieve.TopN, retrieve.MergeOptions{ProjectSlug: reqSlug})
		return h.finalizeAnswer(question, top, reqSlug, opts.Debug)
	}

	scored := retrieve.ScoreWithOptions(question, qVec, chunks, retrieve.MergeOptions{})
	if len(scored) == 0 || scored[0].Final < refuseThreshold {
		top := truncateMerged(scored, retrieve.TopN)
		return h.finalizeAnswer(question, top, "", opts.Debug)
	}

	amb := detectCrossProjectAmbiguity(scored, refuseThreshold, retrieve.TopN)
	if amb.Ambiguity != nil && amb.Ambiguity.Detected {
		if opts.Compare {
			comparisons := make([]comparisonResult, 0, len(amb.Ambiguity.Candidates))
			for _, cand := range amb.Ambiguity.Candidates {
				candTop := retrieve.MergeWithOptions(question, qVec, chunks, retrieve.TopN, retrieve.MergeOptions{ProjectSlug: cand.ProjectSlug})
				candResp, _, err := h.finalizeAnswer(question, candTop, cand.ProjectSlug, opts.Debug)
				if err != nil {
					return askResponse{}, amb.FilteredTop, err
				}
				comparisons = append(comparisons, comparisonResult{
					ProjectSlug: cand.ProjectSlug,
					ProjectName: cand.ProjectName,
					Answer:      candResp.Answer,
					Citations:   candResp.Citations,
					Refused:     candResp.Refused,
					Retried:     candResp.Retried,
					Warnings:    candResp.Warnings,
					Debug:       candResp.Debug,
				})
			}
			return askResponse{
				Answer:          "检测到多个并行项目均包含与该问题相关的规范，已按候选项目分别生成对照回答：",
				Citations:       buildCitations(amb.FilteredTop),
				Refused:         false,
				Retried:         false,
				ProjectResolved: "compare",
				Ambiguity:       amb.Ambiguity,
				Comparisons:     comparisons,
				Debug:           maybeDebug(opts.Debug, amb.FilteredTop),
			}, amb.FilteredTop, nil
		}

		log.Printf("[ask] 检测到跨项目歧义（未调用 LLM），候选=%v：%q", candidateSlugs(amb.Ambiguity.Candidates), question)
		return askResponse{
			Answer:          "检测到多个并行项目均包含与该问题相关的规范，请先确认您要查询的具体项目：",
			Citations:       buildCitations(amb.FilteredTop),
			Refused:         false,
			Retried:         false,
			ProjectResolved: "ambiguous",
			Ambiguity:       amb.Ambiguity,
			Debug:           maybeDebug(opts.Debug, amb.FilteredTop),
		}, amb.FilteredTop, nil
	}

	return h.finalizeAnswer(question, amb.FilteredTop, amb.ProjectResolved, opts.Debug)
}

func (h *Handler) finalizeAnswer(question string, top []retrieve.MergedChunk, projectResolved string, debug bool) (askResponse, []retrieve.MergedChunk, error) {
	if len(top) == 0 || top[0].Final < refuseThreshold {
		log.Printf("[ask] 阈值拒答（Top1 融合分 %.4f < %.2f），未调用 LLM：%q",
			top1Final(top), refuseThreshold, question)
		return askResponse{
			Answer:          generate.RefuseAnswer,
			Citations:       []citation{},
			Refused:         true,
			ProjectResolved: projectResolved,
			Debug:           maybeDebug(debug, top),
		}, top, nil
	}

	answer, retried, ok, err := h.Chat.GenerateAnswer(question, top)
	if err != nil {
		return askResponse{}, top, err
	}

	if generate.IsRefusal(answer) || !ok {
		log.Printf("[ask] 生成侧拒答（模型自拒答=%v, 引用校验通过=%v, 重试过=%v）：%q",
			generate.IsRefusal(answer), ok, retried, question)
		return askResponse{
			Answer:          generate.RefuseAnswer,
			Citations:       []citation{},
			Refused:         true,
			Retried:         retried,
			ProjectResolved: projectResolved,
			Debug:           maybeDebug(debug, top),
		}, top, nil
	}

	if retried {
		log.Printf("[ask] 引用校验重试后通过：%q", question)
	}
	return askResponse{
		Answer:          answer,
		Citations:       buildCitations(top),
		Refused:         false,
		Retried:         retried,
		ProjectResolved: projectResolved,
		Warnings:        buildDeprecatedWarnings(answer, top),
		Debug:           maybeDebug(debug, top),
	}, top, nil
}

type ambiguityEvaluation struct {
	Ambiguity       *ambiguityInfo
	ProjectResolved string
	FilteredTop     []retrieve.MergedChunk
}

// detectCrossProjectAmbiguity 检查 auto 模式下的跨项目高分竞争冲突：
//  1. 考察 Top 8 候选中 Final >= threshold 的切片；
//  2. 记录非 shared 项目各自的最高分 projTopScore；
//  3. 若存在 >=2 个非 shared 项目达标且第 2 名与全场 Top1 分差 <= 0.08，则返回歧义引导（不调用 LLM）；
//  4. 若仅 1 个非 shared 项目处于竞争窗口内，则锁定该方法返回的 project_resolved 并过滤掉其他非 shared 干扰切片后再取 Top N。
func detectCrossProjectAmbiguity(scored []retrieve.MergedChunk, threshold float64, topN int) ambiguityEvaluation {
	if len(scored) == 0 || scored[0].Final < threshold {
		return ambiguityEvaluation{FilteredTop: truncateMerged(scored, topN)}
	}

	limit := ambiguityTopCandidates
	if len(scored) < limit {
		limit = len(scored)
	}

	top1Final := scored[0].Final
	var nonShared []ambiguityCandidate
	seenProj := make(map[string]int)
	hasShared := false

	for _, sc := range scored[:limit] {
		if sc.Final < threshold {
			break
		}
		slug := strings.TrimSpace(sc.Chunk.ProjectSlug)
		if slug == "" {
			continue
		}
		if slug == "shared" {
			hasShared = true
			continue
		}
		if _, exists := seenProj[slug]; exists {
			continue
		}
		seenProj[slug] = len(nonShared)
		projName := strings.TrimSpace(sc.Chunk.ProjectName)
		if projName == "" {
			projName = slug
		}
		nonShared = append(nonShared, ambiguityCandidate{
			ProjectSlug: slug,
			ProjectName: projName,
			TopScore:    sc.Final,
			TopHeading:  sc.Chunk.HeadingPath,
		})
	}

	if len(nonShared) >= 2 && top1Final-nonShared[1].TopScore <= ambiguityCompetitionDiff+ambiguityFloatEpsilon {
		var competing []ambiguityCandidate
		for _, p := range nonShared {
			if top1Final-p.TopScore <= ambiguityCompetitionDiff+ambiguityFloatEpsilon {
				p.TopScore = round4(p.TopScore)
				competing = append(competing, p)
			}
		}
		return ambiguityEvaluation{
			Ambiguity: &ambiguityInfo{
				Detected:   true,
				Candidates: competing,
			},
			ProjectResolved: "ambiguous",
			FilteredTop:     truncateMerged(scored, topN),
		}
	}

	var inWindow []ambiguityCandidate
	for _, p := range nonShared {
		if top1Final-p.TopScore <= ambiguityCompetitionDiff+ambiguityFloatEpsilon {
			inWindow = append(inWindow, p)
		}
	}

	if len(inWindow) == 1 {
		resolved := inWindow[0].ProjectSlug
		filtered := make([]retrieve.MergedChunk, 0, topN)
		for _, sc := range scored {
			if sc.Chunk.ProjectSlug == resolved || sc.Chunk.ProjectSlug == "shared" {
				filtered = append(filtered, sc)
				if len(filtered) >= topN {
					break
				}
			}
		}
		return ambiguityEvaluation{
			ProjectResolved: resolved,
			FilteredTop:     filtered,
		}
	}

	if len(nonShared) == 0 {
		resolved := ""
		if hasShared {
			resolved = "shared"
		}
		return ambiguityEvaluation{
			ProjectResolved: resolved,
			FilteredTop:     truncateMerged(scored, topN),
		}
	}

	// top1 为 shared 且第 1 名非 shared 项目在竞争窗口之外：
	// 若存在多个非 shared 干扰项目则仅保留 shared，避免多项目零散切片混入 Prompt。
	if len(nonShared) >= 2 && strings.TrimSpace(scored[0].Chunk.ProjectSlug) == "shared" {
		filtered := make([]retrieve.MergedChunk, 0, topN)
		for _, sc := range scored {
			if sc.Chunk.ProjectSlug == "shared" {
				filtered = append(filtered, sc)
				if len(filtered) >= topN {
					break
				}
			}
		}
		return ambiguityEvaluation{
			ProjectResolved: "shared",
			FilteredTop:     filtered,
		}
	}

	resolved := ""
	if strings.TrimSpace(scored[0].Chunk.ProjectSlug) == "shared" {
		resolved = "shared"
	}
	return ambiguityEvaluation{
		ProjectResolved: resolved,
		FilteredTop:     truncateMerged(scored, topN),
	}
}

func buildCitations(top []retrieve.MergedChunk) []citation {
	cits := make([]citation, 0, len(top))
	for i, sc := range top {
		cits = append(cits, citation{
			N:           i + 1,
			ChunkID:     sc.Chunk.ID,
			Heading:     sc.Chunk.HeadingPath,
			Content:     sc.Chunk.Content,
			ProjectSlug: sc.Chunk.ProjectSlug,
			ProjectName: sc.Chunk.ProjectName,
			DocTitle:    sc.Chunk.DocTitle,
			DocStatus:   sc.Chunk.DocStatus,
			DocLevel:    sc.Chunk.DocLevel,
			Owner:       sc.Chunk.DocOwner,
		})
	}
	return cits
}

func buildDeprecatedWarnings(answer string, top []retrieve.MergedChunk) []askWarning {
	cited := generate.ExtractCitedNumbers(answer)
	if len(cited) == 0 {
		return nil
	}
	var warnings []askWarning
	for _, n := range cited {
		idx := n - 1
		if idx < 0 || idx >= len(top) {
			continue
		}
		ch := top[idx].Chunk
		if !strings.EqualFold(strings.TrimSpace(ch.DocStatus), "deprecated") {
			continue
		}
		docTitle := strings.TrimSpace(ch.DocTitle)
		if docTitle == "" {
			docTitle = ch.HeadingPath
		}
		ownerPart := ""
		if strings.TrimSpace(ch.DocOwner) != "" {
			ownerPart = fmt.Sprintf("（负责人：%s）", strings.TrimSpace(ch.DocOwner))
		}
		replacePart := "，请注意该文档已废弃。"
		if strings.TrimSpace(ch.ReplacedByTitle) != "" {
			replacePart = fmt.Sprintf("，建议查阅替代文档《%s》。", strings.TrimSpace(ch.ReplacedByTitle))
		} else if ch.ReplacedByDocID != nil {
			replacePart = fmt.Sprintf("，建议查阅替代文档（ID=%d）。", *ch.ReplacedByDocID)
		}
		warnings = append(warnings, askWarning{
			Type:            "deprecated_doc",
			ChunkN:          n,
			DocumentTitle:   docTitle,
			Owner:           ch.DocOwner,
			ReplacedByDocID: ch.ReplacedByDocID,
			ReplacedByTitle: ch.ReplacedByTitle,
			Message:         fmt.Sprintf("引用来源 [%d] 来自已废弃文档《%s》%s%s", n, docTitle, ownerPart, replacePart),
		})
	}
	return warnings
}

func truncateMerged(list []retrieve.MergedChunk, limit int) []retrieve.MergedChunk {
	if limit <= 0 || len(list) == 0 {
		return nil
	}
	if len(list) > limit {
		return list[:limit]
	}
	return list
}

func candidateSlugs(cands []ambiguityCandidate) []string {
	out := make([]string, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.ProjectSlug)
	}
	return out
}

func maybeDebug(debug bool, top []retrieve.MergedChunk) []debugChunk {
	if !debug {
		return nil
	}
	return buildDebug(top)
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
			ProjectSlug:  sc.Chunk.ProjectSlug,
			DocLevel:     sc.Chunk.DocLevel,
			DocStatus:    sc.Chunk.DocStatus,
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
