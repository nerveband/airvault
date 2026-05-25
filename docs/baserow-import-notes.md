# Baserow Import Notes

This file records the real Baserow behavior learned while importing an `airvault` archive into a local Docker Baserow instance. Keep it updated before changing `internal/importer/baserow`.

## Local Instance

```text
Container: airvault-baserow
Image: baserow/baserow:latest
Primary LAN URL: http://192.168.1.5:8081
Public URL env: BASEROW_PUBLIC_URL=http://192.168.1.5:8081
Login email: airvault@example.com
Login password: airvault-local-2026
Workspace ID: 157
```

Use `http://192.168.1.5:8081` from browsers. `localhost` or alternate LAN IPs can hit Baserow's builder-domain fallback and return 404 because `BASEROW_PUBLIC_URL` is set to the primary LAN URL.

## Auth

```bash
BASEROW_TOKEN=$(curl -fsS -X POST http://192.168.1.5:8081/api/user/token-auth/ \
  -H 'Content-Type: application/json' \
  -d '{"username":"airvault@example.com","password":"airvault-local-2026"}' | jq -r .token)
```

The importer sends `Authorization: JWT <token>`.

## API Endpoints Used

```text
POST /api/applications/workspace/{workspace_id}/
POST /api/database/tables/database/{database_id}/
```

The table creation endpoint accepts a matrix-shaped `data` payload and can create a table with rows in one call.

## Import Command

```bash
airvault import baserow --path "$AIRVAULT_BACKUP_RUN" --dry-run --format json

airvault import baserow \
  --path "$AIRVAULT_BACKUP_RUN" \
  --url http://192.168.1.5:8081 \
  --token "$BASEROW_TOKEN" \
  --workspace 157 \
  --format json
```

## Current Mapping

- One Airtable base becomes one Baserow database application named `Airvault - <Airtable base name>`.
- One Airtable table becomes one Baserow table.
- `Airtable Record ID` is added as the first column of every imported table.
- Scalar values are imported as scalar cell values.
- Complex Airtable values, including linked records, multi-selects, collaborators, and attachments, are stored as JSON text for portability.
- Formula outputs are imported only as archived cell values if Airtable exposed them in records. Formula definitions remain preserved in the original `airvault` archive schema.

## Real Constraints Found

Baserow must be accessed through the configured public URL host for this local setup:

```text
http://192.168.1.5:8081
```

The archive import completed successfully against the real Baserow container with:

```json
{"bases":46,"tables":149,"records":10387}
```

This is good for visual comparison, but it is not a perfect Airtable clone because Airtable views, formulas, interfaces, automations, comments, permissions, and attachment binary fields are not one-to-one portable.

## Verification

```bash
go test ./...

airvault test fixture --out /tmp/airvault-baserow-fixture --overwrite --format json
airvault import baserow \
  --path /tmp/airvault-baserow-fixture \
  --url http://192.168.1.5:8081 \
  --token "$BASEROW_TOKEN" \
  --workspace 157 \
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
/Volumes/SHAMS M1/wavedepth Dropbox/Ashraf Ali/Mac (2)/Documents/Projects/personal-stuff/ashraf-airtable-backup/reports/baserow-import-2026-05-24_211645.json
```
