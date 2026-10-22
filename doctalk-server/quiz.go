package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type Quizzer interface {
	GenerateJSON(ctx context.Context, system, user string) ([]byte, error)
}

type quizRequest struct {
	DocumentIDs []string `json:"document_ids"`
	ChunkIDs    []string `json:"chunk_ids"`
	N           *int     `json:"n"`
	Mode        string   `json:"mode"`
}

type quizOptionMap struct {
	A string `json:"A"`
	B string `json:"B"`
	C string `json:"C"`
	D string `json:"D"`
}

type quizQuestionJSON struct {
	Prompt      string        `json:"prompt"`
	Options     quizOptionMap `json:"options"`
	Correct     string        `json:"correct"`
	Explanation string        `json:"explanation"`
	Citation    citationJSON  `json:"citation"`
}

type quizResponse struct {
	Questions []quizQuestionJSON `json:"questions"`
}

type geminiQuizOut struct {
	Questions []struct {
		Prompt      string        `json:"prompt"`
		Options     quizOptionMap `json:"options"`
		Correct     string        `json:"correct"`
		Explanation string        `json:"explanation"`
		SourceIndex int           `json:"source_index"`
	} `json:"questions"`
}

func (s *server) generateQuiz(c *gin.Context) {
	var req quizRequest
	if !requireJSONBody(c, &req) {
		return
	}
	docIDs := uniqueNonEmpty(req.DocumentIDs)
	if len(docIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "document_ids is required"})
		return
	}
	if invalidDocumentIDs(docIDs) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid document_id"})
		return
	}
	chunkIDs := uniqueNonEmpty(req.ChunkIDs)
	if invalidDocumentIDs(chunkIDs) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid chunk_id"})
		return
	}
	maxN := s.cfg.QuizNMax
	if maxN < 1 {
		maxN = defaultQuizNMax
	}
	n, nerr := parseOptionalBounded(req.N, defaultQuizN, maxN, "n")
	if nerr != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": nerr})
		return
	}
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = "generate"
	}
	if mode != "generate" && mode != "from_columns" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "mode must be generate or from_columns"})
		return
	}
	if mode == "generate" && s.quiz == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "GEMINI_API_KEY is not configured"})
		return
	}
	if s.db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server is not configured"})
		return
	}

	timeout := s.cfg.AskTimeout
	if timeout <= 0 {
		timeout = time.Duration(defaultAskTimeoutSec) * time.Second
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
	defer cancel()

	if err := s.ensureAskDocs(ctx, docIDs, guestIDFrom(c)); err != nil {
		if errors.Is(err, errAskDocsBadID) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid document_id"})
			return
		}
		if errors.Is(err, errAskDocsNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "document not found"})
			return
		}
		if errors.Is(err, errAskDocsNotReady) {
			c.JSON(http.StatusConflict, gin.H{"error": "document is not ready"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load documents"})
		return
	}

	chunks, err := s.loadQuizChunks(ctx, docIDs, chunkIDs, n)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load chunks"})
		return
	}
	if len(chunks) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no chunks available"})
		return
	}

	res, rejected := s.reserveQuota(c, rateQuiz)
	if rejected {
		return
	}
	keep := false
	defer func() {
		if !keep {
			res.release()
		}
	}()

	var questions []quizQuestionJSON
	switch mode {
	case "from_columns":
		questions, err = s.buildQuizFromColumns(ctx, chunks, n)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	default:
		questions, err = s.generateQuizFromChunks(ctx, chunks, n)
		if err != nil {
			msg := redactErr(err, s.cfg.GeminiAPIKey)
			c.JSON(http.StatusBadGateway, gin.H{"error": msg})
			return
		}
	}
	keep = true
	c.JSON(http.StatusOK, quizResponse{Questions: questions})
}

