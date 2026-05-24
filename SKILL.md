---
name: airvault
description: Back up Airtable bases into local verifiable archives with schema, records, attachments, checksums, and gap reports.
---

# airvault

Use `airvault` when you need to preserve Airtable data before a plan downgrade, account migration, or emergency archival window.

## Workflow

1. Set `AIRTABLE_TOKEN` in the environment.
2. Run `airvault auth doctor --format json`.
3. Run `airvault estimate --format json` to get record and attachment totals.
4. Run `airvault backup create --out ./airtable-backup --format json`.
5. Run `airvault verify --path ./airtable-backup --format json`.

## Notes

- Attachment URLs expire, so real backup runs download files immediately.
- Linked records are preserved by Airtable record IDs.
- Formula field definitions are preserved in schema metadata when Airtable exposes them, but formulas are not translated to other tools.
- Interfaces, automations, extensions, and permissions are reported in `gap-report.json` as unsupported surfaces.
