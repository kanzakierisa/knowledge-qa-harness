package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
)

func sampleEvalQuestions(chunkID int64) []EvalQuestion {
	questions := make([]EvalQuestion, 25)
	for i := range questions {
		questions[i] = EvalQuestion{Question: fmt.Sprintf("问题 %d", i+1), GoldAnswer: "参考答案"}
		if i < 18 {
			id := chunkID
			questions[i].GoldChunkID = &id
		} else {
			questions[i].ShouldRefuse = true
		}
	}
	return questions
}

func TestSeedRejectsInvalidGoldBeforeWriting(t *testing.T) {
	if err := validateEvalSeed(sampleEvalQuestions(1)); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func([]EvalQuestion) []EvalQuestion{
		"数量不够": func(q []EvalQuestion) []EvalQuestion { return q[:24] },
		"拒答比例错误": func(q []EvalQuestion) []EvalQuestion {
			q[0].ShouldRefuse, q[0].GoldChunkID = true, nil
			return q
		},
		"缺少金标切片": func(q []EvalQuestion) []EvalQuestion { q[0].GoldChunkID = nil; return q },
		"拒答却带切片": func(q []EvalQuestion) []EvalQuestion { q[24].GoldChunkID = q[0].GoldChunkID; return q },
		"重复问题":   func(q []EvalQuestion) []EvalQuestion { q[1].Question = q[0].Question; return q },
		"缺少参考答案": func(q []EvalQuestion) []EvalQuestion { q[0].GoldAnswer = " "; return q },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			if err := validateEvalSeed(mutate(sampleEvalQuestions(1))); err == nil {
				t.Fatal("不合法的金标题不能写入数据库")
			}
		})
	}
}

