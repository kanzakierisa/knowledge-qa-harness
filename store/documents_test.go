package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
)

func TestValidateProjectSlugAndEnums(t *testing.T) {
	for _, valid := range []string{"events", "go-gin", "shared", "team-01", "a"} {
		if err := ValidateProjectSlug(valid); err != nil {
			t.Errorf("slug %q should be valid, got %v", valid, err)
		}
	}
	for _, invalid := range []string{"", "Events", "proj_1", "proj 1", "---", "auto", "all", "中文", strings.Repeat("a", 65)} {
		if err := ValidateProjectSlug(invalid); err == nil {
			t.Errorf("slug %q should be rejected", invalid)
		}
	}

	if lvl, err := NormalizeDocLevel(""); err != nil || lvl != "spec" {
		t.Fatalf("empty doc_level should default to spec, got %q err=%v", lvl, err)
	}
	if _, err := NormalizeDocLevel("draft"); err == nil {
		t.Fatal("invalid doc_level should fail")
	}
	if _, err := ValidateDocStatus("archived"); err == nil {
		t.Fatal("invalid doc status should fail")
	}

	if got := BuildProjectSlug("Team Alpha / v2"); got != "team-alpha-v2" {
		t.Fatalf("BuildProjectSlug ASCII got %q want team-alpha-v2", got)
	}
	if got := BuildProjectSlug("活动运营项目"); ValidateProjectSlug(got) != nil || !strings.HasPrefix(got, "proj-") {
		t.Fatalf("BuildProjectSlug Chinese got %q want valid proj-* slug", got)
	}
	if got := BuildProjectSlug("auto"); !strings.HasPrefix(got, "proj-") {
		t.Fatalf("BuildProjectSlug reserved word got %q want proj-*", got)
	}
}

// --- 纯内存假 SQL 驱动验证 AllChunks 向量缓存与失效逻辑 ---

type fakeVecDriver struct{ queries *int }
type fakeVecConnector struct{ queries *int }
type fakeVecConn struct{ queries *int }
type fakeVecRows struct{ idx int }

