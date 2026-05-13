# XrayR Center + xrayr-agent 第一阶段 MVP 实现结果

> 项目路径：`e:\xrayr-project`  
> 设计依据：`XRAYR_CENTER_AGENT_PHASE1_IMPLEMENTATION_DESIGN.md`  
> 说明：本实现为 **MVP**，独立模块 **`xrayr-center`** 与 **`xrayr-agent`** 各自含 `go.mod`，**未修改**仓库根目录 `go.mod` / `go.sum` 及 XrayR 主程序源码。

---

## 1. 实现范围

### 已实现（Center）

- Docker 编排（PostgreSQL + Center API/Web）
- 管理员登录（JWT）、默认种子用户 `admin` / `admin123`
- 节点分组 CRUD（列表 + 创建）
- 节点创建、`node_install_token`（`token_hash`、过期、`used_at`）
- 一次性安装脚本接口（校验 token 与节点绑定、未使用、未过期）
- Agent 注册、心跳、监控上报、xrayr 状态、`discovery_report` 写入与节点字段更新
- `config_template` / `config_version`（用于 **IMPORTED** 导入配置）
- `operation_log` 审计（创建节点、开放可写）
- WebSocket 占位路由（仅握手与 hello，无业务命令下发）
- 极简 Web：登录、节点列表、创建节点、详情、`discovery_report`、安装脚本下载

### 已实现（Agent）

- 读取 `/etc/xrayr-agent/agent.yml`（可通过 `-config` 指定）
- 首次 `register_token` 注册、回写 `node_id` / `node_secret`、清除 `register_token`
- HMAC-SHA256 与 `X-Node-Id` / `X-Timestamp` / `X-Nonce` / `X-Body-Hash` / `X-Signature`
- 心跳、基础监控（CPU/内存/磁盘/负载/网卡字节）、首次 discovery 上报
- XrayR 安装状态探测（systemd、二进制候选、配置候选、pgrep、错误日志 tail）
- **INSTALLED_RUNNING**：只读备份 `imported-{unix}-{hash12}.yml` 到 `config_backup_dir`，**不** stop/restart/覆盖在线配置
- **未实现破坏性**：`INSTALL_XRAYR` / `UPGRADE_XRAYR` 未写执行器；Agent 无相关动作

### 未完整实现 / 占位

- `INSTALL_XRAYR` / `UPGRADE_XRAYR` 状态机（仅设计与数据库预留，无二进制下发逻辑）
- WS 命令队列、 `APPLY_CONFIG` / `RESTART` / `CLEAN_*` 执行器（Center 侧未接业务）
- `config_deploy_task` / `command_task` 完整 API（表已建，HTTP 未全量暴露）
- Center 对下载类任务的制品托管（需自行提供 `CENTER_AGENT_DOWNLOAD_URL` + `CENTER_AGENT_SHA256`）

---

## 2. 新增目录与文件

### Center（`e:\xrayr-project\xrayr-center\`）

| 路径 | 说明 |
|------|------|
| `go.mod` / `go.sum` | 独立模块依赖 |
| `Dockerfile` | 多阶段构建 Center 二进制 |
| `docker-compose.yml` | Postgres + Center |
| `cmd/center/main.go` | 入口：迁移、种子管理员、HTTP 服务 |
| `internal/config/config.go` | 环境变量配置 |
| `internal/db/db.go` | 连接池 + 嵌入执行 `migrations/001_init.sql` |
| `internal/db/migrations/001_init.sql` | PostgreSQL 全量建表 |
| `internal/hsign/hsign.go` | HMAC 签名与 body hash |
| `internal/noncecache/noncecache.go` | Agent 请求 nonce 短期去重 |
| `internal/authadmin/jwt.go` | 管理员 JWT |
| `internal/httpapi/server.go` | Chi 路由、管理员鉴权、Agent HMAC 中间件 |
| `internal/httpapi/handlers_rest.go` | 其余 Handler、静态页嵌入、WS 占位 |
| `internal/httpapi/static/index.html` | Web 壳 |
| `internal/httpapi/static/app.js` | 极简前端逻辑 |

### Agent（`e:\xrayr-project\xrayr-agent\`）

