# xrayr-center 部署说明

## 一、组件说明

- **xrayr-center**：集中管理端，提供 Web 管理界面与 Admin/Agent HTTP API。  
- **职责**：节点管理、Agent 注册与心跳、制品（XrayR 二进制）登记与上传、配置版本与下发、远程命令（安装/升级 XrayR、重启、应用配置等）。  
- **PostgreSQL**：持久化节点、令牌、命令、配置版本、制品元数据等。  
- **公开安装脚本**：节点执行的一键安装脚本由 Center 托管，路径为 **`GET /install-agent.sh`**（无需登录；脚本内仅注入 Agent 下载 URL 与 sha256，**不包含**数据库密码或 JWT secret）。

## 二、推荐部署方式

推荐使用本目录下的 **Docker Compose** 一键拉起 `postgres` 与 `xrayr-center`，数据与制品使用命名卷持久化。

## 三、服务器准备

**推荐系统**

- Ubuntu 22.04 / 24.04  
- Debian 11 / 12  

**最低参考**

- 1 核 CPU、1GB 内存、约 10GB 可用磁盘  
- 放行 **Center Web 端口**（默认与 `CENTER_PORT` 一致，一般为 **8080**）  

**安装 Docker（Ubuntu / Debian 示例）**

```bash
sudo apt update
sudo apt install -y ca-certificates curl gnupg git
curl -fsSL https://get.docker.com | sudo bash
sudo systemctl enable docker
sudo systemctl start docker
docker version
docker compose version
```

若 **`docker compose version`** 报错，安装 Compose 插件：

```bash
sudo apt install -y docker-compose-plugin
docker compose version
```

## 四、完整安装步骤

### 1. 创建目录并拉取仓库

以下以本仓库 **`siqn3046/center_agent`** 为例（fork 时请替换为你的地址）：

```bash
sudo mkdir -p /opt/xrayr-center
sudo chown -R "$USER:$USER" /opt/xrayr-center
cd /opt/xrayr-center
git clone https://github.com/siqn3046/center_agent.git .
cd xrayr-center
```

### 2. 准备 `.env`

```bash
cp .env.example .env
nano .env   # 或 vim 等
```

### 3. 编辑必填项

至少确认或修改：

- `CENTER_PUBLIC_BASE_URL`：与浏览器及 **Agent 节点访问 Center 的地址** 一致（含协议与端口）。  
- `POSTGRES_PASSWORD`、`CENTER_JWT_SECRET`：生产环境 **必须** 改为强随机值。  
- `CENTER_AGENT_DOWNLOAD_URL` 与 `CENTER_AGENT_SHA256`：**成对填写**，否则 **`/install-agent.sh`** 无法为节点提供可用的 Agent 二进制下载信息（一键安装会失败）。  

### 4. 启动

```bash
docker compose up -d --build
```

### 5. 查看状态

```bash
docker compose ps
```

### 6. 查看日志

```bash
docker compose logs -f xrayr-center
```

### 7. 访问后台

浏览器打开 **`CENTER_PUBLIC_BASE_URL`**。  
默认管理员账号：**`admin`** / **`admin123`**（**登录后请立即修改密码**）。

## 五、`.env` 配置说明

Compose 会将下表中的变量注入 `xrayr-center` 与 `postgres` 服务（详见 `docker-compose.yml`）。

| 变量 | 必填 | 示例 | 说明 |
|------|------|------|------|
| `CENTER_PORT` | 否 | `8080` | 宿主机映射到容器 `8080` 的端口 |
| `CENTER_PUBLIC_BASE_URL` | **是**（生产） | `https://center.example.com` | 生成 Agent 安装命令、制品公开下载 URL 的根地址；节点必须能访问 |
| `POSTGRES_PASSWORD` | **是**（生产） | 强随机密码 | PostgreSQL 密码，与 `DATABASE_URL` 拼接一致 |
| `POSTGRES_USER` | 否 | `xrayr` | 数据库用户名 |
| `POSTGRES_DB` | 否 | `xrayr_center` | 数据库名 |
| `CENTER_JWT_SECRET` | **是**（生产） | 随机长字符串 | 管理端 JWT 签名密钥 |
| `CENTER_ARTIFACT_DIR` | 否（Compose 已设） | `/data/artifacts` | 容器内 **XrayR 节点二进制制品** 根目录；Compose 已挂载卷 `center_artifacts` |
| `CENTER_AGENT_DOWNLOAD_URL` | **是**（若不用制品托管 Agent） | `https://cdn.example.com/xrayr-agent-linux-amd64` | **xrayr-agent** 二进制直链，写入 **`/install-agent.sh`** |
| `CENTER_AGENT_SHA256` | **是**（同上） | 64 位小写 hex | 与直链文件一致的 SHA256 |
| `CENTER_AGENT_VERSION` | 否 | `1.0.0` | 使用 **制品目录** 托管 Agent 时，二进制放在 `$CENTER_ARTIFACT_DIR/$版本/` 下时使用 |
| `CENTER_AGENT_BINARY_NAME` | 否 | `xrayr-agent` | Agent 文件名 |

