//go:build ignore

// 一次性生成 corpus_v2/content-promo/ 下的真实 .docx / .xlsx / .pdf 演示语料，并立即用 ingest.Convert 自检。
package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/gomutex/godocx"
	"github.com/xuri/excelize/v2"

	"knowledge-qa/ingest"
)

func main() {
	outDir := filepath.Join("corpus_v2", "content-promo")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		log.Fatal(err)
	}

	docxBytes, err := buildDemoDOCX()
	if err != nil {
		log.Fatal("生成 DOCX 失败：", err)
	}
	docxPath := filepath.Join(outDir, "winter-creator-incentive-handbook.docx")
	if err := os.WriteFile(docxPath, docxBytes, 0o644); err != nil {
		log.Fatal(err)
	}

	xlsxBytes, err := buildDemoXLSX()
	if err != nil {
		log.Fatal("生成 XLSX 失败：", err)
	}
	xlsxPath := filepath.Join(outDir, "promo-launch-schedule-budget.xlsx")
	if err := os.WriteFile(xlsxPath, xlsxBytes, 0o644); err != nil {
		log.Fatal(err)
	}

	pdfBytes := buildDemoPDF()
	pdfPath := filepath.Join(outDir, "promo-risk-control-sla.pdf")
	if err := os.WriteFile(pdfPath, pdfBytes, 0o644); err != nil {
		log.Fatal(err)
	}

	conv := ingest.NewConverter(ingest.ConvertOptions{Engine: "native"})
	for _, p := range []string{docxPath, xlsxPath, pdfPath} {
		raw, err := os.ReadFile(p)
		if err != nil {
			log.Fatal(err)
		}
		md, srcType, err := conv.Convert(filepath.Base(p), raw)
		if err != nil {
			log.Fatalf("转换校验失败 %s: %v", p, err)
		}
		pieces := ingest.SplitMarkdownV3(md)
		log.Printf("校验成功：%s (source_type=%s, chars=%d, chunks=%d)", p, srcType, len([]rune(md)), len(pieces))
	}
}

