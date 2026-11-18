package ingest

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gomutex/godocx/docx"
	"github.com/gomutex/godocx/packager"
	"github.com/gomutex/godocx/wml/ctypes"
	"github.com/gomutex/godocx/wml/stypes"
	"github.com/ledongthuc/pdf"
	"github.com/xuri/excelize/v2"
)

// minPDFCharsPerPage 是 PDF 文本层有效可读字符平均每页下限；低于该值高度疑似扫描件/无文本层。
const minPDFCharsPerPage = 30

// ErrScannedPDFWithoutVLM 在检测到无文本层或疑似扫描件 PDF 且未配置 LLM_VLM_MODEL 时返回。
var ErrScannedPDFWithoutVLM = errors.New("检测到 PDF 疑似扫描件或无文本层，请先进行 OCR 或配置 LLM_VLM_MODEL")

// Converter 定义多格式原始文件转标准 Markdown 的统一接口。
type Converter interface {
	Convert(filename string, raw []byte) (markdown string, sourceType string, err error)
}

// VLMTranscriber 定义可选多模态识别钩子（用于扫描件 PDF 或图片转 Markdown）。
type VLMTranscriber interface {
	Enabled() bool
	TranscribeImage(mimeType string, raw []byte) (string, error)
	TranscribePDF(filename string, raw []byte) (string, error)
}

// ConvertOptions 配置格式转换器行为。
type ConvertOptions struct {
	Engine string         // "native"（默认纯 Go）或 "markitdown"（本机 CLI）
	VLM    VLMTranscriber // 可选：未配置时为 nil
}

// DefaultConverter 实现 Converter 接口。
type DefaultConverter struct {
	engine string
	vlm    VLMTranscriber
}

// NewConverter 按显式选项构建格式转换器。
func NewConverter(opts ConvertOptions) Converter {
	eng := strings.ToLower(strings.TrimSpace(opts.Engine))
	if eng == "" {
		eng = "native"
	}
	return &DefaultConverter{engine: eng, vlm: opts.VLM}
}

// NewConverterFromEnv 从环境变量（CONVERT_ENGINE、LLM_VLM_MODEL 等）读取配置构建转换器。
func NewConverterFromEnv() Converter {
	vlmModel := strings.TrimSpace(os.Getenv("LLM_VLM_MODEL"))
	var vlm VLMTranscriber
	if vlmModel != "" {
		baseURL := firstNonEmptyEnv("VLM_BASE_URL", "LLM_BASE_URL")
		apiKey := firstNonEmptyEnv("VLM_API_KEY", "LLM_API_KEY")
		if baseURL != "" && apiKey != "" {
			vlm = NewVLMClient(baseURL, apiKey, vlmModel)
		}
	}
	return NewConverter(ConvertOptions{
		Engine: os.Getenv("CONVERT_ENGINE"),
		VLM:    vlm,
	})
}

