# Grist Import Notes

This file records the real Grist behavior learned while testing `airvault import grist` against a local `gristlabs/grist` Docker container. Keep this as the working reference before changing the importer.

## Local Offline Grist

Use Colima plus the official Grist container:

```bash
colima start --cpu 2 --memory 4 --disk 20
docker run -d \
  --name airvault-grist-test \
  -p 8484:8484 \
  -e GRIST_SINGLE_ORG=docs \
  -e GRIST_BOOT_KEY=airvault-boot-key \
  -v /tmp/airvault-grist:/persist \
  gristlabs/grist
```

Bootstrap login for local API testing:

```bash
curl -fsS \
  -H 'Content-Type: application/json' \
  -c /tmp/airvault-grist-cookies.txt \
  -d '{"bootKey":"airvault-boot-key","adminEmail":"airvault@example.com"}' \
  http://localhost:8484/boot/login
```

Then use the cookie with `airvault import grist --cookie "$COOKIE"` if a Grist API key has not been created through the UI yet.

## Real API Behaviors Confirmed

- `POST /api/workspaces/{workspaceID}/docs` with `{"name":"..."}` creates a doc and returns a JSON string doc ID.
- `POST /api/docs/{docID}/tables` accepts table creation with columns.
- `POST /api/docs/{docID}/tables/{tableID}/records` accepts records with `{"records":[{"fields":{...}}]}`.
- Large record payloads can return HTTP 413 `request entity too large`. Current importer inserts records in batches of 25.
- Grist formula columns work when created with column fields:

```json
{"formula":"$Score * 2","isFormula":true}
```

- Attachment uploads use `POST /api/docs/{docID}/attachments` with multipart form field `upload`.
- Cookie-authenticated multipart uploads require `X-Requested-With: XMLHttpRequest`; otherwise Grist returns a 401/CSRF-style error.

## Grist ID Rules Learned

Grist rejects some Airtable-derived IDs:

- Leading underscore column IDs can fail, for example `_airtable_record_id`.
- Column IDs derived from numeric-leading field names can fail, for example `_1st_Interview`.
- Use IDs that start with a letter. Current importer prefixes invalid starts with `C_`.
- `ID` is reserved by Grist and cannot be used as a normal column ID. Current importer renames reserved column IDs with `_Field`.
- Preserve Airtable record IDs in `Airtable_Record_ID`.

## Cell Value Rules Learned

Raw Airtable JSON values are not always valid Grist cell values.

- Scalars are acceptable: string, number, boolean, null.
- `multipleSelects` must be encoded as a Grist list value: `["L", "Choice A", "Choice B"]`.
- Attachment cells must be encoded as a Grist list value of uploaded attachment IDs: `["L", 1, 2]`.
- Unsupported complex Airtable values such as collaborator objects should be serialized to JSON text.
- Linked records are currently serialized to JSON text containing Airtable record IDs; this preserves IDs but does not rebuild Grist refs yet.

## Formula Translation

Airtable formulas in the archive can reference fields in either form:

- `{Field Name}`
- `{fldXXXXXXXXXXXXXX}`

The translator must resolve both to Grist column IDs. In the real backup dry-run, resolving field IDs improved formula status from mostly `needs_review` to:

```json
{
  "formulas": 98,
  "translated": 78,
  "needs_review": 20
}
```

Simple arithmetic formulas like `{Score} * 2` translate to `$Score * 2` and were confirmed by reading computed values back from Grist.

Formulas still need review when they use Airtable-specific functions or URL/string-building behavior, for example:

- `DATETIME_DIFF`
- `RECORD_ID`
- complex `CONCATENATE`
- `ENCODE_URL_COMPONENT`
- Airtable string concatenation with `&`

## Current Full Import Command

```bash
BACKUP="/Volumes/SHAMS M1/wavedepth Dropbox/Ashraf Ali/Mac (2)/Documents/Projects/personal-stuff/ashraf-airtable-backup/runs/2026-05-24_164706"
REPORT="/Volumes/SHAMS M1/wavedepth Dropbox/Ashraf Ali/Mac (2)/Documents/Projects/personal-stuff/ashraf-airtable-backup/reports/grist-import-$(date +%Y-%m-%d_%H%M%S).json"
COOKIE=$(awk '$6=="grist_core" {print "grist_core="$7}' /tmp/airvault-grist-cookies.txt)

./airvault import grist \
  --path "$BACKUP" \
  --url http://localhost:8484 \
  --workspace 3 \
  --cookie "$COOKIE" \
  --include-formulas \
  --include-attachments \
  --report "$REPORT" \
  --json
```

## Validation

After import, verify with the real Grist API:

```bash
DOC=$(jq -r '.docs.appFixture' /tmp/airvault-grist-import.json)
curl -fsS -b /tmp/airvault-grist-cookies.txt \
  "http://localhost:8484/api/docs/$DOC/tables/Fixture_Table/records" | jq '.records'
```

Do not validate this importer with a fake Grist server. The real Grist server enforces payload details that a simple fake will miss.
