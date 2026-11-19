package store

import (
	"database/sql"
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
)

func TestNormalizeGapQuestionAndSlug(t *testing.T) {
	if got := NormalizeGapProjectSlug(""); got != "auto" {
		t.Fatalf("empty slug -> %q, want auto", got)
	}
	if got := NormalizeGapProjectSlug("ALL"); got != "auto" {
		t.Fatalf("ALL slug -> %q, want auto", got)
	}
	if got := NormalizeGapProjectSlug("go-gin"); got != "go-gin" {
		t.Fatalf("go-gin slug -> %q, want go-gin", got)
	}

	n1, k1 := NormalizeGapQuestion("项目 B 报销审批单去哪里领？")
	n2, k2 := NormalizeGapQuestion("  项目   b   报销审批单去哪里领?! ")
	n3, k3 := NormalizeGapQuestion("项目 b 报销审批单去哪里领。")
	if n1 != "项目 b 报销审批单去哪里领" || n1 != n2 || n1 != n3 {
		t.Fatalf("normalized mismatch: %q / %q / %q", n1, n2, n3)
	}
	if len(k1) != 64 || k1 != k2 || k1 != k3 {
		t.Fatalf("key mismatch: %q / %q / %q", k1, k2, k3)
	}
}

func TestMatchReboundChunkScenarios(t *testing.T) {
	newChunks := []chunkSnapshot{
		{ID: 101, HeadingPath: "服务规范 > 端口", Content: "服务默认监听 8092 端口，支持通过环境变量覆盖。"},
		{ID: 102, HeadingPath: "服务规范 > 鉴权", Content: "所有内部接口必须携带 X-Token 请求头进行签名校验。"},
		{ID: 103, HeadingPath: "新版目录 > 路由组", Content: "使用 r.Group(\"/v1\") 可以把同一前缀的路由和中间件归组管理。"},
	}

	// 1. 同标题且内容小幅改动 -> 匹配 101
	id1, reason1 := matchReboundChunk(
		EvalQuestion{Question: "默认端口是多少？", GoldAnswer: "8091"},
		"服务规范 > 端口",
		"服务默认监听 8091 端口，支持通过环境变量覆盖。",
		newChunks,
	)
	if id1 == nil || *id1 != 101 || reason1 != "" {
		t.Fatalf("expected match 101, got id=%v reason=%q", id1, reason1)
	}

	// 2. 标题改名但正文基本保留 -> 跨标题匹配 103
	id2, reason2 := matchReboundChunk(
		EvalQuestion{Question: "怎么建路由组？", GoldAnswer: "使用 r.Group"},
		"Gin > 旧路由组",
		"使用 r.Group(\"/v1\") 可以把同一前缀的路由和中间件归组管理，并统一配置鉴权。",
		newChunks,
	)
	if id2 == nil || *id2 != 103 || reason2 != "" {
		t.Fatalf("expected cross-heading match 103, got id=%v reason=%q", id2, reason2)
	}

	// 3. 段落被删除或彻底重写 -> nil (needs_review)
	id3, reason3 := matchReboundChunk(
		EvalQuestion{Question: "灰度发布比例怎么配？", GoldAnswer: "按 10% 放量"},
		"服务规范 > 灰度发布",
		"灰度发布默认先放量 10%，观察 30 分钟后全量。",
		newChunks,
	)
	if id3 != nil || reason3 == "" {
		t.Fatalf("deleted section should return nil (needs_review), got id=%v reason=%q", id3, reason3)
	}
}

