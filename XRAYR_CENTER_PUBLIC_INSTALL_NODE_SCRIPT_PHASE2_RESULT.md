# XrayR Center：`GET /api/public/install-node.sh` Phase 2 结项说明

## 修改文件

| 文件 | 说明 |
|------|------|
| `xrayr-center/internal/httpapi/server.go` | 注册 `GET /api/public/install-node.sh` |
| `xrayr-center/internal/httpapi/handlers_install_node_public.go` | **新增**：query 白名单与校验、token 校验、脚本生成、`operation_log` 记录 |
| `xrayr-center/internal/httpapi/handlers_install_node_public_test.go` | **新增**：参数校验与脚本安全子串的单元测试 |
| `xrayr-center/internal/httpapi/static/app.js` | 仅更新弹窗脚注文案（说明脚本已动态生成及 Agent 下载依赖） |

未改 DB migration；沿用 `node_install_token`、`operation_log` 现有结构。

## 新增路由

- **`GET /api/public/install-node.sh`**  
  - 处理器：`Server.installNodePublicScript`  
  - 响应：`Content-Type: text/x-shellscript; charset=utf-8`，`Cache-Control: no-store`  
  - 与现有 **`GET /api/admin/nodes/{id}/install-script.sh?register_token=...`** 并存，互不替代。

## 参数白名单

仅允许以下 query 键；任一其它键返回 **400**：`unsupported query parameter: <键名>`

`token`, `os`, `disable_remote_control`, `disable_auto_update`, `insecure_tls`, `github_proxy`, `install_dir`, `service_name`, `agent_only`, `force_replace_xrayr`, `keep_config`, `use_center_config`, `verbose`, `include_nics`, `exclude_nics`, `mount_points`, `interval`

## 参数校验规则（实现摘要）

