# XrayR：VLESS + REALITY + xtls-rprx-vision 与 sniff 路径 VisionReader 兼容修复

## 1. 问题现象

- 环境：XrayR **0.9.8**，xray-core **v1.260327.0**（对应 Release **26.3.27**），对接 Xboard / NewV2board UniProxy；节点 **VLESS + REALITY + TCP**，客户端流控 **xtls-rprx-vision**。
- xray-core 启动正常，客户端 REALITY 握手成功后，服务端进程 **panic** 退出。
- 典型栈：

```text
panic: interface conversion: buf.Reader is *proxy.VisionReader, not *pipe.Reader
github.com/XrayR-project/XrayR/app/mydispatcher.(*DefaultDispatcher).DispatchLink.func1()
    app/mydispatcher/default.go:315
```

- 面板周期同步用户等逻辑在进程崩溃后无法继续。

## 2. 根因

- 在 **`SniffingRequest.Enabled == true`** 时，`Dispatch` / `DispatchLink` 会用 **`cachedReader`** 包装出站链路的 `outbound.Reader`，以便嗅探与按域名覆盖目标。
- 旧实现将 `outbound.Reader` **强制断言**为 `*pipe.Reader`，并依赖其 **`ReadMultiBufferTimeout`** 做短时预读（`Cache`）。
- 新版 xray-core 在 **VLESS Vision（xtls-rprx-vision）** 路径上，出站读端实际类型为 **`github.com/xtls/xray-core/proxy` 包内的 `*proxy.VisionReader`**（实现 **`buf.Reader`**，并 **嵌入** `buf.Reader`，但 **不是** `*pipe.Reader`，也不实现 `ReadMultiBufferTimeout`**）。
- 因此类型断言失败并 panic；与「仅流量统计」无关，属于 **sniff 路径对 reader 具体类型假设过强**。

上游 xray-core 在 **`DispatchLink`** 中通过 **`WrapLink`** 将 `link.Reader` 包一层 **`buf.TimeoutWrapperReader`**，再与 **`buf.TimeoutReader`** 接口配合使用；XrayR 自定义 `mydispatcher` 此前未做等价处理。

## 3. 修改文件

| 文件 | 说明 |
|------|------|
| `app/mydispatcher/default.go` | 去掉对 `*pipe.Reader` 的不安全断言；嗅探路径统一经 **`ensureTimeoutReader`** 适配 |
| `release/config/config.yml.example` | 示例 **`CertConfig`** 默认改为 **`CertMode: none`**，避免未配置云厂商凭据时误用 **dns + alidns** |

## 4. 修改点说明

### 4.1 `default.go`

1. **`ensureTimeoutReader(r buf.Reader) buf.TimeoutReader`**  
   - 若 `r` 已实现 **`buf.TimeoutReader`**（含 `*pipe.Reader`），**原样返回**，行为与升级前一致。  
   - 否则用 **`buf.TimeoutWrapperReader{ Reader: r }`** 包装，由 xray-core 自带逻辑提供 **`ReadMultiBufferTimeout`**，**不引用 `proxy` 包**，避免循环依赖与类型强转。

2. **`cachedReader`**  
   - 内部字段由 `*pipe.Reader` 改为 **`buf.TimeoutReader`**。  
   - `Cache` / `ReadMultiBuffer` / `ReadMultiBufferTimeout` 仍走接口方法，**pipe 专属统计逻辑**在「已是 `*pipe.Reader`」时保持不变；Vision 等场景走 **TimeoutWrapper + 内层 VisionReader** 的兼容路径，**不再 panic**，连接可继续由 `routedDispatch` → `handler.Dispatch` 转发。

3. **`Interrupt`**  
   - 与上游 dispatcher 一致：仅当底层为 **`*pipe.Reader`** 时调用其 **`Interrupt()`**；其它 `buf.TimeoutReader` 实现不在此处强转（避免对 `TimeoutWrapperReader` 误调用不存在的方法）。

4. **`Dispatch` / `DispatchLink`**  
   - **`Dispatch`**（自建 pipe）：嗅探分支仍使用 **`ensureTimeoutReader(outbound.Reader)`**，与 **`getLink`** 产出的 `*pipe.Reader` 兼容。  
   - **`DispatchLink`**（入站传入的 `transport.Link`）：先调用 **`wrapDispatchLinkReader`**（对齐上游 **`WrapLink`** 的 Reader 包装与上下行统计），再构造 **`cachedReader`**；若 Reader 已为 **`buf.TimeoutReader`** 则直接使用，否则回退 **`ensureTimeoutReader`**，避免裸断言 panic。

