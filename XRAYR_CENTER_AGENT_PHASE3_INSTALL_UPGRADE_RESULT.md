# XrayR Center / Agent Phase3：安装与升级闭环 — 结论文档

## 一、本轮目标

在保留 Phase1/2 架构的前提下，实现 **INSTALL_XRAYR / UPGRADE_XRAYR** 的端到端能力：

- Center 登记 XrayR 二进制制品，管理员对节点下发安装/升级命令。
- Agent 通过 **签名 GET** 下载制品、校验 **sha256**，在 **白名单路径** 上安装或替换二进制，维护 **systemd unit**，执行 **daemon-reload / enable / restart**，校验 **is-active**，失败时尽力 **回滚** 并上报错误。
- Dashboard 节点详情「命令」Tab 支持选择制品、二次确认、权限与进度展示。

## 二、修改文件清单

### Center（`xrayr-center`）

| 文件 | 说明 |
|------|------|
| `internal/db/migrations/004_phase3_artifacts.sql` | `xrayr_binary_artifact` 表；`node.allow_install` |
| `internal/db/db.go` | 嵌入并执行迁移 004 |
| `internal/httpapi/handlers_xrayr_artifacts.go` | 制品列表/详情/登记；Agent 签名下载 |
| `internal/httpapi/handlers_phase2.go` | `allow_install` 权限；`artifact_id` 载荷；`mergeCommandResultJSON` 保留进度 |
| `internal/httpapi/server.go` | 路由：`/api/admin/artifacts*`，`/api/agent/artifacts/{id}/download` |
| `internal/httpapi/handlers_rest.go` | `getNode` 返回 `allow_install` |
| `internal/httpapi/handlers_dashboard.go` | `patchNodeSettings` 独立更新 `allow_install` |
| `internal/httpapi/static/app.js` | 命令 Tab：制品选择、安装/升级/权限禁用、进度摘要；设置 Tab：`allow_install` |

### Agent（`xrayr-agent`）

| 文件 | 说明 |
|------|------|
| `internal/agentcfg/config.go` | `service_path`；默认 `binary_path` / `service_path` / 备份与制品缓存目录；`EffectiveBinaryPath` / `EffectiveServicePath` |
| `internal/capi/client.go` | `SignedGETToFile` 大文件签名下载 |
| `internal/cmdexec/xrayr_install_upgrade.go` | 安装/升级主流程、校验、回滚 |
| `cmd/agent/main.go` | 命令分发至 `RunInstallXrayR` / `RunUpgradeXrayR` |

**未改动**：根目录 `go.mod` / `go.sum`；XrayR 主程序源码。

## 三、新增 / 调整 API

### 管理端（JWT）

- `GET /api/admin/artifacts` — 制品列表  
- `GET /api/admin/artifacts/{id}` — 制品详情  
- `POST /api/admin/artifacts` — 登记制品（`display_name`, `target_os`, `target_arch`, `version_label`, `sha256_hex`, `storage_relpath`；磁盘文件须存在且与 sha256 一致）

### Agent（HMAC）

- `GET /api/agent/artifacts/{id}/download` — 流式下载制品（校验节点与制品 `target_os` / `target_arch`）

### 命令下发（请求体统一为 `artifact_id`）

- `POST /api/admin/nodes/{id}/commands/install-xrayr`  
- `POST /api/admin/nodes/{id}/commands/upgrade-xrayr`  

可选字段（JSON）：`initial_config_yaml` + `initial_config_sha256`（安装）；`overwrite_config` + `config_yaml` + `config_sha256`（升级覆盖配置）。

## 四、Agent 安装流程（INSTALL_XRAYR）

1. **CHECKING**：Linux、`allow_commands`、载荷与 `agent.yml` 路径一致性（`target_*` 仅作校验）。  
2. **DOWNLOADING**：`SignedGETToFile(download_path)`。  
3. **VERIFYING**：本地文件 sha256 与载荷一致。  
4. **INSTALLING**：`sudo install` 写入白名单内二进制；可选初始配置。  
5. **SYSTEMD**：生成固定模板 unit，`install` 到白名单 service 路径。  
6. **RESTARTING**：`daemon-reload`、`enable`（安装或载荷要求时）、`restart`。  
7. **VERIFY_SERVICE**：`discovery.IsServiceActive`。  
8. 失败：**ROLLBACK** 尽力恢复二进制/unit/配置（视阶段而定），错误写入 `command_task`。

## 五、Agent 升级流程（UPGRADE_XRAYR）

在已存在二进制前提下：备份二进制与现有 unit（可选备份配置若 `overwrite_config`），下载校验后替换二进制，按需覆盖配置，**reload + restart**，校验 active；失败则回滚旧二进制与旧 unit（及配置备份），再上报。

## 六、回滚策略

- 升级路径保留备份文件路径；回滚使用 `sudo install` 或 `rm`（仅对白名单内路径）。  
- 重启或校验失败后调用 `rollbackFull` / `rollbackUpgrade` 等组合，并再次 `daemon-reload` 与 `restart`。  
- 回滚阶段错误通过 `systemctlRestart` 上报进度中的文字摘要（不含密钥）。

## 七、权限控制

| 命令 | 条件 |
|------|------|
| `INSTALL_XRAYR` | `MANAGED_WRITABLE` 且 `allow_install` |
| `UPGRADE_XRAYR` | `MANAGED_WRITABLE` 且 `allow_upgrade` |
| `RESTART_XRAYR` | `MANAGED_WRITABLE` 且 `allow_restart` |

`enable-writable` 会将 `allow_install` 一并打开。前端按钮与后端校验一致。

## 八、安全限制

- 写入路径仅来自 **Agent `agent.yml`**（`EffectiveBinaryPath` / `ConfigPath` / `EffectiveServicePath`）；Center 载荷中的 `target_*` 仅作相等性校验。  
- `systemctl` 子命令与白名单路径固定；无任意 shell。  
- 制品下载仅 **HMAC 签名** 的 Agent 路由。  
- `backup_dir`、`artifact_cache_dir` 须在允许前缀下（如 `/var/lib/xrayr-agent/...`）。

## 九、测试结果

在仓库内执行：

```text
cd E:\xrayr-project\xrayr-center && go test ./...   # 通过
cd E:\xrayr-project\xrayr-agent   && go test ./...   # 通过
```

（当前子模块无测试文件，均为编译通过。）

## 十、Git 状态说明

提交前请本地执行：

- `git status -uall`  
- `git ls-files \| findstr /i "\.exe"` — 索引中不应出现 `center.exe` / `xrayr-agent.exe`。

建议提交信息：`feat: implement xrayr install and upgrade command flow`

## 十一、未完成项 / 后续建议

1. **agent.yml**：生产环境需在 `allow_commands` 中显式加入 `INSTALL_XRAYR`、`UPGRADE_XRAYR`。  
2. **CENTER_ARTIFACT_DIR**：登记制品前须在目录中放置文件，`storage_relpath` 为相对该目录的路径。  
3. **联调**：在真实 Linux 节点上验证 `sudo -n`、磁盘权限与 XrayR 可执行参数（`--config`）是否与现场一致。  
4. **可选**：安装脚本与默认 `backup_dir`/`artifact_cache_dir` 文案与 Center 安装脚本中的路径完全对齐（若仍引用旧路径可单独调整文档或脚本）。
