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

// 切片器 v2/v3 的长度阈值（按字符数，与 MySQL CHAR_LENGTH 一致）。
const (
	maxChunkChars   = 800 // 超过则按空行/表格行/句末边界二次切分
	minChunkChars   = 100 // 不足且与下一段同父标题时合并
	v3OverlapChars  = 80  // v3 同一标题二次拆分出的相邻文本子段末尾重叠字符数
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

// ---- 切片器 v3：表格保护 + 句末递归切分 + 80 字符 Overlap ----

// SplitMarkdownV3 切片器 v3（保留 SplitMarkdown / SplitMarkdownV2 不变）：
//  1. 代码块保护：继承 v2，``` 围栏内不识别标题、不从中间切断；
//  2. Markdown 表格保护：连续 |...| 表格行视作原子块；<=800 整表保留，>800 按行分片并在每个子切片保留前两行表头；
//  3. 无标题/超长文本递归切分：优先空行，单块 >800 按句末标点（。！？\n 等）拆至 <=800，相邻文本子段保留 80 字符 Overlap；
//  4. 过短段合并：继承 v2（<100 字符且与下一段同父标题时合并，但不打乱已分片的表格头）。
func SplitMarkdownV3(text string) []ChunkPiece {
	normalized := strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	raw := splitByHeading(normalized)
	v3 := splitLongV3(raw)
	return mergeShortV3(v3)
}

type v3BlockKind int

const (
	v3BlockProse v3BlockKind = iota
	v3BlockCode
	v3BlockTable
)

type v3Block struct {
	kind v3BlockKind
	text string
}

type v3Unit struct {
	kind         v3BlockKind
	text         string
	isSplitTable bool
}

type v3Piece struct {
	piece            ChunkPiece
	isSplitTablePart bool
}

func splitLongV3(pieces []ChunkPiece) []v3Piece {
	var out []v3Piece
	for _, p := range pieces {
		if utf8.RuneCountInString(p.Content) <= maxChunkChars {
			out = append(out, v3Piece{piece: p})
			continue
		}

		blocks := splitBlocksV3(p.Content)
		units := expandBlocksToUnitsV3(blocks)

		var buf []string
		bufLen := 0
		hasTableOrCode := false
		lastProseText := ""

		flushBuf := func() {
			if len(buf) == 0 {
				return
			}
			content := strings.TrimSpace(strings.Join(buf, "\n\n"))
			buf = buf[:0]
			bufLen = 0
			wasPureProse := !hasTableOrCode
			hasTableOrCode = false
			if content == "" {
				return
			}
			out = append(out, v3Piece{
				piece: ChunkPiece{HeadingPath: p.HeadingPath, Content: content},
			})
			if wasPureProse {
				lastProseText = content
			} else {
				lastProseText = ""
			}
		}

		for _, u := range units {
			if u.isSplitTable {
				flushBuf()
				lastProseText = ""
				out = append(out, v3Piece{
					piece:            ChunkPiece{HeadingPath: p.HeadingPath, Content: u.text},
					isSplitTablePart: true,
				})
				continue
			}

			uLen := utf8.RuneCountInString(u.text)
			if bufLen == 0 && u.kind == v3BlockProse && lastProseText != "" {
				overlap := tailRunes(lastProseText, v3OverlapChars)
				if overlap != "" {
					ovLen := utf8.RuneCountInString(overlap)
					if ovLen+1+uLen <= maxChunkChars {
						combined := overlap + "\n" + u.text
						buf = append(buf, combined)
						bufLen = utf8.RuneCountInString(combined)
						continue
					}
				}
			}

			sepCost := 0
			if bufLen > 0 {
				sepCost = 2 // "\n\n"
			}
			if bufLen > 0 && bufLen+sepCost+uLen > maxChunkChars {
				flushBuf()
				// 新子段若为普通文本且上一段也是普通文本，自动带入末尾 Overlap
				if u.kind == v3BlockProse && lastProseText != "" {
					overlap := tailRunes(lastProseText, v3OverlapChars)
					if overlap != "" {
						ovLen := utf8.RuneCountInString(overlap)
						if ovLen+1+uLen <= maxChunkChars {
							combined := overlap + "\n" + u.text
							buf = append(buf, combined)
							bufLen = utf8.RuneCountInString(combined)
							continue
						}
					}
				}
			}

			if u.kind != v3BlockProse {
				hasTableOrCode = true
			}
			buf = append(buf, u.text)
			if bufLen == 0 {
				bufLen = uLen
			} else {
				bufLen += 2 + uLen
			}
		}
		flushBuf()
	}
	return out
}

func splitBlocksV3(content string) []v3Block {
	lines := strings.Split(content, "\n")
	var blocks []v3Block
	var cur []string
	flushProse := func() {
		if len(cur) == 0 {
			return
		}
		t := strings.TrimSpace(strings.Join(cur, "\n"))
		cur = cur[:0]
		if t != "" {
			blocks = append(blocks, v3Block{kind: v3BlockProse, text: t})
		}
	}

	for i := 0; i < len(lines); {
		trimmed := strings.TrimSpace(lines[i])
		// 围栏代码块
		if strings.HasPrefix(trimmed, "```") {
			flushProse()
			codeLines := []string{lines[i]}
			i++
			for i < len(lines) {
				codeLines = append(codeLines, lines[i])
				if strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
					i++
					break
				}
				i++
			}
			blocks = append(blocks, v3Block{kind: v3BlockCode, text: strings.Join(codeLines, "\n")})
			continue
		}

		// Markdown 管道表格（首行 |...| + 次行分隔符 |---|）
		if isMarkdownTableStart(lines, i) {
			flushProse()
			tableLines := []string{strings.TrimSpace(lines[i]), strings.TrimSpace(lines[i+1])}
			i += 2
			for i < len(lines) && isTableRowLine(lines[i]) {
				tableLines = append(tableLines, strings.TrimSpace(lines[i]))
				i++
			}
			blocks = append(blocks, v3Block{kind: v3BlockTable, text: strings.Join(tableLines, "\n")})
			continue
		}

		if trimmed == "" {
			flushProse()
			i++
			continue
		}
		cur = append(cur, lines[i])
		i++
	}
	flushProse()
	return blocks
}

