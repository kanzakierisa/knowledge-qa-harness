package retrieve

import (
	"math"
	"testing"

	"knowledge-qa/store"
)

func TestMergeWithOptionsProjectIsolation(t *testing.T) {
	chunks := []store.Chunk{
		{
			ID:          1,
			ProjectSlug: "project-alpha",
			HeadingPath: "Alpha > 端口",
			Content:     "Alpha 服务默认监听端口 8091。",
			Embedding:   "[1,0]",
			DocLevel:    "spec",
			DocStatus:   "active",
		},
		{
			ID:          2,
			ProjectSlug: "project-beta",
			HeadingPath: "Beta > 端口",
			Content:     "Beta 服务默认监听端口 9090。",
			Embedding:   "[1,0]",
			DocLevel:    "spec",
			DocStatus:   "active",
		},
		{
			ID:          3,
			ProjectSlug: "shared",
			HeadingPath: "公共 > 日志",
			Content:     "公共规范：所有服务默认监听端口需写入配置。",
			Embedding:   "[0.8,0.6]",
			DocLevel:    "spec",
			DocStatus:   "active",
		},
	}

	gotAlpha := MergeWithOptions("服务默认监听什么端口", []float64{1, 0}, chunks, TopN, MergeOptions{ProjectSlug: "project-alpha"})
	if len(gotAlpha) != 2 {
		t.Fatalf("expected 2 chunks (project-alpha + shared), got %d", len(gotAlpha))
	}
	for _, r := range gotAlpha {
		if r.Chunk.ProjectSlug == "project-beta" {
			t.Fatalf("project isolation violated: got project-beta chunk %+v", r.Chunk)
		}
		if r.Chunk.ProjectSlug != "project-alpha" && r.Chunk.ProjectSlug != "shared" {
			t.Fatalf("unexpected project slug %q", r.Chunk.ProjectSlug)
		}
	}

	gotShared := MergeWithOptions("服务默认监听什么端口", []float64{1, 0}, chunks, TopN, MergeOptions{ProjectSlug: "shared"})
	if len(gotShared) != 1 || gotShared[0].Chunk.ProjectSlug != "shared" {
		t.Fatalf("expected only shared chunk, got %+v", gotShared)
	}

	// 若目标项目与 shared 均无切片，必须返回空而不偷拿其他项目
	noShared := chunks[:2]
	gotEmpty := MergeWithOptions("端口", []float64{1, 0}, noShared, TopN, MergeOptions{ProjectSlug: "empty-proj"})
	if len(gotEmpty) != 0 {
		t.Fatalf("expected empty result for empty project without shared chunks, got %+v", gotEmpty)
	}
}

func TestMergeWithOptionsGovernanceWeightsAndV1Compat(t *testing.T) {
	chunks := []store.Chunk{
		{ID: 1, ProjectSlug: "p", HeadingPath: "H", Content: "相同内容", Embedding: "[1,0]", DocLevel: "note", DocStatus: "active"},
		{ID: 2, ProjectSlug: "p", HeadingPath: "H", Content: "相同内容", Embedding: "[1,0]", DocLevel: "spec", DocStatus: "deprecated"},
		{ID: 3, ProjectSlug: "p", HeadingPath: "H", Content: "相同内容", Embedding: "[1,0]", DocLevel: "rfc", DocStatus: "active"},
		{ID: 4, ProjectSlug: "p", HeadingPath: "H", Content: "相同内容", Embedding: "[1,0]", DocLevel: "spec", DocStatus: "active"},
	}

	got := MergeWithOptions("完全无关词", []float64{1, 0}, chunks, TopN, MergeOptions{})
	if len(got) != 4 {
		t.Fatalf("expected 4 chunks, got %d", len(got))
	}
	wantOrder := []int64{4, 3, 1, 2} // spec(1.00) > rfc(0.96) > note(0.92) > deprecated(0.78)
	for i, wantID := range wantOrder {
		if got[i].Chunk.ID != wantID {
			t.Fatalf("rank %d: got chunk ID %d want %d (scores=%+v)", i, got[i].Chunk.ID, wantID, got)
		}
		if got[i].Final < 0 || got[i].Final > 1 {
			t.Fatalf("Final score out of [0,1]: %v", got[i].Final)
		}
	}

	// 验证精确倍率公式：Final = base * levelFactor * statusFactor
	base := VectorWeight * 1.0 // 关键词不命中时为 0
	if math.Abs(got[0].Final-base*DocLevelWeightSpec*DocStatusWeightActive) > 1e-9 {
		t.Fatalf("spec+active final mismatch: got %v", got[0].Final)
	}
	if math.Abs(got[1].Final-base*DocLevelWeightRFC*DocStatusWeightActive) > 1e-9 {
		t.Fatalf("rfc+active final mismatch: got %v", got[1].Final)
	}
	if math.Abs(got[2].Final-base*DocLevelWeightNote*DocStatusWeightActive) > 1e-9 {
		t.Fatalf("note+active final mismatch: got %v", got[2].Final)
	}
	if math.Abs(got[3].Final-base*DocLevelWeightSpec*DocStatusWeightDeprecated) > 1e-9 {
		t.Fatalf("spec+deprecated final mismatch: got %v", got[3].Final)
	}

	// V1 兼容：空元数据的切片与 spec+active 切片通过 Merge 与 MergeWithOptions 结果一致
	v1Chunks := []store.Chunk{
		{ID: 10, HeadingPath: "Gin", Content: "Gin 路由组使用 Group。", Embedding: "[0.9,0.1]"},
		{ID: 11, HeadingPath: "Go", Content: "Go defer 后进先出。", Embedding: "[0.1,0.9]"},
	}
	m1 := Merge("Gin Group", []float64{1, 0}, v1Chunks, TopN)
	m2 := MergeWithOptions("Gin Group", []float64{1, 0}, v1Chunks, TopN, MergeOptions{})
	if len(m1) != len(m2) || len(m1) != 2 || math.Abs(m1[0].Final-m2[0].Final) > 1e-12 {
		t.Fatalf("V1 Merge vs MergeWithOptions mismatch: m1=%+v m2=%+v", m1, m2)
	}
}
