#!/usr/bin/env bash
# Smoke-test PDF/Excel ingest, ask, and quiz against a running DocTalk stack.
# Requires GEMINI_API_KEY (never printed) and a reachable API + Postgres.
set -euo pipefail

# Do not enable `set -x`: it would print GEMINI_API_KEY if referenced.

server_dir=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
repo_dir=$(CDPATH= cd -- "$server_dir/.." && pwd)

if [ -f "$repo_dir/.env" ]; then
	set -a
	# shellcheck disable=SC1091
	. "$repo_dir/.env"
	set +a
fi

if [ -z "${GEMINI_API_KEY:-}" ]; then
	echo "GEMINI_API_KEY is not set" >&2
	exit 1
fi

api="${VITE_API_URL:-http://localhost:${API_PORT:-8081}}"
api="${api%/}"

json_field() {
	python3 -c 'import json,sys; print(json.load(sys.stdin)[sys.argv[1]])' "$1"
}

is_uuid() {
	python3 -c 'import re,sys; sys.exit(0 if re.fullmatch(r"[0-9a-fA-F-]{36}", sys.argv[1]) else 1)' "$1"
}

db_query() {
	sql=$1
	if [ -n "${DATABASE_URL:-}" ] && command -v psql >/dev/null 2>&1; then
		psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -At -c "$sql"
		return
	fi
	if command -v docker >/dev/null 2>&1; then
		docker compose -f "$repo_dir/docker-compose.yml" exec -T db \
			psql -U doctalk -d doctalk -v ON_ERROR_STOP=1 -At -c "$sql"
		return
	fi
	echo "need psql + DATABASE_URL, or docker compose" >&2
	exit 1
}

echo "checking $api/health"
curl -sS -f "$api/health" >/dev/null

upload() {
	file=$1
	label=$2
	echo "uploading $label" >&2
	resp=$(curl -sS -F "file=@${file}" "$api/documents")
	id=$(printf '%s' "$resp" | json_field id || true)
	if [ -z "$id" ] || ! is_uuid "$id"; then
		echo "upload $label did not return an id: $resp" >&2
		exit 1
	fi
	printf '%s\n' "$id"
}

poll() {
	id=$1
	label=$2
	i=0
	while [ "$i" -lt 60 ]; do
		body=$(curl -sS "$api/documents/$id")
		status=$(printf '%s' "$body" | json_field status)
		case "$status" in
		done)
			echo "$label done"
			return 0
			;;
		failed)
			err=$(printf '%s' "$body" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("error_message") or "")')
			echo "$label failed: $err" >&2
			exit 1
			;;
		esac
		i=$((i + 1))
		sleep 2
	done
	echo "$label timed out still $status" >&2
	exit 1
}

check_chunks() {
	id=$1
	label=$2
	n=$(db_query "SELECT count(*) FROM chunks WHERE document_id = '$id'")
	if [ "$n" -lt 1 ]; then
		echo "$label has no chunks" >&2
		exit 1
	fi
	bad=$(db_query "SELECT count(*) FROM chunks WHERE document_id = '$id' AND (embedding IS NULL OR vector_dims(embedding) <> 768 OR NOT ((page_number >= 1 AND sheet_name = '') OR (sheet_name <> '' AND row_number >= 1)))")
	if [ "$bad" -ne 0 ]; then
		echo "$label has $bad chunks missing page/sheet citation or 768-d embedding" >&2
		exit 1
	fi
	echo "$label $n chunk(s), page or sheet/row set, vector_dims=768"
}

ask() {
	question=$1
	id=$2
	echo "asking ($id): $question" >&2
	payload=$(python3 -c 'import json,sys; print(json.dumps({"question":sys.argv[1],"document_ids":[sys.argv[2]]}))' "$question" "$id")
	out=$(mktemp)
	code=$(curl -sS -N --max-time 90 -o "$out" -w '%{http_code}' \
		-H 'Content-Type: application/json' -d "$payload" "$api/ask") || true
	if [ -n "${GEMINI_API_KEY:-}" ] && grep -F -- "$GEMINI_API_KEY" "$out" >/dev/null 2>&1; then
		echo "ask leaked GEMINI_API_KEY" >&2
		rm -f "$out"
		exit 1
	fi
	if [ "$code" != "200" ]; then
		echo "ask HTTP $code: $(cat "$out")" >&2
		rm -f "$out"
		exit 1
	fi
	python3 -c '
import json, sys
text = open(sys.argv[1], encoding="utf-8").read()
event = None
answer = []
citations = []
err = ""
for line in text.splitlines():
    if line.startswith("event:"):
        event = line[6:].strip()
    elif line.startswith("data:"):
        raw = line[5:].strip()
        if not raw:
            continue
        data = json.loads(raw)
        if event == "delta":
            answer.append(data.get("text") or "")
        elif event == "final":
            citations = data.get("citations") or []
        elif event == "error":
            err = data.get("error") or "error"
if err:
    sys.stderr.write("ask error: %s\n" % err)
    sys.exit(1)
ans = "".join(answer).strip()
if not ans:
    sys.stderr.write("ask returned empty answer\n")
    sys.exit(1)
print(ans)
print("citations:")
if not citations:
    sys.stderr.write("ask returned no citations\n")
    sys.exit(1)
for c in citations:
    cid = c.get("chunk_id") or ""
    page = c.get("page_number") or 0
    sheet = c.get("sheet_name") or ""
    row = c.get("row_number") or 0
    snippet = c.get("snippet") or ""
    if not cid:
        sys.stderr.write("citation missing chunk_id\n")
        sys.exit(1)
    if page:
        loc = "page %s" % page
    elif sheet and row:
        loc = "sheet %s row %s" % (sheet, row)
    else:
        sys.stderr.write("citation missing page_number or sheet/row\n")
        sys.exit(1)
    print("%s %s chunk %s %s" % (c.get("document_id"), loc, cid, snippet))
' "$out"
	rm -f "$out"
}

