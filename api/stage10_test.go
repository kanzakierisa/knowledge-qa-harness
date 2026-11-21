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
	"sync/atomic"
	"testing"

	"github.com/go-sql-driver/mysql"

	"knowledge-qa/generate"
	"knowledge-qa/ingest"
	"knowledge-qa/retrieve"
	"knowledge-qa/store"
)

func TestDetectCrossProjectAmbiguityBoundaries(t *testing.T) {
	makeChunk := func(id int64, slug, name, heading string, score float64) retrieve.MergedChunk {
		return retrieve.MergedChunk{
			Chunk: store.Chunk{
				ID:          id,
				ProjectSlug: slug,
				ProjectName: name,
				HeadingPath: heading,
				Content:     "content",
				DocLevel:    "spec",
				DocStatus:   "active",
			},
			Final: score,
		}
	}

	// 1. 低于拒答阈值：不触发歧义
	below := detectCrossProjectAmbiguity([]retrieve.MergedChunk{
		makeChunk(1, "project-a", "项目 A", "H1", 0.34),
		makeChunk(2, "project-b", "项目 B", "H2", 0.33),
	}, refuseThreshold, retrieve.TopN)
	if below.Ambiguity != nil {
		t.Fatalf("低于阈值不应触发歧义：%+v", below.Ambiguity)
	}

	// 2. 边界精度：0.68 - 0.60 == 0.08（包含 IEEE-754 浮点边界），必须触发歧义
	boundary := detectCrossProjectAmbiguity([]retrieve.MergedChunk{
		makeChunk(1, "project-a", "项目 A", "A > 端口", 0.68),
		makeChunk(2, "shared", "公共规范库", "公共 > 端口", 0.66),
		makeChunk(3, "project-b", "项目 B", "B > 端口", 0.60),
	}, refuseThreshold, retrieve.TopN)
	if boundary.Ambiguity == nil || !boundary.Ambiguity.Detected || boundary.ProjectResolved != "ambiguous" {
		t.Fatalf("分差 0.08 应触发歧义，实际：%+v", boundary)
	}
	if len(boundary.Ambiguity.Candidates) != 2 ||
		boundary.Ambiguity.Candidates[0].ProjectSlug != "project-a" ||
		boundary.Ambiguity.Candidates[1].ProjectSlug != "project-b" {
		t.Fatalf("候选项目列表不符：%+v", boundary.Ambiguity.Candidates)
	}

	// 3. 仅 1 个非 shared 项目进入竞争窗口：锁定该项目并过滤其他非 shared 干扰切片，补足 Top 5
	singleWinner := detectCrossProjectAmbiguity([]retrieve.MergedChunk{
		makeChunk(1, "project-a", "项目 A", "A1", 0.72),
		makeChunk(2, "project-b", "项目 B", "B1", 0.55), // 0.72 - 0.55 = 0.17 > 0.08，窗口外干扰
		makeChunk(3, "shared", "公共规范库", "S1", 0.54),
		makeChunk(4, "project-a", "项目 A", "A2", 0.52),
		makeChunk(5, "project-b", "项目 B", "B2", 0.50),
		makeChunk(6, "project-a", "项目 A", "A3", 0.49),
		makeChunk(7, "project-a", "项目 A", "A4", 0.48),
	}, refuseThreshold, retrieve.TopN)
	if singleWinner.Ambiguity != nil || singleWinner.ProjectResolved != "project-a" {
		t.Fatalf("应锁定 project-a，实际：%+v", singleWinner)
	}
	if len(singleWinner.FilteredTop) != 5 {
		t.Fatalf("过滤干扰后应补足 5 条，实际 %d", len(singleWinner.FilteredTop))
	}
	for _, sc := range singleWinner.FilteredTop {
		if sc.Chunk.ProjectSlug == "project-b" {
			t.Fatalf("非竞争窗口的 project-b 干扰切片未被过滤：%+v", sc.Chunk)
		}
	}

	// 4. 全部命中 shared：resolved = "shared"，不触发歧义
	onlyShared := detectCrossProjectAmbiguity([]retrieve.MergedChunk{
		makeChunk(1, "shared", "公共规范库", "S1", 0.70),
		makeChunk(2, "shared", "公共规范库", "S2", 0.65),
	}, refuseThreshold, retrieve.TopN)
	if onlyShared.Ambiguity != nil || onlyShared.ProjectResolved != "shared" {
		t.Fatalf("纯 shared 应解析为 shared 且无歧义：%+v", onlyShared)
	}

	// 5. top1 为 shared 且存在多个窗口外非 shared 干扰项目：仅保留 shared，不混入多个非 shared 项目
	sharedDominant := detectCrossProjectAmbiguity([]retrieve.MergedChunk{
		makeChunk(1, "shared", "公共规范库", "S1", 0.78),
		makeChunk(2, "project-a", "项目 A", "A1", 0.50),
		makeChunk(3, "project-b", "项目 B", "B1", 0.48),
	}, refuseThreshold, retrieve.TopN)
	if sharedDominant.Ambiguity != nil || sharedDominant.ProjectResolved != "shared" || len(sharedDominant.FilteredTop) != 1 {
		t.Fatalf("shared 主导时不应混入多个窗口外非 shared 项目：%+v", sharedDominant)
	}
}

