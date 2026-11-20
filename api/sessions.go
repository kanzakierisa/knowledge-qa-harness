package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"knowledge-qa/store"
)

// ListSessions 列出持久化会话任务（置顶优先，按更新时间倒序）。
func (h *Handler) ListSessions(c *gin.Context) {
	if h.Store == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Store 未初始化"})
		return
	}
	sessions, err := h.Store.ListSessions()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"sessions": sessions})
}

// GetSession 查询单条会话任务的全部问答轮次与口径 B 客观统计。
func (h *Handler) GetSession(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "会话 id 必须为正整数"})
		return
	}
	if h.Store == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Store 未初始化"})
		return
	}
	detail, err := h.Store.GetSessionDetail(id)
	if err != nil {
		if errors.Is(err, store.ErrSessionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, detail)
}

type patchSessionRequest struct {
	Title    *string `json:"title"`
	IsPinned *bool   `json:"is_pinned"`
}

// PatchSession 支持重命名或置顶/取消置顶会话任务。
func (h *Handler) PatchSession(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "会话 id 必须为正整数"})
		return
	}
	var req patchSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求 JSON 格式无效"})
		return
	}
	if req.Title != nil && strings.TrimSpace(*req.Title) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "会话标题不能为空"})
		return
	}
	if h.Store == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Store 未初始化"})
		return
	}
	sess, err := h.Store.UpdateSession(id, store.SessionPatch{
		Title:    req.Title,
		IsPinned: req.IsPinned,
	})
	if err != nil {
		if errors.Is(err, store.ErrSessionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if strings.Contains(err.Error(), "不能为空") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, sess)
}

// DeleteSession 删除会话任务及其关联问答轮次。
func (h *Handler) DeleteSession(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "会话 id 必须为正整数"})
		return
	}
	if h.Store == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Store 未初始化"})
		return
	}
	if err := h.Store.DeleteSession(id); err != nil {
		if errors.Is(err, store.ErrSessionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true, "session_id": id})
}

// OnlineStats 从 MySQL 聚合返回全量在线问答的口径 B 客观统计（与金标评测隔离）。
func (h *Handler) OnlineStats(c *gin.Context) {
	if h.Store == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Store 未初始化"})
		return
	}
	stats, err := h.Store.GetOnlineStats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stats)
}
