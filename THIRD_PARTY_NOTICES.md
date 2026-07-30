# Third-party notices

SharePipe OSS depends on third-party software distributed under compatible open-source licenses. The authoritative dependency versions are recorded in `go.mod`, `go.sum`, `web/package.json`, and `web/package-lock.json`.

Principal runtime dependencies include:

- AWS SDK for Go v2 and Smithy Go — Apache-2.0
- Go `x/crypto` and `x/term` — BSD-3-Clause
- modernc.org SQLite and supporting modules — BSD-3-Clause
- React and React DOM — MIT
- Lucide React — ISC
- qrcode — MIT

Development and build dependencies additionally include software under MIT, ISC, BSD, Apache-2.0, MPL-2.0, CC0-1.0, 0BSD, and BlueOak-1.0.0 licenses. Release artifacts include an automatically generated SBOM; recipients should use it together with the lock files when auditing a specific build.

Copyright and license notices in upstream source distributions remain the property of their respective authors.
