package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-sql-driver/mysql"

	"knowledge-qa/eval"
	"knowledge-qa/generate"
	"knowledge-qa/ingest"
	"knowledge-qa/store"
)

// 真实MySQL加本地模型HTTP替身，验证接口装配与持久化，不代替人工金标验收。
func TestEvalHTTPMySQL(t *testing.T) {
	dsn := os.Getenv("KNOWLEDGE_QA_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("设置 KNOWLEDGE_QA_TEST_MYSQL_DSN 后运行真实 MySQL HTTP 集成测试")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("测试DSN格式不正确")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_]+_test$`).MatchString(cfg.DBName) {
		t.Fatal("测试库名必须为安全标识符且以 _test 结尾")
	}
	// 与store包测试分库，两个包并行执行也不会互相清理。
	name := strings.TrimSuffix(cfg.DBName, "_test") + "_api_test"
	cfg.DBName = ""
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	if _, err := admin.Exec("CREATE DATABASE `" + name + "` DEFAULT CHARSET utf8mb4"); err != nil {
		t.Fatalf("API集成测试只能创建原本不存在的专用库：%v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP DATABASE `" + name + "`"); err != nil {
			t.Errorf("清理API专用测试库失败：%v", err)
		}
	})
	cfg.DBName = name
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	raw, err := os.ReadFile("../migrations/001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(raw), ";") {
		// 原迁移的CREATE DATABASE/USE不执行，四张表只落在本测试创建的库中。
		if strings.Contains(statement, "CREATE TABLE") {
			if _, err := db.Exec(statement); err != nil {
				t.Fatal(err)
			}
		}
	}
	st, err := store.New(cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	docID, err := st.InsertDocument("接口测试资料", "api-fixture.md", "Group 创建路由组。")
	if err != nil {
		t.Fatal(err)
	}
	chunkID, err := st.InsertChunk(docID, "Gin > 路由组", "Group 创建路由组。", "[1,0]")
	if err != nil {
		t.Fatal(err)
	}
	fixture := []struct {
		question string
		refuse   bool
		goldID   any
	}{
		{"正常问题", false, chunkID},
		{"有依据但模型拒答", false, chunkID},
		{"资料外问题", true, nil},
	}
	for _, q := range fixture {
		if _, err := db.Exec("INSERT INTO eval_questions (question, gold_answer, should_refuse, gold_chunk_id) VALUES (?, ?, ?, ?)",
			q.question, "测试参考答案", q.refuse, q.goldID); err != nil {
			t.Fatal(err)
		}
	}
	var embeddings, chats atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/embeddings":
			embeddings.Add(1)
			io.WriteString(w, `{"data":[{"embedding":[1,0]}]}`)
		case "/chat/completions":
			chats.Add(1)
			var req struct {
				Messages []struct {
					Content string `json:"content"`
				} `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Messages) != 1 {
				http.Error(w, "无效的对话请求", http.StatusBadRequest)
				return
			}
			prompt := req.Messages[0].Content
			if !strings.Contains(prompt, "[1] Group 创建路由组。") {
				http.Error(w, "真实切片没有进入prompt", http.StatusBadRequest)
				return
			}
			answer := generate.RefuseAnswer
			if strings.Contains(prompt, "问题：正常问题") {
				answer = "使用 Group 创建路由组 [1]。"
			} else if !strings.Contains(prompt, "问题：有依据但模型拒答") && !strings.Contains(prompt, "问题：资料外问题") {
				http.Error(w, "出现了测试范围外的问题", http.StatusBadRequest)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"choices": []any{
				map[string]any{"message": map[string]string{"role": "assistant", "content": answer}},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(provider.Close)
	h := &Handler{
		Store: st, Embedder: ingest.NewEmbedder(provider.URL, "fixture-key", "fixture-embedding"),
		Chat: generate.NewChatClient(provider.URL, "fixture-key", "fixture-chat"),
	}
	server := httptest.NewServer(NewRouter(h))
	t.Cleanup(server.Close)
	post := func(path, body string, status int) []byte {
		t.Helper()
		resp, err := http.Post(server.URL+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != status {
			t.Fatalf("POST %s status=%d，预期%d，body=%s", path, resp.StatusCode, status, raw)
		}
		return raw
	}
	firstRaw := post("/api/eval/run", "", http.StatusOK)
	var fields map[string]any
	if err := json.Unmarshal(firstRaw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"hit_rate", "citation_rate", "refusal_rate"} {
		if _, numeric := fields[key].(float64); !numeric {
			t.Fatalf("%s必须为JSON数字，body=%s", key, firstRaw)
		}
	}
	if value, exists := fields["usable_rate"]; !exists || value != nil {
		t.Fatalf("未人工标注时 usable_rate 必须为 JSON null，body=%s", firstRaw)
	}
	var first eval.Report
	if err := json.Unmarshal(firstRaw, &first); err != nil {
		t.Fatal(err)
	}
	if first.HitRate != 1 || first.CitationRate != 1 || first.UsableRate != nil || first.RefusalRate != 1 {
		t.Fatalf("首批指标错误：%+v", first)
	}
	if len(first.Details) != 3 || first.Counts.AnsweredCount != 1 || first.Counts.MarkedCount != 0 || first.Counts.PendingCount != 1 {
		t.Fatalf("首批明细/计数错误：%+v", first)
	}
	for i, run := range first.Details {
		if run.ID <= 0 || run.Usable != nil || len(run.RetrievedChunkIDs) != 1 || run.RetrievedChunkIDs[0] != chunkID {
			t.Fatalf("第%d条未保留真实Top结果或初始NULL：%+v", i, run)
		}
		if i < 2 {
			if run.Hit == nil || !*run.Hit {
				t.Fatalf("有答案题即使模型拒答，检索仍应命中：%+v", run)
			}
		} else if run.Hit != nil {
			t.Fatal("该拒答题的hit应为NULL")
		}
		if i == 0 {
			if run.Refused || run.CitationOK == nil || !*run.CitationOK {
				t.Fatalf("正常回答应通过引用校验：%+v", run)
			}
		} else if !run.Refused || run.CitationOK != nil {
			t.Fatalf("自拒答结果应refused=true且citation_ok=NULL：%+v", run)
		}
		var sqlRefused bool
		var sqlHit, sqlCitation, sqlUsable sql.NullBool
		var sqlRetrieved string
		if err := db.QueryRow("SELECT refused, hit, citation_ok, usable, retrieved_chunk_ids FROM eval_runs WHERE id = ?", run.ID).
			Scan(&sqlRefused, &sqlHit, &sqlCitation, &sqlUsable, &sqlRetrieved); err != nil {
			t.Fatal(err)
		}
		if sqlRefused != run.Refused || sqlUsable.Valid || sqlHit.Valid != (run.Hit != nil) || sqlCitation.Valid != (run.CitationOK != nil) || sqlRetrieved != fmt.Sprintf("[%d]", chunkID) {
			t.Fatalf("SQL与HTTP三态结果不一致：run=%+v sqlHit=%+v sqlCitation=%+v sqlUsable=%+v ids=%s", run, sqlHit, sqlCitation, sqlUsable, sqlRetrieved)
		}
		if (run.Hit != nil && sqlHit.Bool != *run.Hit) || (run.CitationOK != nil && sqlCitation.Bool != *run.CitationOK) {
			t.Fatalf("SQL与HTTP自动评分不一致：run=%+v sqlHit=%+v sqlCitation=%+v", run, sqlHit, sqlCitation)
		}
	}
	if embeddings.Load() != 3 || chats.Load() != 3 {
		t.Fatalf("每题都应走embedding/生成完整链路：embedding=%d chat=%d", embeddings.Load(), chats.Load())
	}
	var ask askResponse
	if err := json.Unmarshal(post("/api/ask", `{"question":"正常问题"}`, http.StatusOK), &ask); err != nil {
		t.Fatal(err)
	}
	if ask.Answer != first.Details[0].Answer || ask.Refused || len(ask.Citations) != 1 || ask.Citations[0].ChunkID != chunkID {
		t.Fatalf("ask与eval应使用相同链路：ask=%+v eval=%+v", ask, first.Details[0])
	}
	markBody := fmt.Sprintf(`{"run_id":%d,"usable":false}`, first.Details[0].ID)
	for i := 0; i < 2; i++ {
		var result struct {
			OK bool `json:"ok"`
		}
		if err := json.Unmarshal(post("/api/eval/mark", markBody, http.StatusOK), &result); err != nil || !result.OK {
			t.Fatalf("false及重复false标注必须成功：result=%+v err=%v", result, err)
		}
	}
	var marked sql.NullBool
	if err := db.QueryRow("SELECT usable FROM eval_runs WHERE id = ?", first.Details[0].ID).Scan(&marked); err != nil || !marked.Valid || marked.Bool {
		t.Fatalf("SQL必须保存false，不能仍为NULL：usable=%+v err=%v", marked, err)
	}
	post("/api/eval/mark", fmt.Sprintf(`{"run_id":%d}`, first.Details[0].ID), http.StatusBadRequest)
	post("/api/eval/mark", `{"usable":false}`, http.StatusBadRequest)
	post("/api/eval/mark", `{"run_id":9223372036854775807,"usable":false}`, http.StatusNotFound)
	post("/api/eval/mark", fmt.Sprintf(`{"run_id":%d,"usable":true}`, first.Details[1].ID), http.StatusBadRequest)
	var second eval.Report
	if err := json.Unmarshal(post("/api/eval/run", "", http.StatusOK), &second); err != nil {
		t.Fatal(err)
	}
	if len(second.Details) != 3 || second.Counts.MarkedCount != 0 || second.Counts.PendingCount != 1 {
		t.Fatalf("新批次不能带入上一批标注：%+v", second)
	}
	for _, run := range second.Details {
		if run.Usable != nil || run.ID <= first.Details[2].ID {
			t.Fatalf("新批次应全NULL且有独立run_id：%+v", run)
		}
	}
	var total, pending, falseMarks int
	if err := db.QueryRow("SELECT COUNT(*), SUM(usable IS NULL), SUM(usable = 0) FROM eval_runs").Scan(&total, &pending, &falseMarks); err != nil {
		t.Fatal(err)
	}
	if total != 6 || pending != 5 || falseMarks != 1 {
		t.Fatalf("第二批应保留上一批false且本批全NULL：total=%d NULL=%d false=%d", total, pending, falseMarks)
	}
	if err := db.QueryRow("SELECT usable FROM eval_runs WHERE id = ?", first.Details[0].ID).Scan(&marked); err != nil || !marked.Valid || marked.Bool {
		t.Fatalf("新批次不能覆盖上一批false标注：usable=%+v err=%v", marked, err)
	}
	var markSecond eval.MarkResult
	if err := json.Unmarshal(post("/api/eval/mark", fmt.Sprintf(`{"run_id":%d,"usable":true}`, second.Details[0].ID), http.StatusOK), &markSecond); err != nil {
		t.Fatal(err)
	}
	if markSecond.MarkedCount != 1 || markSecond.UsableCount != 1 || markSecond.UsableRate == nil || *markSecond.UsableRate != 1 {
		t.Fatalf("第二批标注不能混入第一批 false 标注：%+v", markSecond)
	}
	if embeddings.Load() != 7 || chats.Load() != 7 {
		t.Fatalf("两批加一次ask应各调用7次：embedding=%d chat=%d", embeddings.Load(), chats.Load())
	}
}

func TestEvalRunSetsAndProjectIsolationMySQL(t *testing.T) {
	dsn := os.Getenv("KNOWLEDGE_QA_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("设置 KNOWLEDGE_QA_TEST_MYSQL_DSN 后运行 V2 评测集与项目隔离 HTTP 集成测试")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("测试DSN格式不正确")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_]+_test$`).MatchString(cfg.DBName) {
		t.Fatal("测试库名必须为安全标识符且以 _test 结尾")
	}
	name := strings.TrimSuffix(cfg.DBName, "_test") + "_v2_eval_api_test"
	cfg.DBName = ""
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	if _, err := admin.Exec("CREATE DATABASE `" + name + "` DEFAULT CHARSET utf8mb4"); err != nil {
		t.Fatalf("V2 API 评测集成测试只能创建原本不存在的专用库：%v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec("DROP DATABASE `" + name + "`")
	})

	cfg.DBName = name
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	raw001, err := os.ReadFile("../migrations/001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(raw001), ";") {
		if strings.Contains(statement, "CREATE TABLE") {
			if _, err := db.Exec(statement); err != nil {
				t.Fatal(err)
			}
		}
	}
	raw002, err := os.ReadFile("../migrations/002_v2_schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	applyV2SchemaForAPITest(t, db, string(raw002))

	st, err := store.New(cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	if _, err := st.CreateProject("alpha", "Alpha", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateProject("beta", "Beta", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.InsertOrReplaceDocument(store.InsertDocParams{
		ProjectSlug: "alpha",
		Filename:    "port.md",
		Content:     "Alpha 服务默认端口 8091",
		SourceType:  "md",
		ContentHash: store.SHA256Hex([]byte("alpha-port")),
		Chunks:      []store.ChunkInput{{HeadingPath: "Port", Content: "Alpha 服务默认端口 8091", Embedding: "[1,0]"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.InsertOrReplaceDocument(store.InsertDocParams{
		ProjectSlug: "beta",
		Filename:    "port.md",
		Content:     "Beta 服务默认端口 9090",
		SourceType:  "md",
		ContentHash: store.SHA256Hex([]byte("beta-port")),
		Chunks:      []store.ChunkInput{{HeadingPath: "Port", Content: "Beta 服务默认端口 9090", Embedding: "[1,0]"}},
	}); err != nil {
		t.Fatal(err)
	}

	alphaChunkID, err := st.FindGoldChunkInProject("alpha", "port.md", "Port", "8091")
	if err != nil {
		t.Fatal(err)
	}
	betaChunkID, err := st.FindGoldChunkInProject("beta", "port.md", "Port", "9090")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := st.AppendEvalQuestions([]store.EvalQuestion{
		{
			Question:    "基线端口问题",
			GoldAnswer:  "Alpha 8091",
			GoldChunkID: &alphaChunkID,
			ProjectSlug: "alpha",
			QuestionSet: "base52",
		},
		{
			Question:    "盲测端口问题",
			GoldAnswer:  "Beta 9090",
			GoldChunkID: &betaChunkID,
			ProjectSlug: "beta",
			QuestionSet: "holdout",
			Category:    "project_governance",
		},
		{
			Question:     "高分陷阱问题",
			GoldAnswer:   "文档中没有依据",
			ShouldRefuse: true,
			ProjectSlug:  "alpha",
			QuestionSet:  "holdout",
			Category:     "near_miss_refusal",
		},
	}); err != nil {
		t.Fatal(err)
	}

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/embeddings":
			io.WriteString(w, `{"data":[{"embedding":[1,0]}]}`)
		case "/chat/completions":
			var req struct {
				Messages []struct {
					Content string `json:"content"`
				} `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Messages) != 1 {
				http.Error(w, "invalid chat req", http.StatusBadRequest)
				return
			}
			prompt := req.Messages[0].Content
			answer := generate.RefuseAnswer
			switch {
			case strings.Contains(prompt, "问题：基线端口问题"):
				if !strings.Contains(prompt, "8091") || strings.Contains(prompt, "9090") {
					http.Error(w, "alpha 隔离失败", http.StatusBadRequest)
					return
				}
				answer = "Alpha 默认端口 8091 [1]。"
			case strings.Contains(prompt, "问题：盲测端口问题"):
				if !strings.Contains(prompt, "9090") || strings.Contains(prompt, "8091") {
					http.Error(w, "beta 隔离失败", http.StatusBadRequest)
					return
				}
				answer = "Beta 默认端口 9090 [1]。"
			case strings.Contains(prompt, "问题：高分陷阱问题"):
				answer = generate.RefuseAnswer
			}
			json.NewEncoder(w).Encode(map[string]any{"choices": []any{
				map[string]any{"message": map[string]string{"role": "assistant", "content": answer}},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(provider.Close)

	h := &Handler{
		Store:    st,
		Embedder: ingest.NewEmbedder(provider.URL, "fixture-key", "fixture-embedding"),
		Chat:     generate.NewChatClient(provider.URL, "fixture-key", "fixture-chat"),
	}
	server := httptest.NewServer(NewRouter(h))
	t.Cleanup(server.Close)

	post := func(path, body string, status int) []byte {
		t.Helper()
		resp, err := http.Post(server.URL+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != status {
			t.Fatalf("POST %s status=%d expected=%d body=%s", path, resp.StatusCode, status, raw)
		}
		return raw
	}

	post("/api/eval/run?set=feedback", "", http.StatusBadRequest)

	var baseReport eval.Report
	if err := json.Unmarshal(post("/api/eval/run?set=base52", "", http.StatusOK), &baseReport); err != nil {
		t.Fatal(err)
	}
	if baseReport.QuestionSet != "base52" || len(baseReport.Details) != 1 || baseReport.Details[0].ProjectSlug != "alpha" {
		t.Fatalf("base52 report mismatch: %+v", baseReport)
	}

	var holdoutReport eval.Report
	if err := json.Unmarshal(post("/api/eval/run?set=holdout", "", http.StatusOK), &holdoutReport); err != nil {
		t.Fatal(err)
	}
	if holdoutReport.QuestionSet != "holdout" || len(holdoutReport.Details) != 2 || holdoutReport.HitRate != 1 || holdoutReport.RefusalRate != 1 {
		t.Fatalf("holdout report mismatch: %+v", holdoutReport)
	}
	if holdoutReport.Categories["project_governance"].HitCount != 1 || holdoutReport.Categories["near_miss_refusal"].RefusalCorrectCount != 1 {
		t.Fatalf("holdout category breakdown mismatch: %+v", holdoutReport.Categories)
	}

	var allReport eval.Report
	if err := json.Unmarshal(post("/api/eval/run?set=all", "", http.StatusOK), &allReport); err != nil {
		t.Fatal(err)
	}
	if allReport.QuestionSet != "all" || len(allReport.Details) != 3 {
		t.Fatalf("all report mismatch: %+v", allReport)
	}

	var markBase, markHoldout eval.MarkResult
	if err := json.Unmarshal(post("/api/eval/mark", fmt.Sprintf(`{"run_id":%d,"usable":false}`, baseReport.Details[0].ID), http.StatusOK), &markBase); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(post("/api/eval/mark", fmt.Sprintf(`{"run_id":%d,"usable":true}`, holdoutReport.Details[0].ID), http.StatusOK), &markHoldout); err != nil {
		t.Fatal(err)
	}
	if markBase.UsableCount != 0 || markBase.MarkedCount != 1 || markHoldout.UsableCount != 1 || markHoldout.MarkedCount != 1 {
		t.Fatalf("batch isolation across ?set= runs failed: base=%+v holdout=%+v", markBase, markHoldout)
	}
}