// 显式启用的真实MySQL测试使用全新_test库，生产库不能作为测试目标。
func TestEvalPersistenceMySQL(t *testing.T) {
	dsn := os.Getenv("KNOWLEDGE_QA_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("设置 KNOWLEDGE_QA_TEST_MYSQL_DSN 后运行真实 MySQL 事务验收")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("测试DSN格式不正确")
	}
	name := cfg.DBName
	if !regexp.MustCompile(`^[A-Za-z0-9_]+_test$`).MatchString(name) {
		t.Fatal("测试库名必须为安全标识符且以 _test 结尾")
	}
	cfg.DBName = ""
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	// 不使用IF NOT EXISTS：已存在的库一律不进入，避免误删既有测试数据。
	if _, err := admin.Exec("CREATE DATABASE `" + name + "` DEFAULT CHARSET utf8mb4"); err != nil {
		t.Fatalf("只能使用一个原本不存在的专用测试库：%v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP DATABASE `" + name + "`"); err != nil {
			t.Errorf("清理专用测试库失败：%v", err)
		}
	})
	st, err := New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	raw, err := os.ReadFile("../migrations/001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(raw), ";") {
		// 跳过迁移里的CREATE DATABASE和USE，仅在测试连接中创建相同的四张表。
		if strings.Contains(statement, "CREATE TABLE") {
			if _, err := st.db.Exec(statement); err != nil {
				t.Fatal(err)
			}
		}
	}
	docID, err := st.InsertDocument("测试文档", "fixture.md", "测试原文")
	if err != nil {
		t.Fatal(err)
	}
	chunkID, err := st.InsertChunk(docID, "测试 > 标题", "唯一锚点：真实切片", "[1,0]")
	if err != nil {
		t.Fatal(err)
	}
	found, err := st.FindGoldChunk("fixture.md", "测试 > 标题", "唯一锚点")
	if err != nil || found != chunkID {
		t.Fatalf("真实切片解析错误：found=%d err=%v", found, err)
	}
	if _, err := st.FindGoldChunk("fixture.md", "测试 > 标题", "没有的锚点"); err == nil {
		t.Fatal("没有命中的切片不能当金标")
	}
	questions := sampleEvalQuestions(chunkID)
	for i := 0; i < 2; i++ {
		if err := st.SeedEvalQuestions(questions); err != nil {
			t.Fatal(err)
		}
	}
	stored, err := st.AllEvalQuestions()
	if err != nil || len(stored) != 25 {
		t.Fatalf("重复seed必须仍为25题：len=%d err=%v", len(stored), err)
	}
	questions[0].GoldAnswer = "企图覆盖已有金标"
	if err := st.SeedEvalQuestions(questions); err == nil {
		t.Fatal("不允许覆盖已确认金标")
	}
	storedAgain, err := st.AllEvalQuestions()
	if err != nil || storedAgain[0].GoldAnswer != stored[0].GoldAnswer {
		t.Fatal("拒绝覆盖后必须保留原金标")
	}
	q := stored[0]
	good := EvalRun{QuestionID: q.ID, Question: q.Question, GoldAnswer: q.GoldAnswer,
		GoldChunkID: q.GoldChunkID, RetrievedChunkIDs: []int64{chunkID}, Answer: "回答 [1]。"}
	bad := good
	bad.QuestionID = 9223372036854775807
	if _, err := st.SaveEvalRuns([]EvalRun{good, bad}); err == nil {
		t.Fatal("外键失败必须使整批失败")
	}
	var count int
	if err := st.db.QueryRow("SELECT COUNT(*) FROM eval_runs").Scan(&count); err != nil || count != 0 {
		t.Fatalf("批次失败不能留下第一条：count=%d err=%v", count, err)
	}
	refused := EvalRun{QuestionID: stored[24].ID, ShouldRefuse: true, Refused: true, Answer: "文档中没有依据"}
	saved, err := st.SaveEvalRuns([]EvalRun{good, refused})
	if err != nil {
		t.Fatal(err)
	}
	ids := []int64{saved[0].ID, saved[1].ID}
	loaded, err := st.LoadEvalRuns(ids)
	if err != nil || len(loaded) != 2 || loaded[0].Usable != nil || loaded[1].CitationOK != nil {
		t.Fatalf("初始标注必须为NULL：%+v err=%v", loaded, err)
	}
	for i := 0; i < 2; i++ {
		if err := st.MarkEvalRun(saved[0].ID, false); err != nil {
			t.Fatalf("false及重复相同标注都必须成功：%v", err)
		}
	}
	loaded, err = st.LoadEvalRuns(ids)
	if err != nil || loaded[0].Usable == nil || *loaded[0].Usable {
		t.Fatalf("false不能被当作未标注：%+v err=%v", loaded, err)
	}
	if err := st.MarkEvalRun(saved[0].ID, true); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkEvalRun(saved[1].ID, true); !errors.Is(err, ErrEvalRunRefused) {
		t.Fatalf("拒答行应报不适用：%v", err)
	}
	if err := st.MarkEvalRun(9223372036854775807, false); !errors.Is(err, ErrEvalRunNotFound) {
		t.Fatalf("不存在的run_id应区分：%v", err)
	}
	if _, err := st.SaveEvalRuns([]EvalRun{good}); err != nil {
		t.Fatal(err)
	}
	loaded, err = st.LoadEvalRuns(ids)
	if err != nil || len(loaded) != 2 || loaded[0].Usable == nil || !*loaded[0].Usable {
		t.Fatalf("读取指定ID不能混入新批次：%+v err=%v", loaded, err)
	}
	// 验证批次隔离：第二批插入包含多题且标注 false，EvalUsableSummaryForRun 只统计对应批次，不与第一批的 true 混算
	good2 := EvalRun{QuestionID: stored[1].ID, Question: stored[1].Question, GoldAnswer: stored[1].GoldAnswer,
		GoldChunkID: stored[1].GoldChunkID, RetrievedChunkIDs: []int64{chunkID}, Answer: "回答 [1]。"}
	batch2, err := st.SaveEvalRuns([]EvalRun{good, good2})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.MarkEvalRun(batch2[0].ID, false); err != nil {
		t.Fatal(err)
	}
	m1, u1, err := st.EvalUsableSummaryForRun(saved[0].ID)
	if err != nil || m1 != 1 || u1 != 1 {
		t.Fatalf("第一批应保持 1/1，得到 %d/%d err=%v", u1, m1, err)
	}
	m2, u2, err := st.EvalUsableSummaryForRun(batch2[0].ID)
	if err != nil || m2 != 1 || u2 != 0 {
		t.Fatalf("第二批应只计入本批 0/1，不应混入第一批：得到 %d/%d err=%v", u2, m2, err)
	}
	if err := st.MarkEvalRun(batch2[1].ID, true); err != nil {
		t.Fatal(err)
	}
	m2Full, u2Full, err := st.EvalUsableSummaryForRun(batch2[0].ID)
	if err != nil || m2Full != 2 || u2Full != 1 {
		t.Fatalf("末批前半段 ID 查询应包含同批后续已标注记录，得到 %d/%d err=%v", u2Full, m2Full, err)
	}
	if err := st.SeedEvalQuestions(sampleEvalQuestions(chunkID)); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRow("SELECT COUNT(*) FROM eval_runs").Scan(&count); err != nil || count != 5 {
		t.Fatalf("重复seed不能删除历史评测：count=%d err=%v", count, err)
	}

	// 阶段 6 增量追加：不破坏原 25 题与历史记录，支持幂等追加并拒绝覆盖已有题目。
	extra := []EvalQuestion{
		{Question: stored[0].Question, GoldAnswer: stored[0].GoldAnswer, ShouldRefuse: stored[0].ShouldRefuse, GoldChunkID: stored[0].GoldChunkID},
		{Question: "阶段 6 增量作答题", GoldAnswer: "增量参考答案", ShouldRefuse: false, GoldChunkID: &chunkID},
		{Question: "阶段 6 增量拒答题", GoldAnswer: "文档中没有依据", ShouldRefuse: true},
	}
	inserted, err := st.AppendEvalQuestions(extra)
	if err != nil || inserted != 2 {
		t.Fatalf("增量追加应新增 2 题，得到 inserted=%d err=%v", inserted, err)
	}
	inserted2, err := st.AppendEvalQuestions(extra)
	if err != nil || inserted2 != 0 {
		t.Fatalf("重复增量追加应幂等跳过，得到 inserted=%d err=%v", inserted2, err)
	}
	storedAfter, err := st.AllEvalQuestions()
	if err != nil || len(storedAfter) != 27 {
		t.Fatalf("追加后应为 27 题，得到 len=%d err=%v", len(storedAfter), err)
	}
	if storedAfter[0].ID != stored[0].ID || storedAfter[0].GoldAnswer != stored[0].GoldAnswer {
		t.Fatal("增量追加不得改动原 25 题")
	}
	if err := st.db.QueryRow("SELECT COUNT(*) FROM eval_runs").Scan(&count); err != nil || count != 5 {
		t.Fatalf("增量追加不得删除历史评测：count=%d err=%v", count, err)
	}
	if _, err := st.AppendEvalQuestions([]EvalQuestion{{Question: "阶段 6 增量作答题", GoldAnswer: "试图覆盖", GoldChunkID: &chunkID}}); err == nil {
		t.Fatal("增量追加不允许覆盖已有题目")
	}
}

