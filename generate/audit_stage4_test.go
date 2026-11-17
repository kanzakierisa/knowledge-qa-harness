package generate

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"knowledge-qa/retrieve"
	"knowledge-qa/store"
)

// 阶段 4 验收自测：引用校验、拒答识别、重试兜底（用 httptest 的假 LLM，不消耗真实额度）。

func TestValidateCitations(t *testing.T) {
	cases := []struct {
		ans  string
		max  int
		want bool
	}{
		{"Gin 用 Group 分组 [1]。", 5, true},
		{"见 [1][3]。", 5, true},
		{"通过下标 `arr[0]` 读取首元素 [1]。", 5, true},
		{"示例：\n```go\nx := nums[9]\n```\n说明见资料 [2]。", 5, true},
		{"示例：\n```go\nx := nums[1]\n```\n正文未引用。", 5, false},
		{"不能说明数据库已经连接成功。sql.Open 只是建立连接池配置，不会真正连接 [1]。可以用 db.Ping() 验证连通性 [1]。", 5, true},
		{"不会运行。os.Exit(1) 立即退出且不执行 defer [1]。", 5, true},
		{"最终路径是组前缀加组内路径，如 `/v1/users`。[1]", 5, true},
		{"指针接收者才能修改结构体内容；值接收者操作的是副本 [2]。", 5, true},
		{"调用 cancel() 通知所有监听者退出 [1]；使用后必须调用 cancel 防止泄漏 [1]。", 5, true},
		{"Go 的切片很好用 [1]。\n示例：\n```go\ns := []int{1, 2}\n```\n说明见 [1]。", 5, true},
		{"Go 字符串是只读的字节序列 [1]。len(s) 返回字节数 [1]。因此带中文的字符串中，一个中文字符通常占多个字节，用 len(s) 得到的字节数不能当作字符数。", 5, false},
		{"不会运行。[1] os.Exit(1) 立即退出且不执行 defer。[1] log.Fatal 打印消息后调用 os.Exit(1)。", 5, false},
		{"不会运行。[1] os.Exit(1) 立即退出且不执行 defer；[1] log.Fatal 打印消息后调用 os.Exit(1)。", 5, false},
		{"第一句有引用 [1]。结尾没引。", 5, false},
		{"这是一句超过十五个字的没有引用标记的前置陈述句。第二句有引用 [1]。", 5, false},
		{"第一句有引用 [1]。中间这句没有引用。第三句有引用 [2]。", 5, false},
		{"没有引用的一句话。", 5, false},
		{"胡说 [7]。", 5, false},
		{"见 [0]。", 5, false},
	}
	for _, c := range cases {
		got, reason := validateCitations(c.ans, c.max)
		if got != c.want {
			t.Fatalf("%q => %v want %v", c.ans, got, c.want)
		}
		if !got {
			t.Logf("%q 失败原因：%s", c.ans, reason)
		}
	}
}

func TestIsRefusal(t *testing.T) {
	if !IsRefusal("文档中没有依据") {
		t.Fatal("应识别为拒答")
	}
	if !IsRefusal("根据资料，文档中没有依据。") {
		t.Fatal("应识别为拒答")
	}
	if IsRefusal("Gin 用 Group 分组 [1]。") {
		t.Fatal("不应识别为拒答")
	}
}

// fakeLLM 按顺序返回预设答案，并记录每次收到的 prompt。
func fakeLLM(t *testing.T, answers []string) (*httptest.Server, *[]string) {
	var prompts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		prompts = append(prompts, req.Messages[0].Content)
		ans := answers[len(prompts)-1]
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": ans}}},
		})
	}))
	return srv, &prompts
}

func sampleTop() []retrieve.MergedChunk {
	return []retrieve.MergedChunk{
		{Chunk: store.Chunk{ID: 1, Content: "路由组用 Group"}},
		{Chunk: store.Chunk{ID: 2, Content: "组前缀拼接"}},
	}
}

func TestGenerateRetryThenSuccess(t *testing.T) {
	srv, prompts := fakeLLM(t, []string{"Gin 用 Group 分组 [7]。", "Gin 用 Group 分组 [1]。"})
	defer srv.Close()
	c := NewChatClient(srv.URL, "k", "m")

	ans, retried, ok, err := c.GenerateAnswer("路由组怎么用", sampleTop())
	if err != nil || !retried || !ok {
		t.Fatalf("期望重试后成功，得到 retried=%v ok=%v err=%v", retried, ok, err)
	}
	if !strings.Contains(ans, "[1]") {
		t.Fatalf("答案应带 [1]，实际：%s", ans)
	}
	if len(*prompts) != 2 {
		t.Fatalf("应调用 LLM 两次，实际 %d", len(*prompts))
	}
	if !strings.Contains((*prompts)[1], "只能使用 [1]-[2]") {
		t.Fatalf("重试 prompt 应包含错误原因，实际：%s", (*prompts)[1])
	}
}

func TestGenerateRetryOnUncitedEndingSentence(t *testing.T) {
	srv, prompts := fakeLLM(t, []string{
		"不会运行。[1] os.Exit(1) 立即退出且不执行 defer。[1] log.Fatal 打印消息后调用 os.Exit(1)。",
		"不会运行。[1] os.Exit(1) 立即退出且不执行 defer [1]。log.Fatal 打印消息后调用 os.Exit(1) [1]。",
	})
	defer srv.Close()
	c := NewChatClient(srv.URL, "k", "m")

	ans, retried, ok, err := c.GenerateAnswer("os.Exit 后 defer 会执行吗", sampleTop())
	if err != nil || !retried || !ok {
		t.Fatalf("期望句级漏标重试后通过，得到 retried=%v ok=%v err=%v", retried, ok, err)
	}
	if !strings.Contains(ans, "os.Exit(1) [1]") {
		t.Fatalf("期望返回重试后带完整结尾引用的答案，实际：%s", ans)
	}
	if len(*prompts) != 2 || !strings.Contains((*prompts)[1], "未标注来源") {
		t.Fatalf("重试 prompt 应包含句级漏标错误原因，实际：%v", *prompts)
	}
}

func TestGenerateRetryStillBad(t *testing.T) {
	srv, _ := fakeLLM(t, []string{"没有引用的回答", "还是乱引 [9]"})
	defer srv.Close()
	c := NewChatClient(srv.URL, "k", "m")

	ans, retried, ok, err := c.GenerateAnswer("路由组怎么用", sampleTop())
	if err != nil {
		t.Fatal(err)
	}
	if !retried || ok {
		t.Fatalf("期望重试且校验失败，得到 retried=%v ok=%v", retried, ok)
	}
	if ans != RefuseAnswer {
		t.Fatalf("应返回拒答文案，实际：%s", ans)
	}
}

func TestGenerateModelSelfRefusal(t *testing.T) {
	srv, prompts := fakeLLM(t, []string{"文档中没有依据"})
	defer srv.Close()
	c := NewChatClient(srv.URL, "k", "m")

	ans, retried, ok, err := c.GenerateAnswer("今天天气", sampleTop())
	if err != nil || retried || !ok || ans != RefuseAnswer {
		t.Fatalf("模型自拒答应直接返回，得到 ans=%q retried=%v ok=%v err=%v", ans, retried, ok, err)
	}
	if len(*prompts) != 1 {
		t.Fatalf("自拒答不应重试，实际调用 %d 次", len(*prompts))
	}
}
