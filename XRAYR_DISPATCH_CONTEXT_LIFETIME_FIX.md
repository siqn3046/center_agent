# XrayR mydispatcher：出站 Dispatch 的 Context 生命周期修复

## 现象

在 `DisableVisionSniffReplay=true`、宿主机 DNS 与出站正常的前提下，访问 Google 仍失败。日志类似：

- `vision_sniff_replay_skipped_by_switch` + `true`
- `default route for tcp:www.google.com:443`
- `vision_outbound_dispatch_ctx_done_before_handler` + `context canceled`
- `proxy/freedom: failed to get IP address for domain www.google.com`

结论：失败并非 DNS / 证书 / REALITY / 端口，而是 **`handler.Dispatch` 使用的 `context.Context` 在调用前已被取消**，导致 freedom 在 DNS lookup / dial 阶段提前退出。

## 根因

`app/proxyman/inbound/worker.go` 中 TCP 连接处理逻辑为：

1. `ctx, cancel := context.WithCancel(w.ctx)`
2. `w.proxy.Process(ctx, ..., w.dispatcher)` —— 入站协议在此内部调用 `dispatcher.DispatchLink`
3. **`Process` 返回后立即 `cancel()`，再 `conn.Close()`**

XrayR 的 `DispatchLink` 原先对 `routedDispatch` 使用了 **`go d.routedDispatch(ctx, ...)`**，即异步调度出站。

因此时序为：

1. `DispatchLink` 启动 goroutine 后 **立即返回 `nil`**
2. VLESS `Process` 随之 **返回**
3. worker 执行 **`cancel()`**，连接级 `ctx` 已取消
4. 延迟运行的 `routedDispatch` / `handler.Dispatch` 拿到 **已取消的 `ctx`**
5. freedom 使用同一 `ctx` 做解析与拨号 → 表现为 `context canceled` 与 DNS 失败

上游 **xray-core** 的 `app/dispatcher/default.go` 中，`DispatchLink` 对 `routedDispatch` 为 **同步调用**（无 `go`），出站会在 `Process` 返回前完成启动，故不存在该竞态。

## 修复要点

### 1. `DispatchLink`：与上游一致，同步 `routedDispatch`

在 `DisableVisionSniffReplay=true` 或关闭 sniff 的分支中，改为 **`d.routedDispatch(mainCtx, outbound, destination)`**，不再 `go`。

在仍走 sniff/replay 的分支中，同样 **整段 sniff + `routedDispatch` 同步执行**，保证在 `Process` 返回前已完成 `handler.Dispatch` 的启动（与上游行为一致）。

### 2. `mainCtx` / `sniffCtx` 拆分

- **`mainCtx`**：在 `Dispatch` / `DispatchLink` 入口保存的入站连接生命周期上下文，用于 **`routedDispatch` → `handler.Dispatch`**。
- **`sniffCtx`**：仅用于 `sniffer(...)`，使用 `context.WithTimeout(context.WithoutCancel(mainCtx), 30s)`，避免将「仅与 sniff 相关的取消/超时」与主出站 ctx 混用；**不**传给 `handler.Dispatch`。

### 3. sniff 错误时仅在「主 ctx 已结束」时中止出站

仅当 `mainCtx.Err() != nil` 且错误为 `context.Canceled` / `context.DeadlineExceeded` 时，才关闭 link 并中止 `routedDispatch`；避免把 sniff 子上下文的超时误判为连接已死。

若子 ctx 已结束而主 ctx 仍有效，打调试日志：`vision_sniff_child_ctx_done`。

### 4. 日志关键字调整

在调用 `handler.Dispatch` 前：

- 主 ctx 仍有效：`vision_outbound_dispatch_main_ctx_ok_before_handler`
- 主 ctx 已结束：`vision_outbound_dispatch_main_ctx_done_before_handler`

`DisableVisionSniffReplay=true` 且路由正常时，预期在 `vision_sniff_replay_skipped_by_switch` 之后能看到 **`vision_outbound_dispatch_main_ctx_ok_before_handler`**，且不应再出现旧的 `vision_outbound_dispatch_ctx_done_before_handler`（已替换为带 `main_ctx` 的命名）。

### 5. `Dispatch`（管道 `getLink` 路径）

未改「无 sniff 时 `go d.routedDispatch`」模式：典型调用方（如 VMess）在 `Dispatch` 返回后仍长时间阻塞在 `task.Run`，`Process` 不会在出站启动前返回，与 VLESS `DispatchLink` 路径不同。

对 **`Dispatch` 中带 sniff 的 goroutine**：同样引入 `mainCtx` + `sniffCtx` 及上述中止条件，避免将来其它竞态下误用已取消 ctx。

## 验证命令

```text
go test ./app/mydispatcher/ -count=1
go build -p 1 -trimpath -ldflags "-s -w -buildid=" -o XrayR .
```

## 服务端验收预期

配置 `DisableVisionSniffReplay=true`，访问 `https://www.google.com`：

1. 不应再出现 `vision_outbound_dispatch_ctx_done_before_handler`（已更名为 `vision_outbound_dispatch_main_ctx_done_before_handler`；成功路径不应出现 `..._done_before_handler`）。
2. 不应再出现 `failed to get IP address for domain www.google.com`（在无其它网络策略问题时）。
3. 可观察到 `transport/internet/tcp dialing TCP to tcp:www.google.com:443` 等正常拨号日志。
4. 浏览器可正常打开 Google。

## 涉及文件

- `app/mydispatcher/default.go`：`Dispatch`、`DispatchLink`、`routedDispatch` 日志与 ctx 使用方式。

---

文档编码：UTF-8。
