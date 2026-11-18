package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"knowledge-qa/ingest"
	"knowledge-qa/store"
)

const maxUploadBytes = 32 << 20 // 单次上传文件上限 32MB

func (h *Handler) converter() ingest.Converter {
	if h.Converter != nil {
		return h.Converter
	}
	return ingest.NewConverterFromEnv()
}

// ListProjects 查询全部项目空间。
func (h *Handler) ListProjects(c *gin.Context) {
	projects, err := h.Store.ListProjects()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"projects": projects})
}

type createProjectRequest struct {
	Slug        string `json:"slug" binding:"required"`
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
}

// CreateProject 创建新项目空间，严格校验 slug 格式。
func (h *Handler) CreateProject(c *gin.Context) {
	var req createProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "slug 与 name 不能为空"})
		return
	}
	p, err := h.Store.CreateProject(req.Slug, req.Name, req.Description)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrInvalidProjectSlug):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, store.ErrProjectExists):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		default:
			if strings.Contains(err.Error(), "不能为空") {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	c.JSON(http.StatusCreated, p)
}

// ListDocuments 列出文档（支持 ?project_slug=&status= 过滤）。
func (h *Handler) ListDocuments(c *gin.Context) {
	projectSlug := c.Query("project_slug")
	status := c.Query("status")
	docs, err := h.Store.ListDocuments(projectSlug, status)
	if err != nil {
		if errors.Is(err, store.ErrInvalidDocStatus) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"documents": docs})
}

type uploadJSONRequest struct {
	ProjectSlug string `json:"project_slug"`
	Filename    string `json:"filename"`
	Title       string `json:"title"`
	Content     string `json:"content"`
	DocLevel    string `json:"doc_level"`
	Owner       string `json:"owner"`
	Overwrite   bool   `json:"overwrite"`
}

type parsedUploadInput struct {
	projectSlug string
	filename    string
	title       string
	docLevel    string
	owner       string
	overwrite   bool
	raw         []byte
}

func parseUploadRequest(c *gin.Context) (parsedUploadInput, error) {
	ct := strings.ToLower(strings.TrimSpace(c.GetHeader("Content-Type")))
	if strings.HasPrefix(ct, "multipart/form-data") {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadBytes+1024*1024)
		fh, err := c.FormFile("file")
		if err != nil {
			return parsedUploadInput{}, errors.New("缺少上传文件字段 file")
		}
		if fh.Size > maxUploadBytes {
			return parsedUploadInput{}, errors.New("文件体积超过 32MB 上限")
		}
		f, err := fh.Open()
		if err != nil {
			return parsedUploadInput{}, err
		}
		defer f.Close()
		raw, err := io.ReadAll(io.LimitReader(f, maxUploadBytes+1))
		if err != nil {
			return parsedUploadInput{}, err
		}
		if len(raw) > maxUploadBytes {
			return parsedUploadInput{}, errors.New("文件体积超过 32MB 上限")
		}
		filename := sanitizeFilename(fh.Filename)
		if filename == "" {
			return parsedUploadInput{}, errors.New("上传文件名无效")
		}
		return parsedUploadInput{
			projectSlug: strings.TrimSpace(c.PostForm("project_slug")),
			filename:    filename,
			title:       strings.TrimSpace(c.PostForm("title")),
			docLevel:    strings.TrimSpace(c.PostForm("doc_level")),
			owner:       strings.TrimSpace(c.PostForm("owner")),
			overwrite:   parseBoolString(c.PostForm("overwrite")),
			raw:         raw,
		}, nil
	}

	var req uploadJSONRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		return parsedUploadInput{}, errors.New("请提供 multipart/form-data 文件或合法 JSON（含 filename 与 content）")
	}
	if strings.TrimSpace(req.Content) == "" {
		return parsedUploadInput{}, errors.New("content 不能为空")
	}
	filename := sanitizeFilename(req.Filename)
	if filename == "" {
		titleBase := sanitizeFilename(req.Title)
		if titleBase == "" {
			return parsedUploadInput{}, errors.New("filename 不能为空")
		}
		filename = titleBase + ".md"
	}
	if filepath.Ext(filename) == "" {
		filename += ".md"
	}
	return parsedUploadInput{
		projectSlug: strings.TrimSpace(req.ProjectSlug),
		filename:    filename,
		title:       strings.TrimSpace(req.Title),
		docLevel:    strings.TrimSpace(req.DocLevel),
		owner:       strings.TrimSpace(req.Owner),
		overwrite:   req.Overwrite,
		raw:         []byte(req.Content),
	}, nil
}

func sanitizeFilename(name string) string {
	// 兼容 Windows / POSIX 路径分隔符，只保留纯文件名
	name = strings.ReplaceAll(strings.TrimSpace(name), "\\", "/")
	base := filepath.Base(name)
	if base == "." || base == "/" || base == "" {
		return ""
	}
	return base
}

