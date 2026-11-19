package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"knowledge-qa/static"
)

func TestWebIndexServedAndSelfContained(t *testing.T) {
	router := NewRouter(&Handler{})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / status=%d want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("Content-Type=%q want text/html", ct)
	}
	if !bytes.Equal(rec.Body.Bytes(), static.IndexHTML) {
		t.Fatalf("response body mismatch with embedded static/index.html")
	}

	headReq := httptest.NewRequest(http.MethodHead, "/", nil)
	headRec := httptest.NewRecorder()
	router.ServeHTTP(headRec, headReq)
	if headRec.Code != http.StatusOK {
		t.Fatalf("HEAD / status=%d want 200", headRec.Code)
	}
	if headRec.Body.Len() != 0 {
		t.Fatalf("HEAD / body length=%d want 0", headRec.Body.Len())
	}

	html := rec.Body.String()
	for _, want := range []string{
		"Go 知识问答 · 可溯源 Demo",
		"文档中没有依据",
		"/api/ask",
		"/api/chunks",
		"/api/projects",
		"/api/documents/upload",
		"智能问答",
		"资料库治理",
		"盲区与反馈",
		"/api/gaps",
		"/api/feedback",
		"evidence-hl",
		"这道回答有帮助吗？",
		"Doc CI/CD 局部回归体检单",
		"运行 feedback 活评测",
		"project-bar",
		"自动识别（防跨项目串台）",
		"ambiguity-box",
		"warnings-banner",
		"已废弃",
		"2026-11-17",
		"COLLAPSE_THRESHOLD = 300",
		"workspace-3col",
		"按主题查询",
		"按材料查询",
		"quick-upload-form",
		"文档原文",
		"/api/documents/",
		"theme-toggle",
		`data-theme="dark"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("index.html missing %q", want)
		}
	}

	// 确保禁止引入外部框架、CDN 或外链资源，以及禁止使用 innerHTML 防止 XSS
	for _, forbidden := range []string{"http://", "https://", "<script src=", "unpkg.com", "jsdelivr", "cdnjs", "innerhtml"} {
		if strings.Contains(strings.ToLower(html), strings.ToLower(forbidden)) {
			t.Errorf("index.html must not contain external/CDN/unsafe reference %q", forbidden)
		}
	}
}
