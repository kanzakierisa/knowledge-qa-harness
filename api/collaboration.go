package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"knowledge-qa/store"
)

// ListGaps 查询团队知识盲区雷达列表（支持 ?project_slug=&status= 过滤）。
func (h *Handler) ListGaps(c *gin.Context) {
	if h.Store == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Store 未初始化"})
		return
	}
	projectSlug := c.Query("project_slug")
	status := c.Query("status")
	gaps, err := h.Store.ListKnowledgeGaps(projectSlug, status)
	if err != nil {
		if errors.Is(err, store.ErrInvalidGapStatus) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"gaps": gaps})
}

type patchGapRequest struct {
	Status string `json:"status" binding:"required"`
}

// PatchGap 更新知识盲区状态（resolved / ignored / open）。
func (h *Handler) PatchGap(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "盲区 id 必须为正整数"})
		return
	}
	var req patchGapRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Status) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "status 不能为空，仅支持 open、resolved、ignored"})
		return
	}
	if h.Store == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Store 未初始化"})
		return
	}
	gap, err := h.Store.UpdateKnowledgeGapStatus(id, req.Status)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrGapNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		case errors.Is(err, store.ErrInvalidGapStatus):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	c.JSON(http.StatusOK, gap)
}

type createFeedbackRequest struct {
	ProjectSlug       string  `json:"project_slug"`
	Question          string  `json:"question" binding:"required"`
	RetrievedChunkIDs []int64 `json:"retrieved_chunk_ids"`
	Answer            string  `json:"answer"`
	Refused           bool    `json:"refused"`
	Vote              string  `json:"vote" binding:"required"`
	Comment           string  `json:"comment"`
}

// CreateFeedback 保存用户对问答卡片的 👍/👎 反馈。
func (h *Handler) CreateFeedback(c *gin.Context) {
	var req createFeedbackRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Question) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "question 与 vote 不能为空"})
		return
	}
	vote := strings.ToLower(strings.TrimSpace(req.Vote))
	if vote != "up" && vote != "down" {
		c.JSON(http.StatusBadRequest, gin.H{"error": store.ErrInvalidFeedbackVote.Error()})
		return
	}
	if h.Store == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Store 未初始化"})
		return
	}
	fb, err := h.Store.CreateFeedback(store.CreateFeedbackParams{
		ProjectSlug:       req.ProjectSlug,
		Question:          req.Question,
		RetrievedChunkIDs: req.RetrievedChunkIDs,
		Answer:            req.Answer,
		Refused:           req.Refused,
		Vote:              vote,
		Comment:           req.Comment,
	})
	if err != nil {
		if errors.Is(err, store.ErrInvalidFeedbackVote) || strings.Contains(err.Error(), "不能") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, fb)
}

// ListFeedback 列出待审与历史用户反馈队列（未转正与 vote='down' 优先）。
func (h *Handler) ListFeedback(c *gin.Context) {
	if h.Store == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Store 未初始化"})
		return
	}
	voteFilter := c.Query("vote")
	promotedFilter := c.Query("promoted")
	list, err := h.Store.ListFeedback(voteFilter, promotedFilter)
	if err != nil {
		if errors.Is(err, store.ErrInvalidFeedbackVote) || errors.Is(err, store.ErrInvalidFeedbackFilter) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"feedback": list})
}

type promoteFeedbackRequest struct {
	GoldAnswer   string `json:"gold_answer"`
	ShouldRefuse bool   `json:"should_refuse"`
	GoldChunkID  *int64 `json:"gold_chunk_id"`
	Category     string `json:"category"`
}

// PromoteFeedback 审阅转正反馈为 eval_questions（question_set='feedback'）并回填 promoted_question_id。
func (h *Handler) PromoteFeedback(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "反馈 id 必须为正整数"})
		return
	}
	var req promoteFeedbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求 JSON 格式无效"})
		return
	}
	if h.Store == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Store 未初始化"})
		return
	}
	res, err := h.Store.PromoteFeedback(id, store.PromoteFeedbackParams{
		GoldAnswer:   req.GoldAnswer,
		ShouldRefuse: req.ShouldRefuse,
		GoldChunkID:  req.GoldChunkID,
		Category:     req.Category,
	})
	if err != nil {
		switch {
		case errors.Is(err, store.ErrFeedbackNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		case errors.Is(err, store.ErrFeedbackAlreadyPromoted), errors.Is(err, store.ErrDuplicateEvalQuestion):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		case errors.Is(err, store.ErrGoldChunkNotFound) ||
			strings.Contains(err.Error(), "不能为空") ||
			strings.Contains(err.Error(), "不能指定") ||
			strings.Contains(err.Error(), "必须指定"):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	c.JSON(http.StatusOK, res)
}
