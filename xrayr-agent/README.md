# xrayr-agent（MVP）

独立进程，配置默认 `/etc/xrayr-agent/agent.yml`。

```bash
go build -o xrayr-agent ./cmd/agent
./xrayr-agent -config ./agent.dev.yml
```

需先通过 Center 创建节点并获取 `register_token`（或安装脚本写入）。
