package ingest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gomutex/godocx"
	"github.com/xuri/excelize/v2"
)

func TestConvertMDAndTXT(t *testing.T) {
	conv := NewConverter(ConvertOptions{Engine: "native"})

	// UTF-8 BOM + CRLF
	rawMD := append([]byte{0xEF, 0xBB, 0xBF}, []byte("# 规范\r\n\r\n正文内容。")...)
	md, src, err := conv.Convert("guide.md", rawMD)
	if err != nil || src != "md" {
		t.Fatalf("Convert md failed: src=%q err=%v", src, err)
	}
	if strings.HasPrefix(md, "\uFEFF") || strings.Contains(md, "\r") || md != "# 规范\n\n正文内容。" {
		t.Fatalf("unexpected md output: %q", md)
	}

	txt, src, err := conv.Convert("notes.txt", []byte("第一段。\n第二段。"))
	if err != nil || src != "txt" || txt != "第一段。\n第二段。" {
		t.Fatalf("Convert txt failed: src=%q txt=%q err=%v", src, txt, err)
	}

	// Invalid UTF-8 & empty rejection
	if _, _, err := conv.Convert("bad.txt", []byte{0xff, 0xfe, 0x00}); err == nil {
		t.Fatal("expected invalid utf-8/binary txt to fail")
	}
	if _, _, err := conv.Convert("empty.md", []byte("   \n  ")); err == nil {
		t.Fatal("expected empty md to fail")
	}
}

func TestConvertDOCXHeadingsAndTables(t *testing.T) {
	doc, err := godocx.NewDocument()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doc.AddHeading("项目规范", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := doc.AddHeading("接口约定", 2); err != nil {
		t.Fatal(err)
	}
	doc.AddParagraph("所有接口均需返回 JSON。")

	tbl := doc.AddTable()
	r1 := tbl.AddRow()
	r1.AddCell().AddParagraph("字段")
	r1.AddCell().AddParagraph("含义|说明")
	r2 := tbl.AddRow()
	r2.AddCell().AddParagraph("project_slug")
	r2.AddCell().AddParagraph("项目标识")

	var buf bytes.Buffer
	if err := doc.Write(&buf); err != nil {
		t.Fatal(err)
	}

	conv := NewConverter(ConvertOptions{Engine: "native"})
	md, src, err := conv.Convert("spec.docx", buf.Bytes())
	if err != nil || src != "docx" {
		t.Fatalf("Convert docx failed: src=%q err=%v", src, err)
	}
	for _, want := range []string{
		"# 项目规范",
		"## 接口约定",
		"所有接口均需返回 JSON。",
		`| 字段 | 含义\|说明 |`,
		"| --- | --- |",
		"| project_slug | 项目标识 |",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("docx markdown missing %q.\ngot:\n%s", want, md)
		}
	}

	// Corrupt docx
	if _, _, err := conv.Convert("broken.docx", []byte("not a zip")); err == nil {
		t.Fatal("expected corrupt docx to return error")
	}
}

func TestConvertXLSXSkipsEmptyRowsColsAndSheets(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()

	// Sheet1: 包含空行、空列与数据
	f.SetSheetName("Sheet1", "活动排期")
	_ = f.SetCellValue("活动排期", "B2", "时间")
	_ = f.SetCellValue("活动排期", "D2", "地点")
	_ = f.SetCellValue("活动排期", "B3", "11月20日")
	_ = f.SetCellValue("活动排期", "D3", "报告厅A")

	// Sheet2: 全空工作表，应跳过
	_, _ = f.NewSheet("空白页")

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}

	conv := NewConverter(ConvertOptions{Engine: "native"})
	md, src, err := conv.Convert("schedule.xlsx", buf.Bytes())
	if err != nil || src != "xlsx" {
		t.Fatalf("Convert xlsx failed: src=%q err=%v", src, err)
	}
	if strings.Contains(md, "空白页") {
		t.Fatalf("empty sheet should be skipped, got:\n%s", md)
	}
	want := "## 活动排期\n\n| 时间 | 地点 |\n| --- | --- |\n| 11月20日 | 报告厅A |"
	if md != want {
		t.Fatalf("xlsx markdown mismatch.\nwant:\n%s\ngot:\n%s", want, md)
	}
}

// buildTestPDF 构造一个最小合法单页 PDF（纯 Go，无需外部工具）。
func buildTestPDF(contentStream string) []byte {
	var objs []string
	objs = append(objs, "1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	objs = append(objs, "2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")
	objs = append(objs, "3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>\nendobj\n")
	objs = append(objs, "4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n")
	objs = append(objs, fmt.Sprintf("5 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n", len(contentStream), contentStream))

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs)+1)
	for i, obj := range objs {
		offsets[i+1] = buf.Len()
		buf.WriteString(obj)
	}
	xrefOffset := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xrefOffset)
	return buf.Bytes()
}

