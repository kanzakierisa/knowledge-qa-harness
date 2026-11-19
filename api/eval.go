package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"knowledge-qa/eval"
	"knowledge-qa/store"
)

// evalHTTPHandler 把内部问答函数注入评测器，不通过 HTTP 自调用 /api/ask。
type evalHTTPHandler struct {
	evaluator *eval.Evaluator
	parent    *Handler
}

func newEvalHTTPHandler(h *Handler) *evalHTTPHandler {
	ask := func(question, projectSlug string) (eval.Answer, error) {
		resp, top, err := h.answerQuestionWithOptions(question, askOptions{
			Debug:       false,
			ProjectSlug: projectSlug,
			RecordGap:   false,
		})
		ids := make([]int64, 0, len(top))
		for _, chunk := range top {
			ids = append(ids, chunk.Chunk.ID)
		}
		return eval.Answer{
			Text:              resp.Answer,
			Refused:           resp.Refused,
			Retried:           resp.Retried,
			RetrievedChunkIDs: ids,
			CitationLimit:     len(top),
		}, err
	}
	return &evalHTTPHandler{evaluator: eval.NewWithProject(h.Store, ask), parent: h}
}

// Run 执行指定评测集（默认 base52，支持 ?set=base52|holdout|feedback|all）；并发批次直接返回 409。
func (h *evalHTTPHandler) Run(c *gin.Context) {
	if !h.parent.evalMu.TryLock() {
		c.JSON(http.StatusConflict, gin.H{"error": eval.ErrConcurrent.Error()})
		return
	}
	defer h.parent.evalMu.Unlock()

	set := strings.ToLower(strings.TrimSpace(c.DefaultQuery("set", "base52")))
	switch set {
	case "base52", "holdout", "feedback", "all":
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": store.ErrInvalidEvalSet.Error()})
		return
	}

	report, err := h.evaluator.RunSet(set)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, eval.ErrConcurrent):
			status = http.StatusConflict
		case errors.Is(err, store.ErrInvalidEvalSet), errors.Is(err, eval.ErrEmptyEvalSet):
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, report)
}

type markRequest struct {
	RunID  int64 `json:"run_id" binding:"required"`
	Usable *bool `json:"usable" binding:"required"`
}

// Mark 只接收人工可用性结论，不在评测代码里自动判断 usable。
func (h *evalHTTPHandler) Mark(c *gin.Context) {
	var req markRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.RunID <= 0 || req.Usable == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "run_id 必须为正整数，usable 必须为 true 或 false"})
		return
	}
	result, err := h.evaluator.Mark(req.RunID, *req.Usable)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrEvalRunNotFound) {
			status = http.StatusNotFound
		} else if errors.Is(err, store.ErrEvalRunRefused) {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}
