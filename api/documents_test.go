package api

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"unsafe"

	"github.com/go-sql-driver/mysql"
	"github.com/xuri/excelize/v2"

	"knowledge-qa/generate"
	"knowledge-qa/ingest"
	"knowledge-qa/store"
)

func TestDocumentsHTTPInputValidation(t *testing.T) {
	router := NewRouter(&Handler{})

	cases := []struct {
		method string
		path   string
		body   string
		ct     string
	}{
		{http.MethodPost, "/api/projects", `{}`, "application/json"},
		{http.MethodPost, "/api/documents/upload", `{"filename":"a.md"}`, "application/json"},
		{http.MethodPost, "/api/documents/upload", `{"filename":"a.md","content":"hello","doc_level":"bad"}`, "application/json"},
		{http.MethodGet, "/api/documents/abc", ``, ""},
		{http.MethodGet, "/api/documents/0", ``, ""},
		{http.MethodPatch, "/api/documents/abc", `{"status":"deprecated"}`, "application/json"},
		{http.MethodPatch, "/api/documents/0", `{"status":"deprecated"}`, "application/json"},
		{http.MethodDelete, "/api/documents/abc", ``, ""},
	}
	for _, tc := range cases {
		t.Run(tc.method+"_"+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.ct != "" {
				req.Header.Set("Content-Type", tc.ct)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d want 400, body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func applyV2SchemaForAPITest(t *testing.T, db *sql.DB, sqlText string) {
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
				t.Fatalf("migration failed: %v\nSQL:\n%s", err, stmt)
			}
		}
	}
}

func buildTestXLSX(t *testing.T) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	f.SetSheetName("Sheet1", "活动排期")
	_ = f.SetCellValue("活动排期", "A1", "时间")
	_ = f.SetCellValue("活动排期", "B1", "地点")
	_ = f.SetCellValue("活动排期", "C1", "议程")
	_ = f.SetCellValue("活动排期", "A2", "2026-11-20 14:00")
	_ = f.SetCellValue("活动排期", "B2", "北京主会场B302")
	_ = f.SetCellValue("活动排期", "C2", "创作者大会开幕式")
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func buildEmptyScannedPDF() []byte {
	stream := ""
	objs := []string{
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n",
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n",
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>\nendobj\n",
		fmt.Sprintf("4 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n", len(stream), stream),
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs)+1)
	for i, obj := range objs {
		offsets[i+1] = buf.Len()
		buf.WriteString(obj)
	}
	xrefOffset := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xrefOffset)
	return buf.Bytes()
}