func buildDemoDOCX() ([]byte, error) {
	doc, err := godocx.NewDocument()
	if err != nil {
		return nil, err
	}
	if _, err := doc.AddHeading("冬季创作激励活动执行手册", 1); err != nil {
		return nil, err
	}
	doc.AddParagraph("本手册面向内容消费团队运营、产品与策略同学，规范冬季创作者激励活动的报名准入、阶梯发奖与作弊拦截口径。")

	if _, err := doc.AddHeading("活动周期与核心目标", 2); err != nil {
		return nil, err
	}
	doc.AddParagraph("冬季创作激励活动周期为每年 12 月 1 日至次年 1 月 15 日，重点扶持科技测评、实用教程与深度图文三类高质量原创内容。")

	if _, err := doc.AddHeading("创作者参与门槛", 2); err != nil {
		return nil, err
	}
	doc.AddParagraph("创作者报名须同时满足以下三项基础门槛：账号粉丝数不少于 1000 人、近 30 天公开发布原创内容不少于 3 篇、且近 90 天内无社区违规处罚记录。")

	if _, err := doc.AddHeading("阶梯激励档位表", 2); err != nil {
		return nil, err
	}
	tbl := doc.AddTable()
	rows := [][]string{
		{"激励档位", "单篇有效阅读量要求", "单篇基础奖励", "额外流量券"},
		{"新锐创作档", "达到 5,000 次及以上", "200 元", "5,000 曝光券"},
		{"优质爆款档", "达到 30,000 次及以上", "600 元", "20,000 曝光券"},
		{"标杆深度档", "达到 100,000 次及以上", "1,200 元", "50,000 曝光券"},
	}
	for _, r := range rows {
		row := tbl.AddRow()
		for _, c := range r {
			row.AddCell().AddParagraph(c)
		}
	}

	if _, err := doc.AddHeading("异常刷量与作弊拦截", 2); err != nil {
		return nil, err
	}
	doc.AddParagraph("系统每日凌晨自动复核前一日有效阅读量与完播率；若命中同设备高频刷量、搬运洗稿或虚假互动特征，当篇内容将直接取消当期激励资格并冻结账号活动申领权限 30 天。")

	var buf bytes.Buffer
	if err := doc.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func buildDemoXLSX() ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()

	f.SetSheetName("Sheet1", "上线排期表")
	scheduleRows := [][]string{
		{"上线阶段", "时间窗口", "核心交付内容", "主负责人", "当前状态"},
		{"规则终审与压测", "11月20日 14:00", "完成三方审批与 `:8082` 服务千并发压测", "陈敏", "已完成"},
		{"灰度放量 10%", "11月25日 15:00", "面向内测创作者白名单开放报名页与发奖看板", "林琳", "进行中"},
		{"全量上线发布", "12月01日 10:00", "全量开启冬季创作激励活动入口与自动结算任务", "周浩", "待上线"},
		{"首轮自动发奖", "01月22日 14:00", "活动结束后 T+5 个工作日内完成对账与自动打款", "结算系统", "待触发"},
	}
	for rIdx, row := range scheduleRows {
		for cIdx, val := range row {
			cell, _ := excelize.CoordinatesToCellName(cIdx+1, rIdx+1)
			_ = f.SetCellValue("上线排期表", cell, val)
		}
	}

	_, _ = f.NewSheet("预算与值班安排")
	budgetRows := [][]string{
		{"模块", "季度预算上限(万元)", "主值班人", "备值班人", "应急处理要求"},
		{"创作者现金激励池", "120", "林琳", "周浩", "超 85% 预算水位自动预警并触发风控复核"},
		{"曝光流量补贴资源", "35", "运营策略组", "陈敏", "按日均配额均匀释放，防止单日集中消耗"},
		{"活动网关与结算服务", "15", "陈敏", "后端值班组", "P0 异常 10 分钟内响应，30 分钟内切换降级"},
	}
	for rIdx, row := range budgetRows {
		for cIdx, val := range row {
			cell, _ := excelize.CoordinatesToCellName(cIdx+1, rIdx+1)
			_ = f.SetCellValue("预算与值班安排", cell, val)
		}
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// buildDemoPDF 构造包含标准 Identity-H ToUnicode CMap 的合法 PDF，确保 ledongthuc/pdf 能无损提取中英文文本。
func buildDemoPDF() []byte {
	lines := []string{
		"# 内容活动风控与接口限流 SLA 指南",
		"1. 报名与领奖接口限流策略：单个账号每分钟最多发起 20 次报名校验请求，超出阈值将统一返回业务错误码 42901 并触发滑动窗口冷却 60 秒。",
		"2. 活动奖品核销幂等性：每次发奖回调必须携带唯一 request_id，服务端依据该幂等键去重 7 天，避免网络重试造成重复发奖。",
		"3. 风控复核 SLA：触发刷量风控拦截的创作者可在活动中心提交申诉工单，风控策略组承诺在 2 个工作日内完成人工复审并反馈结果。",
		"4. 灰度与降级预案：当活动主服务 `:8082` 依赖的积分账本延迟超过 800 毫秒时，系统自动开启异步队列削峰降级并在后台重试对账。",
	}

	var contentStream strings.Builder
	y := 760
	for _, line := range lines {
		var hexText strings.Builder
		for _, r := range line {
			fmt.Fprintf(&hexText, "%04X", r)
		}
		fmt.Fprintf(&contentStream, "BT\n/F1 11 Tf\n50 %d Td\n<%s> Tj\nET\n", y, hexText.String())
		y -= 24
	}

	toUnicode := "/CIDInit /ProcSet findresource begin\n" +
		"12 dict begin\n" +
		"begincmap\n" +
		"/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n" +
		"/CMapName /Adobe-Identity-UCS def\n" +
		"/CMapType 2 def\n" +
		"1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n" +
		"1 beginbfrange\n<0000> <FFFF> <0000>\nendbfrange\n" +
		"endcmap\n" +
		"CMapName currentdict /CMap defineresource pop\n" +
		"end\nend"

	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n")
	offsets := make([]int, 7)
	writeObject := func(num int, body string) {
		offsets[num] = pdf.Len()
		fmt.Fprintf(&pdf, "%d 0 obj\n%s\nendobj\n", num, body)
	}
	writeStream := func(num int, body string) {
		writeObject(num, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body))
	}

	writeObject(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObject(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	writeObject(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 6 0 R >> >> /Contents 4 0 R >>")
	writeStream(4, contentStream.String())
	writeStream(5, toUnicode)
	writeObject(6, "<< /Type /Font /Subtype /Type0 /BaseFont /SimSun /Encoding /Identity-H /ToUnicode 5 0 R >>")

	xrefOffset := pdf.Len()
	pdf.WriteString("xref\n0 7\n0000000000 65535 f \n")
	for i := 1; i <= 6; i++ {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size 7 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefOffset)
	return pdf.Bytes()
}
