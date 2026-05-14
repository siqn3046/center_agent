# XrayR Center — Phase7 傻瓜式一键安装向导（结论文档）

## 一、本轮目标

在 Phase1～Phase6 已有 Docker Compose 与文档能力基础上，新增 **`xrayr-center/install-center.sh`**：用户在 **Ubuntu / Debian** 上一条 `wget` + `sudo bash` 即可完成 Docker 安装、仓库克隆、**交互或非交互生成 `.env`**（自动生成 PostgreSQL 密码与 `CENTER_JWT_SECRET`）、可选立即 **`docker compose up -d --build`**，并更新根目录与 **`xrayr-center/README.md`** 的安装入口说明。

## 二、修改文件清单

| 文件 | 说明 |
|------|------|
| `xrayr-center/install-center.sh` | 新增：一键向导脚本（UTF-8、`set -euo pipefail`、参数解析、交互/非交互、Docker 与依赖、仓库、`.env`、Compose 启动与收尾输出） |
| `README.md` | 增加「Center 一键安装（推荐）」；原长流程改为「手动方式」标题前缀 |
| `xrayr-center/README.md` | 在「推荐部署方式」下增加「一键安装向导」子章节与非交互示例、常用命令、说明与结论文档链接 |
| `XRAYR_CENTER_PHASE7_ONECLICK_INSTALLER_RESULT.md` | 本文件 |

## 三、`install-center.sh` 功能说明

- **系统**：仅 Linux；**root** 执行；**OS**：检测 `/etc/os-release`，仅 **Ubuntu / Debian** 系走自动 `apt-get` / `get.docker.com` 路径，其它发行版 **直接报错退出**，不执行危险命令。  
- **参数**：`--repo-url`、`--branch`、`--install-dir`、`--public-url`、`--port`、`--agent-url`、`--agent-sha256`、`--yes`、`--help`。  
- **安全**：不对用户输入做 `eval`；敏感值通过 **`printf` 写入 `.env`**，成功提示中 **不打印** `POSTGRES_PASSWORD` / `CENTER_JWT_SECRET`。  
- **仓库**：默认 `/opt/xrayr-center`；已存在且为 git 则 `fetch/checkout/pull`；已存在非 git 目录时交互询问是否清空后克隆（`--yes` 下非 git 则失败退出）。  
- **`.env`**：`POSTGRES_PASSWORD`（`openssl rand -base64 24` 经简单字符替换）、`CENTER_JWT_SECRET`（`openssl rand -hex 32`）；已存在 `.env` 时先 **`*.bak.时间戳`** 备份；交互覆盖需确认，`--yes` 自动备份覆盖。  
- **启动**：交互末尾询问是否立即 `docker compose up -d --build`；失败时输出 **`docker compose logs --tail=120 xrayr-center`** 片段。

## 四、交互式安装流程

1. 校验 root / Linux / Ubuntu|Debian。  
2. 依次询问：`CENTER_PUBLIC_BASE_URL`、端口（可回车默认 8080）、Agent 下载 URL、Agent sha256（64 hex，存盘转小写）。  
3. 询问是否立即启动 Center。  
4. 安装 `ca-certificates` `curl` `gnupg` `git` `openssl`；按需安装 Docker 与 `docker-compose-plugin`。  
5. 克隆或更新仓库，确认存在 **`${INSTALL_DIR}/xrayr-center`**。  
6. 写入 `.env`，按需启动 Compose，打印访问地址与常用命令（不含密钥明文）。

## 五、非交互式安装流程

使用 **`--yes`**，并同时提供 **`--public-url`**、**`--agent-url`**、**`--agent-sha256`**（及可选 **`--port`** 等）；缺任一必填项即退出。适合云厂商 userdata / Ansible `shell` 等自动化场景。

## 六、Docker 自动安装说明

- 若已存在 **`docker`** 命令则跳过 **`get.docker.com`**。  
- 若 **`docker compose version`** 不可用则 **`apt-get install -y docker-compose-plugin`**。  
- 仅在检测到 **`apt-get`** 时安装系统包（与 Phase6 文档一致）。

## 七、`.env` 生成说明

生成字段与 **`docker-compose.yml`** 所需变量对齐：`CENTER_PORT`、`CENTER_PUBLIC_BASE_URL`、`POSTGRES_*`、`CENTER_JWT_SECRET`、`CENTER_AGENT_*`（含空的 `CENTER_AGENT_VERSION` 与默认 `CENTER_AGENT_BINARY_NAME`）。文件权限 **`chmod 600`**。

## 八、安全限制

- 不执行用户输入的任意 shell。  
- 成功摘要中不重复打印数据库密码与 JWT。  
- 非目标发行版不尝试盲目包管理操作。

## 九、README 修改说明

- **根 README**：突出一键命令为推荐路径；原八步 Compose 流程保留为「手动方式」。  
- **xrayr-center/README**：与根文档一致给出 raw 链接、非交互示例、安装路径与 **`docker compose`** 常用命令。

## 十、测试结果

在开发机执行：

```text
cd E:\xrayr-project\xrayr-center
go test ./...        → 通过
go build ./cmd/center → 通过

cd E:\xrayr-project\xrayr-agent
go test ./...        → 通过
go build ./cmd/agent → 通过
```

**`bash -n xrayr-center/install-center.sh`**：若当前环境无 bash，可跳过；脚本已按 Bash 语法人工复核（`set -euo pipefail`、函数顺序、`printf` 写 `.env`）。

```text
git ls-files | findstr /i "\.exe"  → 无跟踪 .exe
```

## 十一、Git 状态说明

建议仅暂存本阶段文件：

```bash
git add README.md xrayr-center/README.md xrayr-center/install-center.sh XRAYR_CENTER_PHASE7_ONECLICK_INSTALLER_RESULT.md
git commit -m "feat: add center one-click install wizard"
```

**不要使用 `git add .`**。

## 十二、未完成项

- **其它发行版**（如 RHEL）：需自行安装 Docker 与 Compose 后，仅使用仓库内 **手动 Compose** 流程。  
- **HTTPS 终止**：一键脚本不配置反向代理，需运维自行加 Nginx/Caddy 等。  
- **制品目录预置**：脚本不预置 XrayR 制品；仍通过 Center 后台上传/登记。

---

**结论**：Phase7 已提供 **`install-center.sh`** 与文档更新，满足「一条命令完成 Docker + 仓库 + `.env` + 启动」的向导目标，且不破坏 Phase1～Phase6 既有能力。
