# XrayR Center 一键部署指令弹窗 — Phase 1 结项说明

## 修改文件

| 文件 | 说明 |
|------|------|
| `xrayr-center/internal/httpapi/static/app.js` | 一键部署弹窗逻辑、命令拼接、列表/卡片/详情入口 |
| `xrayr-center/internal/httpapi/static/style.css` | 大弹窗、OS Tab、选项双列、命令预览、全宽复制按钮等样式 |

## 新增函数与行为（app.js）

| 名称 | 作用 |
|------|------|
| `extractRegisterTokenFromInstallCurl(curl)` | 从 `issue-install-token` 返回的 `install_command_curl` 中解析 `-t` 后的 register token（支持单引号 / 双引号包裹，或裸十六进制） |
| `isLikelyInstallRegisterToken(t)` | 判断解析结果是否为可信令牌（排除占位符、长度与十六进制字符集） |
| `readDeployModalForm()` | 从弹窗 DOM 读取各勾选框与条件输入框 |
| `buildOneClickInstallCommand(origin, token, f)` | 按规则拼接 `curl … "/api/public/install-node.sh?…" -o … && bash …` |
| `updateDeployModalCommand()` | 将预览写入 `#dc-cmd` |
| `closeDeployOneClickModal()` | 清空 `#modal-root` 与内存中的 `deployOneClickToken` |
| `wireDeployOneClickModal()` | 绑定遮罩关闭、复制（优先 `navigator.clipboard.writeText` + Toast「复制成功」）、表单 `input/change` 实时刷新命令 |
| `renderDeployOneClickModalShell(msg)` | 加载中 / 错误时的轻量壳 |
| `renderDeployOneClickModalForm()` | 完整表单 HTML（Linux Tab 激活；Windows/macOS 禁用并标注「暂未支持」） |
| `syncDeploySubRows()` | 勾选后显示/隐藏附属输入框并刷新命令 |
| `openOneClickDeployModal(nodeId, opts)` | 打开流程：详情页且当前节点 `install_command_curl` 中已有有效令牌则不再签发；否则 `POST /api/admin/nodes/{id}/issue-install-token` |

模块级变量：`deployOneClickToken`（关闭弹窗后置 `null`）。

## UI 效果说明

1. **节点列表（表格视图）**：新增「操作」列，每行「部署指令」主按钮；点击不进入详情（`stopPropagation` + 行点击忽略 `.dc-row-deploy`）。
2. **节点列表（卡片视图）**：卡片底部「部署指令」；点击卡片其它区域仍进入详情；点按钮仅打开弹窗。
3. **节点详情**：标题行右侧 `detail-actions` 内「部署指令」主按钮，与「高级：install-script.sh」并列。
4. **弹窗**：宽度 `min(960px, 92vw)`，顶栏标题「一键部署指令」+ 关闭；三列 OS 分段按钮；安装选项双列卡片式勾选；附属输入仅在勾选后出现；命令区 `<pre>` 等宽、可滚动；底部全宽「复制」主按钮；脚注说明当前 `install-node.sh` 尚未对接后端。

## 命令生成规则（Phase 1）

- **Host**：`window.location.origin`（去掉末尾 `/` 后再拼路径）。
- **路径**：`/api/public/install-node.sh`（后端未实现，仅生成目标 URL）。
- **固定参数**：`token`（仅出现在命令中）、`os=linux`。
- **可选参数**：仅当用户勾选对应项时追加；布尔类为 `…=true`；带输入项需勾选且（若适用）非空才追加。
- **编码**：键与值均使用 `encodeURIComponent`。
- **insecure_tls**：为 `true` 时下载脚本使用 `curl -fsSL -k`（仅影响该 curl，与需求一致）。
- **令牌来源**：接口未返回独立 `token` 字段时，从 `install_command_curl` 解析；解析失败时在弹窗内展示错误文案。

## 验证结果

| 项 | 结果 |
|----|------|
| `node --check xrayr-center/internal/httpapi/static/app.js` | 通过 |
| `go test ./...`（在 `xrayr-center` 模块目录下） | 通过 |
| `docker compose up -d --build` | 未在本机执行；建议在集成环境按任务要求自测 Center 页面 |

## 后续 Phase 2 待实现内容

1. **后端** `GET /api/public/install-node.sh`：白名单 query、参数校验、安全脚本体（禁止任意 shell / 非白名单拼接）。
2. **安装脚本**：按设计文档实现 XrayR 检测、备份、`agent_only` / `force_replace_xrayr` / `keep_config` / `use_center_config` 等分支。
3. **API 可选增强**：`issue-install-token` 响应增加明文 `token` 字段，可避免依赖解析 `install_command_curl`（需与安全评审一致）。

---

*文档编码：UTF-8*
