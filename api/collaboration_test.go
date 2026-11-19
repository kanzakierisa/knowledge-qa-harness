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

	"knowledge-qa/eval"
	"knowledge-qa/generate"
	"knowledge-qa/ingest"
	"knowledge-qa/store"
)

func TestStage12HTTPInputValidation(t *testing.T) {
	router := NewRouter(&Handler{})
	cases := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPatch, "/api/gaps/abc", `{"status":"resolved"}`},
		{http.MethodPatch, "/api/gaps/0", `{"status":"resolved"}`},
		{http.MethodPatch, "/api/gaps/1", `{}`},
		{http.MethodPost, "/api/feedback", `{"question":"","vote":"down"}`},
		{http.MethodPost, "/api/feedback", `{"question":"q","vote":"invalid"}`},
		{http.MethodPost, "/api/feedback/abc/promote", `{}`},
		{http.MethodPost, "/api/feedback/0/promote", `{}`},
		{http.MethodPost, "/api/feedback/1/promote", `{bad`},
	}
	for _, tc := range cases {
		t.Run(tc.method+"_"+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d want 400, body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestStage12CollaborationHTTPMySQL(t *testing.T) {
	dsn := os.Getenv("KNOWLEDGE_QA_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("设置 KNOWLEDGE_QA_TEST_MYSQL_DSN 后运行阶段 12 HTTP + MySQL 全链路集成测试")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("测试DSN格式不正确")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_]+_test$`).MatchString(cfg.DBName) {
		t.Fatal("测试库名必须为安全标识符且以 _test 结尾")
	}
	name := strings.TrimSuffix(cfg.DBName, "_test") + "_v3_collab_api_test"
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

	if _, err := st.CreateProject("alpha", "项目 Alpha", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateProject("beta", "项目 Beta", ""); err != nil {
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
			vec := "[0,1,0,0]"
			switch {
			case strings.Contains(er.Input, "报销审批单"):
				vec = "[0,0,0,1]"
			case strings.Contains(er.Input, "端口"):
				vec = "[1,0,0,0]"
			case strings.Contains(er.Input, "发奖") || strings.Contains(er.Input, "规则") || strings.Contains(er.Input, "引用损坏") || strings.Contains(er.Input, "模型自拒"):
				vec = "[0,0,1,0]"
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
			var content string
			switch {
			case strings.Contains(prompt, "问题：引用损坏测试"):
				content = "故意越界引用 [99]。"
			case strings.Contains(prompt, "问题：模型自拒测试"):
				content = `{"quotes":[],"decision":"refuse","answer":""}`
			case strings.Contains(prompt, "问题：发奖周期是几天") && strings.Contains(prompt, "5 个工作日"):
				content = "活动结束后 5 个工作日内发奖 [1]。"
			case strings.Contains(prompt, "问题：发奖周期是几天") && strings.Contains(prompt, "3 个工作日"):
				content = "活动结束后 3 个工作日内发奖 [1]。"
			case strings.Contains(prompt, "问题：发奖周期是几天"):
				content = `{"quotes":[],"decision":"refuse","answer":""}`
			case strings.Contains(prompt, "问题：新版本发布流程是什么"):
				if strings.Contains(prompt, "发布前需要经过双人复核") {
					content = "发布前需要经过双人复核 [1]。"
				} else {
					content = `{"quotes":[],"decision":"refuse","answer":""}`
				}
			case strings.Contains(prompt, "问题：评测集拒答题") || strings.Contains(prompt, "问题：评测集边界问题"):
				content = `{"quotes":[],"decision":"refuse","answer":""}`
			case strings.Contains(prompt, "8091"):
				content = "Alpha 默认监听 8091 端口 [1]。"
			case strings.Contains(prompt, "9090"):
				content = "Beta 默认监听 9090 端口 [1]。"
			default:
				content = `{"quotes":[],"decision":"refuse","answer":""}`
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": content}}},
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

	uploadDoc := func(projectSlug, filename, title, content, owner string, overwrite bool) uploadDocumentResponse {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		_ = mw.WriteField("project_slug", projectSlug)
		_ = mw.WriteField("title", title)
		_ = mw.WriteField("owner", owner)
		if overwrite {
			_ = mw.WriteField("overwrite", "true")
		}
		fw, _ := mw.CreateFormFile("file", filename)
		_, _ = io.WriteString(fw, content)
		_ = mw.Close()

		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/documents/upload", &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("upload status=%d body=%s", resp.StatusCode, raw)
		}
		var out uploadDocumentResponse
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	// 准备文档：alpha/beta 同主题端口文档（用于测试歧义不进盲区），alpha 活动规范（用于 Doc CI/CD）
	uploadDoc("alpha", "alpha-port.md", "Alpha 端口", "# 端口\n\nAlpha 服务监听 8091 端口。", "张三", false)
	uploadDoc("beta", "beta-port.md", "Beta 端口", "# 端口\n\nBeta 服务监听 9090 端口。", "李四", false)
	rewardPad := strings.Repeat("奖励发放需完成财务对账与合规复核，确保发放明细与活动台账一致。", 4)
	releasePad := strings.Repeat("发布流程必须遵循变更窗口要求，并在发布平台登记变更单号与回滚方案。", 4)
	dutyPad := strings.Repeat("值班同学需保持企业微信与告警电话畅通，并在十分钟内响应线上突发工单。", 4)

	promoDoc := uploadDoc("alpha", "promo-rules.md", "活动与发布规范",
		"# 活动与发布规范\n\n## 发奖周期\n\n活动结束后 3 个工作日内完成发奖。"+rewardPad+"\n\n## 发布流程\n\n新版本发布前需要经过双人复核。"+releasePad+"\n\n## 值班说明\n\n周末由值班表轮值同学响应。"+dutyPad,
		"王五", false)

	rewardChunkID, err := st.FindGoldChunkInProject("alpha", "promo-rules.md", "活动与发布规范 > 发奖周期", "3 个工作日")
	if err != nil {
		t.Fatal(err)
	}
	releaseChunkID, err := st.FindGoldChunkInProject("alpha", "promo-rules.md", "活动与发布规范 > 发布流程", "双人复核")
	if err != nil {
		t.Fatal(err)
	}
	dutyChunkID, err := st.FindGoldChunkInProject("alpha", "promo-rules.md", "活动与发布规范 > 值班说明", "值班表轮值")
	if err != nil {
		t.Fatal(err)
	}

	// 插入 1 条 base52 应拒答题 + 1 条 base52 仅靠 gold_chunk_id 关联的题 + 1 条关联值班段落的题
	if _, err := st.AppendEvalQuestions([]store.EvalQuestion{
		{Question: "评测集边界问题", GoldAnswer: generate.RefuseAnswer, ShouldRefuse: true, QuestionSet: "base52"},
		{Question: "发奖周期是几天", GoldAnswer: "3 个工作日", GoldChunkID: &rewardChunkID, ProjectSlug: "alpha", QuestionSet: "base52"},
		{Question: "周末谁值班", GoldAnswer: "值班表轮值同学", GoldChunkID: &dutyChunkID, ProjectSlug: "alpha", QuestionSet: "base52", SourceDocID: &promoDoc.DocumentID},
	}); err != nil {
		t.Fatal(err)
	}

	// 1. 线上提问未知问题 2 次 -> 写入 knowledge_gaps 且 hit_count==2
	for _, q := range []string{"项目 B 报销审批单去哪里领？", "项目 B 报销审批单去哪里领"} {
		resp, err := http.Post(srv.URL+"/api/ask", "application/json", strings.NewReader(fmt.Sprintf(`{"question":%q}`, q)))
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("ask failed: %v", err)
		}
		var out askResponse
		_ = json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		if !out.Refused {
			t.Fatalf("expected refusal, got %+v", out)
		}
	}

	// 触发 model_refusal 与 citation_fail（确认 reason 分类准确）
	for _, q := range []string{"模型自拒测试", "引用损坏测试"} {
		resp, _ := http.Post(srv.URL+"/api/ask", "application/json", strings.NewReader(fmt.Sprintf(`{"project_slug":"alpha","question":%q}`, q)))
		resp.Body.Close()
	}

	// 跨项目歧义与 compare 模式：绝对不能写入 knowledge_gaps
	ambResp, _ := http.Post(srv.URL+"/api/ask", "application/json", strings.NewReader(`{"question":"服务默认端口是多少？"}`))
	var ambOut askResponse
	_ = json.NewDecoder(ambResp.Body).Decode(&ambOut)
	ambResp.Body.Close()
	if ambOut.Ambiguity == nil || !ambOut.Ambiguity.Detected {
		t.Fatalf("expected ambiguity detected, got %+v", ambOut)
	}
	cmpResp, _ := http.Post(srv.URL+"/api/ask", "application/json", strings.NewReader(`{"question":"服务默认端口是多少？","compare":true}`))
	cmpResp.Body.Close()

	// 跑一次 base52 评测：评测里的拒答题绝对不能写入 knowledge_gaps
	evalResp, err := http.Post(srv.URL+"/api/eval/run?set=base52", "application/json", nil)
	if err != nil || evalResp.StatusCode != http.StatusOK {
		t.Fatalf("eval run failed: %v", err)
	}
	evalResp.Body.Close()

	gapsResp, err := http.Get(srv.URL + "/api/gaps?status=open")
	if err != nil || gapsResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/gaps failed: %v", err)
	}
	var gapsBody struct {
		Gaps []store.KnowledgeGap `json:"gaps"`
	}
	_ = json.NewDecoder(gapsResp.Body).Decode(&gapsBody)
	gapsResp.Body.Close()

	if len(gapsBody.Gaps) != 3 {
		t.Fatalf("expected exactly 3 open gaps (threshold*2 merged + model_refusal + citation_fail), got %d: %+v", len(gapsBody.Gaps), gapsBody.Gaps)
	}
	if gapsBody.Gaps[0].HitCount != 2 || gapsBody.Gaps[0].RefuseReason != "threshold" || !strings.Contains(gapsBody.Gaps[0].Question, "报销审批单") {
		t.Fatalf("top gap should be 报销审批单 with hit_count=2, got %+v", gapsBody.Gaps[0])
	}
	reasons := map[string]bool{}
	for _, g := range gapsBody.Gaps {
		reasons[g.RefuseReason] = true
		if strings.Contains(g.Question, "评测集") || strings.Contains(g.Question, "默认端口") {
			t.Fatalf("eval/ambiguity question leaked into knowledge_gaps: %+v", g)
		}
	}
	if !reasons["threshold"] || !reasons["model_refusal"] || !reasons["citation_fail"] {
		t.Fatalf("missing expected refuse_reasons: %+v", reasons)
	}

	// PATCH /api/gaps/:id
	patchReq, _ := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/gaps/%d", srv.URL, gapsBody.Gaps[0].ID),
		strings.NewReader(`{"status":"resolved"}`))
	patchReq.Header.Set("Content-Type", "application/json")
	patchResp, err := http.DefaultClient.Do(patchReq)
	if err != nil || patchResp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH /api/gaps failed: %v", err)
	}
	patchResp.Body.Close()

	// 2. 反馈提交与一键转正为 feedback 活评测集
	fbBody := fmt.Sprintf(`{"project_slug":"alpha","question":"新版本发布流程是什么","retrieved_chunk_ids":[%d],"answer":"...","refused":false,"vote":"down","comment":"漏了双人复核"}`, releaseChunkID)
	fbResp, err := http.Post(srv.URL+"/api/feedback", "application/json", strings.NewReader(fbBody))
	if err != nil || fbResp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /api/feedback failed: resp=%v err=%v", fbResp, err)
	}
	var createdFB store.EvalFeedback
	_ = json.NewDecoder(fbResp.Body).Decode(&createdFB)
	fbResp.Body.Close()
	if createdFB.ID <= 0 || len(createdFB.CandidateChunks) != 1 || createdFB.CandidateChunks[0].ID != releaseChunkID {
		t.Fatalf("unexpected created feedback: %+v", createdFB)
	}

	promoReqBody := fmt.Sprintf(`{"gold_answer":"发布前需要经过双人复核。","should_refuse":false,"gold_chunk_id":%d,"category":"release"}`, releaseChunkID)
	promoResp, err := http.Post(fmt.Sprintf("%s/api/feedback/%d/promote", srv.URL, createdFB.ID), "application/json", strings.NewReader(promoReqBody))
	if err != nil || promoResp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/feedback/:id/promote failed: resp=%v err=%v", promoResp, err)
	}
	var promoOut store.PromoteFeedbackResult
	_ = json.NewDecoder(promoResp.Body).Decode(&promoOut)
	promoResp.Body.Close()
	if !promoOut.OK || promoOut.PromotedQuestionID <= 0 || promoOut.Question.QuestionSet != "feedback" {
		t.Fatalf("unexpected promote response: %+v", promoOut)
	}

	// 重复转正返回 409
	dupPromoResp, _ := http.Post(fmt.Sprintf("%s/api/feedback/%d/promote", srv.URL, createdFB.ID), "application/json", strings.NewReader(promoReqBody))
	if dupPromoResp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate promote expected 409, got %d", dupPromoResp.StatusCode)
	}
	dupPromoResp.Body.Close()

	// 运行 ?set=feedback 活评测集
	fbEvalResp, err := http.Post(srv.URL+"/api/eval/run?set=feedback", "application/json", nil)
	if err != nil || fbEvalResp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/eval/run?set=feedback failed: %v", err)
	}
	var fbReport eval.Report
	_ = json.NewDecoder(fbEvalResp.Body).Decode(&fbReport)
	fbEvalResp.Body.Close()
	if fbReport.QuestionSet != "feedback" || fbReport.HitRate != 1 || fbReport.CitationRate != 1 || len(fbReport.Details) != 1 {
		t.Fatalf("feedback eval report mismatch: %+v", fbReport)
	}

	// 3. Doc CI/CD 局部回归：第一次覆写保留全部 3 个段落并小幅修改发奖周期
	over1 := uploadDoc("alpha", "promo-rules.md", "活动与发布规范",
		"# 活动与发布规范\n\n## 发奖周期\n\n活动结束后 5 个工作日内完成发奖。"+rewardPad+"\n\n## 发布流程\n\n新版本发布前需要经过双人复核。"+releasePad+"\n\n## 值班说明\n\n周末由值班表轮值同学响应。"+dutyPad,
		"王五", true)
	if !over1.Replaced || over1.RegressionSummary == nil {
		t.Fatalf("overwrite upload must return regression_summary: %+v", over1)
	}
	if over1.RegressionSummary.Checked != 3 || over1.RegressionSummary.NeedsReview != 0 || over1.RegressionSummary.RefusedUnexpectedly != 0 {
		t.Fatalf("unexpected over1 regression_summary: %+v", over1.RegressionSummary)
	}

	// 第二次覆写：删除“值班说明”段落（triggers needs_review=1）并移除“双人复核”（triggers refused_unexpectedly=1）
	over2 := uploadDoc("alpha", "promo-rules.md", "活动与发布规范",
		"# 活动与发布规范\n\n## 发奖周期\n\n活动结束后 5 个工作日内完成发奖。"+rewardPad+"\n\n## 发布流程\n\n新版本发布需先更新版本号。"+releasePad,
		"王五", true)
	if over2.RegressionSummary == nil {
		t.Fatal("over2 missing regression_summary")
	}
	if over2.RegressionSummary.NeedsReview != 1 || over2.RegressionSummary.RefusedUnexpectedly != 1 || over2.RegressionSummary.Checked != 2 || over2.RegressionSummary.Passed != 1 {
		t.Fatalf("unexpected over2 regression_summary: %+v", over2.RegressionSummary)
	}

	// 最终核验：feedback eval 与两次 Doc CI/CD 均不得污染 knowledge_gaps（依然只有 2 条 open、1 条 resolved）
	var openCnt, totalGapCnt int
	_ = db.QueryRow("SELECT COUNT(*), SUM(status='open') FROM knowledge_gaps").Scan(&totalGapCnt, &openCnt)
	if totalGapCnt != 3 || openCnt != 2 {
		t.Fatalf("Doc CI/CD or feedback eval polluted knowledge_gaps: total=%d open=%d", totalGapCnt, openCnt)
	}
}