func TestV2DocumentsLifecycleHTTPMySQL(t *testing.T) {
	dsn := os.Getenv("KNOWLEDGE_QA_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("设置 KNOWLEDGE_QA_TEST_MYSQL_DSN 后运行 V2 HTTP 全链路集成测试")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("测试DSN格式不正确")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_]+_test$`).MatchString(cfg.DBName) {
		t.Fatal("测试库名必须为安全标识符且以 _test 结尾")
	}
	name := strings.TrimSuffix(cfg.DBName, "_test") + "_v2_api_test"
	cfg.DBName = ""
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	if _, err := admin.Exec("CREATE DATABASE `" + name + "` DEFAULT CHARSET utf8mb4"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec("DROP DATABASE `" + name + "`") })

	cfg.DBName = name
	st, err := store.New(cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	raw001, err := os.ReadFile("../migrations/001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range strings.Split(string(raw001), ";") {
		if strings.Contains(stmt, "CREATE TABLE") {
			if _, err := db.Exec(stmt); err != nil {
				t.Fatal(err)
			}
		}
	}
	raw002, err := os.ReadFile("../migrations/002_v2_schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	applyV2SchemaForAPITest(t, db, string(raw002))

	var embedCalls atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/embeddings":
			embedCalls.Add(1)
			io.WriteString(w, `{"data":[{"embedding":[1,0]}]}`)
		case "/chat/completions":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []any{
					map[string]any{"message": map[string]string{
						"role":    "assistant",
						"content": "活动地点在北京主会场B302 [1]。",
					}},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(provider.Close)

	h := &Handler{
		Store:     st,
		Embedder:  ingest.NewEmbedder(provider.URL, "k", "emb"),
		Chat:      generate.NewChatClient(provider.URL, "k", "chat"),
		Converter: ingest.NewConverter(ingest.ConvertOptions{Engine: "native"}),
	}
	srv := httptest.NewServer(NewRouter(h))
	t.Cleanup(srv.Close)

	// 1. 创建项目 events
	resp, err := http.Post(srv.URL+"/api/projects", "application/json",
		strings.NewReader(`{"slug":"events","name":"活动运营项目","description":"活动排期与规则测试"}`))
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("create project failed: resp=%v err=%v", resp, err)
	}
	resp.Body.Close()

	// 2. 上传 .xlsx
	uploadXLSX := func(overwrite bool) (int, store.InsertDocResult) {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		_ = mw.WriteField("project_slug", "events")
		_ = mw.WriteField("doc_level", "spec")
		_ = mw.WriteField("owner", "张三")
		if overwrite {
			_ = mw.WriteField("overwrite", "true")
		}
		fw, err := mw.CreateFormFile("file", "schedule.xlsx")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write(buildTestXLSX(t))
		_ = mw.Close()

		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/documents/upload", &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		var out store.InsertDocResult
		_ = json.NewDecoder(r.Body).Decode(&out)
		return r.StatusCode, out
	}

	code, up1 := uploadXLSX(false)
	if code != http.StatusOK || up1.Skipped || up1.ChunkCount != 1 || up1.ProjectSlug != "events" {
		t.Fatalf("upload xlsx failed: code=%d res=%+v", code, up1)
	}
	if embedCalls.Load() != 1 {
		t.Fatalf("expected 1 embed call, got %d", embedCalls.Load())
	}

	// 3. 重复上传防重：skipped=true 且不增加 embed 调用
	code, upDup := uploadXLSX(false)
	if code != http.StatusOK || !upDup.Skipped || upDup.DocumentID != up1.DocumentID {
		t.Fatalf("expected skipped=true on duplicate upload: code=%d res=%+v", code, upDup)
	}
	if embedCalls.Load() != 1 {
		t.Fatalf("duplicate upload should not call embed API, got %d", embedCalls.Load())
	}

	// 3b. JSON 上传缺省 project_slug 默认归入 go-gin，并验证 PATCH / GET /api/documents
	jsonResp, err := http.Post(srv.URL+"/api/documents/upload", "application/json",
		strings.NewReader(`{"filename":"rule.md","content":"# 规则\n\n这是规则正文内容。","owner":"王五"}`))
	if err != nil || jsonResp.StatusCode != http.StatusOK {
		t.Fatalf("json upload failed: %v", err)
	}
	var jsonOut store.InsertDocResult
	_ = json.NewDecoder(jsonResp.Body).Decode(&jsonOut)
	jsonResp.Body.Close()
	if jsonOut.ProjectSlug != store.DefaultProjectSlug || jsonOut.ChunkCount != 1 {
		t.Fatalf("expected default project go-gin and 1 chunk, got %+v", jsonOut)
	}

	patchReq, _ := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/documents/%d", srv.URL, up1.DocumentID),
		strings.NewReader(fmt.Sprintf(`{"status":"deprecated","doc_level":"rfc","owner":"李四","replaced_by_doc_id":%d}`, jsonOut.DocumentID)))
	patchReq.Header.Set("Content-Type", "application/json")
	patchResp, err := http.DefaultClient.Do(patchReq)
	if err != nil || patchResp.StatusCode != http.StatusOK {
		t.Fatalf("patch failed: %v", err)
	}
	var patchedDoc store.Document
	_ = json.NewDecoder(patchResp.Body).Decode(&patchedDoc)
	patchResp.Body.Close()
	if patchedDoc.Status != "deprecated" || patchedDoc.DocLevel != "rfc" || patchedDoc.Owner != "李四" ||
		patchedDoc.ReplacedByDocID == nil || *patchedDoc.ReplacedByDocID != jsonOut.DocumentID {
		t.Fatalf("patched doc mismatch: %+v", patchedDoc)
	}

	listResp, _ := http.Get(srv.URL + "/api/documents?project_slug=events&status=deprecated")
	var listBody struct {
		Documents []store.Document `json:"documents"`
	}
	_ = json.NewDecoder(listResp.Body).Decode(&listBody)
	listResp.Body.Close()
	if len(listBody.Documents) != 1 || listBody.Documents[0].ID != up1.DocumentID {
		t.Fatalf("list documents filter mismatch: %+v", listBody)
	}

	detailResp, err := http.Get(fmt.Sprintf("%s/api/documents/%d", srv.URL, up1.DocumentID))
	if err != nil || detailResp.StatusCode != http.StatusOK {
		t.Fatalf("get document detail failed: resp=%v err=%v", detailResp, err)
	}
	var detailBody store.DocumentDetail
	_ = json.NewDecoder(detailResp.Body).Decode(&detailBody)
	detailResp.Body.Close()
	if detailBody.ID != up1.DocumentID || detailBody.Content == "" || len(detailBody.Chunks) == 0 {
		t.Fatalf("document detail mismatch: %+v", detailBody)
	}

	// 删除刚才创建的 rule.md，确保后续测试只有 schedule.xlsx 在库
	delRuleReq, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/documents/%d", srv.URL, jsonOut.DocumentID), nil)
	delRuleResp, _ := http.DefaultClient.Do(delRuleReq)
	delRuleResp.Body.Close()

	// 4. 实时问答：命中刚上传切片并带引用
	askResp, err := http.Post(srv.URL+"/api/ask", "application/json",
		strings.NewReader(`{"question":"活动在哪里举办？"}`))
	if err != nil || askResp.StatusCode != http.StatusOK {
		t.Fatalf("ask failed: %v", err)
	}
	var askOut askResponse
	_ = json.NewDecoder(askResp.Body).Decode(&askOut)
	askResp.Body.Close()
	if askOut.Refused || len(askOut.Citations) != 1 || !strings.Contains(askOut.Answer, "[1]") {
		t.Fatalf("expected live answer with citation [1], got %+v", askOut)
	}

	// 5. 异常护栏：空扫描件 PDF 返回 400 及明确中文提示，不入库
	var pdfBody bytes.Buffer
	mw := multipart.NewWriter(&pdfBody)
	_ = mw.WriteField("project_slug", "events")
	fw, _ := mw.CreateFormFile("file", "scan.pdf")
	_, _ = fw.Write(buildEmptyScannedPDF())
	_ = mw.Close()
	pdfReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/documents/upload", &pdfBody)
	pdfReq.Header.Set("Content-Type", mw.FormDataContentType())
	pdfResp, err := http.DefaultClient.Do(pdfReq)
	if err != nil {
		t.Fatal(err)
	}
	pdfRaw, _ := io.ReadAll(pdfResp.Body)
	pdfResp.Body.Close()
	if pdfResp.StatusCode != http.StatusBadRequest || !strings.Contains(string(pdfRaw), "LLM_VLM_MODEL") {
		t.Fatalf("scanned pdf expected 400 with LLM_VLM_MODEL hint, got status=%d body=%s", pdfResp.StatusCode, pdfRaw)
	}

	// 6. 删除文档后，chunk 数归零，再次提问转为拒答
	delReq, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/documents/%d", srv.URL, up1.DocumentID), nil)
	delResp, err := http.DefaultClient.Do(delReq)
	if err != nil || delResp.StatusCode != http.StatusOK {
		t.Fatalf("delete failed: %v", err)
	}
	delResp.Body.Close()

	chkResp, _ := http.Get(srv.URL + "/api/chunks?project_slug=events")
	var chkBody struct {
		Total int `json:"total"`
	}
	_ = json.NewDecoder(chkResp.Body).Decode(&chkBody)
	chkResp.Body.Close()
	if chkBody.Total != 0 {
		t.Fatalf("expected 0 chunks after delete, got %d", chkBody.Total)
	}

	askResp2, _ := http.Post(srv.URL+"/api/ask", "application/json",
		strings.NewReader(`{"question":"活动在哪里举办？"}`))
	var askOut2 askResponse
	_ = json.NewDecoder(askResp2.Body).Decode(&askOut2)
	askResp2.Body.Close()
	if !askOut2.Refused {
		t.Fatalf("expected refusal after document deletion, got %+v", askOut2)
	}
}