| 路径 | 说明 |
|------|------|
| `go.mod` / `go.sum` | 独立模块 |
| `cmd/agent/main.go` | 注册、discovery、定时心跳与上报 |
| `internal/agentcfg/config.go` | `agent.yml` 结构体与加载/保存 |
| `internal/hsign/hsign.go` | 与 Center 一致的签名算法 |
| `internal/capi/client.go` | HTTP 客户端 + 注册 + SignedJSON |
| `internal/discovery/discover.go` | 安装状态机与只读备份 |
| `internal/monitor/collect.go` | gopsutil 采集 |

---

## 3. Center 后端实现

- **框架**：`chi` + `pgx/v5`  
- **迁移**：启动时执行嵌入的 `001_init.sql`（`IF NOT EXISTS`）  
- **种子**：若无 `admin_user`，插入 `admin` / `admin123`（bcrypt）  
- **Agent 鉴权**：除 `POST /api/agent/register` 外，`/api/agent/*` 均需 HMAC 头；nonce 在 **签名校验通过后** 记入短期缓存防重放  
- **导入**：`POST /api/agent/discovery/report` 在 `INSTALLED_RUNNING` 且带 `config_yaml`+`config_hash` 时插入 `config_version`（`source_type=IMPORTED`），节点 `manage_status=MANAGED_READONLY`  
- **开放可写**：`POST /api/admin/nodes/{id}/enable-writable` 将 `MANAGED_WRITABLE` 及 `allow_*` 置位（便于联调；生产应加 RBAC 与审批）

---

## 4. Center Web 实现

- 单页：`GET /` → `index.html`，`GET /static/app.js`  
- 流程：登录 → 列表 → 创建节点（展示一次性 `register_token`）→ 详情 + `discovery_report` → 安装脚本（需粘贴 `register_token` 作为 query）

---

## 5. Agent 实现

- 配置：`center` / `register_token` / `agent` / `xrayr`（含 `binary_search_paths`、`config_search_paths`）  
- 首次若存在 `register_token` 且无 `node_secret`：调用注册接口并 **回写** 配置文件  
- 启动后执行一次 `discovery.Run`，上报 `discovery/report`  
- 周期：`heartbeat`、`monitor/report`、`xrayr/status`（间隔来自 yaml，默认 30s / 60s）  
- **不**执行 `INSTALL`/`UPGRADE`/shell；路径与 `service_name` **仅**来自本地 yaml  

---

## 6. 安装脚本实现

- **接口**：`GET /api/admin/nodes/{id}/install-script.sh?register_token=...`  
  - **鉴权**：`Authorization: Bearer <JWT>`  
  - **校验**：`sha256(register_token)` 必须匹配该 `node_id` 的 `node_install_token` 且 `used_at IS NULL`、`expires_at` 未过期  
- **脚本行为**：root + systemd 检查；创建 `xrayr-agent` 用户与目录；`curl` 下载 Agent 二进制并 **`sha256sum -c`**；写入 `agent.yml`（含 `register_token`）；写 `xrayr-agent.service`；写 `sudoers.d`（仅 `restart`/`status`/`is-active` **xrayr**）；`daemon-reload` / `enable` / `start` **xrayr-agent**  
- **禁止项**：不碰 XrayR 二进制、不覆盖 `/etc/XrayR/config.yml`、不 `restart xrayr`、不执行 Center 下发的任意 shell（脚本为 Center **模板生成**，固定内容）

> 若环境变量 **`CENTER_AGENT_DOWNLOAD_URL` / `CENTER_AGENT_SHA256`** 为空，脚本会在 VPS 上主动失败，避免无校验下载。

---

## 7. 数据库迁移

- 文件：`e:\xrayr-project\xrayr-center\internal\db\migrations\001_init.sql`  
- 表：`admin_user`、`node_group`、`node`、`node_install_token`、`node_heartbeat`、`node_monitor_snapshot`、`node_network_snapshot`、`xrayr_status_snapshot`、`discovery_report`、`config_template`、`config_version`、`config_deploy_task`、`command_task`、`cleanup_task`、`operation_log`、`alert_event`  
- `node` 含：`install_state`、`manage_status`、`import_mode`、`discovered_*`、`imported_*`、`allow_*`、`node_hmac_key` 等字段，与需求对齐  

---

## 8. 安全机制

