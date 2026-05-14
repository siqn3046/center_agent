# XrayR Center / Agent — Phase5 安装入口与 Docker Compose 结果

本阶段在 **不推翻 Phase1～Phase4** 的前提下，补齐 **Center 一键 Compose 部署** 与 **Agent Komari 风格一键安装命令** 闭环。

## 1. 交付清单

| 项 | 说明 |
|----|------|
| `xrayr-center/docker-compose.yml` | `postgres` + `xrayr-center`、数据卷、制品卷、健康检查、`.env` 变量注入 |
| `xrayr-center/.env.example` | 环境变量示例（勿提交真实密钥） |
| `xrayr-center/Dockerfile` | 已有，沿用多阶段构建 |
| `GET /install-agent.sh` | 公开脚本，`text/x-shellscript; charset=utf-8`，`Cache-Control: no-store`；内容由模板 + Center 解析出的 Agent 下载 URL / sha256 注入（**不含** DB 密码、JWT secret） |
| `POST /api/admin/nodes/{id}/issue-install-token` | 作废未使用旧令牌并签发新令牌，返回 `install_command_wget` / `install_command_curl`（token 仅在命令字符串中） |
| `POST /api/admin/nodes`（创建节点） | 响应含 `install_command_wget` / `install_command_curl`（不再单独返回 `register_token` 字段） |
| `GET /api/admin/nodes/{id}` | 含占位符的一键命令 + `install_command_hint` |
| 管理 UI | 创建节点后弹窗展示 wget/curl + 复制；节点「概览」一键安装区 +「生成新的安装令牌」 |
| 文档 | 本文件 + 根目录 / `xrayr-center` / `xrayr-agent` README 更新 |

## 2. Agent 下载模式（与 Phase4 一致）

- **模式 A（外置直链）**：同时配置 `CENTER_AGENT_DOWNLOAD_URL` 与 `CENTER_AGENT_SHA256`（64 位小写 hex）。`install-agent.sh` 内嵌该 URL 与校验和；节点执行脚本时 **必须** `sha256sum -c`。
- **模式 B（Center 制品目录）**：在 `CENTER_ARTIFACT_DIR` 下放置 `版本/xrayr-agent` 及 `sha256` 文件，并设置 `CENTER_AGENT_VERSION`；**必须**设置可被节点访问的 `CENTER_PUBLIC_BASE_URL`，以便拼出 `/api/public/artifacts/xrayr-agent/...` 下载地址。  
  **后续扩展**（未在本轮实现）：`GET /api/public/agent/download?os=linux&arch=amd64` 等统一入口可在后续迭代。

若两者均未正确配置，`ResolveAgentDownload` 失败：已下发的脚本在运行时会提示配置缺失（或安装阶段下载失败）。

## 3. 一键命令格式

```bash
wget -qO- <CENTER_PUBLIC_BASE_URL>/install-agent.sh | sudo bash -s -- -e <CENTER_PUBLIC_BASE_URL> -t <token>
curl -fsSL <CENTER_PUBLIC_BASE_URL>/install-agent.sh | sudo bash -s -- -e <CENTER_PUBLIC_BASE_URL> -t <token>
```

`CENTER_PUBLIC_BASE_URL` 未配置时，生成命令会尽量使用 **当前请求的 Host + Scheme**（及 `X-Forwarded-Proto`），最后回退 `http://127.0.0.1:8080`（仅开发）。

## 4. Docker Compose 启动

```bash
cd xrayr-center
cp .env.example .env
# 编辑 .env：生产务必修改密码、JWT、PUBLIC_BASE_URL，并填写 Agent 直链 + sha256（或配置制品目录）
docker compose up -d --build
```

浏览器访问 `CENTER_PUBLIC_BASE_URL`（默认 `http://localhost:8080`）。默认管理员见 `xrayr-center/README.md`。

## 5. 验证 Agent 是否上线

- Center 管理端节点列表 / 详情「最近心跳」是否有更新。
- 目标机：`systemctl status xrayr-agent`、`journalctl -u xrayr-agent -e`。

## 6. 常见错误

| 现象 | 可能原因 |
|------|----------|
| 脚本立即提示未注入 URL/sha256 | Center 未配置外置直链 + sha256，且未按制品目录规则放置 Agent |
| sha256 校验失败 | `CENTER_AGENT_SHA256` 与文件不一致或下载被 CDN 替换 |
| register 失败 | token 过期、已使用、已作废，或节点时钟偏差过大 |
| systemd 启动失败 | 权限、SELinux、或 `agent.yml` 路径与 unit 不一致 |
| 安装命令里 Center 地址错误 | 未设置 `CENTER_PUBLIC_BASE_URL` 且通过错误 Host 访问管理端生成命令 |

## 7. 构建验证（本仓库）

```powershell
Set-Location E:\xrayr-project\xrayr-center
go test ./...
go build ./cmd/center

Set-Location E:\xrayr-project\xrayr-agent
go test ./...
go build ./cmd/agent
```

提交建议（仅相关路径）：

```text
git add README.md xrayr-center xrayr-agent XRAYR_CENTER_AGENT_PHASE5_INSTALL_ENTRY_RESULT.md
git commit -m "feat: add center compose deployment and agent install entry"
```

**说明**：根目录 `go test ./...` 可能因主工程其它包依赖外网而失败；以 **xrayr-center**、**xrayr-agent** 子模块为准。