func (d fakeVecDriver) Open(string) (driver.Conn, error) { return fakeVecConn{queries: d.queries}, nil }
func (c fakeVecConnector) Connect(context.Context) (driver.Conn, error) {
	return fakeVecConn{queries: c.queries}, nil
}
func (c fakeVecConnector) Driver() driver.Driver        { return fakeVecDriver{queries: c.queries} }
func (fakeVecConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unused") }
func (fakeVecConn) Close() error                        { return nil }
func (fakeVecConn) Begin() (driver.Tx, error)           { return nil, errors.New("unused") }
func (c fakeVecConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	*c.queries++
	return &fakeVecRows{}, nil
}
func (*fakeVecRows) Columns() []string {
	return []string{"id", "document_id", "heading_path", "content", "embedding"}
}
func (*fakeVecRows) Close() error { return nil }
func (r *fakeVecRows) Next(v []driver.Value) error {
	if r.idx >= 1 {
		return io.EOF
	}
	r.idx++
	copy(v, []driver.Value{int64(10), int64(1), "H1", "内容", "[0.6,0.8]"})
	return nil
}

func TestStoreAllChunksVectorCache(t *testing.T) {
	queries := 0
	db := sql.OpenDB(fakeVecConnector{queries: &queries})
	defer db.Close()

	st := &Store{db: db}
	chunks1, err := st.AllChunks()
	if err != nil || len(chunks1) != 1 {
		t.Fatalf("first AllChunks failed: len=%d err=%v", len(chunks1), err)
	}
	if len(chunks1[0].Vec) != 2 || chunks1[0].Vec[0] != 0.6 {
		t.Fatalf("expected decoded Vec [0.6, 0.8], got %v", chunks1[0].Vec)
	}

	// 验证缓存已填充且可命中
	st.vecMu.RLock()
	cachedLen := len(st.vecCache)
	st.vecMu.RUnlock()
	if cachedLen != 1 {
		t.Fatalf("expected 1 entry in vecCache, got %d", cachedLen)
	}

	chunks2, err := st.AllChunks()
	if err != nil || len(chunks2) != 1 || len(chunks2[0].Vec) != 2 {
		t.Fatalf("second AllChunks failed: %+v err=%v", chunks2, err)
	}
	// 共享同一解码后切片指针（避免重复 json.Unmarshal）
	if &chunks1[0].Vec[0] != &chunks2[0].Vec[0] {
		t.Fatal("expected second AllChunks to reuse cached []float64 slice")
	}

	st.invalidateVecCache()
	st.vecMu.RLock()
	afterClear := st.vecCache
	st.vecMu.RUnlock()
	if afterClear != nil {
		t.Fatal("expected vecCache to be nil after invalidation")
	}
}

type fakeProjDriver struct{ seen *[]string }
type fakeProjConnector struct{ seen *[]string }
type fakeProjConn struct{ seen *[]string }
type fakeProjResult struct{ id int64 }

func (d fakeProjDriver) Open(string) (driver.Conn, error) { return fakeProjConn{seen: d.seen}, nil }
func (c fakeProjConnector) Connect(context.Context) (driver.Conn, error) {
	return fakeProjConn{seen: c.seen}, nil
}
func (c fakeProjConnector) Driver() driver.Driver        { return fakeProjDriver{seen: c.seen} }
func (fakeProjConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unused") }
func (fakeProjConn) Close() error                        { return nil }
func (fakeProjConn) Begin() (driver.Tx, error)           { return nil, errors.New("unused") }
func (c fakeProjConn) ExecContext(_ context.Context, _ string, args []driver.NamedValue) (driver.Result, error) {
	slug, _ := args[0].Value.(string)
	*c.seen = append(*c.seen, slug)
	if len(*c.seen) == 1 {
		return nil, &mysql.MySQLError{Number: 1062, Message: "Duplicate entry"}
	}
	return fakeProjResult{id: int64(len(*c.seen))}, nil
}
func (r fakeProjResult) LastInsertId() (int64, error) { return r.id, nil }
func (r fakeProjResult) RowsAffected() (int64, error) { return 1, nil }

func TestCreateProjectAutoSlugAndCollisionRetry(t *testing.T) {
	seen := make([]string, 0)
	db := sql.OpenDB(fakeProjConnector{seen: &seen})
	defer db.Close()

	st := &Store{db: db}
	p, err := st.CreateProject("", "Team Alpha", "测试自动生成")
	if err != nil {
		t.Fatalf("CreateProject auto slug failed: %v", err)
	}
	if len(seen) != 2 || seen[0] != "team-alpha" || seen[1] != "team-alpha-2" || p.Slug != "team-alpha-2" {
		t.Fatalf("unexpected retry sequence: seen=%v got=%+v", seen, p)
	}
}

// applyV2SchemaForTest 执行 002_v2_schema.sql（兼容 DELIMITER $$ 存储过程写法）。
func applyV2SchemaForTest(t *testing.T, db *sql.DB, sqlText string) {
	t.Helper()
	delim := ";"
	var cur strings.Builder
	for _, rawLine := range strings.Split(strings.ReplaceAll(sqlText, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(rawLine)
		if strings.HasPrefix(strings.ToUpper(trimmed), "DELIMITER ") {
			delim = strings.TrimSpace(trimmed[len("DELIMITER "):])
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		cur.WriteString(rawLine)
		cur.WriteByte('\n')
		bufStr := strings.TrimSpace(cur.String())
		if strings.HasSuffix(bufStr, delim) {
			stmt := strings.TrimSpace(strings.TrimSuffix(bufStr, delim))
			cur.Reset()
			if stmt == "" || strings.HasPrefix(strings.ToUpper(stmt), "SET NAMES") {
				continue
			}
			if _, err := db.Exec(stmt); err != nil {
				t.Fatalf("exec migration stmt failed: %v\nSQL:\n%s", err, stmt)
			}
		}
	}
	if rest := strings.TrimSpace(cur.String()); rest != "" {
		if _, err := db.Exec(rest); err != nil {
			t.Fatalf("exec trailing migration stmt failed: %v\nSQL:\n%s", err, rest)
		}
	}
}

func TestV2ProjectsAndDocumentsMySQL(t *testing.T) {
	dsn := os.Getenv("KNOWLEDGE_QA_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("设置 KNOWLEDGE_QA_TEST_MYSQL_DSN 后运行 V2 文档 CRUD MySQL 集成测试")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("测试DSN格式不正确")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_]+_test$`).MatchString(cfg.DBName) {
		t.Fatal("测试库名必须为安全标识符且以 _test 结尾")
	}
	name := strings.TrimSuffix(cfg.DBName, "_test") + "_v2_store_test"
	cfg.DBName = ""
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	if _, err := admin.Exec("CREATE DATABASE `" + name + "` DEFAULT CHARSET utf8mb4"); err != nil {
		t.Fatalf("创建专用测试库失败：%v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec("DROP DATABASE `" + name + "`")
	})

	cfg.DBName = name
	st, err := New(cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	raw001, err := os.ReadFile("../migrations/001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range strings.Split(string(raw001), ";") {
		if strings.Contains(stmt, "CREATE TABLE") {
			if _, err := st.db.Exec(stmt); err != nil {
				t.Fatal(err)
			}
		}
	}
	raw002, err := os.ReadFile("../migrations/002_v2_schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	// 连续执行两次验证 002_v2_schema.sql 幂等性
	applyV2SchemaForTest(t, st.db, string(raw002))
	applyV2SchemaForTest(t, st.db, string(raw002))

	projects, err := st.ListProjects()
	if err != nil || len(projects) != 2 {
		t.Fatalf("expected 2 seed projects (shared, go-gin), got %+v err=%v", projects, err)
	}
	created, err := st.CreateProject("events", "活动运营项目", "排期测试")
	if err != nil || created.Slug != "events" {
		t.Fatalf("CreateProject failed: %+v err=%v", created, err)
	}
	if _, err := st.CreateProject("events", "重复", ""); !errors.Is(err, ErrProjectExists) {
		t.Fatalf("expected ErrProjectExists, got %v", err)
	}

	hash1 := SHA256Hex([]byte("v1-raw"))
	ins1, err := st.InsertOrReplaceDocument(InsertDocParams{
		ProjectSlug: "events",
		Filename:    "schedule.xlsx",
		Content:     "## Sheet1\n\n| 时间 | 地点 |\n| --- | --- |\n| 10:00 | A厅 |",
		SourceType:  "xlsx",
		ContentHash: hash1,
		DocLevel:    "spec",
		Owner:       "张三",
		Chunks: []ChunkInput{
			{HeadingPath: "Sheet1", Content: "| 时间 | 地点 |\n| --- | --- |\n| 10:00 | A厅 |", Embedding: "[1,0]"},
		},
	})
	if err != nil || ins1.Skipped || ins1.ChunkCount != 1 {
		t.Fatalf("InsertOrReplaceDocument insert failed: %+v err=%v", ins1, err)
	}

	// 同项目同文件同哈希且未传 overwrite -> 防重跳过
	dupRes, err := st.InsertOrReplaceDocument(InsertDocParams{
		ProjectSlug: "events",
		Filename:    "schedule.xlsx",
		Content:     "ignored",
		SourceType:  "xlsx",
		ContentHash: hash1,
		Chunks:      []ChunkInput{{HeadingPath: "x", Content: "x", Embedding: "[1,0]"}},
	})
	if err != nil || !dupRes.Skipped || dupRes.DocumentID != ins1.DocumentID {
		t.Fatalf("expected duplicate skip, got %+v err=%v", dupRes, err)
	}

	// overwrite=true -> 事务内删旧写新
	overRes, err := st.InsertOrReplaceDocument(InsertDocParams{
		ProjectSlug: "events",
		Filename:    "schedule.xlsx",
		Content:     "updated content",
		SourceType:  "xlsx",
		ContentHash: hash1,
		DocLevel:    "rfc",
		Owner:       "李四",
		Overwrite:   true,
		Chunks: []ChunkInput{
			{HeadingPath: "Sheet1", Content: "seg1", Embedding: "[1,0]"},
			{HeadingPath: "Sheet1", Content: "seg2", Embedding: "[0,1]"},
		},
	})
	if err != nil || overRes.Skipped || !overRes.Replaced || overRes.ChunkCount != 2 || overRes.DocumentID != ins1.DocumentID {
		t.Fatalf("overwrite failed: %+v err=%v", overRes, err)
	}

	docs, err := st.ListDocuments("events", "")
	if err != nil || len(docs) != 1 || docs[0].ChunkCount != 2 || docs[0].Owner != "李四" || docs[0].DocLevel != "rfc" {
		t.Fatalf("ListDocuments mismatch: %+v err=%v", docs, err)
	}

	newStatus := "deprecated"
	patched, err := st.UpdateDocumentMeta(ins1.DocumentID, DocumentPatch{Status: &newStatus})
	if err != nil || patched.Status != "deprecated" {
		t.Fatalf("UpdateDocumentMeta failed: %+v err=%v", patched, err)
	}

	// 关联一条 eval_questions.source_doc_id 验证 DeleteDocument 返回受影响数且不删金标题
	if _, err := st.db.Exec("INSERT INTO eval_questions (question, gold_answer, should_refuse, source_doc_id) VALUES ('q', 'a', 0, ?)", ins1.DocumentID); err != nil {
		t.Fatal(err)
	}
	delRes, err := st.DeleteDocument(ins1.DocumentID)
	if err != nil || !delRes.Deleted || delRes.ChunksDeleted != 2 || delRes.AffectedEvalQuestions != 1 {
		t.Fatalf("DeleteDocument mismatch: %+v err=%v", delRes, err)
	}
	var evalCount int
	if err := st.db.QueryRow("SELECT COUNT(*) FROM eval_questions WHERE source_doc_id = ?", ins1.DocumentID).Scan(&evalCount); err != nil || evalCount != 1 {
		t.Fatalf("DeleteDocument must not delete eval_questions rows, count=%d err=%v", evalCount, err)
	}
	if n, err := st.CountChunksByProject("events"); err != nil || n != 0 {
		t.Fatalf("expected 0 chunks in events after delete, got %d err=%v", n, err)
	}
}