func firstNonEmptyEnv(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

// Convert 是使用环境变量默认配置的便捷转换入口。
func Convert(filename string, raw []byte) (markdown string, sourceType string, err error) {
	return NewConverterFromEnv().Convert(filename, raw)
}

// Convert 将 .md / .txt / .docx / .xlsx / .pdf（以及配置 VLM 后的图片）转为标准 Markdown。
func (c *DefaultConverter) Convert(filename string, raw []byte) (string, string, error) {
	if len(raw) == 0 {
		return "", "", errors.New("文件内容为空，无法导入")
	}

	sourceType, err := detectSourceType(filename)
	if err != nil {
		return "", "", err
	}

	switch c.engine {
	case "", "native":
	case "markitdown":
		md, err := convertWithMarkitdown(filename, raw)
		if err != nil {
			return "", sourceType, err
		}
		return md, sourceType, nil
	default:
		return "", sourceType, fmt.Errorf("不支持的 CONVERT_ENGINE=%q，仅支持 native 或 markitdown", c.engine)
	}

	switch sourceType {
	case "md", "txt":
		md, err := convertUTF8Text(raw)
		return md, sourceType, err
	case "docx":
		md, err := convertDOCX(raw)
		return md, sourceType, err
	case "xlsx":
		md, err := convertXLSX(raw)
		return md, sourceType, err
	case "pdf":
		md, err := convertPDF(filename, raw, c.vlm)
		return md, sourceType, err
	case "img":
		if c.vlm == nil || !c.vlm.Enabled() {
			return "", sourceType, errors.New("检测到图片文件，需先配置环境变量 LLM_VLM_MODEL 后方可转写为 Markdown 入库")
		}
		mimeType := imageMIMEType(filepath.Ext(filename))
		md, err := c.vlm.TranscribeImage(mimeType, raw)
		if err != nil {
			return "", sourceType, err
		}
		md = normalizeMarkdown(md)
		if md == "" {
			return "", sourceType, errors.New("VLM 未从图片中识别出有效文本内容")
		}
		return md, sourceType, nil
	default:
		return "", "", fmt.Errorf("不支持的文件格式：%s", filename)
	}
}

func detectSourceType(filename string) (string, error) {
	ext := strings.ToLower(strings.TrimSpace(filepath.Ext(strings.TrimSpace(filename))))
	switch ext {
	case ".md", ".markdown":
		return "md", nil
	case ".txt":
		return "txt", nil
	case ".docx":
		return "docx", nil
	case ".xlsx":
		return "xlsx", nil
	case ".pdf":
		return "pdf", nil
	case ".png", ".jpg", ".jpeg", ".webp":
		return "img", nil
	default:
		return "", fmt.Errorf("不支持的文件格式 %q，仅支持 .md / .txt / .docx / .xlsx / .pdf（以及配置 VLM 后的图片）", ext)
	}
}

func imageMIMEType(ext string) string {
	switch strings.ToLower(ext) {
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		return "image/jpeg"
	}
}

func normalizeMarkdown(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.TrimSpace(s)
}

// ---- .md / .txt ----

func convertUTF8Text(raw []byte) (string, error) {
	cleaned := bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	if bytes.IndexByte(cleaned, 0) >= 0 {
		return "", errors.New("文本文件包含二进制空字节，已拒绝导入")
	}
	if !utf8.Valid(cleaned) {
		return "", errors.New("文件不是合法的 UTF-8 编码文本，请转换为 UTF-8 后重试")
	}
	md := normalizeMarkdown(string(cleaned))
	if md == "" {
		return "", errors.New("文档正文为空，无法导入")
	}
	return md, nil
}

// ---- .docx（pure Go: github.com/gomutex/godocx） ----

func convertDOCX(raw []byte) (string, error) {
	rawCopy := append([]byte(nil), raw...)
	rd, err := packager.Unpack(&rawCopy)
	if err != nil {
		return "", fmt.Errorf("DOCX 解析失败（文件可能损坏或格式不合法）：%w", err)
	}
	if rd == nil || rd.Document == nil || rd.Document.Body == nil {
		return "", errors.New("DOCX 文档结构缺失正文内容")
	}

	// godocx v0.1.5 中 docx.Table 的内部结构未导出，因此对其含表格时通过 Document.MarshalXML 一次提取全部 ctypes.Table，
	// 仍严格按 Body.Children 的原始顺序与段落交错输出。
	var tables []ctypes.Table
	hasTables := false
	for _, child := range rd.Document.Body.Children {
		if child.Table != nil {
			hasTables = true
			break
		}
	}
	if hasTables {
		tables, err = extractDOCXTables(rd.Document)
		if err != nil {
			return "", fmt.Errorf("DOCX 表格解析失败：%w", err)
		}
	}

	var blocks []string
	tableIdx := 0
	for _, child := range rd.Document.Body.Children {
		if child.Para != nil {
			line := formatDOCXParagraph(rd, child.Para)
			if line != "" {
				blocks = append(blocks, line)
			}
			continue
		}
		if child.Table != nil {
			if tableIdx < len(tables) {
				if mdTable := formatDOCXTable(tables[tableIdx]); mdTable != "" {
					blocks = append(blocks, mdTable)
				}
				tableIdx++
			}
		}
	}

	md := normalizeMarkdown(strings.Join(blocks, "\n\n"))
	if md == "" {
		return "", errors.New("DOCX 文档未提取到有效文本内容")
	}
	return md, nil
}

func extractDOCXTables(doc *docx.Document) ([]ctypes.Table, error) {
	xmlBytes, err := xml.Marshal(doc)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Body struct {
			Tables []ctypes.Table `xml:"tbl"`
		} `xml:"body"`
	}
	if err := xml.Unmarshal(xmlBytes, &parsed); err != nil {
		return nil, err
	}
	return parsed.Body.Tables, nil
}

