# Regenerates english.pdf and thai.pdf with real font resources.
# Thai uses embedded Sarabun-Regular.ttf (SIL Open Font License 1.1; see OFL.txt).
from pathlib import Path
from reportlab.pdfgen import canvas
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.ttfonts import TTFont

root = Path(__file__).resolve().parent
pdfmetrics.registerFont(TTFont("Sarabun", str(root / "Sarabun-Regular.ttf")))


def mk(fn, font, pages):
    c = canvas.Canvas(str(fn))
    for lines in pages:
        c.setFont(font, 14)
        y = 800
        for l in lines:
            c.drawString(50, y, l)
            y -= 22
        c.showPage()
    c.save()


mk(
    root / "thai.pdf",
    "Sarabun",
    [
        [
            "หน้าที่หนึ่ง: ประเทศไทยตั้งอยู่ในเอเชียตะวันออกเฉียงใต้",
            "กรุงเทพมหานครเป็นเมืองหลวงของประเทศไทย",
        ],
        [
            "หน้าที่สอง: อาหารไทยมีรสชาติเผ็ดเปรี้ยวหวานเค็ม",
            "ต้มยำกุ้งเป็นอาหารที่มีชื่อเสียงไปทั่วโลก",
        ],
    ],
)
mk(
    root / "english.pdf",
    "Helvetica",
    [
        [
            "Page one: DocTalk lets users ask questions about PDF documents.",
            "Answers include citations with page numbers.",
        ],
        [
            "Page two: Embeddings are stored in Postgres using pgvector.",
            "The Gemini embedding model returns 768 dimensional vectors.",
        ],
    ],
)
