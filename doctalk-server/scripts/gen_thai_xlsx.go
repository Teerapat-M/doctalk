//go:build ignore

package main

import (
	"log"
	"path/filepath"
	"runtime"

	"github.com/xuri/excelize/v2"
)

func main() {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		log.Fatal("caller")
	}
	out := filepath.Join(filepath.Dir(file), "..", "testdata", "thai.xlsx")

	f := excelize.NewFile()
	defer f.Close()
	qa := "คำถาม"
	idx, err := f.NewSheet(qa)
	if err != nil {
		log.Fatal(err)
	}
	f.SetActiveSheet(idx)
	qaRows := [][]string{
		{"คำถาม", "คำตอบ", "คำอธิบาย"},
		{"เมืองหลวงของประเทศไทยคือที่ใด", "กรุงเทพมหานคร", "กรุงเทพมหานครเป็นเมืองหลวงของประเทศไทย"},
		{"สีของธงชาติไทยแถบกลางคือสีใด", "น้ำเงิน", "ธงชาติไทยมีแถบแดง ขาว น้ำเงิน ขาว แดง"},
		{"สกุลเงินของประเทศไทยเรียกว่าอะไร", "บาท", "สกุลเงินของประเทศไทยคือบาท"},
		{"ดอยอินทนนท์ตั้งอยู่ในจังหวัดใด", "เชียงใหม่", "ดอยอินทนนท์อยู่ที่จังหวัดเชียงใหม่"},
	}
	for i, row := range qaRows {
		for j, cell := range row {
			cellName, err := excelize.CoordinatesToCellName(j+1, i+1)
			if err != nil {
				log.Fatal(err)
			}
			if err := f.SetCellValue(qa, cellName, cell); err != nil {
				log.Fatal(err)
			}
		}
	}

	content := "เนื้อหา"
	if _, err := f.NewSheet(content); err != nil {
		log.Fatal(err)
	}
	contentRows := [][]string{
		{"เนื้อหา"},
		{"กรุงเทพมหานครเป็นเมืองหลวงของประเทศไทย มีแม่น้ำเจ้าพระยาไหลผ่าน"},
		{"ดอยอินทนนท์เป็นยอดเขาที่สูงที่สุดในประเทศไทย ตั้งอยู่ที่จังหวัดเชียงใหม่"},
	}
	for i, row := range contentRows {
		for j, cell := range row {
			cellName, err := excelize.CoordinatesToCellName(j+1, i+1)
			if err != nil {
				log.Fatal(err)
			}
			if err := f.SetCellValue(content, cellName, cell); err != nil {
				log.Fatal(err)
			}
		}
	}

	if err := f.DeleteSheet("Sheet1"); err != nil {
		log.Fatal(err)
	}
	if err := f.SaveAs(out); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s", out)
}
