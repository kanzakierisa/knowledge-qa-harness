package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var ErrSessionNotFound = errors.New("会话任务不存在")

// QASession 表示左侧持久化会话任务条目。
type QASession struct {
	ID          int64     `json:"id"`
	Title       string    `json:"title"`
	ProjectSlug string    `json:"project_slug"`
	ProjectName string    `json:"project_name,omitempty"`
	ScopeMode   string    `json:"scope_mode"`
	DocumentIDs []int64   `json:"document_ids"`
	IsPinned    bool      `json:"is_pinned"`
	TurnCount   int       `json:"turn_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// QATurn 表示会话中的单轮完整问答与反馈状态。
type QATurn struct {
	ID                int64           `json:"id"`
	SessionID         int64           `json:"session_id"`
	Question          string          `json:"question"`
	ProjectSlug       string          `json:"project_slug"`
	ScopeLabel        string          `json:"scope_label"`
	Answer            string          `json:"answer"`
	Citations         json.RawMessage `json:"citations"`
	RetrievedChunkIDs []int64         `json:"retrieved_chunk_ids"`
	Warnings          json.RawMessage `json:"warnings,omitempty"`
	Comparisons       json.RawMessage `json:"comparisons,omitempty"`
	Ambiguity         json.RawMessage `json:"ambiguity,omitempty"`
	ProjectResolved   string          `json:"project_resolved,omitempty"`
	Refused           bool            `json:"refused"`
	Retried           bool            `json:"retried"`
	CitationOK        *bool           `json:"citation_ok"`
	FeedbackVote      *string         `json:"feedback_vote"`
	CreatedAt         time.Time       `json:"created_at"`
}

// OnlineStats 描述口径 B 下的真实线上客观统计（不伪造无真值指标）。
type OnlineStats struct {
	TotalTurns         int      `json:"total_turns"`
	AnsweredTurns      int      `json:"answered_turns"`
	CitationOKCount    int      `json:"citation_ok_count"`
	CitationTotalCount int      `json:"citation_total_count"`
	CitationRate       *float64 `json:"citation_rate"`
	FeedbackUpCount    int      `json:"feedback_up_count"`
	FeedbackDownCount  int      `json:"feedback_down_count"`
	FeedbackTotalCount int      `json:"feedback_total_count"`
	FeedbackUsableRate *float64 `json:"feedback_usable_rate"`
	RefusedCount       int      `json:"refused_count"`
	RefusedUpCount     int      `json:"refused_up_count"`
	RefusedDownCount   int      `json:"refused_down_count"`
}

// SessionDetail 返回单条会话的元信息、历史轮次与当前会话客观统计。
type SessionDetail struct {
	Session QASession   `json:"session"`
	Turns   []QATurn    `json:"turns"`
	Stats   OnlineStats `json:"stats"`
}

// CreateSessionParams 描述新建/更新会话范围参数。
type CreateSessionParams struct {
	Title       string
	ProjectSlug string
	ScopeMode   string
	DocumentIDs []int64
}

// CreateTurnParams 描述写入会话单轮问答所需字段。
type CreateTurnParams struct {
	Question          string
	ProjectSlug       string
	ScopeLabel        string
	Answer            string
	CitationsJSON     string
	RetrievedChunkIDs []int64
	WarningsJSON      string
	ComparisonsJSON   string
	AmbiguityJSON     string
	ProjectResolved   string
	Refused           bool
	Retried           bool
	CitationOK        *bool
}

// SessionPatch 描述会话重命名与置顶变更。
type SessionPatch struct {
	Title    *string
	IsPinned *bool
}

var titleFillerPrefixes = []string{
	"请问一下，", "请问一下,", "请问一下",
	"请问，", "请问,", "请问",
	"你好，", "你好,", "你好",
	"麻烦问一下，", "麻烦问一下,", "麻烦问一下",
	"麻烦问下，", "麻烦问下,", "麻烦问下",
	"麻烦帮我查一下", "麻烦帮我看看", "麻烦帮我", "麻烦",
	"帮我查一下", "帮我查下", "帮我看看", "帮我查", "帮我",
	"我想知道一下", "我想知道", "我想了解一下", "我想了解",
	"能否告诉我", "能告诉我", "可以告诉我",
}

// BuildSessionTitle 从首问中提炼核心主题作为会话标题，去除口语前缀并适度截断。
func BuildSessionTitle(question string) string {
	var b strings.Builder
	inSpace := false
	for _, r := range strings.TrimSpace(question) {
		if unicode.IsSpace(r) {
			if !inSpace {
				b.WriteByte(' ')
				inSpace = true
			}
			continue
		}
		inSpace = false
		b.WriteRune(r)
	}
	s := strings.TrimSpace(b.String())
	if s == "" {
		return "新对话"
	}

	// 逐次剥离常见口语前缀
	for changed := true; changed; {
		changed = false
		for _, p := range titleFillerPrefixes {
			if strings.HasPrefix(s, p) {
				rest := strings.TrimSpace(strings.TrimPrefix(s, p))
				rest = strings.TrimLeft(rest, "，,：:、 ")
				if rest != "" {
					s = rest
					changed = true
					break
				}
			}
		}
	}

	s = strings.TrimRight(s, "？?。！!：:、，, ")
	s = strings.TrimSpace(s)
	if s == "" {
		s = strings.TrimSpace(b.String())
	}

	if utf8.RuneCountInString(s) <= 26 {
		return s
	}
	return truncateRunes(s, 25) + "…"
}

func normalizeScopeMode(mode string, docIDs []int64) string {
	if strings.EqualFold(strings.TrimSpace(mode), "material") || len(docIDs) > 0 {
		return "material"
	}
	return "topic"
}

func marshalDocIDsJSON(ids []int64) string {
	if len(ids) == 0 {
		return "[]"
	}
	return marshalCappedChunkIDs(ids, 1000)
}

func normalizeJSONOrEmpty(raw string, fallback string) string {
	t := strings.TrimSpace(raw)
	if t == "" || !json.Valid([]byte(t)) {
		return fallback
	}
	return t
}

// EnsureSessionExists 在调用 LLM 前校验显式指定的 session_id 是否存在（V1 缺表时跳过）。
func (s *Store) EnsureSessionExists(sessionID int64) error {
	if sessionID <= 0 {
		return ErrSessionNotFound
	}
	var id int64
	err := s.db.QueryRow("SELECT id FROM qa_sessions WHERE id = ?", sessionID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrSessionNotFound
	}
	if err != nil && IsV1SchemaMissingError(err) {
		return nil
	}
	return err
}

// CreateSessionAndTurn 在单事务内新建会话并写入首轮问答记录。
func (s *Store) CreateSessionAndTurn(sp CreateSessionParams, tp CreateTurnParams) (sessionID int64, turnID int64, err error) {
	title := strings.TrimSpace(sp.Title)
	if title == "" {
		title = BuildSessionTitle(tp.Question)
	}
	title = truncateRunes(title, 160)
	proj := truncateRunes(strings.TrimSpace(sp.ProjectSlug), 64)
	scopeMode := normalizeScopeMode(sp.ScopeMode, sp.DocumentIDs)
	docIDsJSON := marshalDocIDsJSON(sp.DocumentIDs)

	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`
		INSERT INTO qa_sessions (title, project_slug, scope_mode, document_ids, is_pinned)
		VALUES (?, ?, ?, ?, 0)`,
		title, proj, scopeMode, docIDsJSON,
	)
	if err != nil {
		return 0, 0, err
	}
	sessionID, err = res.LastInsertId()
	if err != nil {
		return 0, 0, err
	}

	turnID, err = insertTurnTx(tx, sessionID, tp)
	if err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return sessionID, turnID, nil
}

// AppendTurnToSession 向既有会话追加一轮问答并刷新范围与 updated_at。
func (s *Store) AppendTurnToSession(sessionID int64, sp CreateSessionParams, tp CreateTurnParams) (int64, error) {
	if sessionID <= 0 {
		return 0, ErrSessionNotFound
	}
	proj := truncateRunes(strings.TrimSpace(sp.ProjectSlug), 64)
	scopeMode := normalizeScopeMode(sp.ScopeMode, sp.DocumentIDs)
	docIDsJSON := marshalDocIDsJSON(sp.DocumentIDs)

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var existsID int64
	if err := tx.QueryRow("SELECT id FROM qa_sessions WHERE id = ? FOR UPDATE", sessionID).Scan(&existsID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrSessionNotFound
		}
		return 0, err
	}

	turnID, err := insertTurnTx(tx, sessionID, tp)
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`
		UPDATE qa_sessions
		SET project_slug = ?, scope_mode = ?, document_ids = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		proj, scopeMode, docIDsJSON, sessionID,
	); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return turnID, nil
}

