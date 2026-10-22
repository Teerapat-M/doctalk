package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestDetectKindSniffsMagicNotFilename(t *testing.T) {
	pdf := []byte("%PDF-1.4\n%")
	if got := detectKind(pdf, "notes.xlsx"); got != kindPDF {
		t.Fatalf("pdf named xlsx: %q", got)
	}
	xlsx := mustThaiXLSX(t)
	if got := detectKind(xlsx, "notes.pdf"); got != kindXLSX {
		t.Fatalf("xlsx named pdf: %q", got)
	}
	if isXLSX([]byte("PK\x03\x04not-a-zip")) {
		t.Fatal("truncated zip must not sniff as xlsx")
	}
	docx := zipWithFiles(t, map[string]string{
		"[Content_Types].xml": "<Types/>",
		"word/document.xml":   "<w/>",
	})
	if isXLSX(docx) {
		t.Fatal("docx must not sniff as xlsx")
	}
	xlsm := zipWithFiles(t, map[string]string{
		"[Content_Types].xml": "<Types/>",
		"xl/workbook.xml":     "<workbook/>",
		"xl/vbaProject.bin":   "macro",
	})
	if !hasVBAProject(xlsm) {
		t.Fatal("expected vba project")
	}
	if isXLSX(xlsm) {
		t.Fatal("macro-enabled xlsx must not sniff as xlsx")
	}
	csv := []byte("question,answer\nก,ข\n")
	if got := detectKind(csv, "quiz.csv"); got != kindCSV {
		t.Fatalf("csv: %q", got)
	}
	if got := detectKind(csv, "quiz.txt"); got != "" {
		t.Fatalf("csv without .csv name: %q", got)
	}
	if got := detectKind(append([]byte{0}, csv...), "quiz.csv"); got != "" {
		t.Fatalf("csv with NUL: %q", got)
	}
}

func TestParseThaiXLSXSheetsAndHeaders(t *testing.T) {
	data := mustThaiXLSX(t)
	sheets, err := parseXLSX(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(sheets) != 2 {
		t.Fatalf("sheets=%d %+v", len(sheets), sheets)
	}
	qa := sheets[0]
	if qa.Name != "คำถาม" {
		t.Fatalf("first sheet=%q", qa.Name)
	}
	if strings.Join(qa.Headers, ",") != "คำถาม,คำตอบ,คำอธิบาย" {
		t.Fatalf("headers=%v", qa.Headers)
	}
	if qa.RowCount != 4 {
		t.Fatalf("row_count=%d", qa.RowCount)
	}
	content := sheets[1]
	if content.Name != "เนื้อหา" || content.RowCount != 2 {
		t.Fatalf("content sheet=%+v", content)
	}
}

func TestMapSheetRowsQuestionAndContent(t *testing.T) {
	data := mustThaiXLSX(t)
	rows, err := loadSheetRows(data, kindXLSX, "คำถาม", columnMap{
		Question:    "คำถาม",
		Answer:      "คำตอบ",
		Explanation: "คำอธิบาย",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("rows=%d", len(rows))
	}
	if rows[0].RowNumber != 2 || rows[0].Question == "" || rows[0].Answer != "กรุงเทพมหานคร" {
		t.Fatalf("row0=%+v", rows[0])
	}
	if !strings.Contains(rows[0].Content, "Question:") || !strings.Contains(rows[0].Content, "กรุงเทพมหานคร") {
		t.Fatalf("content=%q", rows[0].Content)
	}

	contentRows, err := loadSheetRows(data, kindXLSX, "เนื้อหา", columnMap{Content: "เนื้อหา"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(contentRows) != 2 || !strings.Contains(contentRows[0].Content, "กรุงเทพมหานคร") {
		t.Fatalf("content rows=%+v", contentRows)
	}

	csvData, err := os.ReadFile(filepath.Join("testdata", "thai-quiz.csv"))
	if err != nil {
		t.Fatal(err)
	}
	csvRows, err := loadSheetRows(csvData, kindCSV, "csv", columnMap{
		Question:    "question",
		Answer:      "answer",
		Explanation: "explanation",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(csvRows) != 4 || csvRows[3].Answer != "เชียงใหม่" {
		t.Fatalf("csv rows=%+v", csvRows)
	}
}

func TestMapSheetRowsCapsRows(t *testing.T) {
	rows := [][]string{{"a"}, {"1"}, {"2"}, {"3"}}
	if _, err := mapSheetRows(rows, columnMap{Content: "a"}, 1); err == nil || !strings.Contains(err.Error(), "too many rows") {
		t.Fatalf("err=%v", err)
	}
}

func TestMapSheetRowsRejectsBothOrNeither(t *testing.T) {
	rows := [][]string{{"a", "b"}, {"1", "2"}}
	if _, err := mapSheetRows(rows, columnMap{}, 0); err == nil {
		t.Fatal("expected error for missing mapping")
	}
	if _, err := mapSheetRows(rows, columnMap{Content: "a", Question: "b"}, 0); err == nil {
		t.Fatal("expected error for both mappings")
	}
	if _, err := mapSheetRows(rows, columnMap{Content: "missing"}, 0); err == nil {
		t.Fatal("expected unknown column")
	}
}

func TestThaiXLSXFixtureOnDisk(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "thai.xlsx"))
	if err != nil {
		t.Fatal(err)
	}
	if !isXLSX(data) {
		t.Fatal("fixture is not xlsx")
	}
	sheets, err := parseWorkbook(data, kindXLSX)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(sheets)
	if !bytes.Contains(raw, []byte("คำถาม")) {
		t.Fatalf("sheets json=%s", raw)
	}
}

func mustThaiXLSX(t *testing.T) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	qa := "คำถาม"
	idx, err := f.NewSheet(qa)
	if err != nil {
		t.Fatal(err)
	}
	f.SetActiveSheet(idx)
	qaRows := [][]string{
		{"คำถาม", "คำตอบ", "คำอธิบาย"},
		{"เมืองหลวงของประเทศไทยคือที่ใด", "กรุงเทพมหานคร", "กรุงเทพมหานครเป็นเมืองหลวงของประเทศไทย"},
		{"สีของธงชาติไทยแถบกลางคือสีใด", "น้ำเงิน", "ธงชาติไทยมีแถบแดง ขาว น้ำเงิน ขาว แดง"},
		{"สกุลเงินของประเทศไทยเรียกว่าอะไร", "บาท", "สกุลเงินของประเทศไทยคือบาท"},
		{"ดอยอินทนนท์ตั้งอยู่ในจังหวัดใด", "เชียงใหม่", "ดอยอินทนนท์อยู่ที่จังหวัดเชียงใหม่"},
	}
	writeSheet(t, f, qa, qaRows)
	content := "เนื้อหา"
	if _, err := f.NewSheet(content); err != nil {
		t.Fatal(err)
	}
	writeSheet(t, f, content, [][]string{
		{"เนื้อหา"},
		{"กรุงเทพมหานครเป็นเมืองหลวงของประเทศไทย มีแม่น้ำเจ้าพระยาไหลผ่าน"},
		{"ดอยอินทนนท์เป็นยอดเขาที่สูงที่สุดในประเทศไทย ตั้งอยู่ที่จังหวัดเชียงใหม่"},
	})
	_ = f.DeleteSheet("Sheet1")
	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeSheet(t *testing.T, f *excelize.File, name string, rows [][]string) {
	t.Helper()
	for i, row := range rows {
		for j, cell := range row {
			cellName, err := excelize.CoordinatesToCellName(j+1, i+1)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.SetCellValue(name, cellName, cell); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func zipWithFiles(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range files {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