func TestStage12CollaborationStoreMySQL(t *testing.T) {
	dsn := os.Getenv("KNOWLEDGE_QA_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("设置 KNOWLEDGE_QA_TEST_MYSQL_DSN 后运行阶段 12 Store MySQL 集成测试")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("测试DSN格式不正确")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_]+_test$`).MatchString(cfg.DBName) {
		t.Fatal("测试库名必须为安全标识符且以 _test 结尾")
	}
	name := strings.TrimSuffix(cfg.DBName, "_test") + "_v3_collab_store_test"
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
	applyV2SchemaForTest(t, st.db, string(raw002))

	if _, err := st.CreateProject("promo", "运营活动", ""); err != nil {
		t.Fatal(err)
	}
	docRes, err := st.InsertOrReplaceDocument(InsertDocParams{
		ProjectSlug: "promo",
		Title:       "活动规则手册",
		Filename:    "promo-guide.md",
		Content:     "# 活动规则\n\n## 发奖周期\n\n活动结束后 3 个工作日发奖。\n\n## 联系值班\n\n找值班同学处理。",
		SourceType:  "md",
		ContentHash: SHA256Hex([]byte("promo-v1")),
		Owner:       "运营小王",
		Chunks: []ChunkInput{
			{HeadingPath: "活动规则 > 发奖周期", Content: "活动结束后 3 个工作日发奖。", Embedding: "[1,0]"},
			{HeadingPath: "活动规则 > 联系值班", Content: "找值班同学处理。", Embedding: "[0,1]"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	chunk1ID, err := st.FindGoldChunkInProject("promo", "promo-guide.md", "活动规则 > 发奖周期", "3 个工作日")
	if err != nil {
		t.Fatal(err)
	}
	chunk2ID, err := st.FindGoldChunkInProject("promo", "promo-guide.md", "活动规则 > 联系值班", "值班同学")
	if err != nil {
		t.Fatal(err)
	}

	// 1. knowledge_gaps 去重聚合与排序
	if err := st.RecordKnowledgeGap(RecordGapParams{
		ProjectSlug:    "",
		Question:       "项目 B 报销审批单去哪里领？",
		RefuseReason:   "threshold",
		Top1Final:      0.21,
		NearestChunkID: &chunk1ID,
		NearestHeading: "活动规则 > 发奖周期",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordKnowledgeGap(RecordGapParams{
		ProjectSlug:    "auto",
		Question:       "项目 b 报销审批单去哪里领! ",
		RefuseReason:   "threshold",
		Top1Final:      0.24,
		NearestChunkID: &chunk1ID,
		NearestHeading: "活动规则 > 发奖周期",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordKnowledgeGap(RecordGapParams{
		ProjectSlug:  "promo",
		Question:     "测试第二个低频盲区",
		RefuseReason: "model_refusal",
		Top1Final:    0.55,
	}); err != nil {
		t.Fatal(err)
	}

	gaps, err := st.ListKnowledgeGaps("", "open")
	if err != nil || len(gaps) != 2 {
		t.Fatalf("expected 2 open gaps, got %+v err=%v", gaps, err)
	}
	if gaps[0].HitCount != 2 || gaps[0].ProjectSlug != "auto" || gaps[0].NearestDocTitle != "活动规则手册" || gaps[0].NearestDocOwner != "运营小王" {
		t.Fatalf("top gap mismatch: %+v", gaps[0])
	}

	// resolved 后复发应自动重开为 open；ignored 保持忽略
	resolvedGap, err := st.UpdateKnowledgeGapStatus(gaps[0].ID, "resolved")
	if err != nil || resolvedGap.Status != "resolved" {
		t.Fatalf("UpdateKnowledgeGapStatus resolved failed: %+v err=%v", resolvedGap, err)
	}
	if err := st.RecordKnowledgeGap(RecordGapParams{
		ProjectSlug:  "auto",
		Question:     "项目 B 报销审批单去哪里领？",
		RefuseReason: "threshold",
		Top1Final:    0.20,
	}); err != nil {
		t.Fatal(err)
	}
	reopened, err := st.GetKnowledgeGap(gaps[0].ID)
	if err != nil || reopened.Status != "open" || reopened.HitCount != 3 {
		t.Fatalf("resolved gap should reopen on recurrence: %+v err=%v", reopened, err)
	}
	if _, err := st.UpdateKnowledgeGapStatus(gaps[1].ID, "ignored"); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordKnowledgeGap(RecordGapParams{
		ProjectSlug:  "promo",
		Question:     "测试第二个低频盲区",
		RefuseReason: "model_refusal",
		Top1Final:    0.52,
	}); err != nil {
		t.Fatal(err)
	}
	ignoredGap, err := st.GetKnowledgeGap(gaps[1].ID)
	if err != nil || ignoredGap.Status != "ignored" || ignoredGap.HitCount != 2 {
		t.Fatalf("ignored gap should stay ignored: %+v err=%v", ignoredGap, err)
	}

	// 2. eval_feedback 创建、排序与事务转正
	fbUp, err := st.CreateFeedback(CreateFeedbackParams{
		ProjectSlug:       "promo",
		Question:          "发奖要多久？",
		RetrievedChunkIDs: []int64{chunk1ID},
		Answer:            "3 个工作日 [1]。",
		Refused:           false,
		Vote:              "up",
	})
	if err != nil {
		t.Fatal(err)
	}
	fbDown, err := st.CreateFeedback(CreateFeedbackParams{
		ProjectSlug:       "promo",
		Question:          "值班联系谁？",
		RetrievedChunkIDs: []int64{chunk1ID, chunk2ID},
		Answer:            "未找到",
		Refused:           true,
		Vote:              "down",
		Comment:           "文档写了找值班同学",
	})
	if err != nil {
		t.Fatal(err)
	}
	fbList, err := st.ListFeedback("", "")
	if err != nil || len(fbList) != 2 || fbList[0].ID != fbDown.ID || len(fbList[0].CandidateChunks) != 2 {
		t.Fatalf("ListFeedback should prioritize down vote and attach chunk meta: %+v err=%v", fbList, err)
	}

	if _, err := st.PromoteFeedback(fbDown.ID, PromoteFeedbackParams{
		GoldAnswer:   "找不到切片",
		ShouldRefuse: false,
		GoldChunkID:  func() *int64 { x := int64(999999); return &x }(),
	}); !errors.Is(err, ErrGoldChunkNotFound) {
		t.Fatalf("expected ErrGoldChunkNotFound, got %v", err)
	}

	promoRes, err := st.PromoteFeedback(fbDown.ID, PromoteFeedbackParams{
		GoldAnswer:   "找值班同学处理。",
		ShouldRefuse: false,
		GoldChunkID:  &chunk2ID,
		Category:     "ops",
	})
	if err != nil || promoRes.PromotedQuestionID <= 0 || promoRes.Question.QuestionSet != "feedback" {
		t.Fatalf("PromoteFeedback failed: %+v err=%v", promoRes, err)
	}
	if promoRes.Question.SourceDocID == nil || *promoRes.Question.SourceDocID != docRes.DocumentID {
		t.Fatalf("expected source_doc_id=%d, got %+v", docRes.DocumentID, promoRes.Question.SourceDocID)
	}
	if _, err := st.PromoteFeedback(fbDown.ID, PromoteFeedbackParams{ShouldRefuse: true}); !errors.Is(err, ErrFeedbackAlreadyPromoted) {
		t.Fatalf("expected ErrFeedbackAlreadyPromoted, got %v", err)
	}
	// 同名问题再次反馈后尝试转正应返回 ErrDuplicateEvalQuestion
	fbDup, err := st.CreateFeedback(CreateFeedbackParams{
		ProjectSlug: "promo",
		Question:    "值班联系谁？",
		Vote:        "down",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.PromoteFeedback(fbDup.ID, PromoteFeedbackParams{ShouldRefuse: true}); !errors.Is(err, ErrDuplicateEvalQuestion) {
		t.Fatalf("expected ErrDuplicateEvalQuestion, got %v", err)
	}
	// fbUp 仅作未转正占位检查（防止 unused 警告并验证顺序变化）
	fbListAfter, err := st.ListFeedback("", "")
	if err != nil || fbListAfter[0].ID != fbUp.ID && fbListAfter[0].ID != fbDup.ID {
		t.Fatalf("unpromoted feedback should stay ahead of promoted ones: %+v", fbListAfter)
	}

	// 3. Doc CI/CD 重绑：插入一条仅靠 gold_chunk_id 关联（source_doc_id = NULL，类似 base52）的题
	if _, err := st.db.Exec(`INSERT INTO eval_questions
		(question, gold_answer, should_refuse, gold_chunk_id, project_slug, question_set, source_doc_id, category)
		VALUES ('发奖周期是几天？', '5 个工作日发奖', 0, ?, 'promo', 'base52', NULL, '')`, chunk1ID); err != nil {
		t.Fatal(err)
	}

	// 覆写文档：修改 chunk1 为 5 个工作日，删除 chunk2（值班段落）
	overRes, err := st.InsertOrReplaceDocument(InsertDocParams{
		ProjectSlug: "promo",
		Title:       "活动规则手册",
		Filename:    "promo-guide.md",
		Content:     "# 活动规则\n\n## 发奖周期\n\n活动结束后 5 个工作日发奖。",
		SourceType:  "md",
		ContentHash: SHA256Hex([]byte("promo-v2")),
		Overwrite:   true,
		Chunks: []ChunkInput{
			{HeadingPath: "活动规则 > 发奖周期", Content: "活动结束后 5 个工作日发奖。", Embedding: "[1,0]"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !overRes.Replaced || len(overRes.AffectedItems) != 2 {
		t.Fatalf("expected 2 affected items on overwrite, got %+v", overRes)
	}

	var reboundBase, reviewFeedback bool
	for _, it := range overRes.AffectedItems {
		if it.Question.Question == "发奖周期是几天？" {
			reboundBase = !it.NeedsReview && it.NewGoldChunkID != nil && *it.NewGoldChunkID != chunk1ID
		}
		if it.Question.Question == "值班联系谁？" {
			reviewFeedback = it.NeedsReview && it.NewGoldChunkID == nil
		}
	}
	if !reboundBase || !reviewFeedback {
		t.Fatalf("unexpected rebind results: %+v", overRes.AffectedItems)
	}

	// 验证 needs_review 题在 DB 中已补全 source_doc_id 且 gold_chunk_id = NULL
	var dbGold sql.NullInt64
	var dbSrc sql.NullInt64
	if err := st.db.QueryRow("SELECT gold_chunk_id, source_doc_id FROM eval_questions WHERE id = ?", promoRes.PromotedQuestionID).Scan(&dbGold, &dbSrc); err != nil {
		t.Fatal(err)
	}
	if dbGold.Valid || !dbSrc.Valid || dbSrc.Int64 != docRes.DocumentID {
		t.Fatalf("expected gold_chunk_id=NULL and source_doc_id=%d, got gold=%+v src=%+v", docRes.DocumentID, dbGold, dbSrc)
	}

	// 再次覆写把“联系值班”段落加回来，应自动从 needs_review 重新绑定成功
	overRes2, err := st.InsertOrReplaceDocument(InsertDocParams{
		ProjectSlug: "promo",
		Title:       "活动规则手册",
		Filename:    "promo-guide.md",
		Content:     "# 活动规则\n\n## 发奖周期\n\n活动结束后 5 个工作日发奖。\n\n## 联系值班\n\n找值班同学处理。",
		SourceType:  "md",
		ContentHash: SHA256Hex([]byte("promo-v3")),
		Overwrite:   true,
		Chunks: []ChunkInput{
			{HeadingPath: "活动规则 > 发奖周期", Content: "活动结束后 5 个工作日发奖。", Embedding: "[1,0]"},
			{HeadingPath: "活动规则 > 联系值班", Content: "找值班同学处理。", Embedding: "[0,1]"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var restoredFeedback bool
	for _, it := range overRes2.AffectedItems {
		if it.Question.Question == "值班联系谁？" && !it.NeedsReview && it.NewGoldChunkID != nil {
			restoredFeedback = true
		}
	}
	if !restoredFeedback {
		t.Fatalf("expected needs_review question to auto-rebind when section restored: %+v", overRes2.AffectedItems)
	}
}