func formatDOCXParagraph(rd *docx.RootDoc, p *docx.Paragraph) string {
	if p == nil {
		return ""
	}
	ct := p.GetCT()
	if ct == nil {
		return ""
	}
	text := strings.TrimSpace(extractDOCXParagraphText(ct))
	if text == "" {
		return ""
	}

	level := detectDOCXHeadingLevel(rd, p, ct)
	if level >= 1 && level <= 3 {
		// 标题文本合并内部换行，避免把标题拆成多行
		singleLine := strings.Join(strings.Fields(text), " ")
		return strings.Repeat("#", level) + " " + singleLine
	}
	return text
}

func detectDOCXHeadingLevel(rd *docx.RootDoc, p *docx.Paragraph, ct *ctypes.Paragraph) int {
	if ct.Property == nil {
		return 0
	}
	if ct.Property.Style != nil {
		if lvl := matchHeadingStyleName(ct.Property.Style.Val); lvl > 0 {
			return lvl
		}
		if style, err := p.GetStyle(); err == nil && style != nil {
			if style.Name != nil {
				if lvl := matchHeadingStyleName(style.Name.Val); lvl > 0 {
					return lvl
				}
			}
			if style.ID != nil {
				if lvl := matchHeadingStyleName(*style.ID); lvl > 0 {
					return lvl
				}
			}
		} else if rd != nil {
			if style := rd.GetStyleByID(ct.Property.Style.Val, stypes.StyleTypeParagraph); style != nil && style.Name != nil {
				if lvl := matchHeadingStyleName(style.Name.Val); lvl > 0 {
					return lvl
				}
			}
		}
	}
	if ct.Property.OutlineLvl != nil {
		// Word 中 outlineLvl=0/1/2 对应 1/2/3 级标题，9 表示正文
		v := ct.Property.OutlineLvl.Val
		if v >= 0 && v <= 2 {
			return v + 1
		}
	}
	return 0
}

func matchHeadingStyleName(name string) int {
	n := strings.ToLower(strings.TrimSpace(name))
	n = strings.ReplaceAll(n, " ", "")
	switch n {
	case "heading1", "标题1", "title":
		return 1
	case "heading2", "标题2", "subtitle":
		return 2
	case "heading3", "标题3":
		return 3
	default:
		return 0
	}
}

func extractDOCXParagraphText(ct *ctypes.Paragraph) string {
	if ct == nil {
		return ""
	}
	var sb strings.Builder
	for _, ch := range ct.Children {
		if ch.Run != nil {
			appendDOCXRunText(&sb, ch.Run)
		}
		if ch.Link != nil {
			for _, lch := range ch.Link.Children {
				if lch.Run != nil {
					appendDOCXRunText(&sb, lch.Run)
				}
			}
		}
	}
	return sb.String()
}

func appendDOCXRunText(sb *strings.Builder, run *ctypes.Run) {
	if run == nil {
		return
	}
	for _, rc := range run.Children {
		if rc.Text != nil {
			sb.WriteString(rc.Text.Text)
		}
		if rc.Tab != nil {
			sb.WriteByte('\t')
		}
		if rc.Break != nil || rc.CarrRtn != nil {
			sb.WriteByte('\n')
		}
	}
}

func formatDOCXTable(tbl ctypes.Table) string {
	var rows [][]string
	for _, rc := range tbl.RowContents {
		if rc.Row == nil {
			continue
		}
		var cells []string
		for _, tc := range rc.Row.Contents {
			if tc.Cell == nil {
				continue
			}
			cells = append(cells, extractDOCXCellText(tc.Cell))
		}
		rows = append(rows, cells)
	}
	return buildMarkdownTable(rows)
}

func extractDOCXCellText(cell *ctypes.Cell) string {
	var parts []string
	for _, block := range cell.Contents {
		if block.Paragraph != nil {
			if t := strings.TrimSpace(extractDOCXParagraphText(block.Paragraph)); t != "" {
				parts = append(parts, t)
			}
		}
		if block.Table != nil {
			if nested := formatDOCXTable(*block.Table); nested != "" {
				parts = append(parts, strings.ReplaceAll(nested, "\n", " "))
			}
		}
	}
	return strings.Join(parts, " ")
}

// ---- .xlsx（pure Go: github.com/xuri/excelize/v2） ----

func convertXLSX(raw []byte) (string, error) {
	f, err := excelize.OpenReader(bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("XLSX 解析失败（文件可能损坏或已加密）：%w", err)
	}
	defer f.Close()

	var sections []string
	for _, sheet := range f.GetSheetList() {
		rows, err := f.GetRows(sheet)
		if err != nil {
			return "", fmt.Errorf("读取工作表 %q 失败：%w", sheet, err)
		}
		mdTable := buildMarkdownTable(rows)
		if mdTable == "" {
			continue
		}
		sections = append(sections, fmt.Sprintf("## %s\n\n%s", strings.TrimSpace(sheet), mdTable))
	}

	if len(sections) == 0 {
		return "", errors.New("XLSX 文件未包含有效非空工作表")
	}
	return normalizeMarkdown(strings.Join(sections, "\n\n")), nil
}

