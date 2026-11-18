// Package store 负责 MySQL 读写；语料方法在本文件，评测存储在 eval.go。
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/go-sql-driver/mysql"
)

type cachedVec struct {
	raw string
	vec []float64
}

// Store 包装 *sql.DB，对外提供领域方法与轻量向量解码缓存。
type Store struct {
	db       *sql.DB
	vecMu    sync.RWMutex
	vecCache map[int64]cachedVec
}

// New 建立连接并做 ping 校验。
func New(dsn string) (*Store, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("MySQL 连接失败：%w", err)
	}
	return &Store{db: db}, nil
}

// Close 关闭底层连接池。
func (s *Store) Close() error { return s.db.Close() }

// InsertDocument 写入一篇原始文档，返回自增 id。
func (s *Store) InsertDocument(title, filename, content string) (int64, error) {
	res, err := s.db.Exec(
		"INSERT INTO documents (title, filename, content) VALUES (?, ?, ?)",
		title, filename, content)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// InsertChunk 写入一个切片，embedding 为 JSON 数组字符串（可为空，稍后回填）。
func (s *Store) InsertChunk(documentID int64, headingPath, content, embedding string) (int64, error) {
	res, err := s.db.Exec(
		"INSERT INTO chunks (document_id, heading_path, content, embedding) VALUES (?, ?, ?, ?)",
		documentID, headingPath, content, embedding)
	if err != nil {
		return 0, err
	}
	s.invalidateVecCache()
	return res.LastInsertId()
}

// Chunk 是 chunks 表的一行（检索用；在 V2 schema 下附带项目归属与文档治理元数据）。
type Chunk struct {
	ID              int64
	DocumentID      int64
	HeadingPath     string
	Content         string
	Embedding       string    // JSON 数组字符串
	Vec             []float64 // 内存缓存解码后的向量（避免每次问答重复 json.Unmarshal）
	ProjectID       int64
	ProjectSlug     string
	ProjectName     string
	DocTitle        string
	DocLevel        string // spec | rfc | note
	DocStatus       string // active | deprecated
	ReplacedByDocID *int64
	ReplacedByTitle string
	DocOwner        string
}

// InvalidateVecCache 清空内存向量缓存（在文档增删改或外部重灌后调用）。
func (s *Store) InvalidateVecCache() {
	s.invalidateVecCache()
}

func (s *Store) invalidateVecCache() {
	s.vecMu.Lock()
	s.vecCache = nil
	s.vecMu.Unlock()
}

type pendingVec struct {
	idx int
	id  int64
	raw string
}

// lookupVecCache 在持有 vecMu 读锁时复用已解码向量，未命中则登记到 misses。
func (s *Store) lookupVecCache(c *Chunk, outIdx int, misses *[]pendingVec) {
	if s.vecCache != nil {
		if entry, ok := s.vecCache[c.ID]; ok && entry.raw == c.Embedding {
			c.Vec = entry.vec
		}
	}
	if len(c.Vec) == 0 {
		*misses = append(*misses, pendingVec{idx: outIdx, id: c.ID, raw: c.Embedding})
	}
}

// fillVecCache 对缓存未命中的切片执行 JSON 解码并回填内存向量缓存。
func (s *Store) fillVecCache(out []Chunk, misses []pendingVec) {
	if len(misses) == 0 {
		return
	}
	decoded := make(map[int64]cachedVec, len(misses))
	for _, m := range misses {
		var vec []float64
		if err := json.Unmarshal([]byte(m.raw), &vec); err == nil && len(vec) > 0 {
			out[m.idx].Vec = vec
			decoded[m.id] = cachedVec{raw: m.raw, vec: vec}
		}
	}
	if len(decoded) > 0 {
		s.vecMu.Lock()
		if s.vecCache == nil {
			s.vecCache = make(map[int64]cachedVec, len(decoded))
		}
		for id, cv := range decoded {
			s.vecCache[id] = cv
		}
		s.vecMu.Unlock()
	}
}

// AllChunks 读出全部已算好向量的切片，优先复用内存缓存中已解码的 []float64 向量。
func (s *Store) AllChunks() ([]Chunk, error) {
	rows, err := s.db.Query(
		"SELECT id, document_id, heading_path, content, embedding FROM chunks WHERE embedding IS NOT NULL AND embedding != ''")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Chunk
	var misses []pendingVec

	s.vecMu.RLock()
	for rows.Next() {
		var c Chunk
		if err := rows.Scan(&c.ID, &c.DocumentID, &c.HeadingPath, &c.Content, &c.Embedding); err != nil {
			s.vecMu.RUnlock()
			return nil, err
		}
		s.lookupVecCache(&c, len(out), &misses)
		out = append(out, c)
	}
	s.vecMu.RUnlock()

	if err := rows.Err(); err != nil {
		return nil, err
	}
	s.fillVecCache(out, misses)
	return out, nil
}

// AllChunksWithMeta 读出全部已算好向量的切片及所属项目与文档治理元数据；
// 若运行在未执行 002_v2_schema.sql 的 V1 最小库或 V1 单测假驱动上，自动回退到 AllChunks()。
func (s *Store) AllChunksWithMeta() ([]Chunk, error) {
	rows, err := s.db.Query(`
		SELECT c.id, c.document_id, c.heading_path, c.content, c.embedding,
		       d.project_id, p.slug, p.name, d.title, d.doc_level, d.status,
		       d.replaced_by_doc_id, COALESCE(rd.title, ''), d.owner
		FROM chunks c
		JOIN documents d ON d.id = c.document_id
		JOIN projects p ON p.id = d.project_id
		LEFT JOIN documents rd ON rd.id = d.replaced_by_doc_id
		WHERE c.embedding IS NOT NULL AND c.embedding != ''`)
	if err != nil {
		if IsV1SchemaMissingError(err) {
			return s.AllChunks()
		}
		return nil, err
	}
	defer rows.Close()

	// 兼容 V1 审计假驱动（忽略 SQL 并固定返回 5 列）
	if cols, colErr := rows.Columns(); colErr == nil && len(cols) == 5 {
		return s.AllChunks()
	}

	var out []Chunk
	var misses []pendingVec

	s.vecMu.RLock()
	for rows.Next() {
		var c Chunk
		var replaced sql.NullInt64
		if err := rows.Scan(
			&c.ID, &c.DocumentID, &c.HeadingPath, &c.Content, &c.Embedding,
			&c.ProjectID, &c.ProjectSlug, &c.ProjectName, &c.DocTitle, &c.DocLevel, &c.DocStatus,
			&replaced, &c.ReplacedByTitle, &c.DocOwner,
		); err != nil {
			s.vecMu.RUnlock()
			return nil, err
		}
		if replaced.Valid {
			v := replaced.Int64
			c.ReplacedByDocID = &v
		}
		s.lookupVecCache(&c, len(out), &misses)
		out = append(out, c)
	}
	s.vecMu.RUnlock()

	if err := rows.Err(); err != nil {
		return nil, err
	}
	s.fillVecCache(out, misses)
	return out, nil
}

// IsV1SchemaMissingError 判断错误是否为 V1 最小表结构缺少 V2 表或列（MySQL 1054 / 1146）。
func IsV1SchemaMissingError(err error) bool {
	var me *mysql.MySQLError
	if errors.As(err, &me) {
		return me.Number == 1054 || me.Number == 1146
	}
	return false
}

// ClearAll 清空 chunks 与 documents 两表（重灌语料前调用）。
// 先删 chunks 再删 documents，避免外键约束失败。
func (s *Store) ClearAll() error {
	if _, err := s.db.Exec("DELETE FROM chunks"); err != nil {
		return fmt.Errorf("清空 chunks 失败：%w", err)
	}
	if _, err := s.db.Exec("DELETE FROM documents"); err != nil {
		return fmt.Errorf("清空 documents 失败：%w", err)
	}
	s.invalidateVecCache()
	return nil
}

// CountChunks 返回切片总数（健康检查用）。
func (s *Store) CountChunks() (int, error) {
	var n int
	err := s.db.QueryRow("SELECT COUNT(*) FROM chunks").Scan(&n)
	return n, err
}

// CountChunksByProject 返回指定项目下的切片总数。
func (s *Store) CountChunksByProject(projectSlug string) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*)
		FROM chunks c
		JOIN documents d ON d.id = c.document_id
		JOIN projects p ON p.id = d.project_id
		WHERE p.slug = ?`, projectSlug).Scan(&n)
	return n, err
}