func (s *server) loadQuizChunks(ctx context.Context, docIDs, chunkIDs []string, n int) ([]retrievedChunk, error) {
	limit := n * 3
	if limit < 8 {
		limit = 8
	}
	if limit > 40 {
		limit = 40
	}
	q := `
		SELECT c.id::text, c.document_id::text, c.page_number, c.sheet_name, c.row_number,
		       c.content, c.question, c.answer, c.explanation, c.embedding::text,
		       c.choice_a, c.choice_b, c.choice_c, c.choice_d
		FROM chunks c
		WHERE c.document_id = ANY($1::uuid[])
		  AND c.embedding IS NOT NULL
	`
	args := []any{"{" + strings.Join(docIDs, ",") + "}"}
	if len(chunkIDs) > 0 {
		q += ` AND c.id = ANY($2::uuid[]) ORDER BY c.chunk_index LIMIT $3`
		args = append(args, "{"+strings.Join(chunkIDs, ",")+"}", limit)
	} else {
		q += ` ORDER BY c.chunk_index LIMIT $2`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		if isBadUUID(err) {
			return nil, err
		}
		return nil, err
	}
	defer rows.Close()
	out := make([]retrievedChunk, 0)
	for rows.Next() {
		var ch retrievedChunk
		var emb string
		if err := rows.Scan(&ch.ID, &ch.DocumentID, &ch.PageNumber, &ch.SheetName, &ch.RowNumber, &ch.Content, &ch.Question, &ch.Answer, &ch.Explanation, &emb, &ch.ChoiceA, &ch.ChoiceB, &ch.ChoiceC, &ch.ChoiceD); err != nil {
			return nil, err
		}
		ch.Embedding = parseVectorLiteral(emb)
		out = append(out, ch)
	}
	return out, rows.Err()
}

func (s *server) buildQuizFromColumns(ctx context.Context, chunks []retrievedChunk, n int) ([]quizQuestionJSON, error) {
	return assembleQuizFromColumns(ctx, chunks, n, s.quiz)
}

func buildQuizFromColumns(chunks []retrievedChunk, n int) ([]quizQuestionJSON, error) {
	return assembleQuizFromColumns(context.Background(), chunks, n, nil)
}

func assembleQuizFromColumns(ctx context.Context, chunks []retrievedChunk, n int, quiz Quizzer) ([]quizQuestionJSON, error) {
	qa := make([]retrievedChunk, 0, len(chunks))
	for _, ch := range chunks {
		if strings.TrimSpace(ch.Question) != "" && strings.TrimSpace(ch.Answer) != "" {
			qa = append(qa, ch)
		}
	}
	if len(qa) == 0 {
		return nil, fmt.Errorf("no question/answer columns on these rows; ingest with columns.question and columns.answer")
	}
	if n > len(qa) {
		n = len(qa)
	}
	rand.Shuffle(len(qa), func(i, j int) { qa[i], qa[j] = qa[j], qa[i] })
	selected := qa[:n]

	_, rest := splitMappedChoiceRows(selected)
	extras := loadGeminiDistractors(ctx, quiz, rest)
	out := make([]quizQuestionJSON, 0, n)
	for _, ch := range selected {
		q, err := questionFromColumnRow(ch, qa, extras)
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, nil
}

func splitMappedChoiceRows(rows []retrievedChunk) (mapped, rest []retrievedChunk) {
	for _, ch := range rows {
		if mappedChoices(ch) != nil {
			mapped = append(mapped, ch)
		} else {
			rest = append(rest, ch)
		}
	}
	return mapped, rest
}

func mappedChoices(ch retrievedChunk) *quizOptionMap {
	opts := quizOptionMap{
		A: strings.TrimSpace(ch.ChoiceA),
		B: strings.TrimSpace(ch.ChoiceB),
		C: strings.TrimSpace(ch.ChoiceC),
		D: strings.TrimSpace(ch.ChoiceD),
	}
	if opts.A == "" || opts.B == "" || opts.C == "" || opts.D == "" {
		return nil
	}
	return &opts
}

func questionFromColumnRow(ch retrievedChunk, pool []retrievedChunk, extras map[string][]string) (quizQuestionJSON, error) {
	expl := strings.TrimSpace(ch.Explanation)
	if opts := mappedChoices(ch); opts != nil {
		letter, err := correctLetterFromRow(ch, *opts)
		if err != nil {
			return quizQuestionJSON{}, err
		}
		if expl == "" {
			expl = optionText(*opts, letter)
		}
		return quizQuestionJSON{
			Prompt:      strings.TrimSpace(ch.Question),
			Options:     *opts,
			Correct:     letter,
			Explanation: expl,
			Citation:    citationFromChunk(ch),
		}, nil
	}
	correct := strings.TrimSpace(ch.Answer)
	distractors := extras[strings.TrimSpace(ch.Question)]
	if len(distractors) < 3 {
		distractors = pickDistractors(pool, ch, 3)
	}
	if len(distractors) < 3 {
		return quizQuestionJSON{}, fmt.Errorf("need at least 4 distinct answers to build options from columns")
	}
	opts, letter := shuffleOptions(correct, distractors[:3])
	if expl == "" {
		expl = correct
	}
	return quizQuestionJSON{
		Prompt:      strings.TrimSpace(ch.Question),
		Options:     opts,
		Correct:     letter,
		Explanation: expl,
		Citation:    citationFromChunk(ch),
	}, nil
}

func optionText(opts quizOptionMap, letter string) string {
	switch letter {
	case "A":
		return opts.A
	case "B":
		return opts.B
	case "C":
		return opts.C
	case "D":
		return opts.D
	default:
		return ""
	}
}

func correctLetterFromRow(ch retrievedChunk, opts quizOptionMap) (string, error) {
	ans := strings.TrimSpace(ch.Answer)
	up := strings.ToUpper(ans)
	if up == "A" || up == "B" || up == "C" || up == "D" {
		if optionText(opts, up) != "" {
			return up, nil
		}
	}
	want := strings.ToLower(ans)
	for _, letter := range []string{"A", "B", "C", "D"} {
		if strings.ToLower(optionText(opts, letter)) == want && want != "" {
			return letter, nil
		}
	}
	return "", fmt.Errorf("answer %q does not match mapped choices", ans)
}

func pickDistractors(pool []retrievedChunk, correct retrievedChunk, n int) []string {
	want := strings.ToLower(strings.TrimSpace(correct.Answer))
	type scored struct {
		text  string
		score float64
		idx   int
	}
	seen := map[string]struct{}{}
	cands := make([]scored, 0, len(pool))
	for i, ch := range pool {
		a := strings.TrimSpace(ch.Answer)
		key := strings.ToLower(a)
		if a == "" || key == want {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		cands = append(cands, scored{text: a, score: cosineSim(correct.Embedding, ch.Embedding), idx: i})
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		return cands[i].idx < cands[j].idx
	})
	out := make([]string, 0, n)
	for _, c := range cands {
		out = append(out, c.text)
		if len(out) == n {
			break
		}
	}
	return out
}

func cosineSim(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
	}
	if math.IsNaN(dot) {
		return 0
	}
	return dot
}

