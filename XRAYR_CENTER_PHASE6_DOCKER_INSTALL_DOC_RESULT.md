# XrayR Center — Phase6 Docker Compose 完整安装说明（结论文档）

## 一、本轮目标

将根目录 **`README.md`** 中原先过于简略的 Center 启动命令，扩展为 **新服务器从 0 到可访问后台、再到 Agent 一键接入** 的完整说明；在 **`xrayr-center/README.md`** 中提供与运维对齐的分章节部署文档；完善 **`.env.example`** 注释与字段说明，并确认 **`docker-compose.yml`** 满足 Compose 部署要求。

## 二、修改文件清单

| 文件 | 说明 |
|------|------|
| `README.md` | 新增「### Center 与 Agent 集中管控部署」完整八步（准备服务器、Docker、克隆、`.env`、启动、日志、访问、创建节点与 Agent 命令） |
| `xrayr-center/README.md` | 重写为「xrayr-center 部署说明」：组件、推荐方式、服务器准备、完整安装步骤、`.env` 表格、Agent 一键命令、制品目录、常见问题、本地构建 |
| `xrayr-center/.env.example` | 补充中文注释、端口、数据库、JWT、Agent 直链/sha256、制品模式 B 可选变量 |
| `xrayr-center/docker-compose.yml` | **本轮未改结构**（已含 `postgres`、`xrayr-center`、`CENTER_ARTIFACT_DIR=/data/artifacts`、`center_artifacts` / `postgres_data` 卷及 Agent 相关环境变量注入） |
| `XRAYR_CENTER_PHASE6_DOCKER_INSTALL_DOC_RESULT.md` | 本结论文档 |

## 三、根 README 修改说明

- 用分步骤小节替换原先「仅 `cd` + `export` + `docker compose`」三行式说明。  
- 明确 **Ubuntu/Debian**、**Docker / Compose 插件**、目录 **`/opt/xrayr-center`**、克隆地址 **`siqn3046/center_agent`**、进入 **`xrayr-center`** 子目录后 **`cp .env.example .env`**。  
- 强调 **`CENTER_PUBLIC_BASE_URL`**、**`CENTER_AGENT_DOWNLOAD_URL`**、**`CENTER_AGENT_SHA256`** 与 **`CENTER_ARTIFACT_DIR`** 的职责差异。  
- 链接至本结论文档与 **`xrayr-center/README.md`**。

## 四、xrayr-center/README.md 修改说明

- 按 Phase6 要求拆为十章：组件说明、推荐部署、服务器准备、完整安装步骤（1～7）、`.env` 表格、Agent 一键安装、制品与 XrayR 升级、常见问题（6 条）、本地构建、与主仓库关系说明。  
- 与根 README 互补：根文档偏「一页式上线路径」，本文档偏 **运维手册**。

## 五、.env.example 修改说明

- 增加分段注释：端口、`CENTER_PUBLIC_BASE_URL`、PostgreSQL、JWT、Agent 模式 A/B。  
- 保留 **`POSTGRES_DB` / `POSTGRES_USER`** 与 Compose 默认值一致，避免复制后数据库名不一致。  
- 明确 **模式 A**（直链 + sha256）与 **模式 B**（制品目录 + `CENTER_AGENT_VERSION`）的适用场景说明。

## 六、Docker Compose 配置确认

当前 `xrayr-center/docker-compose.yml` 已具备：

- 服务 **`postgres`**（`postgres:16-alpine`、健康检查、`postgres_data` 卷）。  
- 服务 **`xrayr-center`**（`build` 本目录、`depends_on` 健康 Postgres、端口 **`${CENTER_PORT:-8080}:8080`**）。  
- 环境变量：**`DATABASE_URL`**、**`CENTER_LISTEN`**、**`CENTER_PUBLIC_BASE_URL`**、**`CENTER_JWT_SECRET`**、**`CENTER_ARTIFACT_DIR=/data/artifacts`**、**`CENTER_AGENT_*`**。  
- 卷：**`center_artifacts`** 挂载到容器 **`/data/artifacts`**。

