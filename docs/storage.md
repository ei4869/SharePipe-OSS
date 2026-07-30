# 本地、NAS 与 S3

## 本地磁盘

本地对象目录应与 SQLite 数据库分别纳入备份。不要通过 Web 服务器直接暴露对象目录；所有下载必须经过 SharePipe 的授权和响应策略。

## NAS

SharePipe 不保存 NAS 用户名或密码。应先由操作系统以运行 SharePipe 的账户挂载 SMB/NFS，再把稳定的 UNC 或挂载路径配置为本地存储 profile。NAS 离线时应让健康检查失败，不要自动切换到另一个 profile。

## S3 兼容存储

支持 AWS S3、Cloudflare R2 的 S3 API、Backblaze B2、Wasabi、DigitalOcean Spaces、MinIO 和通用 S3 endpoint。使用最小权限凭据，只允许目标 bucket/prefix 的必要对象操作。

浏览器直传需要 bucket CORS 精确允许实际 HTTPS origin，并允许 `PUT`、`GET`、`HEAD`，暴露 `ETag`。不要使用 `*` 放宽生产 CORS。凭据只进入服务端加密存储，不能发送给浏览器或提交到仓库。

每个分享绑定创建时使用的存储 profile。切换默认 profile 不会迁移历史对象；仍有分享引用时不要原地更换 bucket、endpoint 或根路径。
