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

// BuildPrompt prompt v3 模板（阶段 6 迭代 1；迭代 5 的 v4 因引发 q_id=39 误拒回退已回滚至此版本）：
// 在 v2 基础上补充两项明确约束以消除误拒与越界外推：
//  1. 禁止补充资料未写的总结性推论或常识解释；
//  2. 允许依据资料做直接条件/数值推导及前提纠偏，不因问法措辞差异而误拒。
//
// 阶段 10 增量兼容：
//   - 当切片携带 V2 项目与治理元数据时，在 [n] 后标注（项目 | 类型 | 状态），
//     若存在非 spec 或 deprecated 切片则补充状态优先级提示；
//   - 若无 V2 元数据（V1 最小表结构/单测），保持完全相同的 [n] 正文输出格式。
func BuildPrompt(question string, top []retrieve.MergedChunk) string {
	var sb strings.Builder
	sb.WriteString("你是一个严格的文档问答助手。规则：\n")
	sb.WriteString("1. 只能使用下面提供的资料回答，禁止使用自己的外部知识，也不要补充资料未写的总结性推论或常识解释。\n")
	sb.WriteString("2. 若资料中的条款可直接推导出答案（包括数值边界与功能优势），或提问前提与资料不符但资料明确说明了实际行为（如失败时 panic 而非返回 error），请直接依据资料作答或纠正，不要误拒。\n")
	sb.WriteString("3. 每一句（每个句号、分号或独立换行陈述）末尾必须标注来源编号，格式 [n]，可标多个如 [1][3]。\n")
	if needsGovernanceRule(top) {
		sb.WriteString("4. 如果资料仍不足以回答问题，只输出：文档中没有依据。\n")
		sb.WriteString("5. 请注意各段标头中的文档类型（spec 正式规范 > rfc 方案 > note 纪要）与状态：优先依据 active 规范作答，切勿把 deprecated（已废弃）文档当成现行生效规范；若仅 deprecated 文档中包含所问历史规则，仍可依据原文如实作答并标注来源编号 [n]。\n\n")
	} else {
		sb.WriteString("4. 如果资料仍不足以回答问题，只输出：文档中没有依据。\n\n")
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

// AppendRetryReason 在 prompt 末尾追加引用校验失败的原因，供重试使用（阶段 4）。
// 例如："你上次引用了不存在的编号 [7]，只能使用 [1]-[5]"。
func AppendRetryReason(prompt, reason string) string {
	return prompt + "\n\n注意：" + reason
}
