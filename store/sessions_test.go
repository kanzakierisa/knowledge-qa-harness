package store

import (
	"database/sql"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
)

func TestBuildSessionTitle(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "新对话"},
		{"请问一下，Gin 路由组怎么用？", "Gin 路由组怎么用"},
		{"帮我查一下新版活动发奖周期是几天？！", "新版活动发奖周期是几天"},
		{"  麻烦问下   服务默认监听端口是多少。  ", "服务默认监听端口是多少"},
		{"请问在大型多项目并行团队内容消费激励活动规则中关于跨季度的多阶梯返佣计算口径究竟是怎么规定的？", "在大型多项目并行团队内容消费激励活动规则中关于跨季…"},
	}
	for _, tc := range cases {
		if got := BuildSessionTitle(tc.in); got != tc.want {
			t.Fatalf("BuildSessionTitle(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestComputeOnlineStatsFromTurns(t *testing.T) {
	// 空轮次：禁止伪造 100%，比率必须为 nil
	empty := ComputeOnlineStatsFromTurns(nil)
	if empty.TotalTurns != 0 || empty.CitationRate != nil || empty.FeedbackUsableRate != nil {
		t.Fatalf("empty stats mismatch: %+v", empty)
	}

	okTrue := true
	okFalse := false
	up := "up"
	down := "down"
	turns := []QATurn{
		{ID: 1, Refused: false, ProjectResolved: "go-gin", CitationOK: &okTrue, FeedbackVote: &up},
		{ID: 2, Refused: false, ProjectResolved: "content-promo", CitationOK: &okFalse, FeedbackVote: &down},
		{ID: 3, Refused: true, ProjectResolved: "auto", CitationOK: nil, FeedbackVote: &up},
		{ID: 4, Refused: false, ProjectResolved: "ambiguous", CitationOK: nil, FeedbackVote: nil},
	}
	st := ComputeOnlineStatsFromTurns(turns)
	if st.TotalTurns != 4 || st.AnsweredTurns != 2 || st.CitationOKCount != 1 || st.CitationTotalCount != 2 {
		t.Fatalf("counts mismatch: %+v", st)
	}
	if st.CitationRate == nil || *st.CitationRate != 0.5 {
		t.Fatalf("citation_rate mismatch: %+v", st.CitationRate)
	}
	if st.FeedbackUpCount != 2 || st.FeedbackDownCount != 1 || st.FeedbackTotalCount != 3 {
		t.Fatalf("feedback counts mismatch: %+v", st)
	}
	if st.FeedbackUsableRate == nil || *st.FeedbackUsableRate < 0.66 || *st.FeedbackUsableRate > 0.67 {
		t.Fatalf("feedback_usable_rate mismatch: %+v", st.FeedbackUsableRate)
	}
	if st.RefusedCount != 1 || st.RefusedUpCount != 1 || st.RefusedDownCount != 0 {
		t.Fatalf("refused counts mismatch: %+v", st)
	}
}

func TestStage13SessionsAndHiddenProjectsMySQL(t *testing.T) {
	dsn := os.Getenv("KNOWLEDGE_QA_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("设置 KNOWLEDGE_QA_TEST_MYSQL_DSN 后运行阶段 13 Store MySQL 集成测试")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("测试DSN格式不正确")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_]+_test$`).MatchString(cfg.DBName) {
		t.Fatal("测试库名必须为安全标识符且以 _test 结尾")
	}
	name := strings.TrimSuffix(cfg.DBName, "_test") + "_v3_stage13_store_test"
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
	// 幂等重复执行检查
	applyV2SchemaForTest(t, st.db, string(raw002))

	// 1. 隐藏项目隔离校验
	if _, err := st.EnsureProject("project-alpha", "隐藏测试项目", "仅评测使用", true); err != nil {
		t.Fatal(err)
	}
	if _, err := st.InsertOrReplaceDocument(InsertDocParams{
		ProjectSlug: "project-alpha",
		Filename:    "hidden.md",
		Title:       "隐藏文档",
		Content:     "hidden content",
		SourceType:  "md",
		ContentHash: SHA256Hex([]byte("hidden content")),
		Chunks:      []ChunkInput{{HeadingPath: "H", Content: "hidden content", Embedding: "[1,0]"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.InsertOrReplaceDocument(InsertDocParams{
		ProjectSlug: "go-gin",
		Filename:    "visible.md",
		Title:       "可见文档",
		Content:     "visible content",
		SourceType:  "md",
		ContentHash: SHA256Hex([]byte("visible content")),
		Chunks:      []ChunkInput{{HeadingPath: "V", Content: "visible content", Embedding: "[1,0]"}},
	}); err != nil {
		t.Fatal(err)
	}

	visProjs, err := st.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range visProjs {
		if p.Slug == "project-alpha" {
			t.Fatalf("hidden project leaked into ListProjects: %+v", visProjs)
		}
	}
	allProjs, err := st.ListAllProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(allProjs) != len(visProjs)+1 {
		t.Fatalf("ListAllProjects count=%d want %d", len(allProjs), len(visProjs)+1)
	}
	visDocs, err := st.ListDocuments("", "")
	if err != nil || len(visDocs) != 1 || visDocs[0].Filename != "visible.md" {
		t.Fatalf("ListDocuments should hide hidden project docs, got %+v err=%v", visDocs, err)
	}
	explicitDocs, err := st.ListDocuments("project-alpha", "")
	if err != nil || len(explicitDocs) != 1 {
		t.Fatalf("explicit ListDocuments(project-alpha) failed: %+v err=%v", explicitDocs, err)
	}
	cnt, err := st.CountChunks()
	if err != nil || cnt != 1 {
		t.Fatalf("CountChunks should only count visible chunks=1, got %d err=%v", cnt, err)
	}

	// 2. 会话与多轮持久化、置顶、重命名、反馈联动与级联删除
	okTrue := true
	s1ID, t1ID, err := st.CreateSessionAndTurn(
		CreateSessionParams{ProjectSlug: "go-gin", ScopeMode: "topic"},
		CreateTurnParams{
			Question:          "请问 Gin 路由组怎么用？",
			ProjectSlug:       "go-gin",
			ScopeLabel:        "主题：Go/Gin",
			Answer:            "用 r.Group [1]。",
			CitationsJSON:     `[{"n":1,"chunk_id":2,"heading":"V","content":"visible content"}]`,
			RetrievedChunkIDs: []int64{2},
			ProjectResolved:   "go-gin",
			CitationOK:        &okTrue,
		},
	)
	if err != nil || s1ID <= 0 || t1ID <= 0 {
		t.Fatalf("CreateSessionAndTurn failed: s=%d t=%d err=%v", s1ID, t1ID, err)
	}

	t2ID, err := st.AppendTurnToSession(
		s1ID,
		CreateSessionParams{ProjectSlug: "go-gin", ScopeMode: "topic"},
		CreateTurnParams{
			Question:        "文档外的问题是什么？",
			ProjectSlug:     "go-gin",
			ScopeLabel:      "主题：Go/Gin",
			Answer:          "文档中没有依据",
			Refused:         true,
			ProjectResolved: "go-gin",
		},
	)
	if err != nil || t2ID <= t1ID {
		t.Fatalf("AppendTurnToSession failed: t2=%d err=%v", t2ID, err)
	}

	s2ID, _, err := st.CreateSessionAndTurn(
		CreateSessionParams{ProjectSlug: "", ScopeMode: "topic"},
		CreateTurnParams{
			Question:        "第二个会话问题",
			ProjectSlug:     "auto",
			ScopeLabel:      "自动识别",
			Answer:          "回答 [1]。",
			ProjectResolved: "go-gin",
			CitationOK:      &okTrue,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	// 置顶会话 1 并重命名
	pinned := true
	renamed := "Gin 路由组核心用法"
	upd, err := st.UpdateSession(s1ID, SessionPatch{Title: &renamed, IsPinned: &pinned})
	if err != nil || upd.Title != renamed || !upd.IsPinned {
		t.Fatalf("UpdateSession failed: %+v err=%v", upd, err)
	}
	sessions, err := st.ListSessions()
	if err != nil || len(sessions) != 2 || sessions[0].ID != s1ID || sessions[0].TurnCount != 2 {
		t.Fatalf("ListSessions ordering mismatch: %+v err=%v", sessions, err)
	}

	// 提交反馈联动 turn_id
	if _, err := st.CreateFeedback(CreateFeedbackParams{
		TurnID:            &t1ID,
		ProjectSlug:       "go-gin",
		Question:          "请问 Gin 路由组怎么用？",
		RetrievedChunkIDs: []int64{2},
		Answer:            "用 r.Group [1]。",
		Vote:              "up",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateFeedback(CreateFeedbackParams{
		TurnID:      &t2ID,
		ProjectSlug: "go-gin",
		Question:    "文档外的问题是什么？",
		Answer:      "文档中没有依据",
		Refused:     true,
		Vote:        "down",
		Comment:     "其实文档里有",
	}); err != nil {
		t.Fatal(err)
	}

	detail, err := st.GetSessionDetail(s1ID)
	if err != nil || len(detail.Turns) != 2 {
		t.Fatalf("GetSessionDetail failed: %+v err=%v", detail, err)
	}
	if detail.Stats.TotalTurns != 2 || detail.Stats.CitationOKCount != 1 || detail.Stats.CitationTotalCount != 1 ||
		detail.Stats.FeedbackUpCount != 1 || detail.Stats.FeedbackDownCount != 1 || detail.Stats.RefusedCount != 1 {
		t.Fatalf("session stats mismatch: %+v", detail.Stats)
	}

	online, err := st.GetOnlineStats()
	if err != nil || online.TotalTurns != 3 || online.CitationOKCount != 2 || online.FeedbackTotalCount != 2 {
		t.Fatalf("online stats mismatch: %+v err=%v", online, err)
	}

	// 删除会话 1 -> 级联删除 2 轮，只剩会话 2 的 1 轮
	if err := st.DeleteSession(s1ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetSessionDetail(s1ID); err != ErrSessionNotFound {
		t.Fatalf("expected ErrSessionNotFound after delete, got %v", err)
	}
	onlineAfter, err := st.GetOnlineStats()
	if err != nil || onlineAfter.TotalTurns != 1 || onlineAfter.FeedbackTotalCount != 0 {
		t.Fatalf("online stats after delete mismatch: %+v err=%v", onlineAfter, err)
	}
	_ = s2ID
}