func insertTurnTx(tx *sql.Tx, sessionID int64, tp CreateTurnParams) (int64, error) {
	proj := truncateRunes(strings.TrimSpace(tp.ProjectSlug), 64)
	scopeLabel := truncateRunes(strings.TrimSpace(tp.ScopeLabel), 255)
	citsJSON := normalizeJSONOrEmpty(tp.CitationsJSON, "[]")
	retrievedJSON := marshalCappedChunkIDs(tp.RetrievedChunkIDs, 500)
	warningsJSON := normalizeJSONOrEmpty(tp.WarningsJSON, "[]")
	comparisonsJSON := normalizeJSONOrEmpty(tp.ComparisonsJSON, "[]")
	ambiguityJSON := normalizeJSONOrEmpty(tp.AmbiguityJSON, "null")
	resolved := truncateRunes(strings.TrimSpace(tp.ProjectResolved), 64)

	var citOK sql.NullBool
	if tp.CitationOK != nil {
		citOK = sql.NullBool{Bool: *tp.CitationOK, Valid: true}
	}

	res, err := tx.Exec(`
		INSERT INTO qa_turns (
			session_id, question, project_slug, scope_label, answer,
			citations_json, retrieved_chunk_ids_json, warnings_json, comparisons_json, ambiguity_json,
			project_resolved, refused, retried, citation_ok
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sessionID, tp.Question, proj, scopeLabel, tp.Answer,
		citsJSON, retrievedJSON, warningsJSON, comparisonsJSON, ambiguityJSON,
		resolved, tp.Refused, tp.Retried, citOK,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListSessions 按置顶优先、更新时间倒序返回全部持久化会话任务。
func (s *Store) ListSessions() ([]QASession, error) {
	rows, err := s.db.Query(`
		SELECT s.id, s.title, s.project_slug, COALESCE(p.name, ''), s.scope_mode, s.document_ids,
		       s.is_pinned, COUNT(t.id) AS turn_count, s.created_at, s.updated_at
		FROM qa_sessions s
		LEFT JOIN projects p ON p.slug = s.project_slug
		LEFT JOIN qa_turns t ON t.session_id = s.id
		GROUP BY s.id, s.title, s.project_slug, p.name, s.scope_mode, s.document_ids,
		         s.is_pinned, s.created_at, s.updated_at
		ORDER BY s.is_pinned DESC, s.updated_at DESC, s.id DESC`)
	if err != nil {
		if IsV1SchemaMissingError(err) {
			return []QASession{}, nil
		}
		return nil, err
	}
	defer rows.Close()

	out := make([]QASession, 0)
	for rows.Next() {
		sess, err := scanSessionRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

// GetSession 查询单条会话元信息。
func (s *Store) GetSession(id int64) (QASession, error) {
	if id <= 0 {
		return QASession{}, ErrSessionNotFound
	}
	rows, err := s.db.Query(`
		SELECT s.id, s.title, s.project_slug, COALESCE(p.name, ''), s.scope_mode, s.document_ids,
		       s.is_pinned, COUNT(t.id) AS turn_count, s.created_at, s.updated_at
		FROM qa_sessions s
		LEFT JOIN projects p ON p.slug = s.project_slug
		LEFT JOIN qa_turns t ON t.session_id = s.id
		WHERE s.id = ?
		GROUP BY s.id, s.title, s.project_slug, p.name, s.scope_mode, s.document_ids,
		         s.is_pinned, s.created_at, s.updated_at`, id)
	if err != nil {
		return QASession{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return QASession{}, err
		}
		return QASession{}, ErrSessionNotFound
	}
	return scanSessionRow(rows)
}

func scanSessionRow(rows *sql.Rows) (QASession, error) {
	var sess QASession
	var rawDocIDs string
	if err := rows.Scan(
		&sess.ID, &sess.Title, &sess.ProjectSlug, &sess.ProjectName, &sess.ScopeMode,
		&rawDocIDs, &sess.IsPinned, &sess.TurnCount, &sess.CreatedAt, &sess.UpdatedAt,
	); err != nil {
		return QASession{}, err
	}
	sess.DocumentIDs = []int64{}
	if strings.TrimSpace(rawDocIDs) != "" {
		_ = json.Unmarshal([]byte(rawDocIDs), &sess.DocumentIDs)
		if sess.DocumentIDs == nil {
			sess.DocumentIDs = []int64{}
		}
	}
	return sess, nil
}

// GetSessionDetail 查询会话详情、完整历史问答轮次及该会话客观统计。
func (s *Store) GetSessionDetail(id int64) (SessionDetail, error) {
	sess, err := s.GetSession(id)
	if err != nil {
		return SessionDetail{}, err
	}
	rows, err := s.db.Query(`
		SELECT id, session_id, question, project_slug, scope_label, answer,
		       citations_json, retrieved_chunk_ids_json, warnings_json, comparisons_json, ambiguity_json,
		       project_resolved, refused, retried, citation_ok, feedback_vote, created_at
		FROM qa_turns
		WHERE session_id = ?
		ORDER BY id ASC`, id)
	if err != nil {
		return SessionDetail{}, err
	}
	defer rows.Close()

	turns := make([]QATurn, 0)
	for rows.Next() {
		t, err := scanTurnRow(rows)
		if err != nil {
			return SessionDetail{}, err
		}
		turns = append(turns, t)
	}
	if err := rows.Err(); err != nil {
		return SessionDetail{}, err
	}
	return SessionDetail{
		Session: sess,
		Turns:   turns,
		Stats:   ComputeOnlineStatsFromTurns(turns),
	}, nil
}

func scanTurnRow(rows *sql.Rows) (QATurn, error) {
	var t QATurn
	var citsRaw, idsRaw, warnsRaw, cmpRaw, ambRaw string
	var citOK sql.NullBool
	var vote sql.NullString
	if err := rows.Scan(
		&t.ID, &t.SessionID, &t.Question, &t.ProjectSlug, &t.ScopeLabel, &t.Answer,
		&citsRaw, &idsRaw, &warnsRaw, &cmpRaw, &ambRaw,
		&t.ProjectResolved, &t.Refused, &t.Retried, &citOK, &vote, &t.CreatedAt,
	); err != nil {
		return QATurn{}, err
	}
	t.Citations = rawOrFallbackJSON(citsRaw, "[]")
	t.Warnings = optionalRawJSON(warnsRaw)
	t.Comparisons = optionalRawJSON(cmpRaw)
	t.Ambiguity = optionalRawJSON(ambRaw)
	t.RetrievedChunkIDs = []int64{}
	if strings.TrimSpace(idsRaw) != "" {
		_ = json.Unmarshal([]byte(idsRaw), &t.RetrievedChunkIDs)
		if t.RetrievedChunkIDs == nil {
			t.RetrievedChunkIDs = []int64{}
		}
	}
	if citOK.Valid {
		v := citOK.Bool
		t.CitationOK = &v
	}
	if vote.Valid {
		v := vote.String
		t.FeedbackVote = &v
	}
	return t, nil
}

func rawOrFallbackJSON(raw, fallback string) json.RawMessage {
	t := strings.TrimSpace(raw)
	if t == "" || !json.Valid([]byte(t)) {
		return json.RawMessage(fallback)
	}
	return json.RawMessage(t)
}

func optionalRawJSON(raw string) json.RawMessage {
	t := strings.TrimSpace(raw)
	if t == "" || t == "null" || t == "[]" || !json.Valid([]byte(t)) {
		return nil
	}
	return json.RawMessage(t)
}

// ComputeOnlineStatsFromTurns 按口径 B 统计切片内所有问答轮次的真实客观指标。
func ComputeOnlineStatsFromTurns(turns []QATurn) OnlineStats {
	var st OnlineStats
	for _, t := range turns {
		st.TotalTurns++
		if t.Refused {
			st.RefusedCount++
			if t.FeedbackVote != nil {
				if *t.FeedbackVote == "up" {
					st.RefusedUpCount++
				} else if *t.FeedbackVote == "down" {
					st.RefusedDownCount++
				}
			}
		} else if t.ProjectResolved != "ambiguous" {
			st.AnsweredTurns++
		}
		if t.CitationOK != nil {
			st.CitationTotalCount++
			if *t.CitationOK {
				st.CitationOKCount++
			}
		}
		if t.FeedbackVote != nil {
			if *t.FeedbackVote == "up" {
				st.FeedbackUpCount++
				st.FeedbackTotalCount++
			} else if *t.FeedbackVote == "down" {
				st.FeedbackDownCount++
				st.FeedbackTotalCount++
			}
		}
	}
	if st.CitationTotalCount > 0 {
		r := float64(st.CitationOKCount) / float64(st.CitationTotalCount)
		st.CitationRate = &r
	}
	if st.FeedbackTotalCount > 0 {
		r := float64(st.FeedbackUpCount) / float64(st.FeedbackTotalCount)
		st.FeedbackUsableRate = &r
	}
	return st
}

// UpdateSession 更新会话标题（重命名）或置顶状态。
func (s *Store) UpdateSession(id int64, patch SessionPatch) (QASession, error) {
	if id <= 0 {
		return QASession{}, ErrSessionNotFound
	}
	var sets []string
	var args []any

	if patch.Title != nil {
		t := strings.TrimSpace(*patch.Title)
		if t == "" {
			return QASession{}, errors.New("会话标题不能为空")
		}
		sets = append(sets, "title = ?")
		args = append(args, truncateRunes(t, 160))
	}
	if patch.IsPinned != nil {
		sets = append(sets, "is_pinned = ?")
		args = append(args, *patch.IsPinned)
	}
	if len(sets) > 0 {
		// 仅调整置顶或重命名时保留原 updated_at，避免重命名或取消置顶意外改变时间排序？
		// Wait: 置顶按 is_pinned DESC, updated_at DESC；保留 updated_at = updated_at 可防止 ON UPDATE CURRENT_TIMESTAMP 意外刷新时间。
		sets = append(sets, "updated_at = updated_at")
		args = append(args, id)
		query := "UPDATE qa_sessions SET " + strings.Join(sets, ", ") + " WHERE id = ?"
		res, err := s.db.Exec(query, args...)
		if err != nil {
			return QASession{}, err
		}
		aff, _ := res.RowsAffected()
		if aff == 0 {
			if _, err := s.GetSession(id); err != nil {
				return QASession{}, err
			}
		}
	}
	return s.GetSession(id)
}

// DeleteSession 删除会话任务（MySQL ON DELETE CASCADE 同步删除全部关联 qa_turns）。
func (s *Store) DeleteSession(id int64) error {
	if id <= 0 {
		return ErrSessionNotFound
	}
	res, err := s.db.Exec("DELETE FROM qa_sessions WHERE id = ?", id)
	if err != nil {
		return err
	}
	aff, _ := res.RowsAffected()
	if aff == 0 {
		return ErrSessionNotFound
	}
	return nil
}

// GetOnlineStats 从 MySQL 聚合全部真实在线问答轮次统计（与 eval_runs 严格分离）。
func (s *Store) GetOnlineStats() (OnlineStats, error) {
	var st OnlineStats
	var answered, citOK, citTot, fbUp, fbDown, refCnt, refUp, refDown sql.NullInt64
	err := s.db.QueryRow(`
		SELECT
			COUNT(*) AS total_turns,
			COALESCE(SUM(CASE WHEN refused = 0 AND project_resolved <> 'ambiguous' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN citation_ok = 1 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN citation_ok IS NOT NULL THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN feedback_vote = 'up' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN feedback_vote = 'down' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN refused = 1 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN refused = 1 AND feedback_vote = 'up' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN refused = 1 AND feedback_vote = 'down' THEN 1 ELSE 0 END), 0)
		FROM qa_turns`).Scan(
		&st.TotalTurns,
		&answered,
		&citOK,
		&citTot,
		&fbUp,
		&fbDown,
		&refCnt,
		&refUp,
		&refDown,
	)
	if err != nil {
		if IsV1SchemaMissingError(err) {
			return OnlineStats{}, nil
		}
		return OnlineStats{}, fmt.Errorf("聚合在线统计失败：%w", err)
	}
	st.AnsweredTurns = int(answered.Int64)
	st.CitationOKCount = int(citOK.Int64)
	st.CitationTotalCount = int(citTot.Int64)
	st.FeedbackUpCount = int(fbUp.Int64)
	st.FeedbackDownCount = int(fbDown.Int64)
	st.FeedbackTotalCount = st.FeedbackUpCount + st.FeedbackDownCount
	st.RefusedCount = int(refCnt.Int64)
	st.RefusedUpCount = int(refUp.Int64)
	st.RefusedDownCount = int(refDown.Int64)
	if st.CitationTotalCount > 0 {
		r := float64(st.CitationOKCount) / float64(st.CitationTotalCount)
		st.CitationRate = &r
	}
	if st.FeedbackTotalCount > 0 {
		r := float64(st.FeedbackUpCount) / float64(st.FeedbackTotalCount)
		st.FeedbackUsableRate = &r
	}
	return st, nil
}
