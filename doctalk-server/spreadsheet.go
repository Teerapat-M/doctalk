package main

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
)

const (
	kindPDF  = "pdf"
	kindXLSX = "xlsx"
	kindCSV  = "csv"

	maxSpreadsheetRows = 500
	maxSpreadsheetCols = 64

	statusMapping = "mapping"
)

var zipMagic = []byte{'P', 'K', 0x03, 0x04}

type sheetInfo struct {
	Name     string   `json:"name"`
	Headers  []string `json:"headers"`
	RowCount int      `json:"row_count"`
}

type columnMap struct {
	Content     string `json:"content,omitempty"`
	Question    string `json:"question,omitempty"`
	Answer      string `json:"answer,omitempty"`
	Explanation string `json:"explanation,omitempty"`
}

type sheetRow struct {
	RowNumber   int
	Content     string
	Question    string
	Answer      string
	Explanation string
}

func detectKind(data []byte, filename string) string {
	if isPDF(data) {
		return kindPDF
	}
	if isXLSX(data) {
		return kindXLSX
	}
	if isCSV(data, filename) {
		return kindCSV
	}
	return ""
}

func isXLSX(data []byte) bool {
	if !bytes.HasPrefix(data, zipMagic) {
		return false
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return false
	}
	hasTypes := false
	hasXL := false
	for _, f := range zr.File {
		name := strings.ToLower(f.Name)
		if name == "[content_types].xml" {
			hasTypes = true
		}
		if strings.HasPrefix(name, "xl/") {
			hasXL = true
		}
		if hasTypes && hasXL {
			return true
		}
	}
	return false
}

func isCSV(data []byte, filename string) bool {
	if isPDF(data) || bytes.HasPrefix(data, zipMagic) {
		return false
	}
	if !strings.HasSuffix(strings.ToLower(filename), ".csv") {
		return false
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return false
	}
	if bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}) {
		data = data[3:]
	}
	return utf8.Valid(data)
}

func parseWorkbook(data []byte, kind string) ([]sheetInfo, error) {
	switch kind {
	case kindXLSX:
		return parseXLSX(data)
	case kindCSV:
		return parseCSVSheets(data)
	default:
		return nil, fmt.Errorf("unsupported spreadsheet")
	}
}

func parseXLSX(data []byte) ([]sheetInfo, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("xlsx: %w", err)
	}
	defer f.Close()
	names := f.GetSheetList()
	out := make([]sheetInfo, 0, len(names))
	for _, name := range names {
		rows, err := f.GetRows(name)
		if err != nil {
			return nil, fmt.Errorf("xlsx sheet %s: %w", name, err)
		}
		headers, n := headersAndCount(rows)
		if len(headers) == 0 {
			continue
		}
		out = append(out, sheetInfo{Name: name, Headers: headers, RowCount: n})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("xlsx: no sheets with headers")
	}
	return out, nil
}

func parseCSVSheets(data []byte) ([]sheetInfo, error) {
	rows, err := readCSVRows(data)
	if err != nil {
		return nil, err
	}
	headers, n := headersAndCount(rows)
	if len(headers) == 0 {
		return nil, fmt.Errorf("csv: no header row")
	}
	return []sheetInfo{{Name: "csv", Headers: headers, RowCount: n}}, nil
}

func readCSVRows(data []byte) ([][]string, error) {
	if bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}) {
		data = data[3:]
	}
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("csv: %w", err)
	}
	return rows, nil
}

func headersAndCount(rows [][]string) ([]string, int) {
	if len(rows) == 0 {
		return nil, 0
	}
	headers := trimRow(rows[0], maxSpreadsheetCols)
	if !rowHasText(headers) {
		return nil, 0
	}
	n := 0
	for i := 1; i < len(rows); i++ {
		if rowHasText(rows[i]) {
			n++
		}
	}
	return headers, n
}

