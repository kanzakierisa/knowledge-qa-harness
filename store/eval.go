package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// EvalQuestion 是人工确认后的金标题；拒答题没有标准切片。
type EvalQuestion struct {
	ID           int64  `json:"id"`
	Question     string `json:"question"`
	GoldAnswer   string `json:"gold_answer"`
	ShouldRefuse bool   `json:"should_refuse"`
	GoldChunkID  *int64 `json:"gold_chunk_id"`
	ProjectSlug  string `json:"project_slug,omitempty"`
	QuestionSet  string `json:"question_set,omitempty"`
	SourceDocID  *int64 `json:"source_doc_id,omitempty"`
	Category     string `json:"category,omitempty"`
}

// EvalRun 保留原始评测结果和三态评分，nil 表示不适用或尚未人工标注。
type EvalRun struct {
	ID                int64   `json:"run_id"`
	QuestionID        int64   `json:"question_id"`
	Question          string  `json:"question"`
	GoldAnswer        string  `json:"gold_answer"`
	ShouldRefuse      bool    `json:"should_refuse"`
	GoldChunkID       *int64  `json:"gold_chunk_id"`
	ProjectSlug       string  `json:"project_slug,omitempty"`
	QuestionSet       string  `json:"question_set,omitempty"`
	Category          string  `json:"category,omitempty"`
	BatchID           string  `json:"batch_id,omitempty"`
	RetrievedChunkIDs []int64 `json:"retrieved_chunk_ids"`
	Answer            string  `json:"answer"`
	Refused           bool    `json:"refused"`
	Hit               *bool   `json:"hit"`
	CitationOK        *bool   `json:"citation_ok"`
	Usable            *bool   `json:"usable"`
}

var (
	ErrEvalRunNotFound = errors.New("评测记录不存在")
	ErrEvalRunRefused  = errors.New("拒答记录不参与答案可用性标注")
	ErrInvalidEvalSet  = errors.New("question_set 仅支持 base52、holdout、feedback、all")
)

