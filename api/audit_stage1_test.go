package api

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"knowledge-qa/generate"
	"knowledge-qa/ingest"
	"knowledge-qa/store"
)

type auditStage1Driver struct{}
type auditStage1Conn struct{}
type auditStage1Rows struct{ emitted bool }

func (auditStage1Driver) Open(string) (driver.Conn, error)  { return auditStage1Conn{}, nil }
func (auditStage1Conn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unused") }
func (auditStage1Conn) Close() error                        { return nil }
func (auditStage1Conn) Begin() (driver.Tx, error)           { return nil, errors.New("unused") }
func (auditStage1Conn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &auditStage1Rows{}, nil
}
func (*auditStage1Rows) Columns() []string {
	return []string{"id", "document_id", "heading_path", "content", "embedding"}
}
func (*auditStage1Rows) Close() error { return nil }
func (r *auditStage1Rows) Next(v []driver.Value) error {
	if r.emitted {
		return io.EOF
	}
	r.emitted = true
	copy(v, []driver.Value{int64(1), int64(1), "Go", "Go example document", "[1,0]"})
	return nil
}

func auditStage1Ask(t *testing.T, answer, question string) askResponse {
	t.Helper()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/embeddings" {
			io.WriteString(w, `{"data":[{"embedding":[1,0]}]}`)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": answer}}}})
	}))
	defer provider.Close()
	db := sql.OpenDB(auditStage1Connector{})
	defer db.Close()
	st := &store.Store{}
	// Auditor-only injection of a local fake SQL driver. No real MySQL or external API is contacted.
	f := reflect.ValueOf(st).Elem().FieldByName("db")
	reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem().Set(reflect.ValueOf(db))
	h := &Handler{Store: st, Embedder: ingest.NewEmbedder(provider.URL, "test", "test"), Chat: generate.NewChatClient(provider.URL, "test", "test")}
	body, _ := json.Marshal(map[string]string{"question": question})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	NewRouter(h).ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var got askResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	t.Logf("response=%s", w.Body.String())
	return got
}

type auditStage1Connector struct{}

func (auditStage1Connector) Connect(context.Context) (driver.Conn, error) {
	return auditStage1Conn{}, nil
}
func (auditStage1Connector) Driver() driver.Driver { return auditStage1Driver{} }

func TestAuditStage1ModelRefusal(t *testing.T) {
	got := auditStage1Ask(t, generate.RefuseAnswer, "question")
	if !got.Refused || len(got.Citations) != 0 {
		t.Errorf("model refusal must set refused=true and have no citations; got refused=%v citations=%d", got.Refused, len(got.Citations))
	}
}

func TestAuditStage1EmptyAnswer(t *testing.T) {
	got := auditStage1Ask(t, "", "question")
	if got.Answer == "" && !got.Refused {
		t.Error("empty model content returned as successful answer")
	}
}

func TestAuditStage1CitationRange(t *testing.T) {
	got := auditStage1Ask(t, "Claim [99]", "question")
	if strings.Contains(got.Answer, "[99]") && !got.Refused {
		t.Errorf("nonexistent citation accepted: answer=%q mappings=%d", got.Answer, len(got.Citations))
	}
}
