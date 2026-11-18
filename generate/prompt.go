// Package generate 负责拼 prompt 与调用对话模型。
package generate

import (
	"fmt"
	"strings"

	"knowledge-qa/retrieve"
	"knowledge-qa/store"
)

// RefuseAnswer 是检索置信度不足或资料不够时的固定拒答文案。
const RefuseAnswer = "文档中没有依据"

// BuildPrompt 生成阶段 11 G1 的 Quote-First 结构化 JSON 提示词（单轮 user message）：
//   - 保持 V1 切片 "[1] 内容" 与 V2 切片 "[1]（项目 | 类型 | 状态） 内容" 原格式兼容；
//   - 明确要求先在 quotes 中逐字摘录资料原句，再独立给出 decision（"answer"|"refuse"）与自然语言 answer；
//   - 若问句追问资料未明确写出的参数/实现/路径/阈值，即使出现同一 API 名也禁止外推，必须设为 decision="refuse"。
func BuildPrompt(question string, top []retrieve.MergedChunk) string {
	var sb strings.Builder
	sb.WriteString("你是一个严格的文档问答助手，必须只输出一个合法 JSON 对象，不要输出任何额外解释文本。\n\n")
	sb.WriteString("输出 JSON 格式要求：\n")
	sb.WriteString("{\"quotes\":[{\"source\":1,\"text\":\"资料中的逐字原文摘录\"}],\"decision\":\"answer\",\"answer\":\"根据资料得出的结论 [1]。\"}\n")
	sb.WriteString("当资料不足以回答时输出：\n")
	sb.WriteString("{\"quotes\":[],\"decision\":\"refuse\",\"answer\":\"\"}\n\n")
	sb.WriteString("推理与判定规则：\n")
	sb.WriteString("1. Quote-First：只能使用下面提供的资料，禁止使用外部知识，也不要补充资料未写的总结性推论或常识解释；必须先在 quotes 中逐字摘录支撑结论的资料原文，source 为资料编号（1 到 N），text 逐字来自该条资料。\n")
	sb.WriteString("2. 若问句追问具体参数值、默认路径/目录、数值阈值/上限、底层实现库，或以“是否会自动…… / 能否…… / 是否支持……”询问未写明的附加检查或能力边界时：如果要给出否定结论（如“不能/不会/不适合/不支持”），quotes 中必须明确写有禁止、否定或排他条款；只要 quotes 仅描述了常规用途而未明确肯定或否定该附加行为，严禁凭常识补全“不会/不支持”，也严禁在 answer 中出现“资料未说明/未提及……”式解释，必须设为 \"decision\":\"refuse\"、\"quotes\":[]、\"answer\":\"\"。\n")
	sb.WriteString("3. 若 quotes 中的原文可直接回答、通过同义改写/逻辑边界推导回答，或提问前提与资料事实相反但原文明确写出了实际行为（如直接 panic 而非返回 error）、明确禁止的用法或推荐替代方案，应设为 \"decision\":\"answer\"，并在 answer 中严格依据 quotes 如实作答或纠正前提，切勿因问法措辞不同而误拒。\n")
	sb.WriteString("4. 当 decision 为 \"answer\" 时，answer 必须为自然语言正文（不能嵌套 JSON），且每一句（每个句号、分号或独立换行陈述）末尾必须标注来源编号，格式 [n]，可标多个如 [1][3]。\n")
	if needsGovernanceRule(top) {
		sb.WriteString("5. 请注意各段标头中的文档类型（spec 正式规范 > rfc 方案 > note 纪要）与状态：优先依据 active 规范作答，切勿把 deprecated（已废弃）文档当成现行生效规范；若仅 deprecated 文档中包含所问历史规则，仍可依据原文如实作答并标注来源编号 [n]。\n\n")
	} else {
		sb.WriteString("\n")
	}
	sb.WriteString("资料：\n")
	for i, sc := range top {
		if header := formatChunkMetaHeader(sc.Chunk); header != "" {
			fmt.Fprintf(&sb, "[%d]%s %s\n\n", i+1, header, sc.Chunk.Content)
		} else {
			fmt.Fprintf(&sb, "[%d] %s\n\n", i+1, sc.Chunk.Content)
		}
	}
	fmt.Fprintf(&sb, "问题：%s", question)
	return sb.String()
}

func needsGovernanceRule(top []retrieve.MergedChunk) bool {
	for _, sc := range top {
		status := strings.ToLower(strings.TrimSpace(sc.Chunk.DocStatus))
		level := strings.ToLower(strings.TrimSpace(sc.Chunk.DocLevel))
		if status == "deprecated" || (level != "" && level != "spec") {
			return true
		}
	}
	return false
}

func formatChunkMetaHeader(c store.Chunk) string {
	if c.ProjectSlug == "" && c.DocLevel == "" && c.DocStatus == "" {
		return ""
	}
	proj := c.ProjectSlug
	if proj == "" {
		proj = "default"
	}
	level := c.DocLevel
	if level == "" {
		level = "spec"
	}
	status := c.DocStatus
	if status == "" {
		status = "active"
	}
	statusLabel := status
	if strings.EqualFold(status, "deprecated") {
		statusLabel = "deprecated（已废弃）"
		if c.ReplacedByTitle != "" {
			statusLabel += "，替代文档: " + c.ReplacedByTitle
		}
	}
	return fmt.Sprintf("（项目: %s | 类型: %s | 状态: %s）", proj, level, statusLabel)
}

// AppendRetryReason 在 prompt 末尾追加引用/格式校验失败的原因，供重试使用（阶段 4/11）。
// 例如："你上次引用了不存在的编号 [7]，只能使用 [1]-[5]"。
func AppendRetryReason(prompt, reason string) string {
	return prompt + "\n\n注意：" + reason
}