func scanEvalQuestionsV1(rows *sql.Rows) ([]EvalQuestion, error) {
	defer rows.Close()
	out := make([]EvalQuestion, 0)
	for rows.Next() {
		var q EvalQuestion
		var gold sql.NullInt64
		if err := rows.Scan(&q.ID, &q.Question, &q.GoldAnswer, &q.ShouldRefuse, &gold); err != nil {
			return nil, err
		}
		if gold.Valid {
			q.GoldChunkID = &gold.Int64
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func scanEvalQuestionsV2(rows *sql.Rows) ([]EvalQuestion, error) {
	defer rows.Close()
	out := make([]EvalQuestion, 0)
	for rows.Next() {
		var q EvalQuestion
		var gold, srcDoc sql.NullInt64
		if err := rows.Scan(&q.ID, &q.Question, &q.GoldAnswer, &q.ShouldRefuse, &gold,
			&q.ProjectSlug, &q.QuestionSet, &srcDoc, &q.Category); err != nil {
			return nil, err
		}
		if gold.Valid {
			q.GoldChunkID = &gold.Int64
		}
		if srcDoc.Valid {
			v := srcDoc.Int64
			q.SourceDocID = &v
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func (s *Store) AllEvalQuestions() ([]EvalQuestion, error) {
	rows, err := s.db.Query(`SELECT id, question, gold_answer, should_refuse, gold_chunk_id,
		project_slug, question_set, source_doc_id, category FROM eval_questions ORDER BY id`)
	if err != nil {
		if IsV1SchemaMissingError(err) {
			v1Rows, v1Err := s.db.Query("SELECT id, question, gold_answer, should_refuse, gold_chunk_id FROM eval_questions ORDER BY id")
			if v1Err != nil {
				return nil, v1Err
			}
			return scanEvalQuestionsV1(v1Rows)
		}
		return nil, err
	}
	return scanEvalQuestionsV2(rows)
}

// EvalQuestionsBySet 按评测分组读取金标题；支持 base52、holdout、feedback、all（空串等价于 all）。
func (s *Store) EvalQuestionsBySet(questionSet string) ([]EvalQuestion, error) {
	set := strings.ToLower(strings.TrimSpace(questionSet))
	if set == "" || set == "all" {
		return s.AllEvalQuestions()
	}
	switch set {
	case "base52", "holdout", "feedback":
	default:
		return nil, ErrInvalidEvalSet
	}

	rows, err := s.db.Query(`SELECT id, question, gold_answer, should_refuse, gold_chunk_id,
		project_slug, question_set, source_doc_id, category
		FROM eval_questions WHERE question_set = ? ORDER BY id`, set)
	if err != nil {
		if IsV1SchemaMissingError(err) {
			if set == "base52" {
				return s.AllEvalQuestions()
			}
			return []EvalQuestion{}, nil
		}
		return nil, err
	}
	return scanEvalQuestionsV2(rows)
}

// FindGoldChunk 用文档、标题和原文锚点解析实际 ID，避免把重导入后变化的自增 ID 写死。
func (s *Store) FindGoldChunk(filename, heading, anchor string) (int64, error) {
	chunkID, _, err := s.FindGoldChunkWithDoc("", filename, heading, anchor)
	return chunkID, err
}

// FindGoldChunkInProject 在指定项目（为空时不限项目）内解析标准切片 ID。
func (s *Store) FindGoldChunkInProject(projectSlug, filename, heading, anchor string) (int64, error) {
	chunkID, _, err := s.FindGoldChunkWithDoc(projectSlug, filename, heading, anchor)
	return chunkID, err
}

// FindGoldChunkWithDoc 解析标准切片 ID 及其所属 document_id，供 V2 多项目评测绑定 source_doc_id。
func (s *Store) FindGoldChunkWithDoc(projectSlug, filename, heading, anchor string) (chunkID int64, docID int64, err error) {
	if filename == "" || heading == "" || anchor == "" {
		return 0, 0, errors.New("定位标准切片必须提供文档名、标题和原文锚点")
	}
	slug := strings.TrimSpace(projectSlug)
	var rows *sql.Rows
	if slug != "" {
		rows, err = s.db.Query(`SELECT c.id, c.document_id, c.content FROM chunks c
			JOIN documents d ON d.id = c.document_id
			JOIN projects p ON p.id = d.project_id
			WHERE p.slug = ? AND d.filename = ? AND c.heading_path = ? ORDER BY c.id`, slug, filename, heading)
	} else {
		rows, err = s.db.Query(`SELECT c.id, c.document_id, c.content FROM chunks c
			JOIN documents d ON d.id = c.document_id
			WHERE d.filename = ? AND c.heading_path = ? ORDER BY c.id`, filename, heading)
	}
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id, dID int64
		var content string
		if err := rows.Scan(&id, &dID, &content); err != nil {
			return 0, 0, err
		}
		if strings.Contains(content, anchor) {
			chunkID = id
			docID = dID
			count++
		}
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	if count != 1 {
		if slug != "" {
			return 0, 0, fmt.Errorf("标准切片匹配 %d 条，必须唯一：%s / %s / %s", count, slug, filename, heading)
		}
		return 0, 0, fmt.Errorf("标准切片匹配 %d 条，必须唯一：%s / %s", count, filename, heading)
	}
	return chunkID, docID, nil
}

// validateEvalItems 校验每条金标题字段合法、无重复且 should_refuse 与 gold_chunk_id 一致。
func validateEvalItems(questions []EvalQuestion) (refusals int, err error) {
	if len(questions) == 0 {
		return 0, errors.New("金标题列表不能为空")
	}
	seen := make(map[string]bool, len(questions))
	for i, q := range questions {
		if strings.TrimSpace(q.Question) == "" || strings.TrimSpace(q.GoldAnswer) == "" {
			return 0, fmt.Errorf("第 %d 题缺少问题或参考答案", i+1)
		}
		if seen[q.Question] {
			return 0, fmt.Errorf("金标题重复：%s", q.Question)
		}
		seen[q.Question] = true
		if q.ShouldRefuse {
			refusals++
			if q.GoldChunkID != nil {
				return 0, fmt.Errorf("拒答题的 gold_chunk_id 必须为 NULL：%s", q.Question)
			}
		} else if q.GoldChunkID == nil || *q.GoldChunkID <= 0 {
			return 0, fmt.Errorf("有答案题缺少有效 gold_chunk_id：%s", q.Question)
		}
	}
	return refusals, nil
}

func validateEvalSeed(questions []EvalQuestion) error {
	if len(questions) != 25 {
		return fmt.Errorf("阶段 5 金标题必须为 25 条，实际 %d 条", len(questions))
	}
	refusals, err := validateEvalItems(questions)
	if err != nil {
		return err
	}
	if refusals != 7 {
		return fmt.Errorf("金标题必须为 18 条有答案、7 条拒答，实际 %d / %d", len(questions)-refusals, refusals)
	}
	return nil
}

func equalGoldID(a, b *int64) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func normEvalSet(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "base52"
	}
	return s
}

func sameEvalQuestion(a, b EvalQuestion) bool {
	return a.GoldAnswer == b.GoldAnswer &&
		a.ShouldRefuse == b.ShouldRefuse &&
		equalGoldID(a.GoldChunkID, b.GoldChunkID) &&
		strings.TrimSpace(a.ProjectSlug) == strings.TrimSpace(b.ProjectSlug) &&
		normEvalSet(a.QuestionSet) == normEvalSet(b.QuestionSet) &&
		strings.TrimSpace(a.Category) == strings.TrimSpace(b.Category)
}

// withSeedEvalTx 在数据库命名锁和事务内校验切片并读取既有金标题，供全量 seed 与增量追加共用。
func (s *Store) withSeedEvalTx(questions []EvalQuestion, fn func(tx *sql.Tx, existing []EvalQuestion, v2Schema bool) error) error {
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	// 命名锁绑定同一连接，避免并发导入写乱金标题。
	var locked sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK('knowledge_qa:seed_eval', 10)").Scan(&locked); err != nil {
		return err
	}
	if !locked.Valid || locked.Int64 != 1 {
		return errors.New("另一个金标题导入正在执行，请稍后重试")
	}
	defer conn.ExecContext(ctx, "SELECT RELEASE_LOCK('knowledge_qa:seed_eval')")
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// 与插入处于同一事务，任一无效切片都会使整批回滚。
	for _, q := range questions {
		if q.GoldChunkID == nil {
			continue
		}
		var id int64
		if err := tx.QueryRow("SELECT id FROM chunks WHERE id = ? FOR SHARE", *q.GoldChunkID).Scan(&id); err != nil {
			return fmt.Errorf("标准切片 %d 不存在或不可读取：%w", *q.GoldChunkID, err)
		}
	}
	v2Schema := true
	var existing []EvalQuestion
	rows, err := tx.Query(`SELECT id, question, gold_answer, should_refuse, gold_chunk_id,
		project_slug, question_set, source_doc_id, category FROM eval_questions ORDER BY id FOR UPDATE`)
	if err != nil {
		if IsV1SchemaMissingError(err) {
			v2Schema = false
			v1Rows, v1Err := tx.Query("SELECT id, question, gold_answer, should_refuse, gold_chunk_id FROM eval_questions ORDER BY id FOR UPDATE")
			if v1Err != nil {
				return v1Err
			}
			existing, err = scanEvalQuestionsV1(v1Rows)
		} else {
			return err
		}
	} else {
		existing, err = scanEvalQuestionsV2(rows)
	}
	if err != nil {
		return err
	}
	if err := fn(tx, existing, v2Schema); err != nil {
		return err
	}
	return tx.Commit()
}

func indexExistingQuestions(existing []EvalQuestion) (map[string]EvalQuestion, error) {
	byQuestion := make(map[string]EvalQuestion, len(existing))
	for _, q := range existing {
		if _, duplicate := byQuestion[q.Question]; duplicate {
			return nil, errors.New("现有金标题存在重复，未改动金标和历史记录")
		}
		byQuestion[q.Question] = q
	}
	return byQuestion, nil
}

// SeedEvalQuestions 幂等导入已完整解析的 25 题，绝不覆盖金标或删除历史评测。
func (s *Store) SeedEvalQuestions(questions []EvalQuestion) error {
	if err := validateEvalSeed(questions); err != nil {
		return err
	}
	return s.withSeedEvalTx(questions, func(tx *sql.Tx, existing []EvalQuestion, v2Schema bool) error {
		if len(existing) != 0 {
			if len(existing) != len(questions) {
				return fmt.Errorf("已有 %d 条金标题，与本批数量不同；未改动金标和历史记录", len(existing))
			}
			byQuestion, err := indexExistingQuestions(existing)
			if err != nil {
				return err
			}
			for _, q := range questions {
				old, exists := byQuestion[q.Question]
				if !exists || !sameEvalQuestion(old, q) {
					return fmt.Errorf("金标题与已有内容不一致，未覆盖：%s", q.Question)
				}
			}
			return nil
		}
		for _, q := range questions {
			if _, err := insertEvalQuestionTx(tx, q, v2Schema); err != nil {
				return err
			}
		}
		return nil
	})
}

// AppendEvalQuestions 增量去重追加金标题（阶段 6 / 阶段 11 / 阶段 12 扩充评测集用），完整保留已有题目与历史评测记录。
func (s *Store) AppendEvalQuestions(questions []EvalQuestion) (int, error) {
	if _, err := validateEvalItems(questions); err != nil {
		return 0, err
	}
	inserted := 0
	err := s.withSeedEvalTx(questions, func(tx *sql.Tx, existing []EvalQuestion, v2Schema bool) error {
		byQuestion, err := indexExistingQuestions(existing)
		if err != nil {
			return err
		}
		for _, q := range questions {
			if old, exists := byQuestion[q.Question]; exists {
				if !sameEvalQuestion(old, q) {
					return fmt.Errorf("增量金标题与已有内容不一致，未覆盖：%s", q.Question)
				}
				continue
			}
			if _, err := insertEvalQuestionTx(tx, q, v2Schema); err != nil {
				return err
			}
			byQuestion[q.Question] = q
			inserted++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return inserted, nil
}

func insertEvalQuestionTx(tx *sql.Tx, q EvalQuestion, v2Schema bool) (int64, error) {
	if v2Schema {
		res, err := tx.Exec(`INSERT INTO eval_questions
			(question, gold_answer, should_refuse, gold_chunk_id, project_slug, question_set, source_doc_id, category)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			q.Question, q.GoldAnswer, q.ShouldRefuse, q.GoldChunkID,
			strings.TrimSpace(q.ProjectSlug), normEvalSet(q.QuestionSet), q.SourceDocID, strings.TrimSpace(q.Category))
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	}
	res, err := tx.Exec("INSERT INTO eval_questions (question, gold_answer, should_refuse, gold_chunk_id) VALUES (?, ?, ?, ?)",
		q.Question, q.GoldAnswer, q.ShouldRefuse, q.GoldChunkID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// SaveEvalRuns 完整批次一次提交；任何一题写入失败都不会留下半批记录。
func (s *Store) SaveEvalRuns(runs []EvalRun) ([]EvalRun, error) {
	if len(runs) == 0 {
		return nil, errors.New("评测结果为空")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	out := append([]EvalRun(nil), runs...)
	batchID := strings.TrimSpace(out[0].BatchID)
	if batchID == "" {
		batchID = fmt.Sprintf("b-%d", time.Now().UnixNano())
	}
	hasBatchCol := true
	for i := range out {
		if out[i].RetrievedChunkIDs == nil {
			out[i].RetrievedChunkIDs = []int64{}
		}
		if len(out[i].RetrievedChunkIDs) > 5 {
			return nil, errors.New("每题只允许记录检索 Top 5")
		}
		ids, err := json.Marshal(out[i].RetrievedChunkIDs)
		if err != nil {
			return nil, err
		}
		var res sql.Result
		if hasBatchCol {
			out[i].BatchID = batchID
			res, err = tx.Exec(`INSERT INTO eval_runs
				(question_id, retrieved_chunk_ids, answer, refused, hit, citation_ok, usable, batch_id)
				VALUES (?, ?, ?, ?, ?, ?, NULL, ?)`,
				out[i].QuestionID, string(ids), out[i].Answer, out[i].Refused, out[i].Hit, out[i].CitationOK, batchID)
			if err != nil && i == 0 && IsV1SchemaMissingError(err) {
				hasBatchCol = false
				out[i].BatchID = ""
				res, err = tx.Exec(`INSERT INTO eval_runs
					(question_id, retrieved_chunk_ids, answer, refused, hit, citation_ok, usable)
					VALUES (?, ?, ?, ?, ?, ?, NULL)`,
					out[i].QuestionID, string(ids), out[i].Answer, out[i].Refused, out[i].Hit, out[i].CitationOK)
			}
		} else {
			out[i].BatchID = ""
			res, err = tx.Exec(`INSERT INTO eval_runs
				(question_id, retrieved_chunk_ids, answer, refused, hit, citation_ok, usable)
				VALUES (?, ?, ?, ?, ?, ?, NULL)`,
				out[i].QuestionID, string(ids), out[i].Answer, out[i].Refused, out[i].Hit, out[i].CitationOK)
		}
		if err != nil {
			return nil, err
		}
		out[i].ID, err = res.LastInsertId()
		if err != nil {
			return nil, err
		}
		out[i].Usable = nil
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// MarkEvalRun 保存人工判断；false 与未标注 NULL 严格区分，重复标同值也成功。
func (s *Store) MarkEvalRun(id int64, usable bool) error {
	if id <= 0 {
		return ErrEvalRunNotFound
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var refused bool
	err = tx.QueryRow("SELECT refused FROM eval_runs WHERE id = ? FOR UPDATE", id).Scan(&refused)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrEvalRunNotFound
	}
	if err != nil {
		return err
	}
	if refused {
		return ErrEvalRunRefused
	}
	if _, err := tx.Exec("UPDATE eval_runs SET usable = ? WHERE id = ?", usable, id); err != nil {
		return err
	}
	return tx.Commit()
}

func nullableBool(value sql.NullBool) *bool {
	if !value.Valid {
		return nil
	}
	return &value.Bool
}

type evalRunBatchRow struct {
	id         int64
	questionID int64
	refused    bool
	usable     sql.NullBool
}

// EvalUsableSummaryForRun 返回指定 runID 所属批次中已标注的非拒答回答数及其中可用数。
// V2 优先按 batch_id 精确隔离同一批评测（解决 base52 -> holdout 递增 question_id 边界问题）；
// 对 V1 最小表结构或历史 batch_id 为空的记录，保留按 question_id 回落划批逻辑。
func (s *Store) EvalUsableSummaryForRun(runID int64) (marked, usable int, err error) {
	var batchID string
	err = s.db.QueryRow("SELECT batch_id FROM eval_runs WHERE id = ?", runID).Scan(&batchID)
	if err != nil {
		if IsV1SchemaMissingError(err) {
			return s.evalUsableSummaryByOrder(runID, false)
		}
		if errors.Is(err, sql.ErrNoRows) {
			return 0, 0, ErrEvalRunNotFound
		}
		return 0, 0, err
	}
	if strings.TrimSpace(batchID) != "" {
		err = s.db.QueryRow(`SELECT
			COUNT(CASE WHEN refused = 0 AND usable IS NOT NULL THEN 1 END),
			COUNT(CASE WHEN refused = 0 AND usable = 1 THEN 1 END)
			FROM eval_runs WHERE batch_id = ?`, batchID).Scan(&marked, &usable)
		return marked, usable, err
	}
	return s.evalUsableSummaryByOrder(runID, true)
}

func (s *Store) evalUsableSummaryByOrder(runID int64, filterEmptyBatch bool) (marked, usable int, err error) {
	query := "SELECT id, question_id, refused, usable FROM eval_runs ORDER BY id"
	if filterEmptyBatch {
		query = "SELECT id, question_id, refused, usable FROM eval_runs WHERE batch_id = '' ORDER BY id"
	}
	rows, err := s.db.Query(query)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()

	var currentBatch []evalRunBatchRow
	var prevQID int64
	found := false

	for rows.Next() {
		var r evalRunBatchRow
		if err := rows.Scan(&r.id, &r.questionID, &r.refused, &r.usable); err != nil {
			return 0, 0, err
		}
		if len(currentBatch) > 0 && r.questionID <= prevQID {
			if found {
				break
			}
			currentBatch = currentBatch[:0]
		}
		currentBatch = append(currentBatch, r)
		prevQID = r.questionID
		if r.id == runID {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	if !found {
		return 0, 0, ErrEvalRunNotFound
	}
	for _, r := range currentBatch {
		if !r.refused && r.usable.Valid {
			marked++
			if r.usable.Bool {
				usable++
			}
		}
	}
	return marked, usable, nil
}

// LoadEvalRuns 按明确 ID 列表读取一批，避免并发运行或人工标注混入其他批次。
func (s *Store) LoadEvalRuns(ids []int64) ([]EvalRun, error) {
	if len(ids) == 0 {
		return nil, errors.New("必须提供本批评测的 run_id 列表")
	}
	args := make([]any, 0, len(ids))
	marks := make([]string, 0, len(ids))
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			return nil, errors.New("run_id 必须为不重复的正整数")
		}
		seen[id] = true
		args = append(args, id)
		marks = append(marks, "?")
	}
	v2Mode := true
	rows, err := s.db.Query(`SELECT r.id, r.question_id, q.question, q.gold_answer,
		q.should_refuse, q.gold_chunk_id, q.project_slug, q.question_set, q.category, r.batch_id,
		r.retrieved_chunk_ids, r.answer, r.refused, r.hit, r.citation_ok, r.usable
		FROM eval_runs r JOIN eval_questions q ON q.id = r.question_id
		WHERE r.id IN (`+strings.Join(marks, ",")+`) ORDER BY r.id`, args...)
	if err != nil {
		if IsV1SchemaMissingError(err) {
			v2Mode = false
			rows, err = s.db.Query(`SELECT r.id, r.question_id, q.question, q.gold_answer,
				q.should_refuse, q.gold_chunk_id, r.retrieved_chunk_ids, r.answer, r.refused,
				r.hit, r.citation_ok, r.usable
				FROM eval_runs r JOIN eval_questions q ON q.id = r.question_id
				WHERE r.id IN (`+strings.Join(marks, ",")+`) ORDER BY r.id`, args...)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}
	defer rows.Close()
	out := make([]EvalRun, 0, len(ids))
	for rows.Next() {
		var r EvalRun
		var gold sql.NullInt64
		var hit, citation, usable sql.NullBool
		var retrieved sql.NullString
		var answer sql.NullString
		var scanErr error
		if v2Mode {
			scanErr = rows.Scan(&r.ID, &r.QuestionID, &r.Question, &r.GoldAnswer, &r.ShouldRefuse, &gold,
				&r.ProjectSlug, &r.QuestionSet, &r.Category, &r.BatchID,
				&retrieved, &answer, &r.Refused, &hit, &citation, &usable)
		} else {
			scanErr = rows.Scan(&r.ID, &r.QuestionID, &r.Question, &r.GoldAnswer, &r.ShouldRefuse, &gold,
				&retrieved, &answer, &r.Refused, &hit, &citation, &usable)
		}
		if scanErr != nil {
			return nil, scanErr
		}
		if gold.Valid {
			r.GoldChunkID = &gold.Int64
		}
		r.Answer = answer.String
		r.Hit, r.CitationOK, r.Usable = nullableBool(hit), nullableBool(citation), nullableBool(usable)
		r.RetrievedChunkIDs = []int64{}
		if retrieved.Valid && retrieved.String != "" {
			if err := json.Unmarshal([]byte(retrieved.String), &r.RetrievedChunkIDs); err != nil {
				return nil, fmt.Errorf("run_id=%d 检索 ID JSON 无效：%w", r.ID, err)
			}
		}
		if r.RetrievedChunkIDs == nil {
			r.RetrievedChunkIDs = []int64{}
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) != len(ids) {
		return nil, fmt.Errorf("%w：请求 %d 条，读到 %d 条", ErrEvalRunNotFound, len(ids), len(out))
	}
	return out, nil
}