**务必区分：**

- **`CENTER_AGENT_DOWNLOAD_URL`**：给 **xrayr-agent** 进程安装包用（`/install-agent.sh`）。  
- **`CENTER_ARTIFACT_DIR`（及后台登记的制品）**：给 **XrayR 节点程序** 的 `INSTALL_XRAYR` / `UPGRADE_XRAYR` 使用，与 Agent 安装包不是同一概念。

## 六、Agent 一键安装命令

在 Center **创建节点** 后，界面会生成类似命令（**`-t` 中的令牌仅用于首次注册**，勿泄露）：

```bash
wget -qO- <CENTER_PUBLIC_BASE_URL>/install-agent.sh | sudo bash -s -- -e <CENTER_PUBLIC_BASE_URL> -t <register_token>
```

或：

```bash
curl -fsSL <CENTER_PUBLIC_BASE_URL>/install-agent.sh | sudo bash -s -- -e <CENTER_PUBLIC_BASE_URL> -t <register_token>
```

**参数说明**

- **`-e` / `--endpoint`**：Center 根 URL，与 `CENTER_PUBLIC_BASE_URL` 一致。  
- **`-t` / `--token`**：节点一次性注册令牌。  

**脚本行为概要**

- 下载并校验 **xrayr-agent** 二进制；  
- 写入 **`/etc/xrayr-agent/agent.yml`**；  
- 安装并启用 **`xrayr-agent.service`**（systemd）。  

若未保存命令，可在节点详情 **「概览」** 使用占位符命令，或点击 **「生成新的安装令牌」**。

## 七、制品目录与 XrayR 安装升级

- Compose 中已设置 **`CENTER_ARTIFACT_DIR=/data/artifacts`**，并挂载卷 **`center_artifacts`**，数据在 Docker 卷中持久化。  
- 在管理后台可 **上传 / 登记** XrayR 节点二进制制品，供节点执行 **INSTALL_XRAYR**、**UPGRADE_XRAYR** 等命令时使用。  
- **Agent 二进制**（`CENTER_AGENT_*`）与 **XrayR 制品**（`CENTER_ARTIFACT_DIR` + 后台记录）用途不同，请勿混淆。

## 八、常见问题

### 1. `docker compose` 命令不存在

```bash
sudo apt install -y docker-compose-plugin
docker compose version
```

### 2. Center 页面打不开

```bash
docker compose ps
docker compose logs -f xrayr-center
ss -lntp | grep 8080   # 或你配置的 CENTER_PORT
```

检查云厂商安全组、本机 `ufw`/`firewalld` 是否放行端口。

### 3. Agent 安装脚本提示未配置下载地址

检查 `.env` 中 **`CENTER_AGENT_DOWNLOAD_URL`** 与 **`CENTER_AGENT_SHA256`** 是否已填写并 **`docker compose up -d` 或 `restart`** 使容器生效。

### 4. sha256 校验失败

在已下载的 Agent 文件上重新计算并回填 `.env`：

```bash
sha256sum xrayr-agent-linux-amd64
```

（结果须为 **64 位小写十六进制**，与 Center 配置完全一致。）

### 5. Agent 注册失败

- 节点能否访问 **`CENTER_PUBLIC_BASE_URL`**（DNS / 证书 / 端口）？  
- **令牌**是否完整复制、是否过期、是否已被使用（可尝试在后台 **生成新的安装令牌**）？  
- 查看 Center 日志与节点：`sudo journalctl -u xrayr-agent -f`。

### 6. 修改 `.env` 后不生效

```bash
docker compose up -d --build
# 或
docker compose restart xrayr-center
```

必要时 **`docker compose down`** 后再 **`up -d`**（注意数据卷是否需保留）。

## 九、本地构建（非 Docker）

```bash
go build -o center ./cmd/center
```

需自行配置 `DATABASE_URL` 等环境变量并连接 PostgreSQL。

## 十、与 XrayR 主仓库关系

本目录为 **独立 Go module**，不修改上级 XrayR 代理业务源码树中的实现方式；仅作为 **管控面（Center）** 与仓库内 **xrayr-agent** 协同使用。
