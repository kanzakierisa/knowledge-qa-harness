package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEvalMarkRejectsMissingAndInvalidFields(t *testing.T) {
	h := &Handler{}
	router := NewRouter(h)
	for _, body := range []string{
		`{}`, `{"run_id":1}`, `{"run_id":0,"usable":true}`,
		`{"run_id":-1,"usable":false}`, `{"run_id":1,"usable":null}`,
		`{"run_id":1,"usable":"false"}`, `{"run_id":1.5,"usable":true}`,
	} {
		t.Run(body, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/eval/mark", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestEvalRunRejectsConcurrentBatch(t *testing.T) {
	h := &Handler{}
	h.evalMu.Lock()
	defer h.evalMu.Unlock()
	response := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/eval/run", nil))
	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestEvalRunRejectsInvalidSet(t *testing.T) {
	router := NewRouter(&Handler{})
	for _, target := range []string{
		"/api/eval/run?set=invalid",
		"/api/eval/run?set=unknown",
	} {
		t.Run(target, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, target, nil))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for %s, got %d body=%s", target, rec.Code, rec.Body.String())
			}
		})
	}
}
