package generate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"knowledge-qa/retrieve"
)

// ChatClient 调 OpenAI 兼容的 chat completions API。
type ChatClient struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

// NewChatClient 创建对话客户端。
func NewChatClient(baseURL, apiKey, model string) *ChatClient {
	return &ChatClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		client:  &http.Client{Timeout: 120 * time.Second},
	}
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Chat 发送单轮 user 消息，返回助手回复文本。
// Temperature 显式设为 0，保证引用标注与拒答判定在不同批次评测间可复现。
func (c *ChatClient) Chat(userPrompt string) (string, error) {
	reqBody, _ := json.Marshal(chatRequest{
		Model:       c.model,
		Messages:    []chatMessage{{Role: "user", Content: userPrompt}},
		Temperature: 0,
	})
	req, err := http.NewRequest("POST", c.baseURL+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("LLM 请求失败：%w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("LLM API 返回 %d：%s", resp.StatusCode, string(raw))
	}
	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return "", fmt.Errorf("LLM 响应解析失败：%w", err)
	}
	if cr.Error != nil {
		return "", fmt.Errorf("LLM API 错误：%s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("LLM API 返回空 choices")
	}
	return strings.TrimSpace(cr.Choices[0].Message.Content), nil
}

// ---------- 阶段 4：引用后校验 + 重试兜底 ----------

// citeRe 提取答案正文中的引用编号 [n]。
var citeRe = regexp.MustCompile(`\[(\d+)\]`)

// fencedCodeRe 与 inlineCodeRe 用于在校验引用前剥离代码示例，避免 Go 数组/切片下标（如 arr[0]、s[10]）被误判为引用编号。
var (
	fencedCodeRe    = regexp.MustCompile("(?s)```.*?```")
	inlineCodeRe    = regexp.MustCompile("`[^`\n]*`")
	trailingCiteRe  = regexp.MustCompile(`^([ \t]*\[\d+\])+`)
	precedingCiteRe = regexp.MustCompile(`\[\d+\][ \t]*$`)
)

const shortIntroMaxRunes = 15

func stripCodeSpans(answer string) string {
	noFenced := fencedCodeRe.ReplaceAllString(answer, "")
	return inlineCodeRe.ReplaceAllString(noFenced, "")
}

func hasMeaningfulText(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func contentRuneCount(s string) int {
	n := 0
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Han, r) {
			n++
		}
	}
	return n
}

// splitSentencesForCitation 按句末标点与换行切分非代码正文，同时将紧随标点之后的 [n] 归属前句；
// 分号仅在紧邻 [n] 时切分，以兼容同一句内用分号并列、句末统一标引的写法（如 q_id=45）。
func splitSentencesForCitation(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var out []string
	start := 0
	for i := 0; i < len(text); {
		r, w := utf8.DecodeRuneInString(text[i:])
		isHardEnd := r == '。' || r == '！' || r == '？' || r == '\n'
		isSemiWithCite := (r == '；' || r == ';') &&
			(trailingCiteRe.MatchString(text[i+w:]) || precedingCiteRe.MatchString(text[start:i]))
		if !isHardEnd && !isSemiWithCite {
			i += w
			continue
		}
		end := i + w
		if loc := trailingCiteRe.FindStringIndex(text[end:]); loc != nil {
			end += loc[1]
		}
		if seg := strings.TrimSpace(text[start:end]); hasMeaningfulText(seg) {
			out = append(out, seg)
		}
		start = end
		i = end
	}
	if start < len(text) {
		if seg := strings.TrimSpace(text[start:]); hasMeaningfulText(seg) {
			out = append(out, seg)
		}
	}
	return out
}

func isAllowedIntroSentence(s string, idx, total int) bool {
	if idx >= total-1 {
		return false // 结尾句必须标注来源，防止末句越界外推或漏标
	}
	if contentRuneCount(s) > shortIntroMaxRunes {
		return false
	}
	return idx == 0 || strings.HasSuffix(s, "：") || strings.HasSuffix(s, ":")
}