| 规则 | 行为 |
|------|------|
| `token` | 必填、非空；`SHA256` 查 `node_install_token`；`revoked_at` 为空；`used_at` 为空；未过期；且对应 `node` 未 `disabled` |
| `os` | 缺省按 `linux`；仅 `linux` 合法；`windows` / `macos` / `darwin` 返回 **501**，正文 `暂未支持`；其它值 **400** |
| 布尔型 | 缺省 `false`；若出现则仅允许 `true` / `false`（大小写不敏感） |
| `install_dir` | 缺省 `/opt/xrayr`；须为绝对路径；禁止 `; & \| \` $ < >` 及换行、反斜杠 |
| `service_name` | 缺省 `XrayR`；正则 `^[A-Za-z0-9_.-]+$` |
| `interval` | 缺省 `10`；整数 **5–300** |
| `include_nics` / `exclude_nics` | 正则 `^[A-Za-z0-9_.,-]*$`；`exclude_nics` 缺省 `lo,docker0,br-` |
| `mount_points` | 缺省 `/`；逗号分隔；每段须为绝对路径并通过与 `install_dir` 相同的路径危险字符校验 |
| `github_proxy` | 可为空；非空须为 **http/https** 且含 `host`；禁止换行、反引号、`$`、`;`、`&`、`|`、`<`、`>`、`\`；与 Agent URL 拼接为 `TrimSuffix(prefix,"/") + "/" + agentURL`（整段 URL 作为前缀后的路径，**待确认** 是否覆盖所有镜像站写法） |

错误响应为纯英文/中文短句，**不包含** `node_secret`。

## 脚本流程（生成物行为）

1. `set -euo pipefail`（可选 `set -x` 当 `verbose=true`）。  
2. 打印 `XrayR Center node installer`。  
3. 校验 **root**、**Linux**、**systemd**、**amd64/arm64**（`uname -m`）。  
4. 探测已有 XrayR：`command -v XrayR`、`/usr/local/bin/XrayR`、`/etc/XrayR/config.yml`、`systemctl status $SERVICE_NAME`、`systemctl cat $SERVICE_NAME.service`。  
5. **XrayR 二进制替换**：仅当 **`force_replace_xrayr=true` 且已检测到本机已有 XrayR** 时视为需要制品；当前 Center 未在脚本内嵌入可下载的 XrayR 制品 URL，故此分支 **固定失败**，输出 `XrayR artifact is not configured` 并 `exit 1`（不伪装成功）。  
6. **无 XrayR / 未强制替换**：仅安装 **xrayr-agent**（与现有 `ResolveAgentDownload` / 校验和逻辑一致）；不修改本机 XrayR 二进制。  
7. `agent_only=true` 时同样不走 XrayR 替换分支。  
8. `keep_config`：当前脚本**不执行**配置覆盖逻辑，仅占位与 Phase 1 参数对齐；与「仅替换」分支未实现一致。  
9. `use_center_config=true`：脚本 **stderr** 提示需在 Agent 注册后由 Center 下发配置；**不**自动拉取 YAML（无匿名带 token 的公开配置接口，**待确认**）。  
10. 备份：当 `force_replace` 且已存在 XrayR 时，在尝试替换前创建 `/opt/xrayr-backup/<时间戳>/` 并复制常见路径（若替换因制品缺失中止，备份可能已部分创建）。  
11. 安装 Agent：创建用户/目录、`curl` 下载（使用 `CURL_EXTRA` 支持 `insecure_tls` 时的 `-k`）、`sha256sum -c`、写入 `/etc/xrayr-agent/agent.yml`、**systemd unit**、`sudoers`（`systemctl` 目标使用校验后的 **service_name** 字面量）、`daemon-reload`、`systemctl enable --now xrayr-agent`。  
12. **禁止** `eval`、**禁止** `bash -c` 拼接用户 query；变量来自 Go 侧 `fmt %q` / 定界 heredoc。

`agent.yml` 写入字段包含：`center.url`、`register_token`、顶层 `install_dir` / `service_name`（扩展字段，agent 可能忽略）、`xrayr.service_name`、`monitor.interval` / `include_nics` / `exclude_nics` / `mount_points`（**待确认**：xrayr-agent 当前版本是否消费 `monitor` 与顶层扩展键）。

## 安全限制

- 未知 query → 400。  
- 危险字符路径 → 400。  
- Token 不写入 `operation_log` 的 `detail_json`（仅 `node_id`、`has_proxy`）。  
- 脚本内不出现用户 query 的原始拼接执行路径。  
- **注意**：若全局 HTTP Logger 记录完整 Request URI，可能把 `token` 记入访问日志 → **待确认** 是否需对该路径脱敏或关闭 query 日志。

## 验证结果

| 项 | 结果 |
|----|------|
| `go test ./...`（`xrayr-center` 模块） | 通过（含 `handlers_install_node_public_test.go`） |
| `docker compose up -d --build` | 未在本环境执行，请在部署环境自测 |
| 手工 `curl` 场景（对照需求） | 建议在运行 Center 后自测：无 token / `os=windows` / 未知参数 / `install_dir` 含 `;` / `interval=3` / `interval=10` |

## 待确认事项

1. **访问日志**：`chi`/`middleware.Logger` 是否记录 query 中的 `token`，是否需要专项脱敏。  
2. **github_proxy**：与各镜像站 URL 规则是否完全一致（当前为 `prefix + "/" + fullAgentURL`）。  
3. **use_center_config**：是否新增「仅持 install_token 的公开拉配置」接口（否则只能注册后走 HMAC 通道）。  
4. **XrayR 制品**：是否在脚本生成时按 `node_id` / `arch` 解析 `xrayr_binary_artifact` 并嵌入**只读**下载 URL（仍须防篡改与防盗链）。  
5. **agent.yml 扩展字段**：与 xrayr-agent 配置结构对齐及 `yaml` 严格 unmarshaling 策略。  
6. **KEEP_CONFIG**：与真实替换流水线联动的具体 shell 行为。

---

*编码：UTF-8*
