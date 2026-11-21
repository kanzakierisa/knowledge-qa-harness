package generate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"knowledge-qa/retrieve"
	"knowledge-qa/store"
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

type responseFormat struct {
	Type string `json:"type"`
}

type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []chatMessage   `json:"messages"`
	Temperature    float64         `json:"temperature"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type chatMessage struct {
	Role             string `json:"role"`
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		FinishReason string      `json:"finish_reason"`
		Message      chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type httpStatusError struct {
	statusCode int
	body       string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("LLM API 返回 %d：%s", e.statusCode, e.body)
}

const (
	jsonModeMaxTokens          = 2048
	reasoningJSONModeMaxTokens = 4096
)

var reasoningModelSubstrings = []string{
	"deepseek-r1",
	"deepseek-reasoner",
	"deepseek-v4",
	"qwen3",
	"qwq",
	"glm-5",
	"kimi-k3",
	"reasoner",
	"thinking",
}

func isLikelyReasoningModel(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return false
	}
	for _, sub := range reasoningModelSubstrings {
		if strings.Contains(m, sub) {
			return true
		}
	}
	base := m
	if idx := strings.LastIndex(m, "/"); idx >= 0 {
		base = m[idx+1:]
	}
	for _, p := range []string{"o1", "o3", "o4"} {
		if base == p || strings.HasPrefix(base, p+"-") {
			return true
		}
	}
	return false
}

func jsonModeMaxTokensForModel(model string) int {
	if raw := strings.TrimSpace(os.Getenv("LLM_JSON_MAX_TOKENS")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	if isLikelyReasoningModel(model) {
		return reasoningJSONModeMaxTokens
	}
	return jsonModeMaxTokens
}

// Chat 发送单轮 user 消息，返回助手回复文本。
// Temperature 显式设为 0，保证引用标注与拒答判定在不同批次评测间可复现。
func (c *ChatClient) Chat(userPrompt string) (string, error) {
	ans, _, _, err := c.chatOnceMeta(userPrompt, false)
	return ans, err
}

// chatJSON 优先附带 response_format={"type":"json_object"} 发起单轮 user 请求；
// 若端点因不支持该参数返回 400 或返回空白内容，则自动回退到纯文本单轮请求。
func (c *ChatClient) chatJSON(userPrompt string) (string, error) {
	ans, finishReason, hasReasoning, err := c.chatOnceMeta(userPrompt, true)
	if err != nil {
		var statusErr *httpStatusError
		if errors.As(err, &statusErr) && statusErr.statusCode == http.StatusBadRequest {
			log.Printf("[chat] JSON 模式返回 HTTP 400，自动回退纯文本请求：%s", statusErr.body)
			ans2, _, _, err2 := c.chatOnceMeta(userPrompt, false)
			return ans2, err2
		}
		return "", err
	}
	if strings.TrimSpace(ans) == "" {
		if finishReason == "length" || hasReasoning {
			log.Printf("[chat] JSON 模式返回空白内容（model=%s finish_reason=%s max_tokens=%d has_reasoning=%v，可能因思考过程耗尽输出配额），自动回退纯文本请求；可通过 LLM_JSON_MAX_TOKENS 调大配额",
				c.model, finishReason, jsonModeMaxTokensForModel(c.model), hasReasoning)
		} else {
			log.Printf("[chat] JSON 模式返回空白内容，自动回退纯文本请求")
		}
		ans2, _, _, err2 := c.chatOnceMeta(userPrompt, false)
		return ans2, err2
	}
	return ans, nil
}

func (c *ChatClient) chatOnceMeta(userPrompt string, jsonMode bool) (content string, finishReason string, hasReasoning bool, err error) {
	reqPayload := chatRequest{
		Model:       c.model,
		Messages:    []chatMessage{{Role: "user", Content: userPrompt}},
		Temperature: 0,
	}
	if jsonMode {
		reqPayload.MaxTokens = jsonModeMaxTokensForModel(c.model)
		reqPayload.ResponseFormat = &responseFormat{Type: "json_object"}
	}
	reqBody, _ := json.Marshal(reqPayload)
	req, err := http.NewRequest("POST", c.baseURL+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return "", "", false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", "", false, fmt.Errorf("LLM 请求失败：%w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", "", false, &httpStatusError{statusCode: resp.StatusCode, body: string(raw)}
	}
	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return "", "", false, fmt.Errorf("LLM 响应解析失败：%w", err)
	}
	if cr.Error != nil {
		return "", "", false, fmt.Errorf("LLM API 错误：%s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return "", "", false, fmt.Errorf("LLM API 返回空 choices")
	}
	choice := cr.Choices[0]
	return strings.TrimSpace(choice.Message.Content), strings.TrimSpace(choice.FinishReason), strings.TrimSpace(choice.Message.ReasoningContent) != "", nil
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

// ExtractCitedNumbers 从答案非代码正文中提取实际引用到的编号 [n]（去重并升序返回）。
func ExtractCitedNumbers(answer string) []int {
	stripped := stripCodeSpans(answer)
	matches := citeRe.FindAllStringSubmatch(stripped, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[int]bool, len(matches))
	nums := make([]int, 0, len(matches))
	for _, m := range matches {
		n, err := strconv.Atoi(m[1])
		if err != nil || n <= 0 || seen[n] {
			continue
		}
		seen[n] = true
		nums = append(nums, n)
	}
	sort.Ints(nums)
	return nums
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

// GenerateAnswer 生成答案并做 Quote-First 结构化校验 + 引用后校验（阶段 4/11）：
//  1. 用 G1 Quote-First JSON prompt 调 LLM（自动兼容纯文本 Mock/不支持 response_format 的端点）；
//  2. 若返回合法 JSON：decision="refuse" 直接返回 RefuseAnswer 彻底隔离解释文本；decision="answer" 必须通过 quotes 原文核验，再抽取自然语言 answer 走 validateCitations；
//  3. 若返回非 JSON 纯文本：无缝沿用阶段 4 原有 IsRefusal + validateCitations 链路；
//  4. 校验失败则追加错误原因重试一次，仍失败返回 RefuseAnswer 与 ok=false。
func (c *ChatClient) GenerateAnswer(question string, top []retrieve.MergedChunk) (answer string, retried bool, ok bool, err error) {
	prompt := BuildPrompt(question, top)

	raw, err := c.chatJSON(prompt)
	if err != nil {
		return "", false, false, err
	}
	ans, refused, ok, reason := parseStructuredOrPlainAnswer(raw, top)
	if refused || ok {
		return ans, false, true, nil
	}

	retryPrompt := AppendRetryReason(prompt, reason)
	raw2, err2 := c.chatJSON(retryPrompt)
	if err2 != nil {
		return "", true, false, err2
	}
	ans2, refused2, ok2, _ := parseStructuredOrPlainAnswer(raw2, top)
	if refused2 || ok2 {
		return ans2, true, true, nil
	}
	return RefuseAnswer, true, false, nil
}

type structuredQuote struct {
	Source int    `json:"source"`
	Text   string `json:"text"`
}

type structuredAnswer struct {
	Quotes   []structuredQuote `json:"quotes"`
	Decision string            `json:"decision"`
	Answer   string            `json:"answer"`
}

func stripJSONCodeFence(s string) string {
	trimmed := strings.TrimSpace(s)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}
	nl := strings.IndexByte(trimmed, '\n')
	if nl < 0 {
		return trimmed
	}
	firstLine := strings.TrimSpace(trimmed[:nl])
	if !strings.EqualFold(firstLine, "```") && !strings.EqualFold(firstLine, "```json") {
		return trimmed
	}
	body := strings.TrimSpace(trimmed[nl+1:])
	body = strings.TrimSpace(strings.TrimSuffix(body, "```"))
	return body
}