type typedQuizzer interface {
	GenerateTypedJSON(ctx context.Context, system, user string, schema map[string]any) ([]byte, error)
}

func distractorResponseSchema() map[string]any {
	return map[string]any{
		"type": "OBJECT",
		"properties": map[string]any{
			"items": map[string]any{
				"type": "ARRAY",
				"items": map[string]any{
					"type": "OBJECT",
					"properties": map[string]any{
						"distractors": map[string]any{
							"type":  "ARRAY",
							"items": map[string]any{"type": "STRING"},
						},
					},
					"required": []string{"distractors"},
				},
			},
		},
		"required": []string{"items"},
	}
}

func loadGeminiDistractors(ctx context.Context, quiz Quizzer, selected []retrievedChunk) map[string][]string {
	out := map[string][]string{}
	typed, ok := quiz.(typedQuizzer)
	if !ok || len(selected) == 0 {
		return out
	}
	system := `You write wrong answers of the same type as each correct answer.
If the answer is a color, other colors; a city, other cities; a currency, other currencies; a number, other numbers.
Match the language. Do not repeat the correct answer. Do not use unrelated types.`
	var b strings.Builder
	b.WriteString("Return JSON {\"items\":[{\"distractors\":[\"...\",\"...\",\"...\"]}] } with one item per question, three distractors each.\n")
	for i, ch := range selected {
		fmt.Fprintf(&b, "%d. Question: %s\nCorrect: %s\n", i+1, strings.TrimSpace(ch.Question), strings.TrimSpace(ch.Answer))
	}
	raw, err := typed.GenerateTypedJSON(ctx, system, b.String(), distractorResponseSchema())
	if err != nil {
		return out
	}
	var parsed struct {
		Items []struct {
			Distractors []string `json:"distractors"`
		} `json:"items"`
	}
	if err := json.Unmarshal(extractJSONObject(raw), &parsed); err != nil {
		return out
	}
	if len(parsed.Items) != len(selected) {
		return out
	}
	for i, ch := range selected {
		want := strings.ToLower(strings.TrimSpace(ch.Answer))
		seen := map[string]struct{}{want: {}}
		var ds []string
		for _, d := range parsed.Items[i].Distractors {
			d = strings.TrimSpace(d)
			key := strings.ToLower(d)
			if d == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			ds = append(ds, d)
			if len(ds) == 3 {
				break
			}
		}
		if len(ds) == 3 {
			out[strings.TrimSpace(ch.Question)] = ds
		}
	}
	return out
}

