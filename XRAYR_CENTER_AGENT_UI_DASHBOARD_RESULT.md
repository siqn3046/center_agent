# XrayR Center 节点面板（Komari 风格）改造 — 结论文档

## 一、本轮目标

将 xrayr-center 的极简静态 Web 升级为可参考 Komari 的 **VPS 节点监控 + XrayR 配置管理面板**：Dashboard 统计与节点卡片、节点详情多 Tab、网页编辑 YAML 并保存为 `config_version`、一键下发 `APPLY_CONFIG`、命令列表与权限规则、与 Agent WebSocket / 执行器联动。

## 二、参考 Komari 的实现点

- 渐变背景 + 半透明玻璃卡片 + 圆角与阴影。
- Dashboard 顶部统计卡片（时间、在线/总数、区域数、聚合流量与速度）。
- 节点卡片：在线状态、管理状态、安装状态配色、CPU/内存/磁盘条、上下行速度、XrayR 运行态。
- 节点详情：顶部信息与 Tab（概览 / Discovery / XrayR 配置 / 命令 / 设置）。
- 危险操作二次确认（确认框）。

## 三、修改文件清单

### xrayr-center

- `internal/httpapi/server.go`：接入 `hub`、完整路由（Dashboard、节点设置、配置版本、命令、制品、Agent WS、命令进度/结果等）。
- `internal/httpapi/handlers_rest.go`：`getNode` 扩展字段、`listDiscovery` 丰富、`installScript`（制品解析、allow_commands、备份目录、ReadWritePaths）、`style.css` embed。
- `internal/httpapi/handlers_dashboard.go`：**新增** — `GET /api/admin/dashboard/summary`、`listNodes` 增强、`monitor-snapshots`、节点配置版本 CRUD/下发、`config-deploy-tasks`、`PATCH .../settings`。
- `internal/httpapi/handlers_phase2.go`：命令队列、`applyConfigDeployFlow`、Agent `command/progress`、`command/result`、全局 `config-versions` POST 等（与 UI 共用）。
- `internal/httpapi/handlers_ws_agent.go`：**新增** — WebSocket HMAC 鉴权与 `flushPendingCommands`。
- `internal/httpapi/agent_hub.go`、`handlers_artifacts.go`（既有，路由已挂接）。
- `internal/httpapi/static/index.html`、`app.js`、`style.css`：**重写/新增** 前端。
- `internal/db/db.go`、`internal/db/migrations/003_ui_node.sql`：**新增迁移**（region、remark、agent_os/arch、virtualization、pending_deploy、config_version display_name/remark）。

### xrayr-agent

- `cmd/agent/main.go`：心跳使用 `discovery.IsServiceActive`；监控上报附带实时配置 hash；WebSocket 消费与 `commandWorker`。
- `internal/agentcfg/config.go`：`backup_dir`、`artifact_cache_dir` 等默认值。
- `internal/capi/client.go`：`SignedGET` / 统一签名请求。
- `internal/discovery/active.go`：**新增** — `systemctl is-active` 轻量检测。
- `internal/cmdexec/exec.go`：**新增** — `STATUS_XRAYR`、`RESTART_XRAYR`、`APPLY_CONFIG`（下载 YAML、校验 sha256、备份、原子替换、重启、失败回滚）、INSTALL/UPGRADE 占位失败说明。
- `internal/wsclient/client.go`：**新增** — 断线重连、解析 `command_push`。
- `go.mod`：增加 `github.com/gorilla/websocket`。

## 四、新增 API（管理端节选）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/dashboard/summary` | 总节点、在线、区域数、聚合流量与速度等 |
| GET | `/api/admin/nodes` | 支持 `?q=` 搜索；返回卡片所需监控与状态字段 |
| GET | `/api/admin/nodes/{id}/monitor-snapshots` | 最近监控快照 |
| GET | `/api/admin/nodes/{id}/config-versions` | 与本节点相关的配置版本 |
| POST | `/api/admin/nodes/{id}/config-versions` | 保存网页 YAML 为新版本（NODE_UI） |
| GET | `/api/admin/config-versions/{versionId}` | 含 `content_yaml` |
| POST | `/api/admin/nodes/{id}/config-versions/{versionId}/deploy` | 创建 deploy + `APPLY_CONFIG` |
| GET | `/api/admin/nodes/{id}/config-deploy-tasks` | 下发任务与状态 |
| PATCH | `/api/admin/nodes/{id}/settings` | 节点名称、地区、备注、权限、pending 版本等 |

（命令类 `POST .../commands/*`、Agent `command/result` 等与 Phase2 一致，已挂路由。）

## 五、前端页面结构

- 登录页 → Dashboard（10s 自动刷新）。
- 卡片 / 表格视图切换；搜索框。
- 节点详情：返回、安装脚本、Tab 切换；详情整体 10s 刷新；**命令** Tab 3s 刷新。
- Toast、Modal 确认框、Loading 样式类。

## 六、XrayR 配置编辑与下发流程

1. 节点完成 Discovery 后，Center 侧存在导入的 `config_version` 与 `imported_config_version_id`（若可解析）。
2. 在 **XrayR 配置** Tab 可「编辑」YAML，填写名称/备注后「保存为新版本」→ `POST .../config-versions`。
3. 点击「下发」→ `POST .../config-versions/{id}/deploy` → 创建 `config_deploy_task` + `APPLY_CONFIG` 命令 → WebSocket `command_push`。
4. Agent：`SignedGET` 拉取 YAML → 校验 `config_hash` → 校验 `target_config_path` 与本地 `config_path`（若 Center 下发非空则必须一致）→ 备份 → 临时文件 → `rename` → `sudo systemctl restart` → `is-active` 失败则回滚并上报 `FAILED`。

## 七、命令执行流程

- 只读节点（`MANAGED_READONLY`）：前端仅开放 Status；后端 `assertCommandAllowed` 同步限制。
- 写操作需 `MANAGED_WRITABLE` 及对应 `allow_*`。
- 状态经 `POST /api/agent/command/progress` 与 `command/result` 落库。

## 八、安全限制

- Center 不向 Agent 下发任意 shell；路径以 Agent `agent.yml` 为准，`target_config_path` 仅作校验。
- `allow_commands` 白名单；APPLY/RESTART/STATUS 均受控。
- WebSocket 使用与 HTTP 相同的 HMAC 规范（GET `/ws/agent` + 空 body 的 body_hash）。

## 九、构建与测试结果

- 命令：`go test ./...`、`go build`（xrayr-center / xrayr-agent）均已通过（无测试用例的包为 `[no test files]`）。
- Windows 下本地可生成 `center.exe` / `xrayr-agent.exe`，**勿提交**至 Git（见根目录 `.gitignore`）。

## 十、Git 提交号与推送

- **提交号**：`bbe9c66`（`feat: add komari style node dashboard and xrayr config management`）
- **推送**：见下方 `git push` 实际输出。

## 十一、已知未完成项

- Agent **INSTALL_XRAYR / UPGRADE_XRAYR** 仍为占位返回 `FAILED`（需后续白名单安装链路与制品约定）。
- Dashboard **总流量** 为各节点最近一次监控快照中的累计网卡字节估算，非精确计费级统计。
- **虚拟化** 字段已预留，Agent 未自动上报。
- 历史监控 **折线图** 未做，概览以表格/JSON 展示最近快照（预留后续时序 API）。
