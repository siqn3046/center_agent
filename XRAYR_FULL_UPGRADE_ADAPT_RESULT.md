# XrayR 根模块全量升级与适配 — 结论文档

## 1. 本轮目标

在独立分支上对根目录 XrayR 主程序完成 **xray-core / Go / 依赖** 升级与源码适配，使根模块可编译；**不删除、不覆盖** `xrayr-center/`、`xrayr-agent/`、`install.sh` 中的 `siqn3046/center_agent` 仓库地址及 Center/Agent 文档入口。

## 2. 升级前基线（参考）

| 项目 | 旧值（升级前常见状态） |
|------|------------------------|
| Go（`go.mod`） | 1.22.0 |
| xray-core | v1.8.20 |
| 根 `Dockerfile` | `golang:1.22.0-alpine` |
| CI `release.yml` | `go-version: ^1.20` |
| `install.sh` | 校验 Go ≥ 1.21 |

## 3. 升级后版本

| 项目 | 新值 |
|------|------|
| XrayR 主程序（`version` 命令） | **0.9.8**（`cmd/version.go`；升级分支上已由 **0.9.4** 调整） |
| Go | **1.26.1**（`go.mod`） |
| xray-core（Go 模块版本） | **v1.260327.0**（与 GitHub Release **v26.3.27** 对应；官方在 proxy 上使用 `v1.260327.0` 语义化标签，**勿**使用无 `+incompatible` 的裸 `v26.3.27` require） |
| 根 `Dockerfile` | `golang:1.26.1-alpine` |
| CI `.github/workflows/release.yml` | `go-version: "1.26.1"` |
| `codeql-analysis.yml` | 已使用 `go-version-file: go.mod`（随 `go.mod` 自动对齐） |
| `install.sh` | 要求 **Go ≥ 1.26** |
| `README.md` | 安装说明改为 **Go 1.26+** |

主要传递依赖随 `go mod tidy` 已与 xray-core 对齐（如 `golang.org/x/crypto`、`golang.org/x/net`、`google.golang.org/protobuf`、`github.com/stretchr/testify` 等），以根目录 `go.mod` / `go.sum` 为准。

## 4. 修改文件清单

| 模块 | 文件 |
|------|------|
| 版本号 | `cmd/version.go`（`0.9.4` → `0.9.8`） |
| 依赖 | `go.mod`、`go.sum` |
| 发行说明 | `README.md`（仅 Go 版本提示） |
| 安装脚本 | `install.sh`（Go 版本校验与提示；**未改** `REPO`） |
| 容器 | `Dockerfile` |
| CI | `.github/workflows/release.yml` |
| 协议注册 | `cmd/distro/all/all.go`（对齐上游 `main/distro/all`，保留 `mydispatcher` 替代默认 `dispatcher`；补充 `grpc` / `httpupgrade` / `splithttp` / `hysteria` / `taggedimpl` / `observatory` / `fakedns` / `loopback` / `wireguard` 等注册） |
| 调度器 | `app/mydispatcher/default.go`（移除已删除的 `dns.HostsLookup`；对齐上游在路由前行为；`routedDispatch` 中恢复 `ob.Tag = handler.Tag()`；**FakeDNS** 使用 `core.OptionalFeatures`，与上游 `app/dispatcher` 一致，避免未启用 fakedns 时依赖无法解析） |
| 嗅探 | `app/mydispatcher/sniffer.go`（`http.SniffHTTP` 增加 `context` 参数） |
| 入站构建 | `service/controller/inboundbuilder.go`（去掉已删除的 `IVCheck`；`grpc` 使用 `GRPCSettings`；移除已废弃的 `HTTPConfig` / `QUICConfig` 传输分支；将历史 **`http`/`h2`/`h3`/`quic`** 传输名 **映射为 `splithttp`** 并尽量迁移 Host/Path/Headers；**实机需重新验证**） |
| 用户构建 | `service/controller/userbuilder.go`（`shadowsocks_2022.User` → `shadowsocks_2022.Account`） |
| 系统状态 | `common/serverstatus/serverstatus.go`（`fmt.Errorf` 非常量格式串，改为 `fmt.Errorf("%s", ...)` 以满足 vet） |
| 测试 | `service/controller/controller_test.go`（显式构造与 `panel` 一致的 `core.Config` App 链，避免 `conf.Config.Build()` 注入默认 `dispatcher` 导致类型断言失败；去掉无限等待信号）、`service/controller/inboundbuilder_test.go`（补充 `CypherMethod`） |

**未改（按要求）**：`xrayr-center/`、`xrayr-agent/`、`xrayr-center/install-center.sh`、`install.sh` 中的仓库 URL、各 `XRAYR_CENTER_*.md`、`XRAYR_UPGRADE_RESEARCH_AND_PLAN.md`。

## 5. xray-core API / 行为适配说明

| 变更点 | 说明 |
|--------|------|
| 模块版本标签 | 使用 **`github.com/xtls/xray-core v1.260327.0`**；导入路径仍为 `github.com/xtls/xray-core/...`（**无** `/v26` 后缀）。 |
| 默认 `dispatcher` 与 FakeDNS | 上游 `dispatcher` 对 `FakeDNSEngine` 使用 **`core.OptionalFeatures`**；本仓库 `mydispatcher` 已同步，否则 `core.New` 会出现 **not all dependencies are resolved**。 |
| `dns.HostsLookup` | 上游 `routedDispatch` 已移除 hosts 特判；本仓库同步删除，静态 hosts 由新版 DNS/路由链路处理。 |
| `http.SniffHTTP` | 签名增加 **`context.Context`**。 |
| `shadowsocks_2022` 用户消息 | 使用 **`Account{ Key }`**，邮箱/等级仍在 `protocol.User` 上。 |
| `ShadowsocksServerConfig` | 上游去掉 **`IVCheck`** 字段；`DisableIVCheck` 配置项不再映射到该字段（若需等价行为需查阅新版 shadowsocks 实现再定）。 |
| `StreamConfig` | **`HTTPSettings` / `QUICSettings` 等已移除**；gRPC 字段名为 **`GRPCSettings`**。 |
| 传输协议 `http` / `quic` | 上游 **`TransportProtocol.Build()`** 对 `http`/`h2`/`h3`/`quic` 返回移除类错误；XrayR 在 **`InboundBuilder`** 内将上述名称 **归一为 `splithttp`** 并填充 `SplitHTTPConfig`，属**兼容层**，**不保证**与旧 QUIC/HTTP 传输 100% 等效。 |