func parseBoolString(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// UploadDocument 多格式文档热入库（multipart/form-data 或 JSON）。
func (h *Handler) UploadDocument(c *gin.Context) {
	in, err := parseUploadRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if in.projectSlug == "" {
		in.projectSlug = store.DefaultProjectSlug
	}
	level, err := store.NormalizeDocLevel(in.docLevel)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	contentHash := store.SHA256Hex(in.raw)

	// 1. 优先防重检查（未传 overwrite 时若同项目同名同哈希直接跳过，避免浪费 Embedding 配额）
	if !in.overwrite {
		existingID, chunkCount, isDup, err := h.Store.CheckDuplicateDocument(in.projectSlug, in.filename, contentHash)
		if err != nil {
			if errors.Is(err, store.ErrProjectNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if isDup {
			c.JSON(http.StatusOK, store.InsertDocResult{
				DocumentID:  existingID,
				Filename:    in.filename,
				ProjectSlug: in.projectSlug,
				ChunkCount:  chunkCount,
				Skipped:     true,
				Message:     "文档内容未变更，已跳过重复导入",
				Warnings:    []string{},
			})
			return
		}
	} else {
		if _, err := h.Store.GetProjectBySlug(in.projectSlug); err != nil {
			if errors.Is(err, store.ErrProjectNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	// 2. 格式转标准 Markdown（遇扫描件/加密/乱码/不支持格式直接报 400）
	markdown, sourceType, err := h.converter().Convert(in.filename, in.raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 3. v3 切片（表格/代码块保护 + 句末拆分与 80 字符 Overlap）
	pieces := ingest.SplitMarkdownV3(markdown)
	if len(pieces) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文档未切出有效段落，已拒绝入库"})
		return
	}

	if h.Embedder == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Embedder 未初始化"})
		return
	}

	// 4. 逐片计算向量（全部成功后再开启 DB 事务，避免半截脏数据）
	chunkInputs := make([]store.ChunkInput, 0, len(pieces))
	for _, p := range pieces {
		embText := p.Content
		if p.HeadingPath != "" {
			embText = p.HeadingPath + "\n" + p.Content
		}
		vec, err := h.Embedder.Embed(embText)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "生成切片向量失败：" + err.Error()})
			return
		}
		vecJSON, err := json.Marshal(vec)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		chunkInputs = append(chunkInputs, store.ChunkInput{
			HeadingPath: p.HeadingPath,
			Content:     p.Content,
			Embedding:   string(vecJSON),
		})
	}

	// 5. 事务入库
	result, err := h.Store.InsertOrReplaceDocument(store.InsertDocParams{
		ProjectSlug: in.projectSlug,
		Title:       in.title,
		Filename:    in.filename,
		Content:     markdown,
		SourceType:  sourceType,
		ContentHash: contentHash,
		DocLevel:    level,
		Owner:       in.owner,
		Overwrite:   in.overwrite,
		Chunks:      chunkInputs,
	})
	if err != nil {
		if errors.Is(err, store.ErrProjectNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

type optionalInt64 struct {
	set bool
	val *int64
}

func (o *optionalInt64) UnmarshalJSON(data []byte) error {
	o.set = true
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "null" {
		o.val = nil
		return nil
	}
	var n int64
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	if n <= 0 {
		o.val = nil
		return nil
	}
	o.val = &n
	return nil
}

type patchDocumentRequest struct {
	Status          *string       `json:"status"`
	DocLevel        *string       `json:"doc_level"`
	Owner           *string       `json:"owner"`
	ReplacedByDocID optionalInt64 `json:"replaced_by_doc_id"`
}

// PatchDocument 修改文档治理状态、层级、负责人或替代文档指向。
func (h *Handler) PatchDocument(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文档 id 必须为正整数"})
		return
	}
	var req patchDocumentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求 JSON 格式无效"})
		return
	}
	doc, err := h.Store.UpdateDocumentMeta(id, store.DocumentPatch{
		Status:             req.Status,
		DocLevel:           req.DocLevel,
		Owner:              req.Owner,
		SetReplacedByDocID: req.ReplacedByDocID.set,
		ReplacedByDocID:    req.ReplacedByDocID.val,
	})
	if err != nil {
		switch {
		case errors.Is(err, store.ErrDocumentNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		case errors.Is(err, store.ErrInvalidDocStatus), errors.Is(err, store.ErrInvalidDocLevel), errors.Is(err, store.ErrInvalidReplacedBy):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	c.JSON(http.StatusOK, doc)
}

// DeleteDocument 删除指定文档及其切片，返回受影响金标提醒。
func (h *Handler) DeleteDocument(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文档 id 必须为正整数"})
		return
	}
	res, err := h.Store.DeleteDocument(id)
	if err != nil {
		if errors.Is(err, store.ErrDocumentNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}