func loadSheetRows(data []byte, kind, sheet string, mapping columnMap) ([]sheetRow, error) {
	var rows [][]string
	var err error
	switch kind {
	case kindXLSX:
		f, err := excelize.OpenReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("xlsx: %w", err)
		}
		defer f.Close()
		rows, err = f.GetRows(sheet)
		if err != nil {
			return nil, fmt.Errorf("xlsx sheet %s: %w", sheet, err)
		}
	case kindCSV:
		if sheet != "" && sheet != "csv" {
			return nil, fmt.Errorf("csv has a single sheet named csv")
		}
		rows, err = readCSVRows(data)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported spreadsheet")
	}
	return mapSheetRows(rows, mapping)
}

func mapSheetRows(rows [][]string, mapping columnMap) ([]sheetRow, error) {
	if len(rows) == 0 {
		return nil, fmt.Errorf("empty sheet")
	}
	headers := trimRow(rows[0], maxSpreadsheetCols)
	idx := headerIndex(headers)
	useQuestion := mapping.Question != ""
	useContent := mapping.Content != ""
	if useQuestion == useContent {
		return nil, fmt.Errorf("map exactly one of columns.content or columns.question")
	}
	contentIdx, err := requiredCol(idx, mapping.Content, mapping.Question)
	if err != nil {
		return nil, err
	}
	qIdx, err := mappedCol(idx, mapping.Question)
	if err != nil {
		return nil, err
	}
	aIdx, err := mappedCol(idx, mapping.Answer)
	if err != nil {
		return nil, err
	}
	eIdx, err := mappedCol(idx, mapping.Explanation)
	if err != nil {
		return nil, err
	}

	out := make([]sheetRow, 0, len(rows)-1)
	for i := 1; i < len(rows); i++ {
		if !rowHasText(rows[i]) {
			continue
		}
		cell := func(col int) string {
			if col < 0 || col >= len(rows[i]) {
				return ""
			}
			return strings.TrimSpace(rows[i][col])
		}
		sr := sheetRow{RowNumber: i + 1}
		sr.Answer = cell(aIdx)
		sr.Explanation = cell(eIdx)
		if useContent {
			sr.Content = cell(contentIdx)
		} else {
			sr.Question = cell(qIdx)
			sr.Content = formatQAChunk(sr.Question, sr.Answer, sr.Explanation)
		}
		if strings.TrimSpace(sr.Content) == "" {
			continue
		}
		out = append(out, sr)
		if len(out) > maxSpreadsheetRows {
			return nil, fmt.Errorf("too many rows (max %d)", maxSpreadsheetRows)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no data rows in mapped columns")
	}
	return out, nil
}

func requiredCol(idx map[string]int, names ...string) (int, error) {
	for _, name := range names {
		if name == "" {
			continue
		}
		i, ok := idx[name]
		if !ok {
			return -1, fmt.Errorf("unknown column %q", name)
		}
		return i, nil
	}
	return -1, fmt.Errorf("missing required column")
}

func mappedCol(idx map[string]int, name string) (int, error) {
	if name == "" {
		return -1, nil
	}
	i, ok := idx[name]
	if !ok {
		return -1, fmt.Errorf("unknown column %q", name)
	}
	return i, nil
}

func headerIndex(headers []string) map[string]int {
	out := make(map[string]int, len(headers))
	for i, h := range headers {
		if h == "" {
			continue
		}
		if _, exists := out[h]; !exists {
			out[h] = i
		}
	}
	return out
}

func trimRow(row []string, max int) []string {
	if len(row) > max {
		row = row[:max]
	}
	out := make([]string, len(row))
	for i, c := range row {
		out[i] = strings.TrimSpace(c)
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

func rowHasText(row []string) bool {
	for _, c := range row {
		if strings.TrimSpace(c) != "" {
			return true
		}
	}
	return false
}

func formatQAChunk(q, a, e string) string {
	var b strings.Builder
	if q != "" {
		b.WriteString("Question: ")
		b.WriteString(q)
	}
	if a != "" {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("Answer: ")
		b.WriteString(a)
	}
	if e != "" {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("Explanation: ")
		b.WriteString(e)
	}
	return b.String()
}

func sheetsJSON(sheets []sheetInfo) []byte {
	b, err := json.Marshal(sheets)
	if err != nil {
		return []byte("[]")
	}
	return b
}