ingest_sheet() {
	id=$1
	sheet=$2
	columns_json=$3
	label=$4
	payload=$(python3 -c 'import json,sys; print(json.dumps({"sheet":sys.argv[1],"columns":json.loads(sys.argv[2])}))' "$sheet" "$columns_json")
	echo "ingesting $label sheet $sheet" >&2
	code=$(curl -sS -o /tmp/doctalk-ingest.json -w '%{http_code}' \
		-H 'Content-Type: application/json' -d "$payload" "$api/documents/$id/ingest") || true
	if [ "$code" != "202" ]; then
		echo "ingest $label HTTP $code: $(cat /tmp/doctalk-ingest.json)" >&2
		exit 1
	fi
}

quiz() {
	id=$1
	n=$2
	mode=$3
	echo "quiz $mode n=$n ($id)" >&2
	payload=$(python3 -c 'import json,sys; print(json.dumps({"document_ids":[sys.argv[1]],"n":int(sys.argv[2]),"mode":sys.argv[3]}))' "$id" "$n" "$mode")
	out=$(mktemp)
	code=$(curl -sS --max-time 90 -o "$out" -w '%{http_code}' \
		-H 'Content-Type: application/json' -d "$payload" "$api/quiz") || true
	if [ -n "${GEMINI_API_KEY:-}" ] && grep -F -- "$GEMINI_API_KEY" "$out" >/dev/null 2>&1; then
		echo "quiz leaked GEMINI_API_KEY" >&2
		rm -f "$out"
		exit 1
	fi
	if [ "$code" != "200" ]; then
		echo "quiz HTTP $code: $(cat "$out")" >&2
		rm -f "$out"
		exit 1
	fi
	python3 -c '
import json, sys
data = json.load(open(sys.argv[1], encoding="utf-8"))
want = int(sys.argv[2])
questions = data.get("questions") or []
if len(questions) < 1:
    sys.stderr.write("quiz returned no questions\n")
    sys.exit(1)
if len(questions) > want:
    sys.stderr.write("quiz returned %d questions, cap is %d\n" % (len(questions), want))
    sys.exit(1)
for q in questions:
    opts = q.get("options") or {}
    for letter in ("A", "B", "C", "D"):
        if not opts.get(letter):
            sys.stderr.write("quiz question missing option %s\n" % letter)
            sys.exit(1)
    if q.get("correct") not in ("A", "B", "C", "D"):
        sys.stderr.write("quiz question missing a single correct option\n")
        sys.exit(1)
    if not (q.get("prompt") or "").strip() or not (q.get("explanation") or "").strip():
        sys.stderr.write("quiz question missing prompt or explanation\n")
        sys.exit(1)
    cit = q.get("citation") or {}
    page = cit.get("page_number") or 0
    sheet = cit.get("sheet_name") or ""
    row = cit.get("row_number") or 0
    if not ((page and not sheet) or (sheet and row)):
        sys.stderr.write("quiz citation missing page or sheet/row\n")
        sys.exit(1)
print("%d %s question(s)" % (len(questions), sys.argv[3]))
' "$out" "$n" "$mode"
	rm -f "$out"
}

en_id=$(upload "$server_dir/testdata/english.pdf" english.pdf)
th_id=$(upload "$server_dir/testdata/thai.pdf" thai.pdf)
poll "$en_id" english.pdf
poll "$th_id" thai.pdf
check_chunks "$en_id" english.pdf
check_chunks "$th_id" thai.pdf
ask "What dimensionality are the Gemini embedding vectors?" "$en_id"
ask "กรุงเทพมหานครเป็นเมืองหลวงของประเทศใด" "$th_id"

xlsx_id=$(upload "$server_dir/testdata/thai.xlsx" thai.xlsx)
csv_id=$(upload "$server_dir/testdata/thai-quiz.csv" thai-quiz.csv)
ingest_sheet "$xlsx_id" "คำถาม" '{"question":"คำถาม","answer":"คำตอบ","explanation":"คำอธิบาย"}' thai.xlsx
ingest_sheet "$csv_id" "csv" '{"question":"question","answer":"answer","explanation":"explanation"}' thai-quiz.csv
poll "$xlsx_id" thai.xlsx
poll "$csv_id" thai-quiz.csv
check_chunks "$xlsx_id" thai.xlsx
check_chunks "$csv_id" thai-quiz.csv
ask "เมืองหลวงของประเทศไทยคือที่ใด" "$xlsx_id"
quiz "$xlsx_id" 3 generate
quiz "$xlsx_id" 4 from_columns
echo "smoke ok"