func isTableRowLine(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "|") && strings.Count(t, "|") >= 2
}

func isTableSeparatorLine(line string) bool {
	t := strings.TrimSpace(line)
	if !isTableRowLine(t) || !strings.Contains(t, "-") {
		return false
	}
	for _, r := range t {
		switch r {
		case '|', '-', ':', ' ', '\t':
		default:
			return false
		}
	}
	return true
}

func isMarkdownTableStart(lines []string, idx int) bool {
	if idx+1 >= len(lines) {
		return false
	}
	return isTableRowLine(lines[idx]) && isTableSeparatorLine(lines[idx+1])
}

func expandBlocksToUnitsV3(blocks []v3Block) []v3Unit {
	// 二次切分纯文本单块时预留 80 字符 Overlap 空间，保证拼接 Overlap 后总长仍 <= 800
	proseLimit := maxChunkChars - v3OverlapChars - 2
	if proseLimit < 200 {
		proseLimit = maxChunkChars
	}

	var units []v3Unit
	for _, b := range blocks {
		switch b.kind {
		case v3BlockCode:
			units = append(units, v3Unit{kind: v3BlockCode, text: b.text})
		case v3BlockTable:
			if utf8.RuneCountInString(b.text) <= maxChunkChars {
				units = append(units, v3Unit{kind: v3BlockTable, text: b.text})
			} else {
				for _, part := range splitTableBlock(b.text, maxChunkChars) {
					units = append(units, v3Unit{kind: v3BlockTable, text: part, isSplitTable: true})
				}
			}
		default:
			if utf8.RuneCountInString(b.text) <= proseLimit {
				units = append(units, v3Unit{kind: v3BlockProse, text: b.text})
			} else {
				for _, part := range splitProseBySentence(b.text, proseLimit) {
					units = append(units, v3Unit{kind: v3BlockProse, text: part})
				}
			}
		}
	}
	return units
}

