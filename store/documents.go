package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/go-sql-driver/mysql"
)

var (
	ErrProjectNotFound    = errors.New("项目空间不存在")
	ErrProjectExists      = errors.New("项目 slug 已存在")
	ErrInvalidProjectSlug = errors.New("项目 slug 仅允许小写字母、数字和连字符，且不能为保留字 auto 或 all")
	ErrDocumentNotFound   = errors.New("文档不存在")
	ErrInvalidDocLevel    = errors.New("doc_level 仅支持 spec、rfc、note")
	ErrInvalidDocStatus   = errors.New("status 仅支持 active、deprecated")
	ErrInvalidReplacedBy  = errors.New("替代文档不存在或不能指向自身")
)

const DefaultProjectSlug = "go-gin"

var projectSlugRe = regexp.MustCompile(`^[a-z0-9-]+$`)

// Project 表示一个独立项目空间。
type Project struct {
	ID          int64     `json:"id"`
	Slug        string    `json:"slug"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	DocCount    int       `json:"doc_count"`
	ChunkCount  int       `json:"chunk_count"`
	CreatedAt   time.Time `json:"created_at"`
}

// Document 表示带项目归属与治理元数据的文档详情。
type Document struct {
	ID              int64     `json:"id"`
	ProjectID       int64     `json:"project_id"`
	ProjectSlug     string    `json:"project_slug"`
	ProjectName     string    `json:"project_name"`
	Title           string    `json:"title"`
	Filename        string    `json:"filename"`
	SourceType      string    `json:"source_type"`
	ContentHash     string    `json:"content_hash"`
	DocLevel        string    `json:"doc_level"`
	Status          string    `json:"status"`
	ReplacedByDocID *int64    `json:"replaced_by_doc_id"`
	Owner           string    `json:"owner"`
	ChunkCount      int       `json:"chunk_count"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// ChunkInput 描述待事务写入的单个切片。
type ChunkInput struct {
	HeadingPath string
	Content     string
	Embedding   string
}

// InsertDocParams 描述文档热入库请求。
type InsertDocParams struct {
	ProjectSlug string
	Title       string
	Filename    string
	Content     string
	SourceType  string
	ContentHash string
	DocLevel    string
	Owner       string
	Overwrite   bool
	Chunks      []ChunkInput
}

// AffectedEvalItem 描述覆写文档时受影响的金标题及其切片重绑状态（供 Doc CI/CD 局部回归使用）。
type AffectedEvalItem struct {
	Question       EvalQuestion
	OldGoldChunkID *int64
	NewGoldChunkID *int64
	NeedsReview    bool
	ReviewReason   string
}

// InsertDocResult 返回热入库事务结果。
type InsertDocResult struct {
	DocumentID            int64              `json:"document_id"`
	Filename              string             `json:"filename"`
	ProjectSlug           string             `json:"project_slug"`
	SourceType            string             `json:"source_type"`
	ChunkCount            int                `json:"chunk_count"`
	Skipped               bool               `json:"skipped"`
	Replaced              bool               `json:"replaced"`
	AffectedEvalQuestions int                `json:"affected_eval_questions,omitempty"`
	AffectedItems         []AffectedEvalItem `json:"-"`
	Message               string             `json:"message,omitempty"`
	Warnings              []string           `json:"warnings"`
}

// DocumentPatch 描述文档元数据增量更新。
type DocumentPatch struct {
	Status             *string
	DocLevel           *string
	Owner              *string
	SetReplacedByDocID bool
	ReplacedByDocID    *int64
}

// DeleteDocResult 返回文档事务删除结果与关联金标提醒。
type DeleteDocResult struct {
	Deleted               bool     `json:"deleted"`
	DocumentID            int64    `json:"document_id"`
	ChunksDeleted         int64    `json:"chunks_deleted"`
	AffectedEvalQuestions int      `json:"affected_eval_questions"`
	Warnings              []string `json:"warnings"`
}

// SHA256Hex 计算字节切片的 64 位小写十六进制摘要。
func SHA256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// ValidateProjectSlug 校验项目 slug 仅允许小写字母、数字和连字符。
func ValidateProjectSlug(slug string) error {
	if slug == "" || len(slug) > 64 || !projectSlugRe.MatchString(slug) || strings.Trim(slug, "-") == "" {
		return ErrInvalidProjectSlug
	}
	if slug == "auto" || slug == "all" {
		return ErrInvalidProjectSlug
	}
	return nil
}

// NormalizeDocLevel 校验并规范化 doc_level，空串默认 spec。
func NormalizeDocLevel(level string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(level))
	if v == "" {
		return "spec", nil
	}
	switch v {
	case "spec", "rfc", "note":
		return v, nil
	default:
		return "", ErrInvalidDocLevel
	}
}