type docScopeDriver struct{}
type docScopeConn struct{}
type docScopeConnector struct{}
type docScopeRows struct{ idx int }

func (docScopeDriver) Open(string) (driver.Conn, error)                { return docScopeConn{}, nil }
func (docScopeConnector) Connect(context.Context) (driver.Conn, error) { return docScopeConn{}, nil }
func (docScopeConnector) Driver() driver.Driver                        { return docScopeDriver{} }
func (docScopeConn) Prepare(string) (driver.Stmt, error)               { return nil, errors.New("unused") }
func (docScopeConn) Close() error                                      { return nil }
func (docScopeConn) Begin() (driver.Tx, error)                         { return nil, errors.New("unused") }
func (docScopeConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &docScopeRows{}, nil
}
func (*docScopeRows) Columns() []string {
	return []string{"id", "document_id", "heading_path", "content", "embedding"}
}
func (*docScopeRows) Close() error { return nil }
func (r *docScopeRows) Next(v []driver.Value) error {
	rows := [][]driver.Value{
		{int64(1), int64(10), "Doc 10", "Doc 10 port 8081", "[1,0]"},
		{int64(2), int64(20), "Doc 20", "Doc 20 port 9092", "[1,0]"},
	}
	if r.idx >= len(rows) {
		return io.EOF
	}
	copy(v, rows[r.idx])
	r.idx++
	return nil
}