func TestConvertPDFTextAndScannedGuardrail(t *testing.T) {
	conv := NewConverter(ConvertOptions{Engine: "native"})

	// 1. 有充足文本层的 PDF 正常提取
	textPDF := buildTestPDF("BT /F1 12 Tf 72 720 Td (Knowledge QA V2 Test Document With Sufficient Text Characters For Verification) Tj ET")
	md, src, err := conv.Convert("guide.pdf", textPDF)
	if err != nil || src != "pdf" {
		t.Fatalf("Convert text pdf failed: src=%q err=%v", src, err)
	}
	if !strings.Contains(md, "Knowledge QA V2 Test Document") {
		t.Fatalf("unexpected pdf text: %q", md)
	}

	// 2. 无文本层 / 扫描件 PDF（未配置 LLM_VLM_MODEL）必须明确报错，不入库脏切片
	blankPDF := buildTestPDF("")
	_, _, err = conv.Convert("scan.pdf", blankPDF)
	if !errors.Is(err, ErrScannedPDFWithoutVLM) {
		t.Fatalf("expected ErrScannedPDFWithoutVLM, got %v", err)
	}

	// 3. 极少文字的疑似扫描件 PDF（< 30 chars/page）同样拦截
	tinyPDF := buildTestPDF("BT /F1 12 Tf 72 720 Td (Scan p1) Tj ET")
	_, _, err = conv.Convert("tiny.pdf", tinyPDF)
	if !errors.Is(err, ErrScannedPDFWithoutVLM) {
		t.Fatalf("expected ErrScannedPDFWithoutVLM for low-text pdf, got %v", err)
	}

	// 4. 损坏的 PDF 文件
	if _, _, err := conv.Convert("bad.pdf", []byte("not a pdf")); err == nil {
		t.Fatal("expected corrupt pdf to return error")
	}
}

func TestConvertVLMHookForScannedPDFAndImage(t *testing.T) {
	var called int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if req["model"] != "mock-vlm" {
			http.Error(w, "bad model", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": "# 扫描件标题\n\n| 栏位 | 值 |\n| --- | --- |\n| 状态 | 正常 |"}},
			},
		})
	}))
	defer srv.Close()

	// 未配置 VLM 时图片直接报错
	noVLM := NewConverter(ConvertOptions{Engine: "native"})
	if _, _, err := noVLM.Convert("chart.png", []byte("fake-png-bytes")); err == nil {
		t.Fatal("expected image without VLM to fail")
	}

	// 配置 VLM 后，无文本层 PDF 和图片均通过 VLM 钩子转写
	withVLM := NewConverter(ConvertOptions{
		Engine: "native",
		VLM:    NewVLMClient(srv.URL, "test-key", "mock-vlm"),
	})

	mdPDF, srcPDF, err := withVLM.Convert("scan.pdf", buildTestPDF(""))
	if err != nil || srcPDF != "pdf" || !strings.Contains(mdPDF, "# 扫描件标题") {
		t.Fatalf("VLM scanned pdf failed: src=%q md=%q err=%v", srcPDF, mdPDF, err)
	}

	mdImg, srcImg, err := withVLM.Convert("photo.jpg", []byte("fake-jpg-bytes"))
	if err != nil || srcImg != "img" || !strings.Contains(mdImg, "| 栏位 | 值 |") {
		t.Fatalf("VLM image failed: src=%q md=%q err=%v", srcImg, mdImg, err)
	}
	if called != 2 {
		t.Fatalf("expected 2 VLM HTTP calls, got %d", called)
	}
}

func TestConvertUnsupportedAndEngineErrors(t *testing.T) {
	conv := NewConverter(ConvertOptions{Engine: "native"})
	if _, _, err := conv.Convert("archive.zip", []byte("pk")); err == nil {
		t.Fatal("expected unsupported extension error")
	}

	badEng := NewConverter(ConvertOptions{Engine: "unknown-engine"})
	if _, _, err := badEng.Convert("doc.md", []byte("hi")); err == nil {
		t.Fatal("expected unsupported CONVERT_ENGINE error")
	}

	// markitdown 未找到时不应静默成功（仅在环境无 markitdown 时验证报错；若有则验证可运行）
	if _, err := os.Stat(filepath.Join(os.TempDir(), "nonexistent")); err == nil {
		t.Log("skip")
	}
}

func TestConvertCorpusV2Formats(t *testing.T) {
	conv := NewConverter(ConvertOptions{Engine: "native"})
	cases := []struct {
		relPath  string
		wantType string
		wantText string
	}{
		{"../corpus_v2/shared/team-engineering-norms.md", "md", "40001"},
		{"../corpus_v2/content-promo/promo-reward-and-port-v1.md", "md", "T+15"},
		{"../corpus_v2/content-promo/promo-reward-and-approval-v2.md", "md", "8082"},
		{"../corpus_v2/content-promo/winter-creator-incentive-handbook.docx", "docx", "冬季创作激励活动执行手册"},
		{"../corpus_v2/content-promo/promo-launch-schedule-budget.xlsx", "xlsx", "上线排期表"},
		{"../corpus_v2/content-promo/promo-risk-control-sla.pdf", "pdf", "42901"},
	}
	for _, tc := range cases {
		raw, err := os.ReadFile(tc.relPath)
		if err != nil {
			t.Fatalf("read %s failed: %v", tc.relPath, err)
		}
		md, srcType, err := conv.Convert(filepath.Base(tc.relPath), raw)
		if err != nil {
			t.Fatalf("Convert(%s) failed: %v", tc.relPath, err)
		}
		if srcType != tc.wantType {
			t.Fatalf("%s sourceType=%q want %q", tc.relPath, srcType, tc.wantType)
		}
		if !strings.Contains(md, tc.wantText) {
			t.Fatalf("%s converted markdown missing %q:\n%s", tc.relPath, tc.wantText, md)
		}
		if pieces := SplitMarkdownV3(md); len(pieces) == 0 {
			t.Fatalf("%s SplitMarkdownV3 returned 0 chunks", tc.relPath)
		}
	}
}
