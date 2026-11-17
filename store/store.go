// Package store 负责 MySQL 读写；语料方法在本文件，评测存储在 eval.go。
package store

import (
	"database/sql"
	"fmt"

	_ "github.com/go-sql-driver/mysql"
)

// Store 包装 *sql.DB，对外提供领域方法。
type Store struct {
	db *sql.DB
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
	return res.LastInsertId()
}

// Chunk 是 chunks 表的一行（检索用）。
type Chunk struct {
	ID          int64
	DocumentID  int64
	HeadingPath string
	Content     string
	Embedding   string // JSON 数组字符串
}

// AllChunks 读出全部已算好向量的切片（语料 ≤500 段，全量读入内存算余弦）。
func (s *Store) AllChunks() ([]Chunk, error) {
	rows, err := s.db.Query(
		"SELECT id, document_id, heading_path, content, embedding FROM chunks WHERE embedding IS NOT NULL AND embedding != ''")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Chunk
	for rows.Next() {
		var c Chunk
		if err := rows.Scan(&c.ID, &c.DocumentID, &c.HeadingPath, &c.Content, &c.Embedding); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
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
	return nil
}

// CountChunks 返回切片总数（健康检查用）。
func (s *Store) CountChunks() (int, error) {
	var n int
	err := s.db.QueryRow("SELECT COUNT(*) FROM chunks").Scan(&n)
	return n, err
}
