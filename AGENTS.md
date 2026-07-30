# AGENTS.md

## Scope

These instructions apply to the entire SharePipe OSS repository.

## Project rules

- Keep source, dependencies, caches, temporary files, logs, and generated output inside the repository.
- Use UTF-8 without BOM for text files.
- Use project-local Go and npm caches; never install project dependencies globally.
- Put temporary files under `workdir/tmp/`, caches under `workdir/cache/`, logs under `workdir/logs/`, intermediate artifacts under `workdir/intermediate/`, and final artifacts under `outputs/`.
- Do not add Cloudflare Worker, D1, R2 Binding, personal deployment, domain, account, bucket, tunnel, or credential configuration to this repository.
- Keep the v1 UI in Simplified Chinese and centralize visible strings.
- New behavior requires tests proportional to risk.
- Do not commit, push, publish a release, or change repository settings unless explicitly requested.

## Verification

- `go test ./...`
- `go vet ./...`
- `npm --prefix web ci`
- `npm --prefix web run check`
- `powershell -ExecutionPolicy Bypass -File scripts/verify.ps1`