// buildMarkdownTable 跳过全空行与全空列，将二维矩阵规范化为标准 Markdown 管道表格。
func buildMarkdownTable(rawRows [][]string) string {
	var nonEmptyRows [][]string
	maxCols := 0
	for _, row := range rawRows {
		cleaned := make([]string, len(row))
		hasValue := false
		for i, cell := range row {
			v := sanitizeTableCell(cell)
			cleaned[i] = v
			if v != "" {
				hasValue = true
			}
		}
		if !hasValue {
			continue
		}
		if len(cleaned) > maxCols {
			maxCols = len(cleaned)
		}
		nonEmptyRows = append(nonEmptyRows, cleaned)
	}
	if len(nonEmptyRows) == 0 || maxCols == 0 {
		return ""
	}

	// 识别非全空列，避免 Excel 右侧/中间大量空白列污染表格结构
	var activeCols []int
	for col := 0; col < maxCols; col++ {
		hasColVal := false
		for _, row := range nonEmptyRows {
			if col < len(row) && row[col] != "" {
				hasColVal = true
				break
			}
		}
		if hasColVal {
			activeCols = append(activeCols, col)
		}
	}
	if len(activeCols) == 0 {
		return ""
	}

	var sb strings.Builder
	writeTableRow(&sb, nonEmptyRows[0], activeCols)
	sb.WriteByte('|')
	for range activeCols {
		sb.WriteString(" --- |")
	}
	sb.WriteByte('\n')
	for _, row := range nonEmptyRows[1:] {
		writeTableRow(&sb, row, activeCols)
	}
	return strings.TrimSpace(sb.String())
}

func writeTableRow(sb *strings.Builder, row []string, activeCols []int) {
	sb.WriteByte('|')
	for _, col := range activeCols {
		val := ""
		if col < len(row) {
			val = row[col]
		}
		sb.WriteByte(' ')
		sb.WriteString(val)
		sb.WriteString(" |")
	}
	sb.WriteByte('\n')
}

func sanitizeTableCell(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", `\|`)
	return strings.TrimSpace(s)
}

// ---- .pdf（pure Go: github.com/ledongthuc/pdf + 扫描件护栏） ----

func convertPDF(filename string, raw []byte, vlm VLMTranscriber) (string, error) {
	text, numPages, err := extractPDFText(raw)
	if err != nil {
		return "", fmt.Errorf("PDF 解析失败（文件可能已加密或损坏）：%w", err)
	}

	readable, garbled := countTextQuality(text)
	avgPerPage := 0.0
	if numPages > 0 {
		avgPerPage = float64(readable) / float64(numPages)
	}

	// 护栏：每页平均可读字符 < 30 或明显乱码比例过高，均判定为疑似扫描件/无有效文本层
	suspectedScanned := numPages == 0 || avgPerPage < minPDFCharsPerPage || (garbled > 0 && garbled*4 > readable)
	if suspectedScanned {
		if vlm != nil && vlm.Enabled() {
			md, vlmErr := vlm.TranscribePDF(filename, raw)
			if vlmErr != nil {
				return "", fmt.Errorf("PDF 疑似扫描件，调用 VLM 识别失败：%w", vlmErr)
			}
			md = normalizeMarkdown(md)
			if md == "" {
				return "", errors.New("PDF 疑似扫描件，VLM 未返回有效 Markdown 内容")
			}
			return md, nil
		}
		return "", ErrScannedPDFWithoutVLM
	}

	cleaned := cleanupExtractedPDFText(text)
	if cleaned == "" {
		return "", ErrScannedPDFWithoutVLM
	}
	return cleaned, nil
}

func extractPDFText(raw []byte) (text string, numPages int, err error) {
	defer func() {
		if r := recover(); r != nil {
			text = ""
			numPages = 0
			err = fmt.Errorf("内部流解析异常：%v", r)
		}
	}()

	reader, err := pdf.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "", 0, err
	}
	numPages = reader.NumPage()
	if numPages <= 0 {
		return "", 0, nil
	}

	var pages []string
	for i := 1; i <= numPages; i++ {
		p := reader.Page(i)
		pageText, pageErr := p.GetPlainText(nil)
		if pageErr != nil {
			return "", numPages, pageErr
		}
		trimmed := strings.TrimSpace(pageText)
		if trimmed != "" {
			pages = append(pages, trimmed)
		}
	}
	return strings.Join(pages, "\n\n"), numPages, nil
}

