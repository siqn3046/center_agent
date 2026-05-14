# XrayR

[![](https://img.shields.io/badge/TgChat-@XrayR讨论-blue.svg)](https://t.me/XrayR_project)
[![](https://img.shields.io/badge/Channel-@XrayR通知-blue.svg)](https://t.me/XrayR_channel)
![](https://img.shields.io/github/stars/XrayR-project/XrayR)
![](https://img.shields.io/github/forks/XrayR-project/XrayR)
![](https://github.com/XrayR-project/XrayR/actions/workflows/release.yml/badge.svg)
![](https://github.com/XrayR-project/XrayR/actions/workflows/docker.yml/badge.svg)
[![Github All Releases](https://img.shields.io/github/downloads/XrayR-project/XrayR/total.svg)]()


[English](https://github.com/XrayR-project/XrayR/blob/master/README-en.md)|[Iranian](https://github.com/XrayR-project/XrayR/blob/master/README_Fa.md)|[Vietnamese](https://github.com/XrayR-project/XrayR/blob/master/README-vi.md)

A Xray backend framework that can easily support many panels.

一个基于Xray的后端框架，支持V2ay,Trojan,Shadowsocks协议，极易扩展，支持多面板对接。

如果您喜欢本项目，可以右上角点个star+watch，持续关注本项目的进展。

使用教程：[详细使用教程](https://xrayr-project.github.io/XrayR-doc/)


## 免责声明

本项目只是本人个人学习开发并维护，本人不保证任何可用性，也不对使用本软件造成的任何后果负责。

## 特点

* 永久开源且免费。
* 支持V2ray，Trojan， Shadowsocks多种协议。
* 支持Vless和XTLS等新特性。
* 支持单实例对接多面板、多节点，无需重复启动。
* 支持限制在线IP
* 支持节点端口级别、用户级别限速。
* 配置简单明了。
* 修改配置自动重启实例。
* 方便编译和升级，可以快速更新核心版本， 支持Xray-core新特性。

## 功能介绍

| 功能        | v2ray | trojan | shadowsocks |
|-----------|-------|--------|-------------|
| 获取节点信息    | √     | √      | √           |
| 获取用户信息    | √     | √      | √           |
| 用户流量统计    | √     | √      | √           |
| 服务器信息上报   | √     | √      | √           |
| 自动申请tls证书 | √     | √      | √           |
| 自动续签tls证书 | √     | √      | √           |
| 在线人数统计    | √     | √      | √           |
| 在线用户限制    | √     | √      | √           |
| 审计规则      | √     | √      | √           |
| 节点端口限速    | √     | √      | √           |
| 按照用户限速    | √     | √      | √           |
| 自定义DNS    | √     | √      | √           |

## 支持前端

| 前端                                                     | v2ray | trojan | shadowsocks             |
|--------------------------------------------------------|-------|--------|-------------------------|
| sspanel-uim                                            | √     | √      | √ (单端口多用户和V2ray-Plugin) |
| v2board                                                | √     | √      | √                       |
| [PMPanel](https://github.com/ByteInternetHK/PMPanel)   | √     | √      | √                       |
| [ProxyPanel](https://github.com/ProxyPanel/ProxyPanel) | √     | √      | √                       |
| [WHMCS (V2RaySocks)](https://v2raysocks.doxtex.com/)   | √     | √      | √                       |
| [GoV2Panel](https://github.com/pingProMax/gov2panel)   | √     | √      | √                       |
| [BunPanel](https://github.com/pennyMorant/bunpanel-release)   | √     | √      | √                       |

## 软件安装

### 一键安装

原 **XrayR-project/XrayR-release** 一键脚本已不可用，请改用 **本仓库** 根目录的 [`install.sh`](install.sh)（从源码编译节点主程序，需已安装 **Git** 与 **Go 1.21+**）。

在 **Linux** 上执行（安装到 `/usr/local/bin/XrayR` 需 **root**；管道安装请用 `sudo bash`）。将 `main` 换成你仓库的默认分支名（如 `master`）：

```bash
curl -fsSL https://raw.githubusercontent.com/siqn3046/center_agent/main/install.sh | sudo bash
```

```bash
wget -qO- https://raw.githubusercontent.com/siqn3046/center_agent/main/install.sh | sudo bash
```

若希望先保存脚本再运行（便于检查内容）：

```bash
wget -q https://raw.githubusercontent.com/siqn3046/center_agent/main/install.sh -O install.sh && sudo bash install.sh
```

自托管或 fork 时，把上述 URL 中的 `siqn3046/center_agent` 与分支名改成你的仓库即可；也可用环境变量覆盖克隆地址与分支，例如：`XRAYR_INSTALL_REPO=https://github.com/you/your-fork.git` `XRAYR_INSTALL_BRANCH=main`。

### 使用Docker部署软件

[Docker部署教程](https://xrayr-project.github.io/XrayR-doc/xrayr-xia-zai-he-an-zhuang/install/docker)

### 手动安装

[手动安装教程](https://xrayr-project.github.io/XrayR-doc/xrayr-xia-zai-he-an-zhuang/install/manual)

### Center 一键安装（推荐）

在一台新的 **Ubuntu / Debian** 服务器上（需 **root**），一条命令完成：安装 Docker 与依赖、克隆仓库、**交互生成 `.env`**（自动生成数据库密码与 JWT）、启动 Center：

```bash
wget -q https://raw.githubusercontent.com/siqn3046/center_agent/main/xrayr-center/install-center.sh -O install-center.sh && sudo bash install-center.sh
```

非交互示例（CI / 云初始化脚本）见 [`xrayr-center/README.md`](xrayr-center/README.md) 中「一键安装向导」。

安装完成后在浏览器打开你填写的 **Center 对外地址**（如 `http://你的服务器IP:8080`），登录后台创建节点，再复制页面上的 **Agent 一键安装命令** 到节点 VPS 执行。结论文档：[`XRAYR_CENTER_PHASE7_ONECLICK_INSTALLER_RESULT.md`](XRAYR_CENTER_PHASE7_ONECLICK_INSTALLER_RESULT.md)。

---

### Center 与 Agent 集中管控部署（手动方式）

本仓库提供 **xrayr-center**（管理端 + API）与 **xrayr-agent**（节点代理）。若不想使用一键脚本，可按以下步骤手动完成 Docker Compose 部署；更细的说明见 [`xrayr-center/README.md`](xrayr-center/README.md) 与 [`XRAYR_CENTER_PHASE6_DOCKER_INSTALL_DOC_RESULT.md`](XRAYR_CENTER_PHASE6_DOCKER_INSTALL_DOC_RESULT.md)。

#### 1. 准备服务器

**推荐系统**

- Ubuntu 22.04 / 24.04  
- Debian 11 / 12  

**最低参考**

- 1 核 CPU、1GB 内存、约 10GB 磁盘  
- 安全组 / 防火墙放行 Center Web 端口（默认 **8080**，与 `CENTER_PORT` 一致）  

#### 2. 安装 Docker

Ubuntu / Debian 示例：

```bash
sudo apt update
sudo apt install -y ca-certificates curl gnupg git
curl -fsSL https://get.docker.com | sudo bash
sudo systemctl enable docker
sudo systemctl start docker
docker version
docker compose version
```

若提示找不到 `docker compose`，请安装 Compose 插件：

```bash
sudo apt install -y docker-compose-plugin
docker compose version
```

#### 3. 创建目录并拉取仓库

```bash
sudo mkdir -p /opt/xrayr-center
sudo chown -R "$USER:$USER" /opt/xrayr-center
cd /opt/xrayr-center
git clone https://github.com/siqn3046/center_agent.git .
```

#### 4. 配置 Center 环境变量

```bash
cd /opt/xrayr-center/xrayr-center
cp .env.example .env
nano .env   # 或使用 vim 等编辑器
```

`.env` 中 **生产环境务必修改** 的典型项（示例值请按你的环境替换）：

```text
CENTER_PORT=8080
CENTER_PUBLIC_BASE_URL=http://你的服务器公网IP:8080

POSTGRES_PASSWORD=请改成强密码
CENTER_JWT_SECRET=请改成随机长密钥（建议 32 字符以上）

CENTER_AGENT_DOWNLOAD_URL=https://你的分发地址/xrayr-agent-linux-amd64
CENTER_AGENT_SHA256=64位小写十六进制sha256
```

**说明要点**

- **`CENTER_PUBLIC_BASE_URL`**：节点与浏览器访问 Center 的根地址（含协议与端口），用于生成 **Agent 一键安装命令** 与制品下载 URL；有域名与 HTTPS 时建议写 `https://center.example.com`。  
- **`CENTER_AGENT_DOWNLOAD_URL` / `CENTER_AGENT_SHA256`**：供 **`GET /install-agent.sh`** 注入，用于节点下载 **xrayr-agent** 二进制并校验；**两者缺一都会导致一键安装脚本无法正确下发 Agent**。  
- **`CENTER_ARTIFACT_DIR`**：Compose 已默认挂载为容器内 `/data/artifacts`，用于 **XrayR 节点二进制制品**（与上面 Agent 直链不是同一概念，详见 Center README）。

#### 5. 启动 Center

```bash
cd /opt/xrayr-center/xrayr-center
docker compose up -d --build
```

#### 6. 查看运行状态与日志

```bash
docker compose ps
docker compose logs -f xrayr-center
```

#### 7. 访问后台

浏览器打开 **`CENTER_PUBLIC_BASE_URL`**（例如 `http://你的服务器IP:8080` 或 `https://你的域名`）。  
首次默认账号以 [`xrayr-center/README.md`](xrayr-center/README.md) 为准（默认 **`admin` / `admin123`**），**登录后请立即修改密码**。

#### 8. 创建节点并安装 Agent

1. 在 Center 后台进入节点管理，**创建节点**。  
2. 复制界面生成的 **wget / curl 一键安装命令**（或到节点详情「概览」使用占位符 / **生成新的安装令牌**）。  
3. 在 **目标 Linux 节点** 上以 root 或 sudo 执行，例如：

```bash
wget -qO- http://你的服务器IP:8080/install-agent.sh | sudo bash -s -- -e http://你的服务器IP:8080 -t <register_token>
```

或：

```bash
curl -fsSL http://你的服务器IP:8080/install-agent.sh | sudo bash -s -- -e http://你的服务器IP:8080 -t <register_token>
```

将 `http://你的服务器IP:8080` 换成与 **`.env` 中 `CENTER_PUBLIC_BASE_URL` 一致** 的地址。安装完成后 Agent 会注册并连接 Center。

**常见错误速查**：容器未启动 / 端口未放行、`CENTER_PUBLIC_BASE_URL` 节点不可达、未配置 Agent 直链与 sha256、token 过期或复制不完整等——见 [`XRAYR_CENTER_PHASE6_DOCKER_INSTALL_DOC_RESULT.md`](XRAYR_CENTER_PHASE6_DOCKER_INSTALL_DOC_RESULT.md) 第九节。

---

**本地编译 Center / Agent（非 Docker 场景）**

```bash
# Center
cd xrayr-center
go build -o center ./cmd/center
./center   # 需已设置 DATABASE_URL 等环境变量

# Agent（在节点或本机测试）
cd xrayr-agent
go build -o xrayr-agent ./cmd/agent
./xrayr-agent -config /etc/xrayr-agent/agent.yml
```

Windows 下可在对应目录执行 `go build -o center.exe ./cmd/center`、`go build -o xrayr-agent.exe ./cmd/agent`。Agent 默认配置文件路径为 **`/etc/xrayr-agent/agent.yml`**（Linux）；开发环境可用 `-config` 指定本地 yml。

**Agent 接入流程简述**

1. 在 Center **创建节点**，按弹窗复制 **wget / curl 一键安装命令**（或到节点详情「概览」生成新令牌后再复制）。
2. 在 **Linux** 节点上以 root/sudo 执行该命令；无需手写 `agent.yml`（脚本会写入并启动 `xrayr-agent`）。
3. 若需通过 Center **安装/升级 XrayR 节点二进制**，请配置 **`CENTER_ARTIFACT_DIR`**，并在管理端登记或上传制品（详见 [`XRAYR_CENTER_AGENT_PHASE4_DEPLOY_VERIFY_RESULT.md`](XRAYR_CENTER_AGENT_PHASE4_DEPLOY_VERIFY_RESULT.md)）。

安装入口与 Compose 细节见：**[`XRAYR_CENTER_AGENT_PHASE5_INSTALL_ENTRY_RESULT.md`](XRAYR_CENTER_AGENT_PHASE5_INSTALL_ENTRY_RESULT.md)**。

更多细节：**[`xrayr-center/README.md`](xrayr-center/README.md)**、**[`xrayr-agent/README.md`](xrayr-agent/README.md)**。

## 配置文件及详细使用教程

[详细使用教程](https://xrayr-project.github.io/XrayR-doc/)

## Thanks

* [Project X](https://github.com/XTLS/)
* [V2Fly](https://github.com/v2fly)
* [VNet-V2ray](https://github.com/ProxyPanel/VNet-V2ray)
* [Air-Universe](https://github.com/crossfw/Air-Universe)

## Licence

[Mozilla Public License Version 2.0](https://github.com/XrayR-project/XrayR/blob/master/LICENSE)

## Telgram

[XrayR后端讨论](https://t.me/XrayR_project)

[XrayR通知](https://t.me/XrayR_channel)

## Stargazers over time

[![Stargazers over time](https://starchart.cc/XrayR-project/XrayR.svg)](https://starchart.cc/XrayR-project/XrayR)


