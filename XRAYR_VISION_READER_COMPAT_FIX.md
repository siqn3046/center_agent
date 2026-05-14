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
   - 嗅探分支构造 `cachedReader` 时使用 **`reader: ensureTimeoutReader(outbound.Reader)`**，替换 **`outbound.Reader.(*pipe.Reader)`**。

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