func TestValidateEvalItemsAllowsAnyStage6Count(t *testing.T) {
	id := int64(1)
	refusals, err := validateEvalItems([]EvalQuestion{
		{Question: "q1", GoldAnswer: "a1", GoldChunkID: &id},
		{Question: "q2", GoldAnswer: "文档中没有依据", ShouldRefuse: true},
	})
	if err != nil || refusals != 1 {
		t.Fatalf("任意合法增量题组应通过校验：refusals=%d err=%v", refusals, err)
	}
	if _, err := validateEvalItems(nil); err == nil {
		t.Fatal("空列表不能通过校验")
	}
}

func TestEvalV2SetAndBatchMySQL(t *testing.T) {
	dsn := os.Getenv("KNOWLEDGE_QA_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("设置 KNOWLEDGE_QA_TEST_MYSQL_DSN 后运行 V2 评测集合与批次隔离 MySQL 集成测试")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("测试DSN格式不正确")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_]+_test$`).MatchString(cfg.DBName) {
		t.Fatal("测试库名必须为安全标识符且以 _test 结尾")
	}
	name := strings.TrimSuffix(cfg.DBName, "_test") + "_v2_eval_store_test"
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

	if _, err := st.CreateProject("alpha", "Alpha", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateProject("beta", "Beta", ""); err != nil {
		t.Fatal(err)
	}
	resA, err := st.InsertOrReplaceDocument(InsertDocParams{
		ProjectSlug: "alpha",
		Filename:    "service.md",
		Content:     "Alpha 端口 8091",
		SourceType:  "md",
		ContentHash: SHA256Hex([]byte("alpha-svc")),
		Chunks:      []ChunkInput{{HeadingPath: "Port > Config", Content: "Alpha 端口 8091", Embedding: "[1,0]"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	resB, err := st.InsertOrReplaceDocument(InsertDocParams{
		ProjectSlug: "beta",
		Filename:    "service.md",
		Content:     "Beta 端口 9090",
		SourceType:  "md",
		ContentHash: SHA256Hex([]byte("beta-svc")),
		Chunks:      []ChunkInput{{HeadingPath: "Port > Config", Content: "Beta 端口 9090", Embedding: "[1,0]"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	alphaChunkID, alphaDocID, err := st.FindGoldChunkWithDoc("alpha", "service.md", "Port > Config", "Alpha 端口 8091")
	if err != nil || alphaDocID != resA.DocumentID {
		t.Fatalf("FindGoldChunkWithDoc alpha failed: chunk=%d doc=%d err=%v", alphaChunkID, alphaDocID, err)
	}
	betaChunkID, err := st.FindGoldChunkInProject("beta", "service.md", "Port > Config", "Beta 端口 9090")
	if err != nil || betaChunkID == alphaChunkID {
		t.Fatalf("FindGoldChunkInProject beta should isolate from alpha: beta=%d alpha=%d err=%v", betaChunkID, alphaChunkID, err)
	}
	if _, err := st.FindGoldChunkInProject("alpha", "service.md", "Port > Config", "Beta 端口 9090"); err == nil {
		t.Fatal("FindGoldChunkInProject must not cross project boundaries")
	}

	inserted, err := st.AppendEvalQuestions([]EvalQuestion{
		{
			Question:    "base52 测试题",
			GoldAnswer:  "Alpha 端口 8091",
			GoldChunkID: &alphaChunkID,
			ProjectSlug: "alpha",
			QuestionSet: "base52",
		},
		{
			Question:    "holdout 测试题",
			GoldAnswer:  "Beta 端口 9090",
			GoldChunkID: &betaChunkID,
			ProjectSlug: "beta",
			QuestionSet: "holdout",
			SourceDocID: &resB.DocumentID,
			Category:    "project_governance",
		},
		{
			Question:     "holdout 高分陷阱题",
			GoldAnswer:   "文档中没有依据",
			ShouldRefuse: true,
			QuestionSet:  "holdout",
			Category:     "near_miss_refusal",
		},
	})
	if err != nil || inserted != 3 {
		t.Fatalf("expected 3 inserted eval questions, got %d err=%v", inserted, err)
	}

	if qs, err := st.EvalQuestionsBySet("base52"); err != nil || len(qs) != 1 || qs[0].ProjectSlug != "alpha" {
		t.Fatalf("base52 filter mismatch: %+v err=%v", qs, err)
	}
	if qs, err := st.EvalQuestionsBySet("holdout"); err != nil || len(qs) != 2 || qs[0].Category != "project_governance" || qs[1].Category != "near_miss_refusal" {
		t.Fatalf("holdout filter mismatch: %+v err=%v", qs, err)
	}
	if qs, err := st.EvalQuestionsBySet("feedback"); err != nil || len(qs) != 0 {
		t.Fatalf("feedback filter should be empty: %+v err=%v", qs, err)
	}
	if qs, err := st.EvalQuestionsBySet("all"); err != nil || len(qs) != 3 {
		t.Fatalf("all filter mismatch: %+v err=%v", qs, err)
	}
	if _, err := st.EvalQuestionsBySet("unknown"); !errors.Is(err, ErrInvalidEvalSet) {
		t.Fatalf("expected ErrInvalidEvalSet, got %v", err)
	}

	hit := true
	citOK := true
	baseRuns, err := st.SaveEvalRuns([]EvalRun{{
		Question:          "base52 测试题",
		QuestionID:        1,
		GoldAnswer:        "Alpha 端口 8091",
		GoldChunkID:       &alphaChunkID,
		ProjectSlug:       "alpha",
		QuestionSet:       "base52",
		BatchID:           "batch-base52-1",
		RetrievedChunkIDs: []int64{alphaChunkID},
		Answer:            "Alpha 端口 8091 [1]",
		Hit:               &hit,
		CitationOK:        &citOK,
	}})
	if err != nil || len(baseRuns) != 1 {
		t.Fatalf("SaveEvalRuns base52 failed: %+v err=%v", baseRuns, err)
	}
	holdRuns, err := st.SaveEvalRuns([]EvalRun{{
		Question:          "holdout 测试题",
		QuestionID:        2,
		GoldAnswer:        "Beta 端口 9090",
		GoldChunkID:       &betaChunkID,
		ProjectSlug:       "beta",
		QuestionSet:       "holdout",
		Category:          "project_governance",
		BatchID:           "batch-holdout-1",
		RetrievedChunkIDs: []int64{betaChunkID},
		Answer:            "Beta 端口 9090 [1]",
		Hit:               &hit,
		CitationOK:        &citOK,
	}})
	if err != nil || len(holdRuns) != 1 {
		t.Fatalf("SaveEvalRuns holdout failed: %+v err=%v", holdRuns, err)
	}

	if err := st.MarkEvalRun(baseRuns[0].ID, false); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkEvalRun(holdRuns[0].ID, true); err != nil {
		t.Fatal(err)
	}
	mB, uB, err := st.EvalUsableSummaryForRun(baseRuns[0].ID)
	if err != nil || mB != 1 || uB != 0 {
		t.Fatalf("base52 batch isolation failed: marked=%d usable=%d err=%v", mB, uB, err)
	}
	mH, uH, err := st.EvalUsableSummaryForRun(holdRuns[0].ID)
	if err != nil || mH != 1 || uH != 1 {
		t.Fatalf("holdout batch isolation failed: marked=%d usable=%d err=%v", mH, uH, err)
	}
}