func TestAskDocumentIDsIsolationOffline(t *testing.T) {
	var chatCalls atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/embeddings" {
			io.WriteString(w, `{"data":[{"embedding":[1,0]}]}`)
			return
		}
		chatCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{
					"role":    "assistant",
					"content": "Doc 20 listens on 9092 [1]。",
				},
			}},
		})
	}))
	defer provider.Close()

	db := sql.OpenDB(docScopeConnector{})
	defer db.Close()
	st := &store.Store{}
	f := reflect.ValueOf(st).Elem().FieldByName("db")
	reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem().Set(reflect.ValueOf(db))

	h := &Handler{
		Store:    st,
		Embedder: ingest.NewEmbedder(provider.URL, "test", "test"),
		Chat:     generate.NewChatClient(provider.URL, "test", "test"),
	}
	router := NewRouter(h)

	// 仅查材料 20，必须只返回 document_id=20 的引用
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"question":"port","document_ids":[20]}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var got askResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Refused || len(got.Citations) != 1 || got.Citations[0].DocumentID != 20 || got.Citations[0].ChunkID != 2 {
		t.Fatalf("expected only document 20 citation, got %+v", got)
	}

	// 查询不存在切片的材料 999，应直接拒答且不调 LLM
	beforeEmpty := chatCalls.Load()
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"question":"port","document_ids":[999]}`))
	req2.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w2.Code, w2.Body.String())
	}
	var gotEmpty askResponse
	if err := json.Unmarshal(w2.Body.Bytes(), &gotEmpty); err != nil {
		t.Fatal(err)
	}
	if !gotEmpty.Refused || chatCalls.Load() != beforeEmpty {
		t.Fatalf("expected refusal without LLM call for unknown doc id, got %+v", gotEmpty)
	}
}