| 项 | 实现位置 |
|----|----------|
| HMAC-SHA256 | `xrayr-center/internal/hsign`、`xrayr-agent/internal/hsign` |
| `timestamp` 窗口 | Center `noncecache` 默认 skew **90s** |
| `nonce` 防重放 | 校验通过后 `Seen` |
| `body_hash` | 与 body 一致才通过 |
| `node_secret` | 注册时生成随机 hex，存 `node` 表 `node_hmac_key`（MVP：等同共享密钥；生产建议加密存储 + 轮换策略） |
| `node_install_token` | `sha256` 存库；注册成功写 `used_at`；安装脚本校验未使用且未过期 |
| Center 不下发任意路径 / systemd 名 | Agent 仅信本地 yaml；Center 脚本模板固定 |
| `allow_commands` | Agent yaml 中列出；**执行器未接 WS 命令**，后续可强制校验 |

---

## 9. 自动发现状态机

- **实现位置**：`xrayr-agent/internal/discovery/discover.go` 的 `classify` + `Run` 结果映射；Agent **进程内**未显式枚举字符串状态常量，但上报 `install_state` 与文档枚举一致。  
- **Center 节点 `manage_status`**：由 `discovery/report` 更新（`INSTALLED_RUNNING` → `MANAGED_READONLY`；`INSTALLED_BROKEN`/`SERVICE_ONLY` → `REPAIR_REQUIRED` 等）。

---

## 10. XrayR 安装状态判断

- **systemd**：`systemctl is-active` / `status` / `cat`（服务名来自 yaml `service_name`，默认 `xrayr`）  
- **二进制候选**：`/usr/local/bin/XrayR`、`/usr/bin/XrayR`、`/opt/XrayR/XrayR`（可被 yaml 覆盖列表）  
- **配置候选**：`/etc/XrayR/config.yml`、`/etc/XrayR/config.yaml`  
- **日志 tail**：`error_log_path`  
- **进程**：`pgrep -x XrayR`  
- **枚举**：`NOT_INSTALLED`、`INSTALLED_RUNNING`、`INSTALLED_STOPPED`、`INSTALLED_BROKEN`、`CONFIG_ONLY`、`BINARY_ONLY`、`SERVICE_ONLY`、`UNKNOWN`（启发式，极端环境可能落在 `UNKNOWN`）

---

## 11. 现有节点导入流程

1. Agent `INSTALLED_RUNNING`：读取配置全文 → `sha256` → 复制备份到 `config_backup_dir/imported-{unix}-{hash12}.yml`  
2. 上报 `discovery/report`：`install_state`、`config_yaml`、`config_hash`、`imported_backup_path`、`error_tail` 等  
3. Center：写 `discovery_report`；插入 `config_version`（`IMPORTED`）；节点 `MANAGED_READONLY`  
4. **全程**不 `restart` XrayR、不覆盖原 `config.yml`

---

## 12. 未安装节点处理

- 探测为 `NOT_INSTALLED`：`manage_status` 维持/标记为 `DISCOVERED`（见 `discovery/report` 分支），**不**自动安装二进制（`INSTALL_XRAYR` 未实现）  
- 若需自动安装：后续实现命令与 checksum 流程，并扩展 **sudoers**（`daemon-reload` 等）与制品 URL 策略

---

## 13. 未实现但已预留能力

- `INSTALL_XRAYR` / `UPGRADE_XRAYR` / `ROLLBACK_XRAYR` / `REPAIR_XRAYR_SERVICE` 完整状态机与二进制制品链  
- `APPLY_CONFIG` 下载、健康检查、回滚（表 `config_deploy_task` 已建）  
- WS 业务命令、`command_task` 消费  
- Center 对 Agent 二进制托管与版本发布（当前依赖环境变量 URL + SHA256）

---

## 14. 测试与验收

