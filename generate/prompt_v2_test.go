package generate

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"knowledge-qa/retrieve"
	"knowledge-qa/store"
)

func TestBuildPromptV1AndV2MetadataHeaders(t *testing.T) {
	v1Top := []retrieve.MergedChunk{
		{Chunk: store.Chunk{ID: 1, Content: "Group 创建路由组。"}},
	}
	v1Prompt := BuildPrompt("路由组", v1Top)
	if !strings.Contains(v1Prompt, "[1] Group 创建路由组。") {
		t.Fatalf("V1 切片不应附加 V2 标头，实际：\n%s", v1Prompt)
	}
	if strings.Contains(v1Prompt, "deprecated（已废弃）") {
		t.Fatalf("纯 V1 切片不应出现废弃提示规则")
	}

	repID := int64(45)
	v2Top := []retrieve.MergedChunk{
		{Chunk: store.Chunk{
			ID:          1,
			ProjectSlug: "project-a",
			DocLevel:    "spec",
			DocStatus:   "active",
			Content:     "新版端口为 8091。",
		}},
		{Chunk: store.Chunk{
			ID:              2,
			ProjectSlug:     "project-b",
			DocLevel:        "note",
			DocStatus:       "deprecated",
			ReplacedByDocID: &repID,
			ReplacedByTitle: "新版规范 v2",
			Content:         "旧版端口为 8080。",
		}},
	}
	v2Prompt := BuildPrompt("端口是多少", v2Top)
	for _, want := range []string{
		"[1]（项目: project-a | 类型: spec | 状态: active） 新版端口为 8091。",
		"[2]（项目: project-b | 类型: note | 状态: deprecated（已废弃），替代文档: 新版规范 v2） 旧版端口为 8080。",
		"5. 请注意各段标头中的文档类型",
	} {
		if !strings.Contains(v2Prompt, want) {
			t.Errorf("V2 prompt 缺少 %q，实际：\n%s", want, v2Prompt)
		}
	}

	// 验证 V2 标头不会影响阶段 4 句级引用校验
	if ok, reason := ValidateCitations("新版端口为 8091 [1]。", len(v2Top)); !ok {
		t.Fatalf("句级引用校验应通过，原因：%s", reason)
	}
}

func TestExtractCitedNumbers(t *testing.T) {
	ans := "根据规范，先读取配置 `arr[0]` [3]，再监听端口 [1]。\n```go\nx := items[9]\n```\n详见 [3] 与 [2]。"
	got := ExtractCitedNumbers(ans)
	want := []int{1, 2, 3}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ExtractCitedNumbers got %v, want %v", got, want)
	}
	if gotEmpty := ExtractCitedNumbers("`nums[1]` 无正文引用"); len(gotEmpty) != 0 {
		t.Fatalf("expected empty cited numbers when only code span has brackets, got %v", gotEmpty)
	}
}

func TestGenerateStructuredJSONFenceAndBacktickMatch(t *testing.T) {
	top := []retrieve.MergedChunk{
		{Chunk: store.Chunk{ID: 1, HeadingPath: "Gin > 路由组", Content: "使用 `r.Group()` 可以创建\n路由组，组内路径自动拼接前缀。"}},
	}
	var seenMsgs int
	var seenRespType string
	var seenMaxTokens int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode req: %v", err)
		}
		seenMsgs = len(req.Messages)
		if req.ResponseFormat != nil {
			seenRespType = req.ResponseFormat.Type
		}
		seenMaxTokens = req.MaxTokens
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]string{
					"role": "assistant",
					// 外包 ```json 围栏 + 摘录去掉反引号和换行
					"content": "```json\n{\"quotes\":[{\"source\":1,\"text\":\"使用 r.Group() 可以创建 路由组\"}],\"decision\":\"answer\",\"answer\":\"用 r.Group() 创建路由组 [1]。\"}\n```",
				},
			}},
		})
	}))
	defer srv.Close()

	c := NewChatClient(srv.URL, "k", "m")
	ans, retried, ok, err := c.GenerateAnswer("怎么建路由组", top)
	if err != nil || retried || !ok {
		t.Fatalf("expected clean G1 answer, got ans=%q retried=%v ok=%v err=%v", ans, retried, ok, err)
	}
	if seenMsgs != 1 || seenRespType != "json_object" || seenMaxTokens <= 0 {
		t.Fatalf("req contract mismatch: msgs=%d respType=%q maxTokens=%d", seenMsgs, seenRespType, seenMaxTokens)
	}
	if ans != "用 r.Group() 创建路由组 [1]。" || strings.Contains(ans, "{") {
		t.Fatalf("must extract natural prose only, got %q", ans)
	}
}