func countTextQuality(s string) (readable int, garbled int) {
	for _, r := range s {
		if r == utf8.RuneError {
			garbled++
			continue
		}
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			garbled++
			continue
		}
		if unicode.Is(unicode.Han, r) || unicode.IsLetter(r) || unicode.IsDigit(r) {
			readable++
		}
	}
	return readable, garbled
}

func cleanupExtractedPDFText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blankRun := 0
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t")
		if strings.TrimSpace(trimmed) == "" {
			blankRun++
			if blankRun <= 1 && len(out) > 0 {
				out = append(out, "")
			}
			continue
		}
		blankRun = 0
		out = append(out, strings.TrimSpace(trimmed))
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// ---- 可选外部 CLI：markitdown ----

func convertWithMarkitdown(filename string, raw []byte) (string, error) {
	ext := filepath.Ext(strings.TrimSpace(filename))
	tmp, err := os.CreateTemp("", "kqa-convert-*"+ext)
	if err != nil {
		return "", fmt.Errorf("创建临时文件失败：%w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("写入临时文件失败：%w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("关闭临时文件失败：%w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "markitdown", tmpPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return "", fmt.Errorf("markitdown 转换失败：%s", errMsg)
	}
	md := normalizeMarkdown(stdout.String())
	if md == "" {
		return "", errors.New("markitdown 返回空内容")
	}
	return md, nil
}

// ---- 可选 VLM 客户端（OpenAI 兼容多模态 chat/completions） ----

// VLMClient 调用 OpenAI 兼容多模态模型把图片/扫描件转写为标准 Markdown。
type VLMClient struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

// NewVLMClient 创建 VLM 客户端。
func NewVLMClient(baseURL, apiKey, model string) *VLMClient {
	return &VLMClient{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKey:  strings.TrimSpace(apiKey),
		model:   strings.TrimSpace(model),
		client:  &http.Client{Timeout: 120 * time.Second},
	}
}

// Enabled 判断 VLM 是否已配置可用。
func (v *VLMClient) Enabled() bool {
	return v != nil && v.model != "" && v.baseURL != "" && v.apiKey != ""
}

// TranscribeImage 调用多模态模型把图片转写为 Markdown。
func (v *VLMClient) TranscribeImage(mimeType string, raw []byte) (string, error) {
	if !v.Enabled() {
		return "", errors.New("未配置 LLM_VLM_MODEL，无法识别图片")
	}
	dataURL := "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(raw)
	payload := map[string]any{
		"model":       v.model,
		"temperature": 0,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{
						"type": "text",
						"text": "请把图片中的所有文字、标题层级与表格准确转写为标准 Markdown（表格使用 | 列 | 管道语法），不要添加图片中没有的内容。",
					},
					{
						"type": "image_url",
						"image_url": map[string]string{
							"url": dataURL,
						},
					},
				},
			},
		},
	}
	return v.doMultimodalChat(payload)
}

// TranscribePDF 为无文本层 PDF 预留多模态文件转写接口。
func (v *VLMClient) TranscribePDF(filename string, raw []byte) (string, error) {
	if !v.Enabled() {
		return "", ErrScannedPDFWithoutVLM
	}
	dataURL := "data:application/pdf;base64," + base64.StdEncoding.EncodeToString(raw)
	payload := map[string]any{
		"model":       v.model,
		"temperature": 0,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{
						"type": "text",
						"text": "请将该 PDF 扫描件中的所有文字、标题与表格逐页准确转写为标准 Markdown，不要补充原文没有的内容。",
					},
					{
						"type": "file",
						"file": map[string]string{
							"filename":  filepath.Base(filename),
							"file_data": dataURL,
						},
					},
				},
			},
		},
	}
	return v.doMultimodalChat(payload)
}

func (v *VLMClient) doMultimodalChat(payload map[string]any) (string, error) {
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, v.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+v.apiKey)

	resp, err := v.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("VLM 请求失败：%w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("VLM API 返回 %d：%s", resp.StatusCode, string(raw))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("VLM 响应解析失败：%w", err)
	}
	if out.Error != nil {
		return "", fmt.Errorf("VLM API 错误：%s", out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return "", errors.New("VLM API 返回空 choices")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}
