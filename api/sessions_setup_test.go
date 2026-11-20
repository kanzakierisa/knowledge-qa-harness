package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"

	"knowledge-qa/config"
	"knowledge-qa/generate"
	"knowledge-qa/ingest"
	"knowledge-qa/store"
)

func TestSetupModeEndpoints(t *testing.T) {
	// 1. 未开启 setup 模式时 POST /api/setup 必须拒绝
	closedRouter := NewRouter(&Handler{})
	req403 := httptest.NewRequest(http.MethodPost, "/api/setup", strings.NewReader(`{}`))
	req403.Header.Set("Content-Type", "application/json")
	rec403 := httptest.NewRecorder()
	closedRouter.ServeHTTP(rec403, req403)
	if rec403.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when SetupEnabled=false, got %d body=%s", rec403.Code, rec403.Body.String())
	}

	// 2. 开启 setup 模式但缺少模型配置时：/api/ask 与 /api/documents/upload 返回 503 setup_required
	h := &Handler{}
	h.InitSetupFromConfig(&config.Config{ConvertEngine: "native"}, true, []string{"LLM_API_KEY"})
	router := NewRouter(h)

	statusRec := httptest.NewRecorder()
	router.ServeHTTP(statusRec, httptest.NewRequest(http.MethodGet, "/api/setup/status", nil))
	if statusRec.Code != http.StatusOK {
		t.Fatalf("status=%d", statusRec.Code)
	}
	var stBody setupStatusResponse
	_ = json.Unmarshal(statusRec.Body.Bytes(), &stBody)
	if !stBody.SetupEnabled || stBody.Configured || len(stBody.Missing) != 1 {
		t.Fatalf("unexpected setup status before config: %+v", stBody)
	}

	askRec := httptest.NewRecorder()
	askReq := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"question":"hi"}`))
	askReq.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(askRec, askReq)
	if askRec.Code != http.StatusServiceUnavailable || !strings.Contains(askRec.Body.String(), `"setup_required":true`) {
		t.Fatalf("expected 503 setup_required on /api/ask, got %d body=%s", askRec.Code, askRec.Body.String())
	}

	// 3. 通过 POST /api/setup 热更新内存客户端，且 /api/setup/status 绝不泄露 API Key
	setupPayload := `{
		"llm_base_url":"https://api.example.com/v1/",
		"llm_api_key":"sk-secret-memory-only",
		"llm_chat_model":"deepseek-chat",
		"llm_embed_model":"text-embedding-v3",
		"embed_base_url":"https://embed.example.com/v1",
		"embed_api_key":"sk-embed-secret"
	}`
	setupReq := httptest.NewRequest(http.MethodPost, "/api/setup", strings.NewReader(setupPayload))
	setupReq.Header.Set("Content-Type", "application/json")
	setupRec := httptest.NewRecorder()
	router.ServeHTTP(setupRec, setupReq)
	if setupRec.Code != http.StatusOK {
		t.Fatalf("POST /api/setup failed: %d body=%s", setupRec.Code, setupRec.Body.String())
	}
	rawResp := setupRec.Body.String()
	if strings.Contains(rawResp, "sk-secret-memory-only") || strings.Contains(rawResp, "sk-embed-secret") {
		t.Fatalf("setup response leaked API key: %s", rawResp)
	}
	var afterBody setupStatusResponse
	_ = json.Unmarshal(setupRec.Body.Bytes(), &afterBody)
	if !afterBody.Configured || afterBody.ChatModel != "deepseek-chat" || !afterBody.HasEmbedOverride {
		t.Fatalf("unexpected setup status after apply: %+v", afterBody)
	}
	if h.Embedder == nil || h.Chat == nil {
		t.Fatal("expected Embedder and Chat to be hot-rebuilt in memory")
	}
}

func TestStage13SessionsAndHiddenIsolationHTTPMySQL(t *testing.T) {
	dsn := os.Getenv("KNOWLEDGE_QA_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("设置 KNOWLEDGE_QA_TEST_MYSQL_DSN 后运行阶段 13 HTTP + MySQL 全链路集成测试")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("测试DSN格式不正确")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_]+_test$`).MatchString(cfg.DBName) {
		t.Fatal("测试库名必须为安全标识符且以 _test 结尾")
	}
	name := strings.TrimSuffix(cfg.DBName, "_test") + "_v3_stage13_api_test"
	cfg.DBName = ""
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	if _, err := admin.Exec("CREATE DATABASE `" + name + "` DEFAULT CHARSET utf8mb4"); err != nil {
		t.Fatalf("创建专用测试库失败：%v", err)
	}
	t.Cleanup(func() { _, _ = admin.Exec("DROP DATABASE `" + name + "`") })

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

	st, err := store.New(cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	if _, err := st.EnsureProject("project-alpha", "Alpha 夹具", "", true); err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnsureProject("content-promo", "内容运营", "", false); err != nil {
		t.Fatal(err)
	}

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/embeddings":
			var er struct {
				Input string `json:"input"`
			}
			_ = json.NewDecoder(r.Body).Decode(&er)
			vec := "[0,0,1]"
			if strings.Contains(er.Input, "端口") || strings.Contains(er.Input, "8091") || strings.Contains(er.Input, "8082") {
				vec = "[1,0,0]"
			}
			_, _ = io.WriteString(w, fmt.Sprintf(`{"data":[{"embedding":%s}]}`, vec))
		case "/chat/completions":
			var req struct {
				Messages []struct {
					Content string `json:"content"`
				} `json:"messages"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			prompt := req.Messages[0].Content
			ans := `{"quotes":[],"decision":"refuse","answer":""}`
			if strings.Contains(prompt, "8091") {
				ans = "Alpha 监听 8091 端口 [1]。"
			} else if strings.Contains(prompt, "8082") {
				ans = "内容运营活动服务监听 8082 端口 [1]。"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": ans}}},
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

	upload := func(proj, name, content string) {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		_ = mw.WriteField("project_slug", proj)
		fw, _ := mw.CreateFormFile("file", name)
		_, _ = io.WriteString(fw, content)
		_ = mw.Close()
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/documents/upload", &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("upload failed: resp=%v err=%v", resp, err)
		}
		resp.Body.Close()
	}

	upload("project-alpha", "alpha-port.md", "# Alpha 端口\n\nAlpha 服务默认端口 8091。")
	upload("content-promo", "promo-port.md", "# 活动端口\n\n内容运营活动服务默认端口 8082。")

	// 1. GET /api/projects 与 /api/documents 与 /api/chunks 必须隐藏 project-alpha
	projResp, _ := http.Get(srv.URL + "/api/projects")
	var projBody struct {
		Projects []store.Project `json:"projects"`
	}
	_ = json.NewDecoder(projResp.Body).Decode(&projBody)
	projResp.Body.Close()
	for _, p := range projBody.Projects {
		if p.Slug == "project-alpha" {
			t.Fatalf("hidden project returned in /api/projects: %+v", projBody.Projects)
		}
	}

	// 2. auto 模式日常提问绝对不能命中隐藏的 project-alpha（不会触发歧义，只回答 content-promo 8082）
	ask1Resp, err := http.Post(srv.URL+"/api/ask", "application/json",
		strings.NewReader(`{"question":"请问一下，服务默认端口是多少？"}`))
	if err != nil || ask1Resp.StatusCode != http.StatusOK {
		t.Fatalf("ask1 failed: %v", err)
	}
	var ask1 askResponse
	_ = json.NewDecoder(ask1Resp.Body).Decode(&ask1)
	ask1Resp.Body.Close()
	if ask1.Ambiguity != nil || ask1.Refused || !strings.Contains(ask1.Answer, "8082") || ask1.SessionID <= 0 || ask1.TurnID <= 0 {
		t.Fatalf("expected clean content-promo answer with session/turn IDs, got %+v", ask1)
	}
	if ask1.CitationOK == nil || !*ask1.CitationOK {
		t.Fatalf("expected citation_ok=true, got %+v", ask1.CitationOK)
	}

	// 3. 同一会话追加第二轮拒答提问
	ask2Resp, err := http.Post(srv.URL+"/api/ask", "application/json",
		strings.NewReader(fmt.Sprintf(`{"question":"完全不相关的报销流程","session_id":%d}`, ask1.SessionID)))
	if err != nil || ask2Resp.StatusCode != http.StatusOK {
		t.Fatalf("ask2 failed: %v", err)
	}
	var ask2 askResponse
	_ = json.NewDecoder(ask2Resp.Body).Decode(&ask2)
	ask2Resp.Body.Close()
	if !ask2.Refused || ask2.SessionID != ask1.SessionID || ask2.TurnID <= ask1.TurnID {
		t.Fatalf("ask2 mismatch: %+v", ask2)
	}

	// 4. 评测跑批显式指定 project-alpha 仍可查到隐藏项目，且不写入 qa_sessions / qa_turns
	alphaChunkID, err := st.FindGoldChunkInProject("project-alpha", "alpha-port.md", "Alpha 端口", "8091")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AppendEvalQuestions([]store.EvalQuestion{
		{Question: "评测隐藏项目端口", GoldAnswer: "8091", GoldChunkID: &alphaChunkID, ProjectSlug: "project-alpha", QuestionSet: "holdout"},
	}); err != nil {
		t.Fatal(err)
	}
	evalResp, _ := http.Post(srv.URL+"/api/eval/run?set=holdout", "application/json", nil)
	if evalResp.StatusCode != http.StatusOK {
		t.Fatalf("holdout eval on hidden project failed: %d", evalResp.StatusCode)
	}
	evalResp.Body.Close()

	// 5. 提交反馈联动 turn_id，并检查 /api/sessions/:id 与 /api/stats/online（未被 eval 污染）
	fbResp, _ := http.Post(srv.URL+"/api/feedback", "application/json",
		strings.NewReader(fmt.Sprintf(`{"turn_id":%d,"project_slug":"content-promo","question":"服务默认端口是多少？","answer":"...","vote":"up"}`, ask1.TurnID)))
	if fbResp.StatusCode != http.StatusCreated {
		t.Fatalf("feedback failed: %d", fbResp.StatusCode)
	}
	fbResp.Body.Close()

	statsResp, _ := http.Get(srv.URL + "/api/stats/online")
	var online store.OnlineStats
	_ = json.NewDecoder(statsResp.Body).Decode(&online)
	statsResp.Body.Close()
	if online.TotalTurns != 2 || online.CitationOKCount != 1 || online.CitationTotalCount != 1 ||
		online.FeedbackUpCount != 1 || online.FeedbackTotalCount != 1 || online.RefusedCount != 1 {
		t.Fatalf("online stats mismatch: %+v", online)
	}

	// 6. 删除会话后统计归零
	delReq, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/sessions/%d", srv.URL, ask1.SessionID), nil)
	delResp, _ := http.DefaultClient.Do(delReq)
	if delResp.StatusCode != http.StatusOK {
		t.Fatalf("delete session failed: %d", delResp.StatusCode)
	}
	delResp.Body.Close()

	statsAfterResp, _ := http.Get(srv.URL + "/api/stats/online")
	var onlineAfter store.OnlineStats
	_ = json.NewDecoder(statsAfterResp.Body).Decode(&onlineAfter)
	statsAfterResp.Body.Close()
	if onlineAfter.TotalTurns != 0 || onlineAfter.CitationRate != nil || onlineAfter.FeedbackUsableRate != nil {
		t.Fatalf("online stats after session delete should be zero/null: %+v", onlineAfter)
	}
}
