// Package ingest 负责语料切片与 embedding 调用。
package ingest

import (
	"strings"
	"unicode/utf8"
)

// ChunkPiece 是切片器输出的一段：标题路径 + 正文。
type ChunkPiece struct {
	HeadingPath string
	Content     string
}

// 切片器 v2 的两条长度阈值（按字符数，与 MySQL CHAR_LENGTH 一致）。
const (
	maxChunkChars = 800 // 超过则按空行二次切分
	minChunkChars = 100 // 不足且与下一段同父标题时合并
)

// SplitMarkdown 是切片器 v1：只按 # / ## / ### 标题切，代码块不特殊处理。
// 保留它是为了阶段 2 验收里的"v1 粗切 vs v2 细切"对比实验。
func SplitMarkdown(text string) []ChunkPiece {
	var pieces []ChunkPiece
	var stack [3]string
	var body []string
	flush := func() {
		content := strings.TrimSpace(strings.Join(body, "\n"))
		body = body[:0]
		if content == "" {
			return
		}
		pieces = append(pieces, ChunkPiece{
			HeadingPath: joinPath(stack),
			Content:     content,
		})
	}

	for _, line := range strings.Split(text, "\n") {
		if level := headingLevel(line); level > 0 {
			flush()
			pushHeading(&stack, level, line)
			continue
		}
		body = append(body, line)
	}
	flush()
	return pieces
}

// SplitMarkdownV2 切片器 v2，在 v1 基础上加三条规则：
//  1. 代码块保护：``` 围栏内不识别标题、二次切分时代码块整体不可切；
//  2. 超长二次切分：单段 >800 字符时按空行（段落边界）再切，子段继承同一 heading_path；
//  3. 过短合并：<100 字符且与下一段同父标题时，与下一段合并。
func SplitMarkdownV2(text string) []ChunkPiece {
	return mergeShort(splitLong(splitByHeading(text)))
}

// ---- 第一步：按标题切（代码块内的 # 行不算标题） ----

// splitByHeading 按标题层级切分，围栏代码块内的内容原样归属最近的标题。
func splitByHeading(text string) []ChunkPiece {
	var pieces []ChunkPiece
	var stack [3]string
	var body []string
	inCode := false // 是否处于 ``` 围栏内

	flush := func() {
		content := strings.TrimSpace(strings.Join(body, "\n"))
		body = body[:0]
		if content == "" {
			return
		}
		pieces = append(pieces, ChunkPiece{
			HeadingPath: joinPath(stack),
			Content:     content,
		})
	}

	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		// 围栏开关：进入或退出代码块
		if strings.HasPrefix(trimmed, "```") {
			inCode = !inCode
			body = append(body, line)
			continue
		}
		// 代码块内的行一律当正文，不识别标题
		if !inCode {
			if level := headingLevel(line); level > 0 {
				flush()
				pushHeading(&stack, level, line)
				continue
			}
		}
		body = append(body, line)
	}
	flush()
	return pieces
}

// ---- 第二步：超长段按空行二次切分 ----

// splitLong 把超过 maxChunkChars 的段按"块"（空行分隔的段落 / 完整代码块）再切。
// 块是最小单位：代码块不会被从中间切断，因此单个巨大代码块可能超过阈值，属允许例外。
func splitLong(pieces []ChunkPiece) []ChunkPiece {
	var out []ChunkPiece
	for _, p := range pieces {
		if utf8.RuneCountInString(p.Content) <= maxChunkChars {
			out = append(out, p)
			continue
		}
		blocks := splitBlocks(p.Content)
		var buf []string
		bufLen := 0
		flushBuf := func() {
			content := strings.TrimSpace(strings.Join(buf, "\n\n"))
			buf = buf[:0]
			bufLen = 0
			if content != "" {
				out = append(out, ChunkPiece{HeadingPath: p.HeadingPath, Content: content})
			}
		}
		for _, b := range blocks {
			bLen := utf8.RuneCountInString(b)
			// 当前缓冲非空、且加入本块会超阈值 → 先落盘，开新子段
			if bufLen > 0 && bufLen+bLen > maxChunkChars {
				flushBuf()
			}
			buf = append(buf, b)
			bufLen += bLen
		}
		flushBuf()
	}
	return out
}

// splitBlocks 把正文切成"块"：围栏代码块为一块，其余按空行分段。
func splitBlocks(content string) []string {
	var blocks []string
	var cur []string
	inCode := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCode = !inCode
			cur = append(cur, line)
			continue
		}
		if !inCode && trimmed == "" {
			if len(cur) > 0 {
				blocks = append(blocks, strings.Join(cur, "\n"))
				cur = cur[:0]
			}
			continue
		}
		cur = append(cur, line)
	}
	if len(cur) > 0 {
		blocks = append(blocks, strings.Join(cur, "\n"))
	}
	return blocks
}

// ---- 第三步：过短段与下一段合并 ----

// mergeShort 不足 minChunkChars 且与下一段同父标题时，并入下一段。
func mergeShort(pieces []ChunkPiece) []ChunkPiece {
	var out []ChunkPiece
	for i := 0; i < len(pieces); i++ {
		p := pieces[i]
		// 过短 且 存在下一段 且 与下一段同父标题 → 并入下一段开头
		if utf8.RuneCountInString(p.Content) < minChunkChars &&
			i+1 < len(pieces) &&
			parentPath(p.HeadingPath) == parentPath(pieces[i+1].HeadingPath) {
			pieces[i+1].Content = p.Content + "\n\n" + pieces[i+1].Content
			continue
		}
		out = append(out, p)
	}
	return out
}

// ---- 共用工具 ----

// headingLevel 返回标题级别（1~3），不是标题返回 0。
func headingLevel(line string) int {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "### ") {
		return 3
	}
	if strings.HasPrefix(trimmed, "## ") {
		return 2
	}
	if strings.HasPrefix(trimmed, "# ") {
		return 1
	}
	return 0
}

// pushHeading 把新标题压入标题栈，并清掉更低级（数字更大）的标题。
func pushHeading(stack *[3]string, level int, line string) {
	trimmed := strings.TrimSpace(line)
	stack[level-1] = strings.TrimSpace(trimmed[level+1:])
	for i := level; i < 3; i++ {
		stack[i] = ""
	}
}

// joinPath 把标题栈拼成 "一级 > 二级 > 三级"，跳过空层。
func joinPath(stack [3]string) string {
	var parts []string
	for _, h := range stack {
		if h != "" {
			parts = append(parts, h)
		}
	}
	return strings.Join(parts, " > ")
}

// parentPath 取 heading_path 去掉最后一段的"父路径"；顶层标题的父路径等于它自身。
func parentPath(path string) string {
	idx := strings.LastIndex(path, " > ")
	if idx < 0 {
		return path
	}
	return path[:idx]
}