// splitTableBlock 把超过 maxChars 的 Markdown 表格按行拆片，每片都复制前两行表头（|...| 与 |---|）。
func splitTableBlock(tableText string, maxChars int) []string {
	lines := strings.Split(tableText, "\n")
	if len(lines) <= 2 {
		return []string{tableText}
	}
	header := lines[0] + "\n" + lines[1]
	headerRunes := utf8.RuneCountInString(header)

	var chunks []string
	cur := []string{lines[0], lines[1]}
	curLen := headerRunes

	for _, row := range lines[2:] {
		rowLen := utf8.RuneCountInString(row)
		// 仅当当前片已有至少 1 条数据行且加入本行会超限才切分；单行自身超长也保留完整行不切断
		if len(cur) > 2 && curLen+1+rowLen > maxChars {
			chunks = append(chunks, strings.Join(cur, "\n"))
			cur = []string{lines[0], lines[1]}
			curLen = headerRunes
		}
		cur = append(cur, row)
		curLen += 1 + rowLen
	}
	if len(cur) > 2 {
		chunks = append(chunks, strings.Join(cur, "\n"))
	}
	return chunks
}

// splitProseBySentence 按句末标点（。！？\n，以及英文 .!?）递归将超长文本拆为 <= maxChars 的片段。
func splitProseBySentence(text string, maxChars int) []string {
	segments := splitSentenceSegments(text)
	var out []string
	var cur strings.Builder
	curLen := 0

	flush := func() {
		s := strings.TrimSpace(cur.String())
		cur.Reset()
		curLen = 0
		if s != "" {
			out = append(out, s)
		}
	}

	for _, seg := range segments {
		segLen := utf8.RuneCountInString(seg)
		if segLen > maxChars {
			flush()
			for _, hardPart := range splitRunesHard(seg, maxChars) {
				out = append(out, hardPart)
			}
			continue
		}
		if curLen > 0 && curLen+segLen > maxChars {
			flush()
		}
		cur.WriteString(seg)
		curLen += segLen
	}
	flush()
	return out
}

func splitSentenceSegments(text string) []string {
	var segs []string
	start := 0
	for i := 0; i < len(text); {
		r, w := utf8.DecodeRuneInString(text[i:])
		isEnd := r == '。' || r == '！' || r == '？' || r == '\n'
		// 英文句末标点仅在后随空白或位于末尾时切分，避免拆断小数点（3.14）或代码标识符（r.Use）
		if !isEnd && (r == '.' || r == '!' || r == '?') {
			nextIdx := i + w
			if nextIdx >= len(text) {
				isEnd = true
			} else {
				nextR, _ := utf8.DecodeRuneInString(text[nextIdx:])
				if nextR == ' ' || nextR == '\t' || nextR == '\n' {
					isEnd = true
				}
			}
		}
		i += w
		if isEnd {
			if seg := text[start:i]; strings.TrimSpace(seg) != "" {
				segs = append(segs, seg)
			}
			start = i
		}
	}
	if start < len(text) {
		if seg := text[start:]; strings.TrimSpace(seg) != "" {
			segs = append(segs, seg)
		}
	}
	return segs
}

func splitRunesHard(s string, maxChars int) []string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) == 0 {
		return nil
	}
	var out []string
	for start := 0; start < len(runes); start += maxChars {
		end := start + maxChars
		if end > len(runes) {
			end = len(runes)
		}
		if part := strings.TrimSpace(string(runes[start:end])); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func tailRunes(s string, maxRunes int) string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) == 0 || maxRunes <= 0 {
		return ""
	}
	if len(runes) <= maxRunes {
		return strings.TrimSpace(string(runes))
	}
	return strings.TrimSpace(string(runes[len(runes)-maxRunes:]))
}

func mergeShortV3(pieces []v3Piece) []ChunkPiece {
	out := make([]ChunkPiece, 0, len(pieces))
	for i := 0; i < len(pieces); i++ {
		p := pieces[i]
		if !p.isSplitTablePart &&
			i+1 < len(pieces) &&
			!pieces[i+1].isSplitTablePart &&
			utf8.RuneCountInString(p.piece.Content) < minChunkChars &&
			parentPath(p.piece.HeadingPath) == parentPath(pieces[i+1].piece.HeadingPath) &&
			utf8.RuneCountInString(p.piece.Content)+2+utf8.RuneCountInString(pieces[i+1].piece.Content) <= maxChunkChars {
			pieces[i+1].piece.Content = p.piece.Content + "\n\n" + pieces[i+1].piece.Content
			continue
		}
		out = append(out, p.piece)
	}
	return out
}
