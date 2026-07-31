# Windows 部署

## 前提

- Windows 10/11 或受支持的 Windows Server amd64。
- Go 1.26 与 Node.js 24/npm；若只使用 Release ZIP，则不需要开发工具链。
- NTFS 或其他支持可靠锁、原子重命名和 SQLite WAL 的本地文件系统。

## 使用 Release ZIP

下载并校验 Windows amd64 ZIP，解压后执行：

```powershell
Copy-Item .\config\sharepipe.example.json .\config\sharepipe.json
.\scripts\initialize-secrets.ps1
.\Start-SharePipe.cmd
```

编辑配置时至少确认监听地址、公开基础 URL、数据库路径、对象目录和备份目录。正式实例不要把数据库放在 OneDrive、FAT/exFAT 或不可靠的网络共享上。

## 从源码构建

```powershell
.\scripts\build.ps1 -Version 0.1.0
Copy-Item .\config\sharepipe.example.json .\config\sharepipe.json
```

## 初始化和启动

```powershell
.\scripts\initialize-secrets.ps1
.\scripts\start.ps1
```

密钥使用当前 Windows 用户的 DPAPI 加密，存放于被 Git 忽略的 `workdir/secrets/`。启动脚本只把解密值注入当前子进程，不修改系统环境变量。

默认监听 `127.0.0.1:8080`。本机访问 `/admin` 使用 loopback 管理入口。公网访问应由 HTTPS 反向代理负责，并按照威胁模型限制管理端访问；不要直接把 loopback 信任扩展到公网。

## 轮换

```powershell
.\scripts\rotate-secrets.ps1 -Mode session
.\scripts\rotate-secrets.ps1 -Mode admin
```

轮换 session 会使现有签名会话失效。管理员恢复密码只用于首次数据库初始化或本机恢复；活动管理员密码应从本机后台维护。

本仓库不提供 Windows Service 安装器。若使用任务计划程序或第三方服务管理器，必须以拥有数据目录、备份目录和 DPAPI 密钥的同一 Windows 账户运行。
