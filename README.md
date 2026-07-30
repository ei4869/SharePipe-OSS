# SharePipe OSS

> SharePipe OSS is a self-hosted temporary file sharing service for trusted individuals and small teams. The supported v0.1 interface is Simplified Chinese. Windows amd64 is the supported deployment target; Linux source builds are experimental.

SharePipe OSS 是一个面向可信个人和小团队的临时文件分享服务。上传者使用邀请码创建分享，接收方通过高熵链接访问；支持可选密码、到期清理、下载次数限制、文件夹结构、断点续传、本地磁盘、NAS 和通用 S3 兼容存储。

本仓库只包含 Go + React + SQLite 自托管运行时，不包含 Cloudflare Worker、D1、R2 Binding、任何托管实例或个人生产环境配置。

## 安全边界

- SharePipe 不是端到端加密系统，服务器和存储管理员可以读取文件内容。
- 它适合可信小团队，不应作为匿名公共文件托管服务。
- 分享链接和管理链接都是 bearer secret，获得链接即获得相应权限。
- 默认只监听 `127.0.0.1`；公网部署应使用受控的 HTTPS 反向代理并限制管理员入口。
- 未知或主动内容类型只作为附件下载，不在浏览器中内联执行。

## Windows 快速体验

前提：Windows PowerShell、Go 1.26、Node.js 24/npm，以及 Chrome 或 Edge。

```powershell
.\scripts\start-test.ps1
```

该命令会在项目内构建一个临时实例，只监听 loopback，并将测试数据写入 `workdir/tmp/quick-start/`。

正式构建：

```powershell
.\scripts\build.ps1 -Version 0.1.0
Copy-Item .\config\sharepipe.example.json .\config\sharepipe.json
.\scripts\initialize-secrets.ps1
.\scripts\start.ps1
```

详细步骤见[Windows 部署](docs/windows.md)。存储配置见[本地、NAS 与 S3](docs/storage.md)，数据保护见[备份与恢复](docs/backup-restore.md)。

## 开发

```powershell
npm --prefix web ci
go test ./...
go vet ./...
npm --prefix web run check
.\scripts\build.ps1 -Version dev
```

完整验证：

```powershell
.\scripts\verify.ps1 -DependencyAudit
```

## 支持范围

- Windows amd64：正式支持并发布未签名 ZIP；请校验 Release 中的 SHA-256。
- Linux：核心 Go 代码持续执行编译和测试，但首版不提供正式二进制、systemd 或发行版支持。
- 存储：本地文件系统、已由操作系统挂载的 NAS，以及 AWS S3、Cloudflare R2、Backblaze B2、Wasabi、DigitalOcean Spaces、MinIO 和通用 S3 API。
- 界面：v1 仅提供简体中文，字符串集中管理以便未来国际化。

Bug 和功能建议请使用 GitHub Issues。安全问题请按照 [SECURITY.md](SECURITY.md) 使用 GitHub 私密漏洞报告，不要在公开 Issue 中粘贴密钥、分享链接或真实文件名。

## 许可证

SharePipe OSS 使用 [Apache License 2.0](LICENSE)。第三方组件说明见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