func TestBuildDeprecatedWarningsOnlyForCitedChunks(t *testing.T) {
	repID := int64(45)
	top := []retrieve.MergedChunk{
		{Chunk: store.Chunk{
			ID:        10,
			DocTitle:  "现行规范 v2",
			DocStatus: "active",
			DocOwner:  "张三",
		}},
		{Chunk: store.Chunk{
			ID:              11,
			DocTitle:        "旧版激励活动规则 v1",
			DocStatus:       "deprecated",
			DocOwner:        "李四",
			ReplacedByDocID: &repID,
			ReplacedByTitle: "新版激励活动规则 v2",
		}},
	}

	// 未引用 [2]（即使代码中出现 arr[2]）→ 不触发警告
	noWarn := buildDeprecatedWarnings("按现行规范执行 [1]。\n示例：`arr[2]`", top)
	if len(noWarn) != 0 {
		t.Fatalf("未在正文引用 deprecated 切片时不应产生警告，实际：%+v", noWarn)
	}

	// 引用了 [2] → 必须返回 deprecated_doc 警告与替代文档信息
	warns := buildDeprecatedWarnings("旧规则按 3 天周期结算 [2]。", top)
	if len(warns) != 1 {
		t.Fatalf("expected 1 warning, got %+v", warns)
	}
	w := warns[0]
	if w.Type != "deprecated_doc" || w.ChunkN != 2 || w.DocumentTitle != "旧版激励活动规则 v1" ||
		w.Owner != "李四" || w.ReplacedByDocID == nil || *w.ReplacedByDocID != 45 || w.ReplacedByTitle != "新版激励活动规则 v2" {
		t.Fatalf("warning fields mismatch: %+v", w)
	}
	if !strings.Contains(w.Message, "李四") || !strings.Contains(w.Message, "新版激励活动规则 v2") {
		t.Fatalf("warning message missing owner or replacement: %q", w.Message)
	}
}