func shuffleOptions(correct string, distractors []string) (quizOptionMap, string) {
	vals := []string{correct, distractors[0], distractors[1], distractors[2]}
	rand.Shuffle(4, func(i, j int) { vals[i], vals[j] = vals[j], vals[i] })
	letters := []string{"A", "B", "C", "D"}
	letter := "A"
	for i, v := range vals {
		if v == correct {
			letter = letters[i]
			break
		}
	}
	return quizOptionMap{A: vals[0], B: vals[1], C: vals[2], D: vals[3]}, letter
}

func (s *server) generateQuizFromChunks(ctx context.Context, chunks []retrievedChunk, n int) ([]quizQuestionJSON, error) {
	system, user := buildQuizPrompt(chunks, n)
	raw, err := s.quiz.GenerateJSON(ctx, system, user)
	if err != nil {
		return nil, err
	}
	questions, err := parseQuizJSON(raw, chunks, n)
	if err == nil {
		return questions, nil
	}
	raw, err2 := s.quiz.GenerateJSON(ctx, system, user+"\n\nReturn valid JSON only, matching the schema.")
	if err2 != nil {
		return nil, err
	}
	return parseQuizJSON(raw, chunks, n)
}

func buildQuizPrompt(chunks []retrievedChunk, n int) (system, user string) {
	system = `You write multiple-choice quizzes grounded in the provided excerpts.
Create exactly the requested number of questions.
Each question has options A, B, C, D and exactly one correct option.
Write a short explanation.
Use source_index as the 1-based excerpt number the question is based on.
Do not use outside knowledge. Match the language of the source (Thai or English).`
	var b strings.Builder
	fmt.Fprintf(&b, "Create %d multiple-choice questions.\n\nExcerpts:\n", n)
	for i, ch := range chunks {
		fmt.Fprintf(&b, "[%d] (%s)\n%s\n\n", i+1, chunkSourceLabel(ch), ch.Content)
	}
	return system, b.String()
}

func parseQuizJSON(raw []byte, chunks []retrievedChunk, n int) ([]quizQuestionJSON, error) {
	raw = extractJSONObject(raw)
	var parsed geminiQuizOut
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("quiz json: %w", err)
	}
	if len(parsed.Questions) == 0 {
		return nil, fmt.Errorf("quiz json: no questions")
	}
	if len(parsed.Questions) < n {
		return nil, fmt.Errorf("quiz json: got %d questions, want %d", len(parsed.Questions), n)
	}
	if len(parsed.Questions) > n {
		parsed.Questions = parsed.Questions[:n]
	}
	out := make([]quizQuestionJSON, 0, len(parsed.Questions))
	for _, q := range parsed.Questions {
		letter := strings.ToUpper(strings.TrimSpace(q.Correct))
		if letter != "A" && letter != "B" && letter != "C" && letter != "D" {
			return nil, fmt.Errorf("quiz json: invalid correct option")
		}
		if strings.TrimSpace(q.Prompt) == "" || q.Options.A == "" || q.Options.B == "" || q.Options.C == "" || q.Options.D == "" {
			return nil, fmt.Errorf("quiz json: incomplete question")
		}
		idx := q.SourceIndex - 1
		if idx < 0 || idx >= len(chunks) {
			idx = 0
		}
		out = append(out, quizQuestionJSON{
			Prompt:      strings.TrimSpace(q.Prompt),
			Options:     q.Options,
			Correct:     letter,
			Explanation: strings.TrimSpace(q.Explanation),
			Citation:    citationFromChunk(chunks[idx]),
		})
	}
	return out, nil
}

func extractJSONObject(raw []byte) []byte {
	s := strings.TrimSpace(string(raw))
	if i := strings.Index(s, "```"); i >= 0 {
		s = s[i+3:]
		s = strings.TrimPrefix(s, "json")
		if j := strings.Index(s, "```"); j >= 0 {
			s = s[:j]
		}
		s = strings.TrimSpace(s)
	}
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		s = s[start : end+1]
	}
	return []byte(s)
}