### 4.2 `config.yml.example`

- 将示例节点 **`CertConfig.CertMode`** 从 **`dns`** 改为 **`none`**，并说明：VLESS+REALITY 本地联调优先 **`none`**，若使用 **`dns`** 需在 **`DNSEnv`** 中配置真实厂商变量。  
- 将占位 **`ALICLOUD_*`** 改为 **`DNSEnv: {}`**，避免复制示例即触发「缺少阿里云凭据」类错误。  
- 注释块中第二段示例的 CertConfig 同步为 **`none` + `DNSEnv: {}`**。

## 5. 验证步骤

1. **编译**

```bash
cd /path/to/xrayr-project
go build -trimpath -ldflags "-s -w -buildid=" -o XrayR .
```

2. **版本**

```bash
./XrayR version
# 期望含：XrayR 0.9.8 (A Xray backend that supports many panels)
```

3. **单元测试（嗅探相关包）**

```bash
go test ./app/mydispatcher/ -count=1
```

4. **实机（关键）**  
   - 节点：**VLESS + REALITY + xtls-rprx-vision**，面板开启需嗅探/路由域名等依赖 sniff 的配置时重点回归。  
   - 确认：**不再出现** `*proxy.VisionReader` / `*pipe.Reader` 的 interface conversion panic；客户端可正常代理；服务端日志中 **用户同步周期任务仍正常执行**。

## 6. 是否影响非 Vision 节点

| 场景 | 影响 |
|------|------|
| 出站 `Reader` 为 **`*pipe.Reader`**（常见 TCP 入站自建 pipe） | **无行为变化**：`ensureTimeoutReader` 直接返回原 reader，不额外包装。 |
| **VMess / Trojan / Shadowsocks** 等未走 Vision 读包装 | **无类型断言 panic**；嗅探逻辑与升级前一致或更安全。 |
| **VLESS + xtls-rprx-vision** | **修复 panic**；嗅探预读通过 **`TimeoutWrapperReader`** 与 Vision 内层协作，**不修改** `xrayr-center/`、`xrayr-agent/` 业务逻辑。 |

---

**结论**：根因是 sniff 路径假设 `outbound.Reader` 必为 `*pipe.Reader`；新版 Vision 使用 `*proxy.VisionReader`。通过 **`buf.TimeoutReader` + `ensureTimeoutReader`** 与上游思路对齐，在 **不引入 `proxy` 包** 的前提下消除 panic，并保留 pipe 场景的原有语义。

---

## 第二轮：sniff cache replay 修复

### 1. 新问题现象

- 第一轮修复后：**不再 panic**，REALITY 握手成功，服务端 access 日志可出现 `accepted tcp:... >> ...`。
- Windows 客户端经本地 SOCKS 访问 HTTPS（如 `curl.exe -x socks5h://... https://www.gstatic.com/generate_204`）时，**schannel: failed to receive handshake, SSL/TLS connection failed**。
- 同机服务端直连 `curl https://www.google.com` / `gstatic` / `cp.cloudflare.com` **均正常**，说明**不是**机房间出口、端口监听或「完全未建链」类问题。

### 2. 为什么不是端口 / 证书 / 服务器出口

| 判断 | 说明 |
|------|------|
| 出口 | 服务端本机对目标站 HTTPS 正常，说明路由与 TLS 出网可用。 |
| 端口 | SOCKS 侧已 `SOCKS5 request granted`，说明入站与转发链路已建立。 |
| REALITY | 日志显示已接受连接并完成到默认 outbound 的调度，REALITY 层握手并非整段失败。 |
| 面板配置项缺失 | 用户侧已排除 Xboard 页面缺项；问题集中在**数据面读路径**。 |

### 3. 根因分析