// IsRefusal 判断模型输出是否为拒答文案（生成侧拒答保险）。
// 允许少量前后缀差异：只要输出包含固定拒答文案且正文中没有给出任何引用编号，即视为拒答。
func IsRefusal(answer string) bool {
	return strings.Contains(answer, RefuseAnswer) && !citeRe.MatchString(stripCodeSpans(answer))
}

// validateCitations 校验答案中的每个 [n] 都在 1~maxN 之间，且所有实质性陈述句与结尾句均含引用。
// 返回错误原因（供追加进 prompt 重试）；ok=true 时 reason 为空。
func validateCitations(answer string, maxN int) (ok bool, reason string) {
	return validate(answer, maxN)
}

// ValidateCitations 暴露阶段 4 的同一套校验逻辑供评测复用。
func ValidateCitations(answer string, maxN int) (ok bool, reason string) {
	return validate(answer, maxN)
}

func validate(answer string, maxN int) (ok bool, reason string) {
	stripped := stripCodeSpans(answer)
	matches := citeRe.FindAllStringSubmatch(stripped, -1)
	if len(matches) == 0 {
		return false, fmt.Sprintf("你上次的回答没有标注任何来源编号，每句结论性陈述末尾必须用 [1]-[%d] 标注来源", maxN)
	}
	bad := map[int]bool{}
	for _, m := range matches {
		n, err := strconv.Atoi(m[1])
		if err != nil || n < 1 || n > maxN {
			bad[n] = true
		}
	}
	if len(bad) > 0 {
		parts := make([]string, 0, len(bad))
		for n := range bad {
			parts = append(parts, fmt.Sprintf("[%d]", n))
		}
		return false, fmt.Sprintf("你上次引用了不存在的编号 %s，只能使用 [1]-[%d]",
			strings.Join(parts, ""), maxN)
	}
	sentences := splitSentencesForCitation(stripped)
	for i, s := range sentences {
		if citeRe.MatchString(s) {
			continue
		}
		if isAllowedIntroSentence(s, i, len(sentences)) {
			continue
		}
		return false, fmt.Sprintf("你上次的回答中存在未标注来源的陈述或结尾句（%q），每句实质性陈述及结尾句末尾都必须用 [1]-[%d] 标注来源", s, maxN)
	}
	return true, ""
}

// GenerateAnswer 生成答案并做引用后校验（阶段 4 硬约束）：
//  1. 用 v2 prompt 调 LLM；
//  2. 模型输出为拒答文案 → 直接返回（refused 由调用方判定，此处 ok=true 交回原文）；
//  3. 引用编号非法或完全无引用 → 把错误原因追加进 prompt 重试一次；
//  4. 重试仍失败 → ok=false，调用方整题按拒答处理。
//
// 返回：answer 最终答案；retried 是否发生了重试；ok 引用校验是否通过。
func (c *ChatClient) GenerateAnswer(question string, top []retrieve.MergedChunk) (answer string, retried bool, ok bool, err error) {
	prompt := BuildPrompt(question, top)

	answer, err = c.Chat(prompt)
	if err != nil {
		return "", false, false, err
	}
	// 模型主动拒答：不做引用校验，直接交回
	if IsRefusal(answer) {
		return answer, false, true, nil
	}
	ok, reason := validateCitations(answer, len(top))
	if ok {
		return answer, false, true, nil
	}
	// 校验失败：把错误原因追加进 prompt 重试一次
	retryPrompt := AppendRetryReason(prompt, reason)
	answer2, err2 := c.Chat(retryPrompt)
	if err2 != nil {
		return "", true, false, err2
	}
	if IsRefusal(answer2) {
		return answer2, true, true, nil
	}
	if ok2, _ := validateCitations(answer2, len(top)); ok2 {
		return answer2, true, true, nil
	}
	// 重试仍失败：整题按拒答处理
	return RefuseAnswer, true, false, nil
}