func looksLikeJSONObject(s string) bool {
	if !strings.HasPrefix(s, "{") {
		return false
	}
	return strings.HasSuffix(s, "}") || strings.Contains(s, `"decision"`) || strings.Contains(s, `"quotes"`)
}

func parseStructuredOrPlainAnswer(raw string, top []retrieve.MergedChunk) (answer string, refused bool, ok bool, retryReason string) {
	trimmed := strings.TrimSpace(raw)
	candidate := stripJSONCodeFence(trimmed)
	if looksLikeJSONObject(candidate) {
		var sa structuredAnswer
		if err := json.Unmarshal([]byte(candidate), &sa); err != nil {
			return "", false, false, fmt.Sprintf("你上次输出了无效的 JSON（%v），请严格输出单个合法 JSON 对象", err)
		}
		dec := strings.ToLower(strings.TrimSpace(sa.Decision))
		if dec == "refuse" || dec == "abstain" {
			return RefuseAnswer, true, true, ""
		}
		if dec != "answer" {
			return "", false, false, "你上次 JSON 中的 decision 必须是 \"answer\" 或 \"refuse\""
		}
		if len(sa.Quotes) == 0 {
			return "", false, false, fmt.Sprintf("当 decision 为 \"answer\" 时，quotes 不能为空，必须先从 [1]-[%d] 逐字摘录原文；若资料不足以回答请设 decision 为 \"refuse\"", len(top))
		}
		for i, q := range sa.Quotes {
			if q.Source < 1 || q.Source > len(top) {
				return "", false, false, fmt.Sprintf("quotes 中的 source=%d 越界，只能使用 [1]-[%d]", q.Source, len(top))
			}
			if !quoteMatchesChunk(q.Text, top[q.Source-1].Chunk) {
				return "", false, false, fmt.Sprintf("quotes 中的第 %d 条摘录（%q）在资料 [%d] 中找不到对应原文，请逐字摘录资料原句；若资料不足以回答请设 decision 为 \"refuse\"", i+1, q.Text, q.Source)
			}
		}
		ans := strings.TrimSpace(sa.Answer)
		if ans == "" {
			return "", false, false, "当 decision 为 \"answer\" 时，answer 不能为空；若资料不足以回答请设 decision 为 \"refuse\""
		}
		if IsRefusal(ans) {
			return RefuseAnswer, true, true, ""
		}
		citOK, citReason := validateCitations(ans, len(top))
		if !citOK {
			return "", false, false, citReason
		}
		return ans, false, true, ""
	}

	// 纯文本回退链路：完全保留 V1 阶段 4 与 Mock 单测兼容
	if IsRefusal(trimmed) {
		return trimmed, true, true, ""
	}
	citOK, citReason := validateCitations(trimmed, len(top))
	if !citOK {
		return "", false, false, citReason
	}
	return trimmed, false, true, ""
}

func compactForQuoteMatch(s string) string {
	s = strings.ReplaceAll(s, "**", "")
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsSpace(r) || r == '`' {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

func quoteMatchesChunk(quoteText string, chunk store.Chunk) bool {
	haystack := compactForQuoteMatch(chunk.HeadingPath + "\n" + chunk.Content)
	if haystack == "" {
		return false
	}
	// 若模型使用省略号拼接同片断句，要求各片段均达到最小长度并按序出现在原切片中
	if strings.Contains(quoteText, "...") || strings.Contains(quoteText, "……") {
		normalized := strings.ReplaceAll(quoteText, "……", "...")
		parts := strings.Split(normalized, "...")
		nonEmpty := 0
		offset := 0
		for _, part := range parts {
			comp := compactForQuoteMatch(part)
			if comp == "" {
				continue
			}
			nonEmpty++
			if utf8.RuneCountInString(comp) < 4 {
				return false
			}
			idx := strings.Index(haystack[offset:], comp)
			if idx < 0 {
				return false
			}
			offset += idx + len(comp)
		}
		return nonEmpty > 0
	}
	needle := compactForQuoteMatch(quoteText)
	if utf8.RuneCountInString(needle) < 2 {
		return false
	}
	return strings.Contains(haystack, needle)
}