// ValidateDocStatus 校验文档状态是否合法。
func ValidateDocStatus(status string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(status))
	switch v {
	case "active", "deprecated":
		return v, nil
	default:
		return "", ErrInvalidDocStatus
	}
}

// ListProjects 返回所有项目空间及其文档/切片计数。
func (s *Store) ListProjects() ([]Project, error) {
	rows, err := s.db.Query(`
		SELECT p.id, p.slug, p.name, p.description, p.created_at,
		       COUNT(DISTINCT d.id) AS doc_count,
		       COUNT(c.id) AS chunk_count
		FROM projects p
		LEFT JOIN documents d ON d.project_id = p.id
		LEFT JOIN chunks c ON c.document_id = d.id
		GROUP BY p.id, p.slug, p.name, p.description, p.created_at
		ORDER BY p.id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Project, 0)
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Slug, &p.Name, &p.Description, &p.CreatedAt, &p.DocCount, &p.ChunkCount); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetProjectBySlug 按 slug 查询项目空间。
func (s *Store) GetProjectBySlug(slug string) (Project, error) {
	var p Project
	err := s.db.QueryRow(`
		SELECT p.id, p.slug, p.name, p.description, p.created_at,
		       COUNT(DISTINCT d.id) AS doc_count,
		       COUNT(c.id) AS chunk_count
		FROM projects p
		LEFT JOIN documents d ON d.project_id = p.id
		LEFT JOIN chunks c ON c.document_id = d.id
		WHERE p.slug = ?
		GROUP BY p.id, p.slug, p.name, p.description, p.created_at`, slug).
		Scan(&p.ID, &p.Slug, &p.Name, &p.Description, &p.CreatedAt, &p.DocCount, &p.ChunkCount)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrProjectNotFound
	}
	if err != nil {
		return Project{}, err
	}
	return p, nil
}

// CreateProject 创建新的项目空间。
func (s *Store) CreateProject(slug, name, description string) (Project, error) {
	slug = strings.TrimSpace(slug)
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if err := ValidateProjectSlug(slug); err != nil {
		return Project{}, err
	}
	if name == "" {
		return Project{}, errors.New("项目名称不能为空")
	}

	res, err := s.db.Exec(
		"INSERT INTO projects (slug, name, description) VALUES (?, ?, ?)",
		slug, name, description,
	)
	if err != nil {
		var me *mysql.MySQLError
		if errors.As(err, &me) && me.Number == 1062 {
			return Project{}, ErrProjectExists
		}
		return Project{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Project{}, err
	}
	return Project{
		ID:          id,
		Slug:        slug,
		Name:        name,
		Description: description,
		CreatedAt:   time.Now(),
	}, nil
}

// ListDocuments 按可选项目 slug 与状态列出文档及切片数。
func (s *Store) ListDocuments(projectSlug, status string) ([]Document, error) {
	var conds []string
	var args []any

	projectSlug = strings.TrimSpace(projectSlug)
	if projectSlug != "" && projectSlug != "auto" && projectSlug != "all" {
		conds = append(conds, "p.slug = ?")
		args = append(args, projectSlug)
	}
	status = strings.TrimSpace(status)
	if status != "" {
		st, err := ValidateDocStatus(status)
		if err != nil {
			return nil, err
		}
		conds = append(conds, "d.status = ?")
		args = append(args, st)
	}

	whereClause := ""
	if len(conds) > 0 {
		whereClause = "WHERE " + strings.Join(conds, " AND ")
	}

	query := fmt.Sprintf(`
		SELECT d.id, d.project_id, p.slug, p.name, d.title, d.filename, d.source_type,
		       d.content_hash, d.doc_level, d.status, d.replaced_by_doc_id, d.owner,
		       d.created_at, d.updated_at, COUNT(c.id) AS chunk_count
		FROM documents d
		JOIN projects p ON p.id = d.project_id
		LEFT JOIN chunks c ON c.document_id = d.id
		%s
		GROUP BY d.id, d.project_id, p.slug, p.name, d.title, d.filename, d.source_type,
		         d.content_hash, d.doc_level, d.status, d.replaced_by_doc_id, d.owner,
		         d.created_at, d.updated_at
		ORDER BY d.updated_at DESC, d.id DESC`, whereClause)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Document, 0)
	for rows.Next() {
		d, err := scanDocumentRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DocumentChunkPreview 描述文档详情中的单条切片预览。
type DocumentChunkPreview struct {
	ID          int64  `json:"id"`
	HeadingPath string `json:"heading_path"`
	Content     string `json:"content"`
}

// DocumentDetail 返回文档完整元数据、原文内容与有序切片列表。
type DocumentDetail struct {
	Document
	Content string                 `json:"content"`
	Chunks  []DocumentChunkPreview `json:"chunks"`
}

// GetDocument 查询单篇文档详情。
func (s *Store) GetDocument(id int64) (Document, error) {
	row := s.db.QueryRow(`
		SELECT d.id, d.project_id, p.slug, p.name, d.title, d.filename, d.source_type,
		       d.content_hash, d.doc_level, d.status, d.replaced_by_doc_id, d.owner,
		       d.created_at, d.updated_at, COUNT(c.id) AS chunk_count
		FROM documents d
		JOIN projects p ON p.id = d.project_id
		LEFT JOIN chunks c ON c.document_id = d.id
		WHERE d.id = ?
		GROUP BY d.id, d.project_id, p.slug, p.name, d.title, d.filename, d.source_type,
		         d.content_hash, d.doc_level, d.status, d.replaced_by_doc_id, d.owner,
		         d.created_at, d.updated_at`, id)
	var d Document
	var replaced sql.NullInt64
	err := row.Scan(&d.ID, &d.ProjectID, &d.ProjectSlug, &d.ProjectName, &d.Title, &d.Filename, &d.SourceType,
		&d.ContentHash, &d.DocLevel, &d.Status, &replaced, &d.Owner, &d.CreatedAt, &d.UpdatedAt, &d.ChunkCount)
	if errors.Is(err, sql.ErrNoRows) {
		return Document{}, ErrDocumentNotFound
	}
	if err != nil {
		return Document{}, err
	}
	if replaced.Valid {
		d.ReplacedByDocID = &replaced.Int64
	}
	return d, nil
}

// GetDocumentDetail 查询单篇文档元数据、原文内容及其有序切片列表。
func (s *Store) GetDocumentDetail(id int64) (DocumentDetail, error) {
	var detail DocumentDetail
	var replaced sql.NullInt64
	err := s.db.QueryRow(`
		SELECT d.id, d.project_id, p.slug, p.name, d.title, d.filename, d.source_type,
		       d.content_hash, d.doc_level, d.status, d.replaced_by_doc_id, d.owner,
		       d.created_at, d.updated_at, COALESCE(d.content, '')
		FROM documents d
		JOIN projects p ON p.id = d.project_id
		WHERE d.id = ?`, id).
		Scan(&detail.ID, &detail.ProjectID, &detail.ProjectSlug, &detail.ProjectName, &detail.Title,
			&detail.Filename, &detail.SourceType, &detail.ContentHash, &detail.DocLevel, &detail.Status,
			&replaced, &detail.Owner, &detail.CreatedAt, &detail.UpdatedAt, &detail.Content)
	if errors.Is(err, sql.ErrNoRows) {
		return DocumentDetail{}, ErrDocumentNotFound
	}
	if err != nil {
		return DocumentDetail{}, err
	}
	if replaced.Valid {
		detail.ReplacedByDocID = &replaced.Int64
	}

	rows, err := s.db.Query(`
		SELECT id, heading_path, content
		FROM chunks
		WHERE document_id = ?
		ORDER BY id ASC`, id)
	if err != nil {
		return DocumentDetail{}, err
	}
	defer rows.Close()

	chunks := make([]DocumentChunkPreview, 0)
	for rows.Next() {
		var cp DocumentChunkPreview
		if err := rows.Scan(&cp.ID, &cp.HeadingPath, &cp.Content); err != nil {
			return DocumentDetail{}, err
		}
		chunks = append(chunks, cp)
	}
	if err := rows.Err(); err != nil {
		return DocumentDetail{}, err
	}
	detail.Chunks = chunks
	detail.ChunkCount = len(chunks)
	return detail, nil
}

func scanDocumentRow(rows *sql.Rows) (Document, error) {
	var d Document
	var replaced sql.NullInt64
	err := rows.Scan(&d.ID, &d.ProjectID, &d.ProjectSlug, &d.ProjectName, &d.Title, &d.Filename, &d.SourceType,
		&d.ContentHash, &d.DocLevel, &d.Status, &replaced, &d.Owner, &d.CreatedAt, &d.UpdatedAt, &d.ChunkCount)
	if err != nil {
		return Document{}, err
	}
	if replaced.Valid {
		d.ReplacedByDocID = &replaced.Int64
	}
	return d, nil
}

// CheckDuplicateDocument 在调用 Embedding 前快速校验项目是否存在以及同项目同文件是否内容未变更。
func (s *Store) CheckDuplicateDocument(projectSlug, filename, contentHash string) (existingID int64, chunkCount int, isDup bool, err error) {
	slug := strings.TrimSpace(projectSlug)
	if slug == "" {
		slug = DefaultProjectSlug
	}
	var projectID int64
	err = s.db.QueryRow("SELECT id FROM projects WHERE slug = ?", slug).Scan(&projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, false, ErrProjectNotFound
	}
	if err != nil {
		return 0, 0, false, err
	}

	var existingHash string
	err = s.db.QueryRow(
		"SELECT id, content_hash FROM documents WHERE project_id = ? AND filename = ? ORDER BY id DESC LIMIT 1",
		projectID, filename,
	).Scan(&existingID, &existingHash)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, err
	}

	if contentHash != "" && existingHash == contentHash {
		if err := s.db.QueryRow("SELECT COUNT(*) FROM chunks WHERE document_id = ?", existingID).Scan(&chunkCount); err != nil {
			return 0, 0, false, err
		}
		return existingID, chunkCount, true, nil
	}
	return existingID, 0, false, nil
}

// InsertOrReplaceDocument 在单事务内完成防重跳过、覆盖更新或新建文档与切片写入。
func (s *Store) InsertOrReplaceDocument(params InsertDocParams) (InsertDocResult, error) {
	slug := strings.TrimSpace(params.ProjectSlug)
	if slug == "" {
		slug = DefaultProjectSlug
	}
	level, err := NormalizeDocLevel(params.DocLevel)
	if err != nil {
		return InsertDocResult{}, err
	}
	if strings.TrimSpace(params.Filename) == "" {
		return InsertDocResult{}, errors.New("文件名不能为空")
	}
	title := strings.TrimSpace(params.Title)
	if title == "" {
		title = strings.TrimSuffix(params.Filename, extWithDot(params.Filename))
	}
	sourceType := strings.TrimSpace(params.SourceType)
	if sourceType == "" {
		sourceType = "md"
	}
	contentHash := strings.TrimSpace(params.ContentHash)
	if contentHash == "" {
		contentHash = SHA256Hex([]byte(params.Content))
	}
	owner := strings.TrimSpace(params.Owner)

	tx, err := s.db.Begin()
	if err != nil {
		return InsertDocResult{}, err
	}
	defer tx.Rollback()

	var projectID int64
	err = tx.QueryRow("SELECT id FROM projects WHERE slug = ?", slug).Scan(&projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return InsertDocResult{}, ErrProjectNotFound
	}
	if err != nil {
		return InsertDocResult{}, err
	}

	var existingID int64
	var existingHash string
	err = tx.QueryRow(
		"SELECT id, content_hash FROM documents WHERE project_id = ? AND filename = ? ORDER BY id DESC LIMIT 1 FOR UPDATE",
		projectID, params.Filename,
	).Scan(&existingID, &existingHash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return InsertDocResult{}, err
	}

	if err == nil {
		if !params.Overwrite && existingHash == contentHash {
			var chunkCount int
			if err := tx.QueryRow("SELECT COUNT(*) FROM chunks WHERE document_id = ?", existingID).Scan(&chunkCount); err != nil {
				return InsertDocResult{}, err
			}
			if err := tx.Commit(); err != nil {
				return InsertDocResult{}, err
			}
			return InsertDocResult{
				DocumentID:  existingID,
				Filename:    params.Filename,
				ProjectSlug: slug,
				SourceType:  sourceType,
				ChunkCount:  chunkCount,
				Skipped:     true,
				Message:     "文档内容未变更，已跳过重复导入",
				Warnings:    []string{},
			}, nil
		}

		if len(params.Chunks) == 0 {
			return InsertDocResult{}, errors.New("文档切片不能为空")
		}
		// Doc CI/CD 第 1 步：必须在删除旧切片之前捕获受影响金标题及旧切片原文定位信息
		oldChunksByID, err := loadDocChunksForUpdateTx(tx, existingID)
		if err != nil {
			return InsertDocResult{}, err
		}
		affectedQuestions, err := loadAffectedEvalQuestionsTx(tx, existingID)
		if err != nil {
			return InsertDocResult{}, err
		}
		if _, err := tx.Exec("DELETE FROM chunks WHERE document_id = ?", existingID); err != nil {
			return InsertDocResult{}, fmt.Errorf("删除旧切片失败：%w", err)
		}
		if _, err := tx.Exec(`UPDATE documents
			SET title = ?, content = ?, source_type = ?, content_hash = ?, doc_level = ?, status = 'active', replaced_by_doc_id = NULL, owner = ?
			WHERE id = ?`,
			title, params.Content, sourceType, contentHash, level, owner, existingID); err != nil {
			return InsertDocResult{}, fmt.Errorf("更新文档失败：%w", err)
		}
		newChunks, err := insertChunksTxWithIDs(tx, existingID, params.Chunks)
		if err != nil {
			return InsertDocResult{}, err
		}
		affectedItems, err := rebindAffectedEvalQuestionsTx(tx, existingID, affectedQuestions, oldChunksByID, newChunks)
		if err != nil {
			return InsertDocResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return InsertDocResult{}, err
		}
		s.invalidateVecCache()

		affectedEval := len(affectedItems)
		warnings := []string{}
		if affectedEval > 0 {
			warnings = append(warnings, fmt.Sprintf("该文档关联了 %d 条金标评测题，已执行重绑与局部回归", affectedEval))
		}
		return InsertDocResult{
			DocumentID:            existingID,
			Filename:              params.Filename,
			ProjectSlug:           slug,
			SourceType:            sourceType,
			ChunkCount:            len(params.Chunks),
			Skipped:               false,
			Replaced:              true,
			AffectedEvalQuestions: affectedEval,
			AffectedItems:         affectedItems,
			Message:               "文档已覆盖更新并重新切片入库",
			Warnings:              warnings,
		}, nil
	}

	if len(params.Chunks) == 0 {
		return InsertDocResult{}, errors.New("文档切片不能为空")
	}
	res, err := tx.Exec(`INSERT INTO documents
		(project_id, title, filename, content, source_type, content_hash, doc_level, status, owner)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'active', ?)`,
		projectID, title, params.Filename, params.Content, sourceType, contentHash, level, owner)
	if err != nil {
		return InsertDocResult{}, fmt.Errorf("写入文档失败：%w", err)
	}
	docID, err := res.LastInsertId()
	if err != nil {
		return InsertDocResult{}, err
	}
	if err := insertChunksTx(tx, docID, params.Chunks); err != nil {
		return InsertDocResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return InsertDocResult{}, err
	}
	s.invalidateVecCache()

	return InsertDocResult{
		DocumentID:  docID,
		Filename:    params.Filename,
		ProjectSlug: slug,
		SourceType:  sourceType,
		ChunkCount:  len(params.Chunks),
		Skipped:     false,
		Message:     "文档已成功切片并入库",
		Warnings:    []string{},
	}, nil
}

type chunkSnapshot struct {
	ID          int64
	HeadingPath string
	Content     string
}

func insertChunksTx(tx *sql.Tx, documentID int64, chunks []ChunkInput) error {
	_, err := insertChunksTxWithIDs(tx, documentID, chunks)
	return err
}

func insertChunksTxWithIDs(tx *sql.Tx, documentID int64, chunks []ChunkInput) ([]chunkSnapshot, error) {
	stmt, err := tx.Prepare("INSERT INTO chunks (document_id, heading_path, content, embedding) VALUES (?, ?, ?, ?)")
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	out := make([]chunkSnapshot, 0, len(chunks))
	for _, ch := range chunks {
		res, err := stmt.Exec(documentID, ch.HeadingPath, ch.Content, ch.Embedding)
		if err != nil {
			return nil, fmt.Errorf("写入切片失败：%w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return nil, err
		}
		out = append(out, chunkSnapshot{
			ID:          id,
			HeadingPath: ch.HeadingPath,
			Content:     ch.Content,
		})
	}
	return out, nil
}

func loadDocChunksForUpdateTx(tx *sql.Tx, docID int64) (map[int64]chunkSnapshot, error) {
	rows, err := tx.Query("SELECT id, heading_path, content FROM chunks WHERE document_id = ? FOR UPDATE", docID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]chunkSnapshot)
	for rows.Next() {
		var cs chunkSnapshot
		if err := rows.Scan(&cs.ID, &cs.HeadingPath, &cs.Content); err != nil {
			return nil, err
		}
		out[cs.ID] = cs
	}
	return out, rows.Err()
}

func loadAffectedEvalQuestionsTx(tx *sql.Tx, docID int64) ([]EvalQuestion, error) {
	rows, err := tx.Query(`
		SELECT id, question, gold_answer, should_refuse, gold_chunk_id,
		       project_slug, question_set, source_doc_id, category
		FROM eval_questions
		WHERE source_doc_id = ?
		   OR gold_chunk_id IN (SELECT id FROM chunks WHERE document_id = ?)
		ORDER BY id ASC
		FOR UPDATE`, docID, docID)
	if err != nil {
		return nil, err
	}
	return scanEvalQuestionsV2(rows)
}

func rebindAffectedEvalQuestionsTx(
	tx *sql.Tx,
	docID int64,
	questions []EvalQuestion,
	oldChunks map[int64]chunkSnapshot,
	newChunks []chunkSnapshot,
) ([]AffectedEvalItem, error) {
	if len(questions) == 0 {
		return nil, nil
	}
	items := make([]AffectedEvalItem, 0, len(questions))
	for _, q := range questions {
		oldGold := q.GoldChunkID
		if q.ShouldRefuse {
			if _, err := tx.Exec("UPDATE eval_questions SET source_doc_id = ? WHERE id = ?", docID, q.ID); err != nil {
				return nil, fmt.Errorf("更新拒答题 source_doc_id 失败：%w", err)
			}
			docCopy := docID
			q.SourceDocID = &docCopy
			items = append(items, AffectedEvalItem{
				Question:       q,
				OldGoldChunkID: oldGold,
				NeedsReview:    false,
			})
			continue
		}

		var oldHeading, oldContent string
		if q.GoldChunkID != nil {
			if cs, ok := oldChunks[*q.GoldChunkID]; ok {
				oldHeading = cs.HeadingPath
				oldContent = cs.Content
			}
		}
		matchedID, reason := matchReboundChunk(q, oldHeading, oldContent, newChunks)
		if matchedID != nil {
			if _, err := tx.Exec("UPDATE eval_questions SET gold_chunk_id = ?, source_doc_id = ? WHERE id = ?", *matchedID, docID, q.ID); err != nil {
				return nil, fmt.Errorf("重绑金标切片失败：%w", err)
			}
			docCopy := docID
			q.GoldChunkID = matchedID
			q.SourceDocID = &docCopy
			items = append(items, AffectedEvalItem{
				Question:       q,
				OldGoldChunkID: oldGold,
				NewGoldChunkID: matchedID,
				NeedsReview:    false,
			})
			continue
		}

		// 无法自动匹配时，补写 source_doc_id 以便后续覆写继续追踪，并将 gold_chunk_id 置 NULL 显式标示待人工复核
		if _, err := tx.Exec("UPDATE eval_questions SET gold_chunk_id = NULL, source_doc_id = ? WHERE id = ?", docID, q.ID); err != nil {
			return nil, fmt.Errorf("标记金标题 needs_review 失败：%w", err)
		}
		docCopy := docID
		q.GoldChunkID = nil
		q.SourceDocID = &docCopy
		items = append(items, AffectedEvalItem{
			Question:       q,
			OldGoldChunkID: oldGold,
			NewGoldChunkID: nil,
			NeedsReview:    true,
			ReviewReason:   reason,
		})
	}
	return items, nil
}

func matchReboundChunk(q EvalQuestion, oldHeading, oldContent string, newChunks []chunkSnapshot) (*int64, string) {
	if len(newChunks) == 0 {
		return nil, "新文档未切出可用段落"
	}
	if oldContent != "" {
		for i := range newChunks {
			if newChunks[i].HeadingPath == oldHeading && newChunks[i].Content == oldContent {
				id := newChunks[i].ID
				return &id, ""
			}
		}
		for i := range newChunks {
			if newChunks[i].Content == oldContent {
				id := newChunks[i].ID
				return &id, ""
			}
		}
	}

	normOldHeading := normalizeRebindHeading(oldHeading)
	bestSameHeadingID := int64(0)
	bestSameScore := -1.0
	bestCrossID := int64(0)
	bestCrossContentSim := -1.0
	bestCrossQA := -1.0

	qaText := strings.TrimSpace(q.Question + " " + q.GoldAnswer)

	for _, nc := range newChunks {
		qaSim := rebindTextOverlap(qaText, nc.HeadingPath+" "+nc.Content)
		contentSim := 0.0
		if oldContent != "" {
			contentSim = rebindTextOverlap(oldContent, nc.Content)
		}

		sameHeading := normOldHeading != "" && normalizeRebindHeading(nc.HeadingPath) == normOldHeading
		if sameHeading && oldContent != "" {
			if contentSim >= 0.35 || qaSim >= 0.45 {
				score := 0.7*contentSim + 0.3*qaSim
				if score > bestSameScore {
					bestSameScore = score
					bestSameHeadingID = nc.ID
				}
			}
		}

		if oldContent != "" {
			if contentSim >= 0.55 {
				if contentSim > bestCrossContentSim || (contentSim == bestCrossContentSim && qaSim > bestCrossQA) {
					bestCrossContentSim = contentSim
					bestCrossQA = qaSim
					bestCrossID = nc.ID
				}
			}
		} else if qaSim >= 0.55 {
			if qaSim > bestCrossQA {
				bestCrossQA = qaSim
				bestCrossID = nc.ID
			}
		}
	}

	if bestSameHeadingID > 0 {
		return &bestSameHeadingID, ""
	}
	if bestCrossID > 0 {
		return &bestCrossID, ""
	}
	if oldHeading != "" {
		return nil, fmt.Sprintf("原段落（%s）可能已改名或删除，无法自动重绑标准切片", oldHeading)
	}
	return nil, "未能匹配到新文档对应切片，需人工确认 gold_chunk_id"
}

func normalizeRebindHeading(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func rebindTextOverlap(a, b string) float64 {
	ta := tokenizeForRebind(a)
	tb := tokenizeForRebind(b)
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	inter := 0
	for k := range ta {
		if tb[k] {
			inter++
		}
	}
	if inter == 0 {
		return 0
	}
	minLen := len(ta)
	if len(tb) < minLen {
		minLen = len(tb)
	}
	overlap := float64(inter) / float64(minLen)
	dice := (2.0 * float64(inter)) / float64(len(ta)+len(tb))
	return 0.65*overlap + 0.35*dice
}

func tokenizeForRebind(s string) map[string]bool {
	tokens := make(map[string]bool)
	s = strings.ToLower(s)
	runes := []rune(s)

	// 1. 提取英文/数字/代码标识符（含拆分点号/下划线子词）
	for i := 0; i < len(runes); {
		if !isASCIIAlnumRebind(runes[i]) {
			i++
			continue
		}
		start := i
		i++
		for i < len(runes) {
			if isASCIIAlnumRebind(runes[i]) {
				i++
				continue
			}
			if (runes[i] == '.' || runes[i] == '_' || runes[i] == '-') && i+1 < len(runes) && isASCIIAlnumRebind(runes[i+1]) {
				i++
				continue
			}
			break
		}
		tok := string(runes[start:i])
		if len(tok) >= 2 {
			tokens[tok] = true
			for _, part := range strings.FieldsFunc(tok, func(r rune) bool { return r == '.' || r == '_' || r == '-' }) {
				if len(part) >= 2 {
					tokens[part] = true
				}
			}
		}
	}

	// 2. 提取去虚词后的中文 2-gram
	var hanRun []rune
	flushHan := func() {
		if len(hanRun) >= 2 {
			for i := 0; i+1 < len(hanRun); i++ {
				tokens["zh:"+string(hanRun[i:i+2])] = true
			}
		}
		hanRun = hanRun[:0]
	}
	for _, r := range runes {
		if unicode.Is(unicode.Han, r) {
			if isRebindStopHan(r) {
				flushHan()
				continue
			}
			hanRun = append(hanRun, r)
		} else {
			flushHan()
		}
	}
	flushHan()
	return tokens
}

func isASCIIAlnumRebind(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
}

func isRebindStopHan(r rune) bool {
	switch r {
	case '的', '了', '和', '与', '或', '是', '在', '有', '中', '为', '对', '把', '被', '将', '用', '可', '以', '要', '就', '都', '而', '等':
		return true
	default:
		return false
	}
}

func extWithDot(filename string) string {
	idx := strings.LastIndex(filename, ".")
	if idx < 0 {
		return ""
	}
	return filename[idx:]
}

func countAffectedEvalQuestionsTx(tx *sql.Tx, docID int64) (int, error) {
	var n int
	err := tx.QueryRow(`SELECT COUNT(*) FROM eval_questions
		WHERE source_doc_id = ?
		   OR gold_chunk_id IN (SELECT id FROM chunks WHERE document_id = ?)`, docID, docID).Scan(&n)
	return n, err
}

// UpdateDocumentMeta 修改文档治理元数据（status / doc_level / owner / replaced_by_doc_id）。
func (s *Store) UpdateDocumentMeta(id int64, patch DocumentPatch) (Document, error) {
	if id <= 0 {
		return Document{}, ErrDocumentNotFound
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Document{}, err
	}
	defer tx.Rollback()

	var existingID int64
	if err := tx.QueryRow("SELECT id FROM documents WHERE id = ? FOR UPDATE", id).Scan(&existingID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Document{}, ErrDocumentNotFound
		}
		return Document{}, err
	}

	var sets []string
	var args []any

	if patch.Status != nil {
		st, err := ValidateDocStatus(*patch.Status)
		if err != nil {
			return Document{}, err
		}
		sets = append(sets, "status = ?")
		args = append(args, st)
	}
	if patch.DocLevel != nil {
		lvl, err := NormalizeDocLevel(*patch.DocLevel)
		if err != nil {
			return Document{}, err
		}
		sets = append(sets, "doc_level = ?")
		args = append(args, lvl)
	}
	if patch.Owner != nil {
		sets = append(sets, "owner = ?")
		args = append(args, strings.TrimSpace(*patch.Owner))
	}
	if patch.SetReplacedByDocID {
		if patch.ReplacedByDocID == nil || *patch.ReplacedByDocID <= 0 {
			sets = append(sets, "replaced_by_doc_id = NULL")
		} else {
			targetID := *patch.ReplacedByDocID
			if targetID == id {
				return Document{}, ErrInvalidReplacedBy
			}
			var tmp int64
			if err := tx.QueryRow("SELECT id FROM documents WHERE id = ?", targetID).Scan(&tmp); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return Document{}, ErrInvalidReplacedBy
				}
				return Document{}, err
			}
			sets = append(sets, "replaced_by_doc_id = ?")
			args = append(args, targetID)
		}
	}

	if len(sets) > 0 {
		args = append(args, id)
		query := "UPDATE documents SET " + strings.Join(sets, ", ") + " WHERE id = ?"
		if _, err := tx.Exec(query, args...); err != nil {
			return Document{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Document{}, err
	}
	s.invalidateVecCache()
	return s.GetDocument(id)
}

// DeleteDocument 在事务内删除文档及其切片，并提示关联金标题数量（不误删金标记录）。
func (s *Store) DeleteDocument(id int64) (DeleteDocResult, error) {
	if id <= 0 {
		return DeleteDocResult{}, ErrDocumentNotFound
	}
	tx, err := s.db.Begin()
	if err != nil {
		return DeleteDocResult{}, err
	}
	defer tx.Rollback()

	var existingID int64
	if err := tx.QueryRow("SELECT id FROM documents WHERE id = ? FOR UPDATE", id).Scan(&existingID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DeleteDocResult{}, ErrDocumentNotFound
		}
		return DeleteDocResult{}, err
	}

	affectedEval, err := countAffectedEvalQuestionsTx(tx, id)
	if err != nil {
		return DeleteDocResult{}, err
	}

	if _, err := tx.Exec("UPDATE documents SET replaced_by_doc_id = NULL WHERE replaced_by_doc_id = ?", id); err != nil {
		return DeleteDocResult{}, err
	}
	resChunks, err := tx.Exec("DELETE FROM chunks WHERE document_id = ?", id)
	if err != nil {
		return DeleteDocResult{}, fmt.Errorf("删除文档切片失败：%w", err)
	}
	chunksDeleted, _ := resChunks.RowsAffected()

	if _, err := tx.Exec("DELETE FROM documents WHERE id = ?", id); err != nil {
		return DeleteDocResult{}, fmt.Errorf("删除文档失败：%w", err)
	}
	if err := tx.Commit(); err != nil {
		return DeleteDocResult{}, err
	}
	s.invalidateVecCache()

	warnings := []string{}
	if affectedEval > 0 {
		warnings = append(warnings, fmt.Sprintf("注意：有 %d 条金标评测题关联了该文档（金标记录未删除，但对应切片已移除）", affectedEval))
	}
	return DeleteDocResult{
		Deleted:               true,
		DocumentID:            id,
		ChunksDeleted:         chunksDeleted,
		AffectedEvalQuestions: affectedEval,
		Warnings:              warnings,
	}, nil
}