| # | 验收项 | 状态 |
|---|--------|------|
| 1 | `docker compose up` 启动 Center + Postgres | 需本机 Docker；`docker-compose.yml` 已提供 |
| 2 | Center 创建节点 | Web `POST /api/admin/nodes` 已实现 |
| 3 | 生成一次性脚本 | `GET /api/admin/nodes/{id}/install-script.sh?register_token=...` |
| 4 | token 过期不可用 | `expires_at` 校验 |
| 5 | token 使用后不可复用 | 注册写 `used_at`；脚本亦校验 `used_at` |
| 6 | VPS 执行脚本安装 Agent | 需配置 `CENTER_AGENT_*` |
| 7–9 | 脚本不 stop/restart XrayR、不覆盖配置 | 脚本内容满足 |
| 10 | Agent 注册 | `POST /api/agent/register` |
| 11–17 | 各 `install_state` | 分类逻辑已实现（依赖真实 Linux systemd） |
| 18 | `INSTALLED_RUNNING` 不中断 | 无 restart/stop |
| 19 | 备份现有配置 | 仅导入副本文件 |
| 20–21 | `discovery_report` 上报与展示 | API + Web 列表 |
| 22 | 默认 `MANAGED_READONLY` | Center 在 `INSTALLED_RUNNING` 分支设置 |
| 23 | 只读态禁止写命令 | **执行器未接**；需后续在 WS/executor 层硬拒绝 |
| 24 | 未实现 INSTALL/UPGRADE 不产生破坏 | 无对应代码路径 |
| 25 | `allow_commands` | yaml 已列出；命令执行未接 |
| 26 | 下载校验 sha256 | 安装脚本层对 **Agent 二进制** 校验；其它下载待后续 |

---

## 15. 风险与后续任务

- **`node_hmac_key` 明文存储**：MVP 简化，生产需加密或 KMS、支持轮换  
- **安装脚本 `register_token` 出现在 URL**：可能进日志；建议改为 **POST 生成脚本** 且一次性 body 返回  
- **WS 未鉴权**：`/ws/agent` 当前占位，生产必须接入与 HTTP 一致的签名或短期 ticket  
- **discovery 分类启发式**：需在生产环境用真实节点样本调参，减少 `UNKNOWN`  
- **Agent 心跳中 `xrayr_running`**：当前沿用首次 discovery 结果，后续应轻量 `is-active` 刷新  
- **与 XrayR 主仓库关系**：Center/Agent 为 **旁路运维**；XrayR 仍按 `main.go → panel.Start` 直连 XBoard，不受影响  

---

## 附录 A：HTTP API 一览（相对 `CENTER_PUBLIC_BASE_URL`）

### 管理员

| 方法 | URL | 说明 |
|------|-----|------|
| `POST` | `/api/admin/login` | body: `username`,`password` → `token` |
| `GET` | `/api/admin/me` | JWT |
| `GET` | `/api/admin/node-groups` | 列表 |
| `POST` | `/api/admin/node-groups` | 创建分组 |
| `GET` | `/api/admin/nodes` | 节点列表 |
| `POST` | `/api/admin/nodes` | 创建节点，返回 `register_token` |
| `GET` | `/api/admin/nodes/{id}` | 节点详情 |
| `GET` | `/api/admin/nodes/{id}/install-script.sh?register_token=` | 安装脚本 |
| `GET` | `/api/admin/nodes/{id}/discovery-reports` | discovery 列表 |
| `POST` | `/api/admin/nodes/{id}/enable-writable` | 切换可写（联调） |

### Agent

| 方法 | URL | 鉴权 |
|------|-----|------|
| `POST` | `/api/agent/register` | 无 HMAC，body 含 `register_token` |
| `POST` | `/api/agent/heartbeat` | HMAC |
| `POST` | `/api/agent/monitor/report` | HMAC |
| `POST` | `/api/agent/xrayr/status` | HMAC |
| `POST` | `/api/agent/discovery/report` | HMAC |

### Web / WS

| 方法 | URL |
|------|-----|
| `GET` | `/` |
| `GET` | `/static/app.js` |
| `GET` | `/ws/agent` | 占位 WebSocket |

---

## 附录 B：本地构建命令（参考）

```text
cd e:\xrayr-project\xrayr-center
go build -o center.exe ./cmd/center
cd e:\xrayr-project\xrayr-agent
go build -o xrayr-agent.exe ./cmd/agent
```

Docker：

```text
cd e:\xrayr-project\xrayr-center
docker compose build
docker compose up -d
```

---

## 最终结论

已在 **`xrayr-center`** 与 **`xrayr-agent`** 两个独立 Go 模块中落地 **第一阶段 MVP 主干**：Docker 化 Center、PostgreSQL 全量表、管理员 JWT、节点与一次性纳管 token、安装脚本模板、Agent 注册与 HMAC 上报、**只读 discovery + 导入配置版本**、极简 Web 与 WS 占位。**未**改动 XrayR 核心仓库业务代码与根 `go.mod`。**安装/升级/配置下发/命令执行** 等强运维能力保留在表结构与文档层面，按 `XRAYR_CENTER_AGENT_PHASE1_IMPLEMENTATION_DESIGN.md` 继续迭代即可。
