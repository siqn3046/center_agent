# XrayR Center（MVP）

## 一键启动（Docker Compose，推荐）

```bash
cd xrayr-center
cp .env.example .env
# 编辑 .env：生产环境必须修改 POSTGRES_PASSWORD、CENTER_JWT_SECRET、CENTER_PUBLIC_BASE_URL
# 为 install-agent.sh 能下载 Agent，需同时设置：
#   CENTER_AGENT_DOWNLOAD_URL
#   CENTER_AGENT_SHA256
# 或使用制品目录：CENTER_ARTIFACT_DIR（容器内默认 /data/artifacts）+ CENTER_AGENT_VERSION + 正确放置二进制与 sha256 文件，且 CENTER_PUBLIC_BASE_URL 对节点可达
docker compose up -d --build
```

浏览器访问：`CENTER_PUBLIC_BASE_URL`（默认 `http://localhost:8080`）。  
默认账号：`admin` / `admin123`（**请立即修改**）。

## Agent 一键安装（Komari 风格）

创建节点后，界面会给出类似命令（token 仅在命令中）：

```bash
wget -qO- https://你的Center/install-agent.sh | sudo bash -s -- -e https://你的Center -t <一次性令牌>
```

若未保存命令，可在节点详情 **概览** 使用占位符命令，或点击 **生成新的安装令牌** 获取新命令。

公开脚本：`GET /install-agent.sh`（无需登录；内容不含数据库密码或 JWT secret）。

## 环境变量

| 变量 | 说明 |
|------|------|
| `DATABASE_URL` | PostgreSQL 连接串（Compose 已自动注入） |
| `CENTER_LISTEN` | 监听地址，默认 `:8080` |
| `CENTER_JWT_SECRET` | JWT 密钥（至少足够长度，生产必换） |
| `CENTER_PUBLIC_BASE_URL` | 对外根 URL，**用于生成安装命令与制品下载链接** |
| `CENTER_ARTIFACT_DIR` | 制品根目录；Compose 默认 `/data/artifacts`（卷挂载） |
| `CENTER_AGENT_VERSION` | 使用制品目录托管 Agent 时的版本目录名 |
| `CENTER_AGENT_BINARY_NAME` | 默认 `xrayr-agent` |
| `CENTER_AGENT_DOWNLOAD_URL` | Agent 二进制直链（与 sha256 成对使用） |
| `CENTER_AGENT_SHA256` | Agent 二进制 sha256（小写 64 位 hex） |

## 本地构建

```bash
go build -o center ./cmd/center
```

## 与 XrayR 主仓库关系

本目录为 **独立 Go module**，不修改上级 `XrayR` 代理源码。