func TestGenerateStructuredJSONRefusalIsolation(t *testing.T) {
	top := []retrieve.MergedChunk{
		{Chunk: store.Chunk{ID: 1, Content: "c.ShouldBindJSON 把 JSON 解码到结构体。"}},
	}
	srv, _ := fakeLLM(t, []string{
		`{"quotes":[{"source":1,"text":"c.ShouldBindJSON 把 JSON 解码到结构体"}],"decision":"refuse","answer":"文档中没有依据。资料[1]未提及底层 JSON 库。"}`,
	})
	defer srv.Close()

	c := NewChatClient(srv.URL, "k", "m")
	ans, retried, ok, err := c.GenerateAnswer("c.ShouldBindJSON 内部用 encoding/json 吗？", top)
	if err != nil || retried || !ok {
		t.Fatalf("expected clean refusal isolation, got ans=%q retried=%v ok=%v err=%v", ans, retried, ok, err)
	}
	if ans != RefuseAnswer || !IsRefusal(ans) {
		t.Fatalf("decision=refuse must isolate explanation and return RefuseAnswer, got %q", ans)
	}
}

func TestGenerateStructuredJSONQuoteCheckRetryAndNeverLeakJSON(t *testing.T) {
	top := []retrieve.MergedChunk{
		{Chunk: store.Chunk{ID: 1, Content: "gin.New() 返回不带任何中间件的空引擎。"}},
	}

	t.Run("fabricated quote triggers retry then refuses", func(t *testing.T) {
		srv, prompts := fakeLLM(t, []string{
			`{"quotes":[{"source":1,"text":"默认把日志写入 /var/log/gin.log"}],"decision":"answer","answer":"写入 /var/log/gin.log [1]。"}`,
			`{"quotes":[],"decision":"refuse","answer":""}`,
		})
		defer srv.Close()
		c := NewChatClient(srv.URL, "k", "m")
		ans, retried, ok, err := c.GenerateAnswer("日志路径在哪里", top)
		if err != nil || !retried || !ok || ans != RefuseAnswer {
			t.Fatalf("expected retry to clean refusal, got ans=%q retried=%v ok=%v err=%v", ans, retried, ok, err)
		}
		if len(*prompts) != 2 || !strings.Contains((*prompts)[1], "找不到对应原文") {
			t.Fatalf("expected quote mismatch reason on retry prompt, got %+v", *prompts)
		}
	})

	t.Run("empty quotes retries then succeeds", func(t *testing.T) {
		srv, _ := fakeLLM(t, []string{
			`{"quotes":[],"decision":"answer","answer":"改用 gin.New() [1]。"}`,
			`{"quotes":[{"source":1,"text":"gin.New() 返回不带任何中间件的空引擎"}],"decision":"answer","answer":"改用 gin.New() [1]。"}`,
		})
		defer srv.Close()
		c := NewChatClient(srv.URL, "k", "m")
		ans, retried, ok, err := c.GenerateAnswer("不要默认中间件用什么", top)
		if err != nil || !retried || !ok || ans != "改用 gin.New() [1]。" {
			t.Fatalf("expected retry success, got ans=%q retried=%v ok=%v err=%v", ans, retried, ok, err)
		}
	})

	t.Run("malformed JSON never leaks raw JSON", func(t *testing.T) {
		srv, _ := fakeLLM(t, []string{
			`{"quotes":[{"source":1`,
			`{"quotes":[{"source":1`,
		})
		defer srv.Close()
		c := NewChatClient(srv.URL, "k", "m")
		ans, retried, ok, err := c.GenerateAnswer("不要默认中间件用什么", top)
		if err != nil || !retried || ok || ans != RefuseAnswer {
			t.Fatalf("malformed JSON must refuse and never leak raw JSON: ans=%q retried=%v ok=%v err=%v", ans, retried, ok, err)
		}
	})
}

func TestGenerateJSONModeHTTP400AndBlankFallback(t *testing.T) {
	top := []retrieve.MergedChunk{
		{Chunk: store.Chunk{ID: 1, Content: "路由组用 Group"}},
	}

	t.Run("HTTP 400 unsupported response_format falls back cleanly", func(t *testing.T) {
		calls := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			var req chatRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.ResponseFormat != nil {
				http.Error(w, `{"error":"response_format is not supported"}`, http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": "路由组用 Group [1]。"}}},
			})
		}))
		defer srv.Close()
		c := NewChatClient(srv.URL, "k", "m")
		ans, retried, ok, err := c.GenerateAnswer("怎么建路由组", top)
		if err != nil || retried || !ok || ans != "路由组用 Group [1]。" || calls != 2 {
			t.Fatalf("fallback failed: ans=%q retried=%v ok=%v calls=%d err=%v", ans, retried, ok, calls, err)
		}
	})

	t.Run("blank json_object response falls back cleanly", func(t *testing.T) {
		calls := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			var req chatRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			w.Header().Set("Content-Type", "application/json")
			content := "   "
			if req.ResponseFormat == nil {
				content = RefuseAnswer
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": content}}},
			})
		}))
		defer srv.Close()
		c := NewChatClient(srv.URL, "k", "m")
		ans, retried, ok, err := c.GenerateAnswer("今天天气", top)
		if err != nil || retried || !ok || ans != RefuseAnswer || calls != 2 {
			t.Fatalf("blank fallback failed: ans=%q retried=%v ok=%v calls=%d err=%v", ans, retried, ok, calls, err)
		}
	})
}
