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

var (
	ErrGapNotFound             = errors.New("知识盲区记录不存在")
	ErrInvalidGapStatus        = errors.New("status 仅支持 open、resolved、ignored")
	ErrFeedbackNotFound        = errors.New("反馈记录不存在")
	ErrFeedbackAlreadyPromoted = errors.New("该反馈已转正为评测题")
	ErrInvalidFeedbackVote     = errors.New("vote 仅支持 up 或 down")
	ErrInvalidFeedbackFilter   = errors.New("筛选参数无效")
	ErrGoldChunkNotFound       = errors.New("指定的 gold_chunk_id 不存在")
	ErrDuplicateEvalQuestion   = errors.New("eval_questions 中已存在相同问题，请勿重复转正")
)

// KnowledgeGap 表示团队知识盲区雷达的一条高频未覆盖问题记录。
type KnowledgeGap struct {
	ID                 int64     `json:"id"`
	ProjectSlug        string    `json:"project_slug"`
	Question           string    `json:"question"`
	NormalizedKey      string    `json:"normalized_key"`
	RefuseReason       string    `json:"refuse_reason"`
	Top1Final          float64   `json:"top1_final"`
	NearestChunkID     *int64    `json:"nearest_chunk_id"`
	NearestHeading     string    `json:"nearest_heading"`
	NearestDocID       *int64    `json:"nearest_doc_id,omitempty"`
	NearestDocTitle    string    `json:"nearest_doc_title,omitempty"`
	NearestDocFilename string    `json:"nearest_doc_filename,omitempty"`
	NearestDocOwner    string    `json:"nearest_doc_owner,omitempty"`
	NearestProjectSlug string    `json:"nearest_project_slug,omitempty"`
	HitCount           int       `json:"hit_count"`
	Status             string    `json:"status"`
	LastAskedAt        time.Time `json:"last_asked_at"`
	CreatedAt          time.Time `json:"created_at"`
}

// RecordGapParams 描述线上拒答写入知识盲区雷达所需字段。
type RecordGapParams struct {
	ProjectSlug    string
	Question       string
	RefuseReason   string
	Top1Final      float64
	NearestChunkID *int64
	NearestHeading string
}

// FeedbackChunkMeta 为待审反馈附带检索切片的文档与标题线索，便于人工转正定位 gold_chunk_id。
type FeedbackChunkMeta struct {
	ID          int64  `json:"id"`
	DocumentID  int64  `json:"document_id"`
	ProjectSlug string `json:"project_slug"`
	DocTitle    string `json:"doc_title"`
	HeadingPath string `json:"heading_path"`
	Preview     string `json:"preview"`
}

// EvalFeedback 表示用户提交的回答反馈记录。
type EvalFeedback struct {
	ID                 int64               `json:"id"`
	ProjectSlug        string              `json:"project_slug"`
	Question           string              `json:"question"`
	RetrievedChunkIDs  []int64             `json:"retrieved_chunk_ids"`
	CandidateChunks    []FeedbackChunkMeta `json:"candidate_chunks,omitempty"`
	Answer             string              `json:"answer"`
	Refused            bool                `json:"refused"`
	Vote               string              `json:"vote"`
	Comment            string              `json:"comment"`
	PromotedQuestionID *int64              `json:"promoted_question_id"`
	CreatedAt          time.Time           `json:"created_at"`
}

// CreateFeedbackParams 描述用户反馈提交入参。
type CreateFeedbackParams struct {
	TurnID            *int64
	ProjectSlug       string
	Question          string
	RetrievedChunkIDs []int64
	Answer            string
	Refused           bool
	Vote              string
	Comment           string
}

// PromoteFeedbackParams 描述将反馈转正为活评测金标题的参数。
type PromoteFeedbackParams struct {
	GoldAnswer   string
	ShouldRefuse bool
	GoldChunkID  *int64
	Category     string
}

// PromoteFeedbackResult 返回反馈转正结果。
type PromoteFeedbackResult struct {
	OK                 bool         `json:"ok"`
	FeedbackID         int64        `json:"feedback_id"`
	PromotedQuestionID int64        `json:"promoted_question_id"`
	Question           EvalQuestion `json:"question"`
}

