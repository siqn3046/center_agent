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

```
wget -N https://raw.githubusercontent.com/XrayR-project/XrayR-release/master/install.sh && bash install.sh
```

### 使用Docker部署软件

[Docker部署教程](https://xrayr-project.github.io/XrayR-doc/xrayr-xia-zai-he-an-zhuang/install/docker)

### 手动安装

[手动安装教程](https://xrayr-project.github.io/XrayR-doc/xrayr-xia-zai-he-an-zhuang/install/manual)

### Center 与 xrayr-agent（集中管控，可选）

本仓库在经典 XrayR 节点程序之外，还提供 **xrayr-center**（Web 管理端 + Admin/Agent API）与 **xrayr-agent**（部署在节点上的管控代理）。不需要集中管控时，可忽略本节，仅按上文安装节点端 XrayR 即可。

**环境要求**

- Docker：用于一键拉起 Center 与 Postgres（推荐）。
- 或本地安装 **Go**（建议 1.22+）与 **PostgreSQL**，自行配置 `DATABASE_URL`。

**用 Docker 启动 Center（推荐）**

```bash
cd xrayr-center
# 可选：安装脚本中 Agent 二进制来源（直链 + 校验和）
export CENTER_AGENT_DOWNLOAD_URL=https://你的分发地址/xrayr-agent
export CENTER_AGENT_SHA256=<64 位小写十六进制 sha256>
docker compose up -d --build
```

浏览器访问默认 `http://localhost:8080`（以 `CENTER_LISTEN` 为准）。首次默认账号见 `xrayr-center/README.md`（**登录后请立即改密**）。

**常用环境变量（Center）**

| 变量 | 说明 |
|------|------|
| `DATABASE_URL` | PostgreSQL 连接串（必填） |
| `CENTER_LISTEN` | 监听地址，默认 `:8080` |
| `CENTER_JWT_SECRET` | 管理端 JWT 密钥 |
| `CENTER_PUBLIC_BASE_URL` | 对外访问根 URL（安装脚本、回调等） |
| `CENTER_ARTIFACT_DIR` | 节点 **XrayR 二进制制品** 存放目录（绝对路径）；不配则无法在管理端上传/登记制品供安装升级使用 |
| `CENTER_AGENT_DOWNLOAD_URL` / `CENTER_AGENT_SHA256` | 节点安装脚本中 **Agent 二进制** 的直链与 sha256（与 `CENTER_ARTIFACT_DIR` 二选一策略见 `xrayr-center` 内配置说明） |

更完整的变量说明见 [`xrayr-center/README.md`](xrayr-center/README.md)。

**本地编译 Center / Agent（不依赖 Docker 二进制时）**

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


