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

// InsertDocResult 返回热入库事务结果。
type InsertDocResult struct {
	DocumentID            int64    `json:"document_id"`
	Filename              string   `json:"filename"`
	ProjectSlug           string   `json:"project_slug"`
	SourceType            string   `json:"source_type"`
	ChunkCount            int      `json:"chunk_count"`
	Skipped               bool     `json:"skipped"`
	Replaced              bool     `json:"replaced"`
	AffectedEvalQuestions int      `json:"affected_eval_questions,omitempty"`
	Message               string   `json:"message,omitempty"`
	Warnings              []string `json:"warnings"`
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
		affectedEval, err := countAffectedEvalQuestionsTx(tx, existingID)
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
		if err := insertChunksTx(tx, existingID, params.Chunks); err != nil {
			return InsertDocResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return InsertDocResult{}, err
		}
		s.invalidateVecCache()

		warnings := []string{}
		if affectedEval > 0 {
			warnings = append(warnings, fmt.Sprintf("该文档关联了 %d 条金标评测题，覆写后建议执行局部回归确认", affectedEval))
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

func insertChunksTx(tx *sql.Tx, documentID int64, chunks []ChunkInput) error {
	stmt, err := tx.Prepare("INSERT INTO chunks (document_id, heading_path, content, embedding) VALUES (?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, ch := range chunks {
		if _, err := stmt.Exec(documentID, ch.HeadingPath, ch.Content, ch.Embedding); err != nil {
			return fmt.Errorf("写入切片失败：%w", err)
		}
	}
	return nil
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