func TestStage10MultiProjectAndWarningsMySQL(t *testing.T) {
	dsn := os.Getenv("KNOWLEDGE_QA_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("设置 KNOWLEDGE_QA_TEST_MYSQL_DSN 后运行阶段 10 MySQL 全链路集成测试")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("测试DSN格式不正确")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_]+_test$`).MatchString(cfg.DBName) {
		t.Fatal("测试库名必须为安全标识符且以 _test 结尾")
	}
	name := strings.TrimSuffix(cfg.DBName, "_test") + "_v2_stage10_test"
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

	var chatCalls atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/embeddings":
			var er struct {
				Input string `json:"input"`
			}
			_ = json.NewDecoder(r.Body).Decode(&er)
			vec := "[0.2,0.9,0]"
			switch {
			case strings.Contains(er.Input, "历史") || strings.Contains(er.Input, "奖励"):
				vec = "[0,0,1]"
			case strings.Contains(er.Input, "端口") || strings.Contains(er.Input, "8091") || strings.Contains(er.Input, "9090"):
				vec = "[1,0,0]"
			}
			_, _ = io.WriteString(w, fmt.Sprintf(`{"data":[{"embedding":%s}]}`, vec))
		case "/chat/completions":
			chatCalls.Add(1)
			var req struct {
				Messages []struct {
					Content string `json:"content"`
				} `json:"messages"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			prompt := req.Messages[0].Content
			var ans string
			switch {
			case strings.Contains(prompt, "8091") && !strings.Contains(prompt, "9090"):
				ans = "服务默认监听 8091 端口 [1]。"
			case strings.Contains(prompt, "9090") && !strings.Contains(prompt, "8091"):
				ans = "服务默认监听 9090 端口 [1]。"
			case strings.Contains(prompt, "历史奖励规则") || strings.Contains(prompt, "奖励"):
				ans = "旧版历史奖励为每单 50 积分 [1]。"
			default:
				ans = generate.RefuseAnswer
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

	createProj := func(slug, projName string) {
		resp, err := http.Post(srv.URL+"/api/projects", "application/json",
			strings.NewReader(fmt.Sprintf(`{"slug":%q,"name":%q}`, slug, projName)))
		if err != nil || resp.StatusCode != http.StatusCreated {
			t.Fatalf("create project %s failed: resp=%v err=%v", slug, resp, err)
		}
		resp.Body.Close()
	}
	uploadDoc := func(projectSlug, filename, title, content, level, owner string) int64 {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		_ = mw.WriteField("project_slug", projectSlug)
		_ = mw.WriteField("doc_level", level)
		_ = mw.WriteField("owner", owner)
		_ = mw.WriteField("title", title)
		fw, _ := mw.CreateFormFile("file", filename)
		_, _ = io.WriteString(fw, content)
		_ = mw.Close()
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/documents/upload", &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("upload %s failed: resp=%v err=%v", filename, resp, err)
		}
		defer resp.Body.Close()
		var res store.InsertDocResult
		_ = json.NewDecoder(resp.Body).Decode(&res)
		return res.DocumentID
	}

	// 创建空项目并测试空项目提示（此时 shared 也为空）
	createProj("empty-proj", "空项目")
	emptyAskResp, err := http.Post(srv.URL+"/api/ask", "application/json",
		strings.NewReader(`{"project_slug":"empty-proj","question":"服务默认监听什么端口？"}`))
	if err != nil || emptyAskResp.StatusCode != http.StatusOK {
		t.Fatalf("empty project ask failed: %v", err)
	}
	var emptyAsk askResponse
	_ = json.NewDecoder(emptyAskResp.Body).Decode(&emptyAsk)
	emptyAskResp.Body.Close()
	if !emptyAsk.Refused || !strings.Contains(emptyAsk.Answer, "暂无资料") || chatCalls.Load() != 0 {
		t.Fatalf("empty project should refuse cleanly without LLM call: %+v chats=%d", emptyAsk, chatCalls.Load())
	}

	// 不存在的项目返回 404
	notFoundResp, err := http.Post(srv.URL+"/api/ask", "application/json",
		strings.NewReader(`{"project_slug":"no-such-proj","question":"端口"}`))
	if err != nil || notFoundResp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown project expected 404, got %v err=%v", notFoundResp, err)
	}
	notFoundResp.Body.Close()

	// 准备 project-alpha（8091）与 project-beta（9090 + deprecated 旧规则 + 替代新规则）
	createProj("project-alpha", "项目 Alpha")
	createProj("project-beta", "项目 Beta")
	uploadDoc("project-alpha", "alpha-port.md", "Alpha 端口规范", "# 服务端口\n\nAlpha 服务默认监听端口为 8091。", "spec", "张三")
	uploadDoc("project-beta", "beta-port.md", "Beta 端口规范", "# 服务端口\n\nBeta 服务默认监听端口为 9090。", "spec", "李四")
	oldDocID := uploadDoc("project-beta", "legacy-reward.md", "旧版激励规则 v1", "# 历史奖励规则\n\n旧版历史奖励为每单 50 积分。", "spec", "李四")
	newDocID := uploadDoc("project-beta", "new-reward.md", "新版激励规则 v2", "# 新版激励规则\n\n新版改为按月度阶梯发放。", "spec", "王五")

	patchReq, _ := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/documents/%d", srv.URL, oldDocID),
		strings.NewReader(fmt.Sprintf(`{"status":"deprecated","replaced_by_doc_id":%d}`, newDocID)))
	patchReq.Header.Set("Content-Type", "application/json")
	patchResp, err := http.DefaultClient.Do(patchReq)
	if err != nil || patchResp.StatusCode != http.StatusOK {
		t.Fatalf("deprecate doc failed: %v", err)
	}
	patchResp.Body.Close()

	// 验收 2：硬隔离测试（指定 project-alpha）
	isoResp, err := http.Post(srv.URL+"/api/ask", "application/json",
		strings.NewReader(`{"project_slug":"project-alpha","question":"服务默认监听什么端口？"}`))
	if err != nil || isoResp.StatusCode != http.StatusOK {
		t.Fatalf("isolation ask failed: %v", err)
	}
	var iso askResponse
	_ = json.NewDecoder(isoResp.Body).Decode(&iso)
	isoResp.Body.Close()
	if iso.Refused || !strings.Contains(iso.Answer, "8091") || iso.ProjectResolved != "project-alpha" {
		t.Fatalf("hard isolation expected 8091 answer for project-alpha, got %+v", iso)
	}
	for _, cit := range iso.Citations {
		if cit.ProjectSlug == "project-beta" {
			t.Fatalf("hard isolation leaked project-beta citation: %+v", cit)
		}
	}

	// 验收 3：跨项目歧义拦截测试（auto 模式不调用 LLM）
	chatsBeforeAmb := chatCalls.Load()
	ambResp, err := http.Post(srv.URL+"/api/ask", "application/json",
		strings.NewReader(`{"project_slug":"","question":"服务默认监听什么端口？"}`))
	if err != nil || ambResp.StatusCode != http.StatusOK {
		t.Fatalf("ambiguity ask failed: %v", err)
	}
	var amb askResponse
	_ = json.NewDecoder(ambResp.Body).Decode(&amb)
	ambResp.Body.Close()
	if amb.Ambiguity == nil || !amb.Ambiguity.Detected || amb.ProjectResolved != "ambiguous" || len(amb.Ambiguity.Candidates) < 2 {
		t.Fatalf("expected cross-project ambiguity detected, got %+v", amb)
	}
	if chatCalls.Load() != chatsBeforeAmb {
		t.Fatalf("ambiguity interception must NOT call LLM: before=%d after=%d", chatsBeforeAmb, chatCalls.Load())
	}

	// 可选加分：compare=true 返回各项目对照回答
	cmpResp, err := http.Post(srv.URL+"/api/ask", "application/json",
		strings.NewReader(`{"project_slug":"auto","question":"服务默认监听什么端口？","compare":true}`))
	if err != nil || cmpResp.StatusCode != http.StatusOK {
		t.Fatalf("compare ask failed: %v", err)
	}
	var cmp askResponse
	_ = json.NewDecoder(cmpResp.Body).Decode(&cmp)
	cmpResp.Body.Close()
	if len(cmp.Comparisons) != 2 {
		t.Fatalf("expected 2 comparisons, got %+v", cmp)
	}

	// 验收 4：废弃文档警告测试
	depResp, err := http.Post(srv.URL+"/api/ask", "application/json",
		strings.NewReader(`{"project_slug":"project-beta","question":"旧版历史奖励规则是什么？"}`))
	if err != nil || depResp.StatusCode != http.StatusOK {
		t.Fatalf("deprecated ask failed: %v", err)
	}
	var dep askResponse
	_ = json.NewDecoder(depResp.Body).Decode(&dep)
	depResp.Body.Close()
	if dep.Refused || len(dep.Warnings) == 0 || dep.Warnings[0].Type != "deprecated_doc" ||
		dep.Warnings[0].ReplacedByTitle != "新版激励规则 v2" || dep.Warnings[0].Owner != "李四" {
		t.Fatalf("expected deprecated_doc warning with replacement doc info, got %+v", dep)
	}
	if len(dep.Citations) == 0 || dep.Citations[0].DocStatus != "deprecated" || dep.Citations[0].ProjectSlug != "project-beta" {
		t.Fatalf("citation missing Stage 10 metadata: %+v", dep.Citations)
	}
}

func TestCrossSpaceRefusalHintPreservesIsolation(t *testing.T) {
	h := &Handler{}
	qVec := []float64{1, 0, 0}

	makeChunk := func(id, docID int64, slug, name, heading, content string, vec []float64, hidden bool) store.Chunk {
		return store.Chunk{
			ID:            id,
			DocumentID:    docID,
			ProjectSlug:   slug,
			ProjectName:   name,
			ProjectHidden: hidden,
			HeadingPath:   heading,
			Content:       content,
			DocLevel:      "spec",
			DocStatus:     "active",
			Vec:           vec,
		}
	}

	// Case A: 锁定 project-alpha 时，命中的资料只在 project-beta -> 保持 refused=true，并提示切换 project-beta
	chunksA := []store.Chunk{
		makeChunk(1, 10, "project-alpha", "项目 Alpha", "Alpha 指南", "与网文小说激励有关。", []float64{0, 1, 0}, false),
		makeChunk(2, 20, "project-beta", "项目 Beta", "Beta 活动发奖", "新版活动发奖规则在结束后 T+5 个工作日内完成打款，服务默认端口 8082。", []float64{1, 0, 0}, false),
	}
	respA := askResponse{Answer: generate.RefuseAnswer, Citations: []citation{}, Refused: true}
	h.enrichCrossSpaceRefusalHint("新版活动发奖规则是什么？", qVec, chunksA, "project-alpha", nil, &respA)
	if !respA.Refused || len(respA.Citations) != 0 {
		t.Fatalf("Case A 必须严格守住拒答隔离，不应夹带其他空间答案或引用：%+v", respA)
	}
	if respA.SuggestedProjectSlug != "project-beta" || !strings.Contains(respA.RefusalHint, "项目 Beta") {
		t.Fatalf("Case A 期望提示切换至 project-beta，实际：%+v", respA)
	}

	// Case B: 锁定 project-alpha 时，project-beta 与 project-gamma 同时高分命中 -> 建议切回 auto 对照
	chunksB := []store.Chunk{
		makeChunk(1, 10, "project-alpha", "项目 Alpha", "Alpha 指南", "无关内容", []float64{0, 1, 0}, false),
		makeChunk(2, 20, "project-beta", "项目 Beta", "Beta 默认端口", "服务默认监听端口为 8082", []float64{1, 0, 0}, false),
		makeChunk(3, 30, "project-gamma", "项目 Gamma", "Gamma 默认端口", "服务默认监听端口为 8085", []float64{0.99, 0.05, 0}, false),
	}
	respB := askResponse{Answer: generate.RefuseAnswer, Citations: []citation{}, Refused: true}
	h.enrichCrossSpaceRefusalHint("服务默认监听端口是多少？", qVec, chunksB, "project-alpha", nil, &respB)
	if respB.SuggestedProjectSlug != "auto" || !strings.Contains(respB.RefusalHint, "自动识别") {
		t.Fatalf("Case B 期望建议切回 auto 歧义对照，实际：%+v", respB)
	}

	// Case C: 其他空间命中项属于隐藏评测夹具（ProjectHidden=true）-> 绝不泄露提示
	chunksC := []store.Chunk{
		makeChunk(1, 10, "project-alpha", "项目 Alpha", "Alpha 指南", "无关内容", []float64{0, 1, 0}, false),
		makeChunk(2, 99, "hidden-fixture", "隐藏评测夹具", "隐藏默认端口", "隐藏项目服务默认监听端口 8080", []float64{1, 0, 0}, true),
	}
	respC := askResponse{Answer: generate.RefuseAnswer, Citations: []citation{}, Refused: true}
	h.enrichCrossSpaceRefusalHint("隐藏服务默认监听端口是多少？", qVec, chunksC, "project-alpha", nil, &respC)
	if respC.SuggestedProjectSlug != "" || respC.RefusalHint != "" {
		t.Fatalf("Case C 不应提示隐藏评测项目，实际：%+v", respC)
	}

	// Case D: 全库均无依据 -> 保持普通拒答，不给出建议按钮
	chunksD := []store.Chunk{
		makeChunk(1, 10, "project-alpha", "项目 Alpha", "Alpha 指南", "运营规则说明", []float64{0, 0, 1}, false),
		makeChunk(2, 20, "project-beta", "项目 Beta", "Beta 指南", "版权结算规则", []float64{0, 1, 0}, false),
	}
	respD := askResponse{Answer: generate.RefuseAnswer, Citations: []citation{}, Refused: true}
	h.enrichCrossSpaceRefusalHint("怎么做法式红酒炖牛肉？", qVec, chunksD, "project-alpha", nil, &respD)
	if respD.SuggestedProjectSlug != "" || respD.RefusalHint != "" {
		t.Fatalf("Case D 全库无依据不应给出跨空间建议，实际：%+v", respD)
	}
}