## 6. 协议与配置适配（摘要）

| 类型 | 说明 |
|------|------|
| VMess / VLESS / Trojan / Shadowsocks | 仍由 `InboundBuilder` / `userbuilder` 生成；SS 测试补全 **`CypherMethod`**。 |
| REALITY / TLS | 既有分支保留；需随官方文档做实机回归。 |
| XHTTP / splithttp / httpupgrade / grpc | `cmd/distro/all` 已注册；入站 transport 映射见上。 |
| Hysteria | 注册 `transport/internet/hysteria`（与上游一致）。 |
| DNS / Routing / Stats / API | 与 `panel.loadCore` 及 `mydispatcher` 统计逻辑一致方向；未改面板 HTTP 合同。 |

## 7. 编译与测试结果

| 命令 | 结果 |
|------|------|
| `go build -o NUL .`（仓库根目录） | **通过** |
| `go test ./service/controller/ -count=1` | **通过** |
| `go test ./app/mydispatcher/ -count=1` | **通过**（此前抽样） |
| `cd xrayr-center && go test ./... && go build -o NUL ./cmd/center` | **通过** |
| `cd xrayr-agent && go test ./... && go build -o NUL ./cmd/agent` | **通过** |
| `go test ./...`（根模块全量） | **未要求全绿**：`api/bunpanel` 等依赖本机面板 HTTP；`common/mylego` 依赖外网 ACME/本地证书文件，在无环境时失败属预期。升级相关改动已通过 **编译** 与 **`service/controller` 单测**。 |
| `docker build -t xrayr-upgrade-test .` | **未执行**（当前为 Windows 环境，未在此轮验证 Docker 构建；`Dockerfile` 已更新为 Go 1.26.1 镜像）。 |

实际生产构建命令（与仓库一致）：

```bash
cd /path/to/xrayr-project
go build -trimpath -ldflags "-s -w -buildid=" -o XrayR .
```

（等价于对 `main` 包执行 `go build .`。）

## 8. 已知风险

1. **大版本 xray-core**：须做 **全协议实机回归**（VLESS/REALITY、Trojan、SS、VMess+WS、gRPC、XHTTP 等）。
2. **`http`/`quic` 传输映射为 `splithttp`**：仅为避免配置阶段直接失败；**不保证**与旧版行为一致，需在文档/运维侧明确迁移路径。
3. **面板与流量**：HTTP API 未改，但数据面行为依赖 xray-core，**流量统计 / 在线 / 限速**需线上验证。
4. **自动 TLS / Lego**：与 `go-acme/lego` 传递依赖相关，升级后首次申请证书建议灰度。
5. **Center / Agent**：本轮未改业务逻辑；制品与 `install.sh` 仍指向本仓库；建议在 Center 上传新构建的 `XrayR` 二进制后跑一轮 **Agent INSTALL/UPGRADE** 演练。

## 9. 回滚方式

```bash
git checkout main
git branch -D upgrade/xray-core-latest
```

若已合并，使用 `git revert <merge_commit>` 或回到升级前 tag。

## 10. 后续实机验收清单

- [ ] V2board / SSPanel 等节点拉取与同步用户  
- [ ] Trojan / VLESS（含 REALITY）/ Shadowsocks / VMess+WS  
- [ ] gRPC / XHTTP（splithttp）/ httpupgrade  
- [ ] 流量上报、在线人数、限速  
- [ ] 自动证书（http/dns/tls 等模式）  
- [ ] 根 `Dockerfile` 镜像构建与运行  
- [ ] `install.sh` 冷机安装  
- [ ] Center 上传新版 XrayR 制品、Agent `UPGRADE_XRAYR`  

## 11. Git 与提交说明

- 当前分支：**`upgrade/xray-core-latest`**（请在合并前 `git status -uall` 确认无意外二进制）。  
- **勿** `git add .`；勿将 `*.exe` 加入索引。  

建议提交（由你本地确认后执行）：

```bash
git add go.mod go.sum Dockerfile .github/workflows/release.yml README.md install.sh cmd conf common core panel service api controller app XRAYR_FULL_UPGRADE_ADAPT_RESULT.md
git commit -m "chore: upgrade xray-core and adapt root module"
```

（若 `go.sum` 或个别路径无变更，可从 `git add` 列表中删去对应项。）

## 12. 本轮说明

- 升级分支显示版本号已从 **0.9.4** 调整为 **0.9.8**；**xray-core** 仍为 **v1.260327.0**，**Go** 仍为 **1.26.1**。
- 已按上游 **`main/distro/all`** 更新空导入，并保留 **XrayR 自定义 `mydispatcher`**。  
- **Center / Agent 子模块**未做逻辑升级，仅验证可编译。  
- 完整 `go test ./...` 受环境依赖限制；**生产门禁**建议 CI 中分 job：`vet` + 核心包单测 + 可选集成。
