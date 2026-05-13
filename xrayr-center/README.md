# XrayR Center（MVP）

## 快速启动（Docker）

```bash
cd xrayr-center
# 可选：为安装脚本提供 Agent 二进制下载地址与校验和
export CENTER_AGENT_DOWNLOAD_URL=https://your-cdn/xrayr-agent
export CENTER_AGENT_SHA256=<sha256sum 十六进制>
docker compose up -d --build
```

浏览器访问：`http://localhost:8080`  
默认账号：`admin` / `admin123`（**请立即修改**）。

## 环境变量

| 变量 | 说明 |
|------|------|
| `DATABASE_URL` | PostgreSQL 连接串 |
| `CENTER_LISTEN` | 监听地址，默认 `:8080` |
| `CENTER_JWT_SECRET` | JWT 密钥 |
| `CENTER_PUBLIC_BASE_URL` | 对外 Base URL（安装脚本内 `CENTER_URL`） |
| `CENTER_AGENT_DOWNLOAD_URL` | Agent 二进制直链 |
| `CENTER_AGENT_SHA256` | Agent 二进制 sha256（小写十六进制） |

## 本地构建

```bash
go build -o center ./cmd/center
```

## 与 XrayR 主仓库关系

本目录为 **独立 Go module**，不修改上级 `XrayR` 代理源码。
