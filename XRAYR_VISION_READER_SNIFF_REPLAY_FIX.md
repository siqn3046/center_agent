# XrayR：Vision / REALITY 下 sniff 与 TLS 回放修复说明

## 1. 问题复现

- 分支：`upgrade/xray-core-latest`。
- 节点：**VLESS + REALITY + TCP**，`flow: xtls-rprx-vision`。
- 服务端 REALITY 握手成功（`hs.handshake()`、`readClientFinished()` 正常，`accepted tcp:目标:443`）。
- Windows 客户端经 **HTTP 代理**（如 v2rayN 本地 `http://127.0.0.1:10808`）执行：

```powershell
curl.exe -x http://127.0.0.1:10808 https://www.google.com -I -v
```

- 现象：`HTTP/1.1 200 Connection established` 后 **schannel: failed to receive handshake, SSL/TLS connection failed**；多域名（Google、Cursor API）一致。
- 服务端本机直连 HTTPS 正常，排除纯出口/端口/密钥配置为根因。

## 2. 根因分析

1. **`buf.TimeoutWrapperReader`**：`ReadMultiBufferTimeout` 在 **超时分支**返回时，内层 `ReadMultiBuffer` 可能仍在 goroutine 中执行；随后 **sniff 与 handler** 对同一 `buf.Reader`（含 **Vision** 路径）的消费顺序若与「超时即视为未读」不一致，会出现 **首段 TLS 字节未完整回灌** 到后续 handler，表现为 CONNECT 成功但 **ServerHello 侧无有效数据**。
2. **`cachedReader`** 依赖底层 **`ReadMultiBufferTimeout`** 的语义：**超时不得丢已成功读入的字节**；上述竞态会破坏该语义。
3. **入站 sniff 默认**：`DestOverride` 含 `tls` 等，且未对 **Vision** 默认 **`routeOnly`**，与官方「仅用 sniff 做路由、避免误改目标」的最佳实践不一致，易放大路由/嗅探与数据面耦合风险。

## 3. 修改文件

| 文件 | 说明 |
|------|------|
| `app/mydispatcher/queued_timeout_reader.go` | **新建**：`queuedTimeoutBufReader`，在超时后 **阻塞吸收** 未完成内层读，并入 **`pending`** FIFO，保证后续 **`ReadMultiBuffer`** 先回放再读内层 |
| `app/mydispatcher/default.go` | `ensureTimeoutReader` 改为包装 **`queuedTimeoutBufReader`**；`wrapDispatchLinkReader` 解包已有 `TimeoutWrapperReader` 后统一 **`queuedTimeoutBufReader`**；**`cachedReader`** 增加 **`logCtx`**、调试日志；**`Interrupt`** 支持 **`queuedTimeoutBufReader`** |
| `app/mydispatcher/sniff_replay_test.go` | **新建**：超时回放、Cache+Read 全量一致、`routeOnly` 无关的回放完整性单测 |
| `service/controller/inboundbuilder.go` | **VLESS + flow 含 `vision`** 时 **`RouteOnly: true`**，与官方「sniff 用于路由、不默认改目标」一致 |

## 4. 修复点

1. **`queuedTimeoutBufReader`**：实现 **`buf.TimeoutReader`**；超时返回 **`buf.ErrReadTimeout`**（与 pipe 语义一致）；下次 **`ReadMultiBuffer` / `ReadMultiBufferTimeout`** 入口 **`absorbInflight`**，将迟到数据 **`MergeMulti`** 到 **`pending`**，避免 **Vision 单消费语义** 下字节丢失。
2. **`wrapDispatchLinkReader`**：若入站已套 **`buf.TimeoutWrapperReader`**，先 **解包内层** 再套 **`queuedTimeoutBufReader`**，避免双层超时包装；统计 **`Counter`** 仍挂在 **`queuedTimeoutBufReader`** 上。
3. **调试日志**（`errors.LogDebug`，不含 UUID/域名等敏感字段，仅协议名与计数）：`vision_sniff_start`、`vision_sniff_bytes_read`、`vision_sniff_replay_bytes`、`vision_sniff_result`、`vision_sniff_dest_override`、`vision_sniff_route_only`、`vision_sniff_handler_reader_ready`、`vision_sniff_error_but_replay_continue`。
4. **`inboundbuilder`**：Vision 流默认 **`routeOnly: true`**，降低 **`destOverride`** 对连接目标的干扰；需关闭 sniff 仍可用现有 **`DisableSniffing`**。

## 5. 测试结果

本地执行：

```bash
cd E:\xrayr-project
go test ./app/mydispatcher/ -count=1
go build -trimpath -ldflags "-s -w -buildid=" -o NUL .
```

- `go test ./app/mydispatcher/`：**通过**（含 `TestQueuedTimeoutReaderTimeoutThenReplay`、`TestCachedReaderSniffThenHandlerRead`、`TestRouteOnlySniffCacheIntact`）。
- 根目录 **`go build`**：**通过**。

全量 `go test ./...` 可能因环境依赖（面板 HTTP、ACME 等）部分包失败；与本次改动无关时以 **`app/mydispatcher`** 与 **`go build`** 为主门禁。

部署后请用客户端复测：

```powershell
curl.exe -x http://127.0.0.1:10808 https://www.google.com -I -v
```

预期：TLS 握手完成并出现 **HTTP 状态行**（如 `HTTP/2 200` / `301`），不再仅 **schannel** 握手失败。

## 6. 回滚方案

```bash
git checkout -- app/mydispatcher/queued_timeout_reader.go app/mydispatcher/default.go app/mydispatcher/sniff_replay_test.go service/controller/inboundbuilder.go
git rm -f XRAYR_VISION_READER_SNIFF_REPLAY_FIX.md   # 若已提交需按分支策略 revert
```

或针对单次提交：`git revert <commit_hash>`。

---

**结论**：通过 **`queuedTimeoutBufReader`** 修复 **`TimeoutWrapperReader` 与 Vision 组合下的 sniff 超时丢字节**；通过 **Vision 默认 `routeOnly`** 降低 sniff 对目标的误改风险；调试日志便于在 **debug** 级别核对读字节与回放路径。