- 嗅探路径中 **`cachedReader.Cache`** 通过 **`ReadMultiBufferTimeout`** 预读 TLS **ClientHello** 片段，合并进 **`cache`**，后续 **`ReadMultiBuffer`** 须**先耗尽 `cache` 再读底层**，否则对端收不到完整握手首包。
- 第一轮仅在类型上兼容 Vision，但 **`Cache` / `sniffer` 与上游 dispatcher 仍不一致**：例如忽略 **`ReadMultiBufferTimeout` 返回的 error**、固定短超时与过小 payload、未处理 **`protocol.ErrProtoNeedMoreData`** 等，易导致预读与重放节奏与 xray-core **26.3.27** 不一致；在 **`buf.TimeoutWrapperReader` + Vision** 组合下，更容易出现「嗅探阶段与转发阶段对同一字节流消费顺序不符合上游约定」的表现，客户端侧即表现为 **TLS 握手收不到 ServerHello**（schannel 报错）。
- 上游在 **`DispatchLink`** 入口对 **`transport.Link`** 统一执行 **`WrapLink`**：先将 **`Reader`** 包成 **`buf.TimeoutWrapperReader`** 再套 **`cachedReader`**。XrayR 此前未做等价步骤，与上游 sniff 栈**不完全对齐**。

### 4. 修改文件

| 文件 | 说明 |
|------|------|
| `app/mydispatcher/default.go` | 增加 **`wrapDispatchLinkReader`**；**`cachedReader.Cache(b, deadline) error`** 对齐上游；**`sniffer`** 对齐上游（`buf.NewWithSize(32767)`、`cacheDeadline`、`ErrProtoNeedMoreData` 分支）；**`DispatchLink`** 在嗅探前调用 **`wrapDispatchLinkReader`**；嗅探分支内对 **`buf.TimeoutReader`** 使用类型断言 + **`ensureTimeoutReader`** 兜底 |
| `XRAYR_VISION_READER_COMPAT_FIX.md` | 本文档追加第二轮说明 |

### 5. 修改点

1. **`wrapDispatchLinkReader`**：等价于上游 **`WrapLink`** 中与 Reader 相关的部分——**`TimeoutWrapperReader`**、上行 counter、下行 **`SizeStatWriter`**、**`UserOnline`** 的 **`OnlineMap`**，使 **`DispatchLink`** 与 core 的链路与统计语义一致。  
2. **`cachedReader.Cache`**：将预读结果 **`MergeMulti`** 进 **`cache`**；向 sniff 缓冲区拷贝 **`min(r.cache.Len(), b.Cap())`** 字节；**透传 `ReadMultiBufferTimeout` 的 error**（如 pipe 超时），与上游一致。  
3. **`sniffer`**：总预读预算约 **200ms**、递减 **`cacheDeadline`**；**`protocol.ErrProtoNeedMoreData`** 时不增加 **`totalAttempt`**，允许继续读满 ClientHello。  
4. **`DispatchLink`**：在分支判断前先 **`outbound = wrapDispatchLinkReader(...)`**，保证 **`cachedReader`** 底层为 **`buf.TimeoutReader`**；构造 **`cachedReader`** 时使用 **`buf.TimeoutReader` 断言成功则用原值，否则 `ensureTimeoutReader`**，避免非预期类型 panic。

### 6. 验证命令

```bash
cd /path/to/xrayr-project
go build -trimpath -ldflags "-s -w -buildid=" -o XrayR .
./XrayR version
go test ./app/mydispatcher/ -count=1
```

实机（v2rayN：VLESS + REALITY + TCP + xtls-rprx-vision，开启 sniff 的场景）：

```text
curl.exe -x socks5h://127.0.0.1:10808 https://www.gstatic.com/generate_204 -v
curl.exe -x socks5h://127.0.0.1:10808 https://www.google.com -I -v
curl.exe -x socks5h://127.0.0.1:10808 https://cp.cloudflare.com/generate_204 -v
```

期望：**HTTP/2 204 / 200（或 301/302）**，无 schannel 握手失败；服务端无 **`VisionReader` / `pipe.Reader`** 的 interface conversion panic。

### 7. 是否影响非 Vision 节点

| 场景 | 影响 |
|------|------|
| **`Dispatch`** + 自建 pipe | 仍走 **`ensureTimeoutReader(pipe)`**，行为与第一轮一致。 |
| **`DispatchLink`** 非 Vision | 多一层 **`TimeoutWrapperReader`** 与统计，与 **xray-core 上游 `DispatchLink`** 一致，属预期。 |
| VMess / Trojan / Shadowsocks 等 | 不修改协议栈本身；仅统一 sniff 与链路与 core 对齐，**不应破坏**既有节点。 |

**第二轮结论**：HTTPS 失败主要来自 **sniff 预读 / cache 回放 / 与 `WrapLink` 链路与上游不一致`**；通过 **`wrapDispatchLinkReader` + 上游同款 `Cache`/`sniffer`** 修复，在保持 **不引用 `proxy` 包** 的前提下，优先保证 **TLS 字节流完整送达远端**。