// NormalizeGapProjectSlug 规范化盲区/反馈归属项目：空、"auto"、"all" 统一记为 "auto"。
func NormalizeGapProjectSlug(projectSlug string) string {
	s := strings.TrimSpace(projectSlug)
	if s == "" || strings.EqualFold(s, "auto") || strings.EqualFold(s, "all") {
		return "auto"
	}
	return s
}

// NormalizeGapQuestion 对问句做去首尾空白、小写、折叠连续空白与去除末尾中英文标点，返回规范文本与 64 位 SHA-256 键。
func NormalizeGapQuestion(question string) (normalized string, key string) {
	trimmed := strings.TrimSpace(question)
	if trimmed == "" {
		return "", ""
	}
	var b strings.Builder
	b.Grow(len(trimmed))
	inSpace := false
	for _, r := range strings.ToLower(trimmed) {
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
	norm := strings.TrimSpace(b.String())
	for norm != "" {
		r, size := utf8.DecodeLastRuneInString(norm)
		if r == '？' || r == '?' || r == '。' || r == '！' || r == '!' || unicode.IsSpace(r) {
			norm = norm[:len(norm)-size]
			continue
		}
		break
	}
	norm = strings.TrimSpace(norm)
	if norm == "" {
		norm = strings.ToLower(trimmed)
	}
	return norm, SHA256Hex([]byte(norm))
}

// ValidateGapStatus 校验盲区状态枚举。
func ValidateGapStatus(status string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(status))
	switch v {
	case "open", "resolved", "ignored":
		return v, nil
	default:
		return "", ErrInvalidGapStatus
	}
}

func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 || utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	var b strings.Builder
	count := 0
	for _, r := range s {
		if count >= maxRunes {
			break
		}
		b.WriteRune(r)
		count++
	}
	return b.String()
}

// RecordKnowledgeGap 写入或累加一条线上拒答盲区记录。
// 同一项目+同规范问法重复触发时累加 hit_count 并刷新 last_asked_at 与近似线索；
// 若之前标为 resolved 但再次拒答，则自动重开为 open（ignored 保持忽略）。
func (s *Store) RecordKnowledgeGap(params RecordGapParams) error {
	q := strings.TrimSpace(params.Question)
	if q == "" {
		return errors.New("question 不能为空")
	}
	_, normKey := NormalizeGapQuestion(q)
	proj := truncateRunes(NormalizeGapProjectSlug(params.ProjectSlug), 64)
	reason := truncateRunes(strings.TrimSpace(params.RefuseReason), 64)
	if reason == "" {
		reason = "threshold"
	}
	heading := truncateRunes(strings.TrimSpace(params.NearestHeading), 500)

	var nearestID any
	if params.NearestChunkID != nil && *params.NearestChunkID > 0 {
		nearestID = *params.NearestChunkID
	}

	_, err := s.db.Exec(`
		INSERT INTO knowledge_gaps
			(project_slug, question, normalized_key, refuse_reason, top1_final, nearest_chunk_id, nearest_heading, hit_count, status, last_asked_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 1, 'open', CURRENT_TIMESTAMP)
		ON DUPLICATE KEY UPDATE
			question = VALUES(question),
			refuse_reason = VALUES(refuse_reason),
			top1_final = VALUES(top1_final),
			nearest_chunk_id = VALUES(nearest_chunk_id),
			nearest_heading = VALUES(nearest_heading),
			hit_count = hit_count + 1,
			last_asked_at = CURRENT_TIMESTAMP,
			status = CASE WHEN status = 'resolved' THEN 'open' ELSE status END`,
		proj, q, normKey, reason, params.Top1Final, nearestID, heading,
	)
	return err
}

// EnsureDemoKnowledgeGap 幂等插入示例盲区（仅当同项目同问法不存在时写入，不重复累加 hit_count）。
func (s *Store) EnsureDemoKnowledgeGap(params RecordGapParams, initialHitCount int) error {
	q := strings.TrimSpace(params.Question)
	if q == "" {
		return errors.New("question 不能为空")
	}
	if initialHitCount <= 0 {
		initialHitCount = 1
	}
	_, normKey := NormalizeGapQuestion(q)
	proj := truncateRunes(NormalizeGapProjectSlug(params.ProjectSlug), 64)
	reason := truncateRunes(strings.TrimSpace(params.RefuseReason), 64)
	if reason == "" {
		reason = "threshold"
	}
	heading := truncateRunes(strings.TrimSpace(params.NearestHeading), 500)

	var nearestID any
	if params.NearestChunkID != nil && *params.NearestChunkID > 0 {
		nearestID = *params.NearestChunkID
	}

	_, err := s.db.Exec(`
		INSERT IGNORE INTO knowledge_gaps
			(project_slug, question, normalized_key, refuse_reason, top1_final, nearest_chunk_id, nearest_heading, hit_count, status, last_asked_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'open', CURRENT_TIMESTAMP)`,
		proj, q, normKey, reason, params.Top1Final, nearestID, heading, initialHitCount,
	)
	return err
}

