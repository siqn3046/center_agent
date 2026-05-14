# XrayR Center / Agent — Phase4 部署验收与安全加固结果

本文档记录 **Phase4**（在 Phase1–3 基础上的部署验收、制品与命令链路加固、管理端补强）的实现要点与验收方式，便于运维与二次开发对照。

## 1. 范围与目标

- **Center**：`CENTER_ARTIFACT_DIR` 下制品路径解析安全（禁止逃逸）、登记/上传校验（`name`/`version`、`os`=linux、`arch`∈{amd64,arm64}、SHA256、文件大小）、仅 **启用** 制品可被 Agent 下载；管理员 **PATCH** 启停；**multipart 上传**（`sha256` 可留空，服务端落盘后计算并入库）。
- **Agent**：安装/升级路径中对 **service_name** 等本地参数做字符集与长度校验，避免命令注入面扩大（与 `systemctl` 调用配合）。
- **前端**：Dashboard **制品管理**（列表、上传、登记、启停）；节点 **命令** Tab 区分 **进度**（`progress` / `log_summary`）与 **结果**（`stage` / `message` / `error` 及其余 `result_json` 字段摘要）。
- **数据库**：迁移 `005_phase4_artifact_harden.sql` 为 `xrayr_binary_artifact` 增加 `size_bytes`（可空），与登记/上传时写入一致。

## 2. 关键文件

| 组件 | 路径 |
|------|------|
| Center 迁移 | `xrayr-center/internal/db/migrations/005_phase4_artifact_harden.sql` |
| Center DB 嵌入 | `xrayr-center/internal/db/db.go` |
| 制品 API | `xrayr-center/internal/httpapi/handlers_xrayr_artifacts.go` |
| 安装/升级命令与制品校验 | `xrayr-center/internal/httpapi/handlers_phase2.go` |
| 路由 | `xrayr-center/internal/httpapi/server.go`（注意 `POST /artifacts/upload` 在 `GET /artifacts/{id}` 之前） |
| 管理 UI | `xrayr-center/internal/httpapi/static/app.js` |
| Agent 安装升级 | `xrayr-agent/internal/cmdexec/xrayr_install_upgrade.go` |

## 3. 环境变量与目录

- **Center**：配置 **`CENTER_ARTIFACT_DIR`**（绝对路径、仅 Center 进程可写），上传写入相对路径 `版本/linux-{arch}/文件名`；登记时可填 **`storage_relpath`**（相对该目录），无需再填 `filename`。
- **Agent**：仅信任 **`agent.yml`**（及既定 capi 配置路径策略，见 Phase1–3 文档）；下载 URL 为 Center 下发的 `/api/agent/artifacts/{id}/download`，响应头含 `X-Artifact-Sha256`、`X-Artifact-Id`（不暴露易敏感业务名）。

## 4. API 摘要（管理员 JWT）

- `GET /api/admin/artifacts` — 列表（含停用项；JSON 含 `name`/`version`/`os`/`arch`/`sha256`/`size_bytes`/`enabled` 及兼容字段）。
- `POST /api/admin/artifacts` — JSON 登记（`sha256` 与磁盘一致校验）。
- `POST /api/admin/artifacts/upload` — `multipart/form-data`：`name`、`version`、`arch`（可空默认 amd64）、`os`（可空默认 linux）、`sha256`（可空则服务端计算）、`file`。
- `PATCH /api/admin/artifacts/{id}` — body：`{ "enabled": true|false }`。

## 5. 建议验收步骤（手工）

1. **启动 Center**（含 Postgres、配置 `CENTER_ARTIFACT_DIR`），确认迁移执行无报错。
2. **登记或上传** 一版 linux/amd64（或 arm64）制品，在 Dashboard 表格中可见；对停用制品执行 **启用/停用**，确认 Agent 侧仅启用项可下载。
3. **节点** 上报 `agent_os`/`agent_arch` 后，在 **命令** Tab 选择制品下发 **安装/升级**（需节点 `allow_install`/`allow_upgrade` 等开关）；观察命令表 **进度** 与 **结果** 列是否分别展示。
4. **失败回滚**：升级失败时不应破坏已有 `agent.yml` 与业务配置策略（与 Agent 实现一致；具体以当次命令 `result_json` 为准）。

## 6. 构建与测试命令（本仓库）

在 **Windows PowerShell** 下（路径按本机调整）：

```powershell
Set-Location e:\xrayr-project\xrayr-center
go test ./...
go build -o $env:TEMP\xrayr-center-test.exe ./cmd/center

Set-Location e:\xrayr-project\xrayr-agent
go test ./...
go build -o $env:TEMP\xrayr-agent-test.exe ./cmd/agent
```

说明：

- 仓库根目录 `go test ./...` 会包含 **主工程其他子模块**（如依赖外网或本地证书的测试），在未搭 mock 的环境下可能失败；**Phase4 以 `xrayr-center`、`xrayr-agent` 子模块结果为准**。
- `xrayr-center`、`xrayr-agent` 根目录无 `main`，勿对目录根执行 `go build .`，应使用 **`./cmd/center`** / **`./cmd/agent`**。

## 7. Git 与制品约束

- 验收：`git ls-files | Select-String '\.exe$'` 应 **无输出**（不把 Windows 构建产物纳入版本库）。
- 推荐仅暂存与本功能相关的树：

  ```powershell
  git add xrayr-center xrayr-agent XRAYR_CENTER_AGENT_PHASE4_DEPLOY_VERIFY_RESULT.md
  git commit -m "feat: harden xrayr artifact deployment workflow"
  ```

## 8. 已知/未完成项（若后续迭代）

- 根模块全量 `go test ./...` 的网络与证书相关用例需单独 CI 或 `-short` 策略（未纳入 Phase4 交付范围）。
- 若需 **审计日志导出** 或 **制品病毒扫描**，可在 Center 侧扩展钩子，本 Phase 未实现。

---

**结论**：Phase4 在 Center 制品链路与管理 UI、Agent 安装参数校验、DB `size_bytes` 等层面已闭环；部署时请配置 `CENTER_ARTIFACT_DIR`，并按第 5、6 节做冒烟与构建验证。
