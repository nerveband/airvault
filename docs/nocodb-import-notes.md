# NocoDB Import Notes

This file records the real NocoDB behavior learned while importing an `airvault` archive into a local Docker NocoDB instance. Keep it updated before changing `internal/importer/nocodb`.

## Local Instance

```text
Container: airvault-nocodb
Image: nocodb/nocodb:latest
Primary LAN URL: http://192.168.1.5:8080
Public URL env: NC_PUBLIC_URL=http://192.168.1.5:8080
Login email: airvault@example.com
Login password: airvault-local-2026
```

Prefer the primary LAN URL from browsers and API clients. This instance is meant for local comparison and uses Docker volume `airvault_nocodb`.

## Auth

```bash
NOCODB_TOKEN=$(curl -fsS -X POST http://192.168.1.5:8080/api/v1/auth/user/signin \
  -H 'Content-Type: application/json' \
  -d '{"email":"airvault@example.com","password":"airvault-local-2026"}' | jq -r .token)
```

The importer sends the token using the `xc-auth` header.

## API Endpoints Used

Against NocoDB `2026.05.1`:

```text
POST /api/v2/meta/bases
POST /api/v2/meta/bases/{base_id}/tables
POST /api/v2/tables/{table_id}/records
```

Useful inspection endpoints:

```text
GET /api/v1/health
GET /api/v1/version
GET /api/v1/db/meta/nocodb/info
```

## Import Command

```bash
airvault import nocodb --path "$AIRVAULT_BACKUP_RUN" --dry-run --format json

airvault import nocodb \
  --path "$AIRVAULT_BACKUP_RUN" \
  --url http://192.168.1.5:8080 \
  --token "$NOCODB_TOKEN" \
  --format json
```

## Current Mapping

- One Airtable base becomes one NocoDB base named `Airvault - <Airtable base name>`.
- One Airtable table becomes one NocoDB table.
- `Airtable Record ID` is added to every imported table.
- Scalar values are imported as native JSON scalar values.
- Complex Airtable values, including linked records, multi-selects, collaborators, and attachments, are stored as JSON text for portability.
- Formula outputs are not recalculated in NocoDB. Formula definitions remain preserved in the original `airvault` archive schema.

## Real Constraints Found

NocoDB rejects some base-name characters that Airtable allows. The importer sanitizes base names to characters accepted by NocoDB.

NocoDB creates system/storage columns and normalizes field names internally. The importer avoids conflicts with reserved or normalized names like:

```text
ID
CreatedAt
UpdatedAt
Created At
Updated At
Created By
Updated By
nc_order
```

Duplicate or normalized-duplicate field names are suffixed with a number.

Failed live attempts can leave partially-created NocoDB bases. Clean those up in the NocoDB UI before a visual comparison if needed.

## Verification

```bash
go test ./...

airvault test fixture --out /tmp/airvault-nocodb-fixture --overwrite --format json
airvault import nocodb \
  --path /tmp/airvault-nocodb-fixture \
  --url http://192.168.1.5:8080 \
  --token "$NOCODB_TOKEN" \
  --format json
```

Expected fixture counts:

```json
{"bases":1,"tables":1,"records":2}
```

Expected full archive counts:

```json
{"bases":46,"tables":149,"records":10387}
```

Current successful full import report:

```text
/Volumes/SHAMS M1/wavedepth Dropbox/Ashraf Ali/Mac (2)/Documents/Projects/personal-stuff/ashraf-airtable-backup/reports/nocodb-import-2026-05-24_212313.json
```