// DeleteKnowledgeGapByQuestion 按精确问题文本删除测试遗留盲区记录。
func (s *Store) DeleteKnowledgeGapByQuestion(question string) (int64, error) {
	res, err := s.db.Exec("DELETE FROM knowledge_gaps WHERE question = ?", strings.TrimSpace(question))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ListKnowledgeGaps 按 hit_count DESC, last_asked_at DESC 列出盲区记录，附带近似文档与负责人线索。
func (s *Store) ListKnowledgeGaps(projectSlug, status string) ([]KnowledgeGap, error) {
	var conds []string
	var args []any

	proj := strings.TrimSpace(projectSlug)
	if proj != "" && !strings.EqualFold(proj, "all") {
		conds = append(conds, "g.project_slug = ?")
		args = append(args, proj)
	}
	st := strings.TrimSpace(status)
	if st != "" && !strings.EqualFold(st, "all") {
		validSt, err := ValidateGapStatus(st)
		if err != nil {
			return nil, err
		}
		conds = append(conds, "g.status = ?")
		args = append(args, validSt)
	}

	whereClause := ""
	if len(conds) > 0 {
		whereClause = "WHERE " + strings.Join(conds, " AND ")
	}

	query := fmt.Sprintf(`
		SELECT g.id, g.project_slug, g.question, g.normalized_key, g.refuse_reason,
		       g.top1_final, g.nearest_chunk_id, g.nearest_heading,
		       d.id, COALESCE(d.title, ''), COALESCE(d.filename, ''), COALESCE(d.owner, ''), COALESCE(p.slug, ''),
		       g.hit_count, g.status, g.last_asked_at, g.created_at
		FROM knowledge_gaps g
		LEFT JOIN chunks c ON c.id = g.nearest_chunk_id
		LEFT JOIN documents d ON d.id = c.document_id
		LEFT JOIN projects p ON p.id = d.project_id
		%s
		ORDER BY g.hit_count DESC, g.last_asked_at DESC, g.id DESC`, whereClause)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]KnowledgeGap, 0)
	for rows.Next() {
		g, err := scanKnowledgeGap(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// GetKnowledgeGap 按 ID 查询单条知识盲区记录。
func (s *Store) GetKnowledgeGap(id int64) (KnowledgeGap, error) {
	if id <= 0 {
		return KnowledgeGap{}, ErrGapNotFound
	}
	rows, err := s.db.Query(`
		SELECT g.id, g.project_slug, g.question, g.normalized_key, g.refuse_reason,
		       g.top1_final, g.nearest_chunk_id, g.nearest_heading,
		       d.id, COALESCE(d.title, ''), COALESCE(d.filename, ''), COALESCE(d.owner, ''), COALESCE(p.slug, ''),
		       g.hit_count, g.status, g.last_asked_at, g.created_at
		FROM knowledge_gaps g
		LEFT JOIN chunks c ON c.id = g.nearest_chunk_id
		LEFT JOIN documents d ON d.id = c.document_id
		LEFT JOIN projects p ON p.id = d.project_id
		WHERE g.id = ?`, id)
	if err != nil {
		return KnowledgeGap{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return KnowledgeGap{}, err
		}
		return KnowledgeGap{}, ErrGapNotFound
	}
	return scanKnowledgeGap(rows)
}

func scanKnowledgeGap(rows *sql.Rows) (KnowledgeGap, error) {
	var g KnowledgeGap
	var nearestChunkID, nearestDocID sql.NullInt64
	err := rows.Scan(
		&g.ID, &g.ProjectSlug, &g.Question, &g.NormalizedKey, &g.RefuseReason,
		&g.Top1Final, &nearestChunkID, &g.NearestHeading,
		&nearestDocID, &g.NearestDocTitle, &g.NearestDocFilename, &g.NearestDocOwner, &g.NearestProjectSlug,
		&g.HitCount, &g.Status, &g.LastAskedAt, &g.CreatedAt,
	)
	if err != nil {
		return KnowledgeGap{}, err
	}
	if nearestChunkID.Valid {
		v := nearestChunkID.Int64
		g.NearestChunkID = &v
	}
	if nearestDocID.Valid {
		v := nearestDocID.Int64
		g.NearestDocID = &v
	}
	return g, nil
}

// UpdateKnowledgeGapStatus 更新盲区处理状态（open / resolved / ignored）。
func (s *Store) UpdateKnowledgeGapStatus(id int64, status string) (KnowledgeGap, error) {
	if id <= 0 {
		return KnowledgeGap{}, ErrGapNotFound
	}
	validSt, err := ValidateGapStatus(status)
	if err != nil {
		return KnowledgeGap{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return KnowledgeGap{}, err
	}
	defer tx.Rollback()

	var existsID int64
	if err := tx.QueryRow("SELECT id FROM knowledge_gaps WHERE id = ? FOR UPDATE", id).Scan(&existsID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return KnowledgeGap{}, ErrGapNotFound
		}
		return KnowledgeGap{}, err
	}
	if _, err := tx.Exec("UPDATE knowledge_gaps SET status = ? WHERE id = ?", validSt, id); err != nil {
		return KnowledgeGap{}, err
	}
	if err := tx.Commit(); err != nil {
		return KnowledgeGap{}, err
	}
	return s.GetKnowledgeGap(id)
}

// CreateFeedback 保存用户对问答结果的 👍 / 👎 反馈，并在提供 turn_id 时同步更新对应会话轮次的反馈标记。
func (s *Store) CreateFeedback(params CreateFeedbackParams) (EvalFeedback, error) {
	q := strings.TrimSpace(params.Question)
	if q == "" {
		return EvalFeedback{}, errors.New("question 不能为空")
	}
	vote := strings.ToLower(strings.TrimSpace(params.Vote))
	if vote != "up" && vote != "down" {
		return EvalFeedback{}, ErrInvalidFeedbackVote
	}
	comment := strings.TrimSpace(params.Comment)
	if utf8.RuneCountInString(comment) > 500 {
		return EvalFeedback{}, errors.New("comment 不能超过 500 字")
	}
	proj := truncateRunes(NormalizeGapProjectSlug(params.ProjectSlug), 64)
	ids := params.RetrievedChunkIDs
	if ids == nil {
		ids = []int64{}
	}
	idsJSON := marshalCappedChunkIDs(ids, 500)

	tx, err := s.db.Begin()
	if err != nil {
		return EvalFeedback{}, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`
		INSERT INTO eval_feedback
			(project_slug, question, retrieved_chunk_ids, answer, refused, vote, comment)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		proj, q, idsJSON, params.Answer, params.Refused, vote, comment,
	)
	if err != nil {
		return EvalFeedback{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return EvalFeedback{}, err
	}
	if params.TurnID != nil && *params.TurnID > 0 {
		if _, err := tx.Exec("UPDATE qa_turns SET feedback_vote = ? WHERE id = ?", vote, *params.TurnID); err != nil && !IsV1SchemaMissingError(err) {
			return EvalFeedback{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return EvalFeedback{}, err
	}
	return s.GetFeedback(id)
}

func marshalCappedChunkIDs(ids []int64, maxBytes int) string {
	cur := append([]int64(nil), ids...)
	for len(cur) > 0 {
		raw, err := json.Marshal(cur)
		if err == nil && len(raw) <= maxBytes {
			return string(raw)
		}
		cur = cur[:len(cur)-1]
	}
	return "[]"
}

// GetFeedback 按 ID 查询单条反馈记录并附带候选切片元数据。
func (s *Store) GetFeedback(id int64) (EvalFeedback, error) {
	if id <= 0 {
		return EvalFeedback{}, ErrFeedbackNotFound
	}
	rows, err := s.db.Query(`
		SELECT id, project_slug, question, retrieved_chunk_ids, answer, refused, vote, comment, promoted_question_id, created_at
		FROM eval_feedback WHERE id = ?`, id)
	if err != nil {
		return EvalFeedback{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return EvalFeedback{}, err
		}
		return EvalFeedback{}, ErrFeedbackNotFound
	}
	fb, err := scanFeedbackRow(rows)
	if err != nil {
		return EvalFeedback{}, err
	}
	list := []EvalFeedback{fb}
	if err := s.attachFeedbackChunkMeta(list); err != nil {
		return EvalFeedback{}, err
	}
	return list[0], nil
}

// ListFeedback 列出反馈队列，默认按未转正（promoted_question_id IS NULL）与 vote='down' 优先排序。
func (s *Store) ListFeedback(voteFilter, promotedFilter string) ([]EvalFeedback, error) {
	var conds []string
	var args []any

	vf := strings.ToLower(strings.TrimSpace(voteFilter))
	switch vf {
	case "", "all":
	case "up", "down":
		conds = append(conds, "vote = ?")
		args = append(args, vf)
	default:
		return nil, ErrInvalidFeedbackVote
	}

	pf := strings.ToLower(strings.TrimSpace(promotedFilter))
	switch pf {
	case "", "all":
	case "pending", "false", "unpromoted":
		conds = append(conds, "promoted_question_id IS NULL")
	case "promoted", "true":
		conds = append(conds, "promoted_question_id IS NOT NULL")
	default:
		return nil, ErrInvalidFeedbackFilter
	}

	whereClause := ""
	if len(conds) > 0 {
		whereClause = "WHERE " + strings.Join(conds, " AND ")
	}

	query := fmt.Sprintf(`
		SELECT id, project_slug, question, retrieved_chunk_ids, answer, refused, vote, comment, promoted_question_id, created_at
		FROM eval_feedback
		%s
		ORDER BY (promoted_question_id IS NULL) DESC, (vote = 'down') DESC, created_at DESC, id DESC`, whereClause)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]EvalFeedback, 0)
	for rows.Next() {
		fb, err := scanFeedbackRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, fb)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.attachFeedbackChunkMeta(out); err != nil {
		return nil, err
	}
	return out, nil
}

func scanFeedbackRow(rows *sql.Rows) (EvalFeedback, error) {
	var fb EvalFeedback
	var rawIDs string
	var promoted sql.NullInt64
	if err := rows.Scan(
		&fb.ID, &fb.ProjectSlug, &fb.Question, &rawIDs, &fb.Answer,
		&fb.Refused, &fb.Vote, &fb.Comment, &promoted, &fb.CreatedAt,
	); err != nil {
		return EvalFeedback{}, err
	}
	if promoted.Valid {
		v := promoted.Int64
		fb.PromotedQuestionID = &v
	}
	fb.RetrievedChunkIDs = []int64{}
	if strings.TrimSpace(rawIDs) != "" {
		_ = json.Unmarshal([]byte(rawIDs), &fb.RetrievedChunkIDs)
		if fb.RetrievedChunkIDs == nil {
			fb.RetrievedChunkIDs = []int64{}
		}
	}
	return fb, nil
}

func (s *Store) attachFeedbackChunkMeta(list []EvalFeedback) error {
	if len(list) == 0 {
		return nil
	}
	seen := make(map[int64]bool)
	var ids []int64
	for _, fb := range list {
		for _, id := range fb.RetrievedChunkIDs {
			if id > 0 && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	marks := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		marks[i] = "?"
		args[i] = id
	}
	rows, err := s.db.Query(`
		SELECT c.id, c.document_id, p.slug, d.title, c.heading_path, c.content
		FROM chunks c
		JOIN documents d ON d.id = c.document_id
		JOIN projects p ON p.id = d.project_id
		WHERE c.id IN (`+strings.Join(marks, ",")+`)`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	byID := make(map[int64]FeedbackChunkMeta, len(ids))
	for rows.Next() {
		var m FeedbackChunkMeta
		var content string
		if err := rows.Scan(&m.ID, &m.DocumentID, &m.ProjectSlug, &m.DocTitle, &m.HeadingPath, &content); err != nil {
			return err
		}
		m.Preview = truncateRunes(strings.TrimSpace(content), 160)
		byID[m.ID] = m
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range list {
		if len(list[i].RetrievedChunkIDs) == 0 {
			continue
		}
		cands := make([]FeedbackChunkMeta, 0, len(list[i].RetrievedChunkIDs))
		for _, id := range list[i].RetrievedChunkIDs {
			if m, ok := byID[id]; ok {
				cands = append(cands, m)
			}
		}
		list[i].CandidateChunks = cands
	}
	return nil
}

// PromoteFeedback 在单事务内将反馈转正为 question_set='feedback' 的金标评测题，并回填 promoted_question_id。
func (s *Store) PromoteFeedback(id int64, params PromoteFeedbackParams) (PromoteFeedbackResult, error) {
	if id <= 0 {
		return PromoteFeedbackResult{}, ErrFeedbackNotFound
	}
	shouldRefuse := params.ShouldRefuse
	goldAnswer := strings.TrimSpace(params.GoldAnswer)
	if shouldRefuse && goldAnswer == "" {
		goldAnswer = "文档中没有依据"
	}
	if !shouldRefuse && goldAnswer == "" {
		return PromoteFeedbackResult{}, errors.New("gold_answer 不能为空")
	}
	if shouldRefuse && params.GoldChunkID != nil && *params.GoldChunkID > 0 {
		return PromoteFeedbackResult{}, errors.New("拒答题不能指定 gold_chunk_id")
	}
	if !shouldRefuse && (params.GoldChunkID == nil || *params.GoldChunkID <= 0) {
		return PromoteFeedbackResult{}, errors.New("非拒答题必须指定有效 gold_chunk_id")
	}

	category := truncateRunes(strings.TrimSpace(params.Category), 64)
	if category == "" {
		category = "feedback"
	}

	tx, err := s.db.Begin()
	if err != nil {
		return PromoteFeedbackResult{}, err
	}
	defer tx.Rollback()

	var fbProjSlug, fbQuestion string
	var promoted sql.NullInt64
	err = tx.QueryRow(`
		SELECT project_slug, question, promoted_question_id
		FROM eval_feedback WHERE id = ? FOR UPDATE`, id).
		Scan(&fbProjSlug, &fbQuestion, &promoted)
	if errors.Is(err, sql.ErrNoRows) {
		return PromoteFeedbackResult{}, ErrFeedbackNotFound
	}
	if err != nil {
		return PromoteFeedbackResult{}, err
	}
	if promoted.Valid {
		return PromoteFeedbackResult{}, ErrFeedbackAlreadyPromoted
	}

	var goldChunkID *int64
	var sourceDocID *int64
	if !shouldRefuse {
		var docID int64
		err := tx.QueryRow("SELECT document_id FROM chunks WHERE id = ? FOR SHARE", *params.GoldChunkID).Scan(&docID)
		if errors.Is(err, sql.ErrNoRows) {
			return PromoteFeedbackResult{}, ErrGoldChunkNotFound
		}
		if err != nil {
			return PromoteFeedbackResult{}, err
		}
		cid := *params.GoldChunkID
		goldChunkID = &cid
		sourceDocID = &docID
	}

	var dupID int64
	err = tx.QueryRow("SELECT id FROM eval_questions WHERE question = ? LIMIT 1 FOR SHARE", fbQuestion).Scan(&dupID)
	if err == nil {
		return PromoteFeedbackResult{}, ErrDuplicateEvalQuestion
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return PromoteFeedbackResult{}, err
	}

	evalProjSlug := strings.TrimSpace(fbProjSlug)
	if strings.EqualFold(evalProjSlug, "auto") || strings.EqualFold(evalProjSlug, "all") {
		evalProjSlug = ""
	}

	eq := EvalQuestion{
		Question:     fbQuestion,
		GoldAnswer:   goldAnswer,
		ShouldRefuse: shouldRefuse,
		GoldChunkID:  goldChunkID,
		ProjectSlug:  evalProjSlug,
		QuestionSet:  "feedback",
		SourceDocID:  sourceDocID,
		Category:     category,
	}
	newQID, err := insertEvalQuestionTx(tx, eq, true)
	if err != nil {
		return PromoteFeedbackResult{}, fmt.Errorf("写入 eval_questions 失败：%w", err)
	}
	eq.ID = newQID

	if _, err := tx.Exec("UPDATE eval_feedback SET promoted_question_id = ? WHERE id = ?", newQID, id); err != nil {
		return PromoteFeedbackResult{}, fmt.Errorf("回填 promoted_question_id 失败：%w", err)
	}
	if err := tx.Commit(); err != nil {
		return PromoteFeedbackResult{}, err
	}
	return PromoteFeedbackResult{
		OK:                 true,
		FeedbackID:         id,
		PromotedQuestionID: newQID,
		Question:           eq,
	}, nil
}
