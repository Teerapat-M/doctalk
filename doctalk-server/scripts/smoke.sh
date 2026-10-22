#!/usr/bin/env bash
# Smoke-test PDF ingest and ask against a running DocTalk stack.
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

api="${VITE_API_URL:-http://localhost:${API_PORT:-8181}}"
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
	bad=$(db_query "SELECT count(*) FROM chunks WHERE document_id = '$id' AND (page_number IS NULL OR page_number < 1 OR embedding IS NULL OR vector_dims(embedding) <> 768)")
	if [ "$bad" -ne 0 ]; then
		echo "$label has $bad chunks missing page_number or 768-d embedding" >&2
		exit 1
	fi
	echo "$label $n chunk(s), page_number set, vector_dims=768"
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
    page = c.get("page_number")
    snippet = c.get("snippet") or ""
    if not cid or not page:
        sys.stderr.write("citation missing chunk_id or page_number\n")
        sys.exit(1)
    print("%s page %s chunk %s %s" % (c.get("document_id"), page, cid, snippet))
' "$out"
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
echo "smoke ok"