## 七、完整安装流程（摘要）

1. 准备 Linux 服务器并放行端口。  
2. 安装 Docker 与 `docker-compose-plugin`。  
3. `git clone` 本仓库到如 `/opt/xrayr-center`，进入 **`xrayr-center`**。  
4. **`cp .env.example .env`**，填写 **`CENTER_PUBLIC_BASE_URL`**、数据库密码、JWT、Agent 直链与 sha256。  
5. **`docker compose up -d --build`**。  
6. **`docker compose ps`** / **`logs -f xrayr-center`** 排错。  
7. 浏览器访问 **`CENTER_PUBLIC_BASE_URL`**，登录后改密。  
8. 创建节点，在目标机执行页面生成的 **wget/curl** 一键命令。

## 八、Agent 一键安装说明

- 脚本地址：**`GET /install-agent.sh`**（与 **`CENTER_PUBLIC_BASE_URL`** 拼接）。  
- 创建节点后，管理端返回 **`install_command_wget` / `install_command_curl`**（令牌仅在命令内）。  
- **`-e`**：Center 根 URL；**`-t`**：一次性注册令牌。  
- 若 `.env` 未配置 Agent 直链与 sha256，脚本内注入为空，节点执行时会失败——见下文「常见问题」第三节。

## 九、常见问题说明

与 **`xrayr-center/README.md` 第八章** 一致，主要包括：

1. **`docker compose` 不存在** → 安装 **`docker-compose-plugin`**。  
2. **Center 页面打不开** → `ps` / `logs` / `ss -lntp` / 安全组。  
3. **脚本提示未配置下载地址** → 检查 **`.env`** 中 **`CENTER_AGENT_DOWNLOAD_URL`** 与 **`CENTER_AGENT_SHA256`** 并重启容器。  
4. **sha256 校验失败** → 本地 **`sha256sum`** 重算并回填。  
5. **Agent 注册失败** → **`CENTER_PUBLIC_BASE_URL` 可达性**、令牌是否有效、Center / `journalctl` 日志。  
6. **改 `.env` 不生效** → **`docker compose up -d --build`** 或 **`restart xrayr-center`**。

## 十、测试结果（本机执行）

在 **Windows** 开发环境下执行（路径按本机调整）：

```text
cd E:\xrayr-project\xrayr-center
go test ./...     → 通过（子包多为无测试文件）
go build ./cmd/center → 通过

cd E:\xrayr-project\xrayr-agent
go test ./...     → 通过
go build ./cmd/agent → 通过
```

```text
git ls-files | findstr /i "\.exe"  → 无跟踪的 .exe
```

## 十一、Git 状态说明

提交时请 **仅** `git add` 本阶段相关文件（**不要使用 `git add .`**），例如：

```bash
git add README.md xrayr-center/README.md xrayr-center/.env.example xrayr-center/docker-compose.yml XRAYR_CENTER_PHASE6_DOCKER_INSTALL_DOC_RESULT.md
git commit -m "docs: add complete center docker compose install guide"
```

若 **`docker-compose.yml`** 本轮无改动，可从 `git add` 列表中省略。

## 十二、未完成项

- **HTTPS / 反向代理**：文档仅提示可使用 `https://` 域名，具体 Nginx / Caddy / Traefik 配置由运维自行落地。  
- **生产备份与监控**：PostgreSQL 与卷的备份策略、日志外送未纳入本轮。  
- **根仓库其它未跟踪设计文档**：请勿随本阶段误提交。

---

**结论**：Phase6 已将 Center 的 Docker Compose 部署与 Agent 一键接入流程写进根 **`README.md`** 与 **`xrayr-center/README.md`**，并完善 **`.env.example`**；Compose 文件本身已满足部署要求，结论文档如上。
