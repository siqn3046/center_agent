# XrayR Vision Sniff / Replay 与 `lookup canceled` 对照说明

## 现象

系统 DNS 与 `curl` 出站正常，但经 XrayR 转发后出现：

- `proxy/freedom: failed to open connection to tcp:www.google.com:443`
- `dial tcp: lookup www.google.com: operation was canceled`

日志中可见 `vision_sniff_start`（`dispatch` / `dispatchLink`）、`vision_sniff_handler_reader_ready` 与 `default route`，说明问题集中在 **mydispatcher 在 sniff / replay 之后把已取消的 context 交给 outbound**（例如 freedom 在 dial 时使用该 context 做解析），而非面板证书、REALITY 或宿主机 DNS。

## 根因（代码层面）

`app/mydispatcher/default.go` 中 `sniffer` 在 **内容嗅探失败但元数据嗅探成功** 时存在合并分支：

```go
if contentErr != nil && metadataErr == nil {
    return metaresult, nil
}
```

当 `contentErr` 为 **`context.Canceled` / `context.DeadlineExceeded`**（例如在 `select` 中读到 `ctx.Done()`，或嗅探循环因上游取消返回）时，上述逻辑会把错误 **吞掉**，对调用方表现为 `err == nil`，随后仍执行 `routedDispatch` → `handler.Dispatch(ctx, link)`。此时 **`ctx` 已结束**，freedom 等 outbound 在 DNS lookup / dial 阶段即报 `operation was canceled`。

这与「系统 DNS 正常」不矛盾：取消来自 **连接级 / 调度级 context**，而非 `resolv.conf`。

## 修复

1. **不再吞掉 context 类错误**：在 `contentErr != nil && metadataErr == nil` 分支中，若 `contentErr` 为 `context.Canceled` 或 `context.DeadlineExceeded`，改为 **`return metaresult, contentErr`**，并打 debug 日志 `vision_sniff_ctx_err_not_swallowed`。
2. **Dispatch / DispatchLink 嗅探协程**：若 `sniffer` 返回上述 context 错误，则 **不再调用 `routedDispatch`**，关闭 `outbound.Writer` 并对 `outbound.Reader` 执行 `Interrupt`，避免在已取消的 context 上继续拨号；日志 `vision_sniff_ctx_dead_abort_routed_dispatch`。

## 临时开关（对照实验）

在 **面板根配置**（与 `Nodes` 同级）增加：

```yaml
DisableVisionSniffReplay: true
```

`panel` 在 `loadCore` 内、创建 `core.Instance` 之前会调用：

`mydispatcher.SetDisableVisionSniffReplay(panelConfig.DisableVisionSniffReplay)`

为 `true` 时：即使入站仍开启 Sniffing（含 VLESS Vision + `routeOnly`），**mydispatcher 也会跳过 `cachedReader` / `sniffer` / replay**，行为与「对该连接关闭 dispatcher 侧 sniff」一致，用于验证关闭 sniff 后访问 Google 等站点是否恢复。

> 注意：此为进程级全局开关；多节点共进程时一并生效。

## 调试日志（需将 core 日志级别开到 debug）

| 关键字 | 含义 |
|--------|------|
| `vision_sniff_start` | sniff 开始；含 `start_epoch_ms`、`dispatch` / `dispatchLink`、`routeOnly` 等 |
| `vision_sniff_replay_skipped_by_switch` | 因 `DisableVisionSniffReplay` 跳过 sniff/replay |
| `vision_sniff_metadata_err` | 元数据嗅探错误 |
| `vision_sniff_return_err` | `sniffer` 返回给协程的错误 |
| `vision_sniff_ctx_dead_abort_routed_dispatch` | 因 context 已结束，中止后续 `routedDispatch` |
| `vision_sniff_ctx_err_not_swallowed` | 合并分支中不再吞掉 context 错误 |
| `vision_sniff_bytes_read` / `vision_sniff_replay_bytes` | 已缓存读入与回放到 handler 的字节量 |
| `vision_outbound_dispatch_ctx_ok_before_handler` / `vision_outbound_dispatch_ctx_done_before_handler` | 调用 `handler.Dispatch` 前 `ctx.Err()` 是否已非 nil（对照 freedom dial 前 context 状态） |

## `queued_timeout_reader` 与 cancel

`queued_timeout_bufReader.ReadMultiBufferTimeout` 在超时时返回 `buf.ErrReadTimeout`，**不会取消**入站传入的 `context`；超时后未读到的字节由后台 goroutine 填入 `pending`，供后续读取 **重放**。若仍出现 `canceled`，应优先查 **context 生命周期** 与上述 **sniffer 错误吞掉** 问题，而不是把超时误判为 DNS 故障。

## 建议验证步骤

1. 设 `DisableVisionSniffReplay: true`，重启 XrayR，访问 `https://www.google.com`。若恢复，说明问题与 **dispatcher 侧 sniff/replay + context** 强相关。
2. 关闭该开关，拉取含本修复的版本，开 debug，确认不再在 `ctx` 已取消时仍进入 `routedDispatch`。
3. 若两步后仍失败，再查 **Xray 内 DNS 客户端**、routing 内 `domain` 策略等（与本文证书/REALITY/系统 DNS 结论解耦）。

## 涉及文件

- `app/mydispatcher/default.go`：context 传播、日志、`Dispatch` / `DispatchLink` 分支。
- `app/mydispatcher/vision_sniff_switch.go`：全局开关。
- `panel/config.go`、`panel/panel.go`：YAML 项与启动时注入。
