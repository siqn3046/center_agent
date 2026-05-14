# xrayr-agent（MVP）

独立进程，默认配置文件 **`/etc/xrayr-agent/agent.yml`**。

## 推荐：从 Center 一键安装

在 **Linux** 节点上（root 或 sudo），使用 Center 管理端创建节点后给出的命令，例如：

```bash
wget -qO- https://你的Center/install-agent.sh | sudo bash -s -- -e https://你的Center -t <一次性令牌>
```

或 `curl -fsSL ... | sudo bash -s -- ...`。  
脚本会下载 Agent 二进制、校验 sha256、写入 `agent.yml`、安装并启动 `xrayr-agent.service`。

**前提**：Center 已配置 `CENTER_AGENT_DOWNLOAD_URL` + `CENTER_AGENT_SHA256`，或已按文档将 Agent 放入 `CENTER_ARTIFACT_DIR` 并设置 `CENTER_PUBLIC_BASE_URL` / `CENTER_AGENT_VERSION` 等。

安装脚本参数：`--endpoint` / `--token` / `--config`（默认 `/etc/xrayr-agent/agent.yml`）/ `--help`。

## 本地构建与手动运行

```bash
go build -o xrayr-agent ./cmd/agent
./xrayr-agent -config ./agent.dev.yml
```

手动配置时需包含 `center.url`、`register_token`（首次注册）等字段，结构见 `internal/agentcfg/config.go`。
