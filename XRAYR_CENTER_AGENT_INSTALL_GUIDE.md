# XrayR Center + Agent 安装与使用说明（含 Komari 风格面板）

本文档为 UTF-8。适用于与主仓库同级的独立模块：`xrayr-center`、`xrayr-agent`。

## 1. 如何进入 Center 面板

1. 启动 Center（见下文 Docker 或本地 `go run`）。
2. 浏览器访问 Center 根地址，例如 `http://127.0.0.1:8080/`。
3. 使用管理员账号登录（默认 `admin` / `admin123`，**生产环境请立即修改**）。

## 2. Dashboard 使用

登录后即进入 **Dashboard**：

- 顶部统计卡片：服务器时间、在线/总节点、区域数、上下行总速度、累计流量（基于各节点最近监控快照估算）。
- 搜索框：按节点名称、代码、IP、地区、系统关键字过滤。
- **卡片 / 表格** 切换视图。
- **创建节点**：生成一次性 `register_token`，请妥善保存。

页面默认 **每 10 秒** 自动刷新列表与统计。

## 3. 节点详情

点击节点卡片（或表格行）进入详情：

- 顶部：节点名称、ID、管理状态、安装状态、下载安装脚本。
- **Tab**：概览、Discovery、XrayR 配置、命令、设置。
- 详情页 **每 10 秒** 刷新；**命令** Tab **每 3 秒** 刷新命令列表。

## 4. 网页编辑 XrayR 配置

1. 打开节点 → **XrayR 配置** Tab。
2. 若存在导入版本，会自动加载 YAML 草稿（只读模式下不可改）。
3. 点击 **编辑** 解锁文本框。
4. 填写 **配置名称**、**备注**，修改 **YAML**。
5. 点击 **保存为新版本**：调用 `POST /api/admin/nodes/{id}/config-versions`，在 Center 侧生成新的 `config_version`（`NODE_UI`）。

## 5. 下发配置到 VPS

1. 确保节点已 **enable-writable**（设置页一键按钮或自行 `PATCH` 为 `MANAGED_WRITABLE` 并打开 `allow_config_apply`）。
2. 在配置版本表格中点击某版本的 **下发**，或先在 **设置** 中填写 `pending_deploy_version_id` 后在 **命令** Tab 使用「Apply Config」。
3. Center 创建 `config_deploy_task` 与 `APPLY_CONFIG` 命令，经 **WebSocket** 推送给 Agent。
4. Agent：签名 `GET` 拉取 YAML → **sha256 校验** → 备份 → 原子写入 `agent.yml` 中的 `config_path` → `sudo systemctl restart` → `is-active` 检查。

## 6. 查看下发结果

在 **XrayR 配置** Tab 底部的 **下发任务** 表：状态、`backup_path`、错误信息、完成时间。  
在 **命令** Tab 查看 `command_id`、状态、`log_summary`、`result_json.progress` 等。

## 7. 失败与回滚

若重启后服务未 **active**，Agent 会尝试将旧配置写回并再次重启，并在命令结果中标记失败与 `rolled_back`。

## 8. Agent 与 Center 前置条件

- PostgreSQL 已迁移（含 `003_ui_node.sql`）。
- Center 环境变量：`CENTER_PUBLIC_BASE_URL`、`DATABASE_URL`、`CENTER_JWT_SECRET` 等。
- Agent 安装脚本会写入 `agent.yml`，包含 `allow_commands`（含 `STATUS_XRAYR`、`RESTART_XRAYR`、`APPLY_CONFIG` 等）、`backup_dir`、`artifact_cache_dir`。
- 需保证 Agent 用户对配置目录与 `sudo systemctl` 的权限与安装脚本中 **sudoers** 一致。

## 9. 常见问题

- **WS 无法连接**：检查 `CENTER_PUBLIC_BASE_URL` 是否与浏览器访问的 Center 地址一致；防火墙放行；HTTPS 使用 `wss://`。
- **APPLY_CONFIG 报 sha256 不匹配**：确认 Center 侧 `config_version.content_sha256` 与下载内容一致。
- **路径不一致**：Center 下发的 `target_config_path` 若非空，必须与 `agent.yml` 的 `config_path` 完全一致。

## 10. 安全说明

- Center 不下发任意 shell；危险命令受 `manage_status` 与 `allow_*` 约束。
- 默认 `MANAGED_READONLY` 仅允许 **Status XrayR** 类只读探测；写操作需显式可写模式。
