# 用 REA 逆向一个 180MB 的 Go CLI:agy(Google Antigravity 命令行）内部长什么样

> 本教程只做静态/动态分析学习，不修改二进制、不涉及任何规避服务端风控的内容。分析对象是自己机器上合法安装的 agy 1.3.3。
>
> 本仓库的 `main.go` 把第 3、4 节的手工步骤自动化成一个命令：`go run . <binary> ./out` 直接产出种子文件。

## 0. 背景

`agy` 是 Google Antigravity 的命令行 AI agent(gemini-cli 的 fork 系）。之前我们用黑盒抓包摸清了它的网络行为（请求形状、TLS 指纹、钥匙串登录）。这次换个视角：**直接拆二进制**，回答三个黑盒回答不了的问题：

1. 登录判定逻辑到底怎么写的？
2. 资格检查（ineligibleTiers）在代码哪个位置、怎么生效？
3. 二进制里还藏着什么端点和 flag？

工具：REA(github.com/morluto/rea)，一个把反汇编器（Ghidra/Hopper/IDA）包装成 MCP 工具 + CLI 的开源项目，让 agent 可以像查文档一样查二进制。

## 1. 环境搭建（含三个真实的坑）

```bash
npm install --global rea-agents@6.4.0
```

**坑 1：裸 `npm i -g rea-agents` 给我装了 4.0.1**,`npm view` 显示 latest 是 6.4.0。显式指定版本解决。

**坑 2:REA 对 Node 版本挑剔**——只支持 22.x(≥22.19)/24.x(≥24.11)/26+，我这台机器的 Node 25.9 被 `rea doctor` 判为 missing_dependency。CLI 实际能跑，但你知道它没保证。

**坑 3:Ghidra 官方发布包不带 macOS 原生反编译器**。12.1.4 的 zip 里 `Decompiler/os/` 只有 linux_x86_64 和 win_x86_64。官方 GettingStarted 写明 macOS 要自己编译：

```bash
brew install openjdk@21          # Ghidra 12 要 JDK 21+
cd ~/tools/ghidra_12.1.4_PUBLIC/support/gradle
JAVA_HOME=/opt/homebrew/opt/openjdk@21/libexec/openjdk.jdk/Contents/Home ./gradlew buildNatives
# 1m39s 后得到 Ghidra/Features/Decompiler/build/os/mac_arm_64/decompile
```

然后配环境变量让 REA 找到它：

```bash
export GHIDRA_INSTALL_DIR=~/tools/ghidra_12.1.4_PUBLIC
export JAVA_HOME=/opt/homebrew/opt/openjdk@21/libexec/openjdk.jdk/Contents/Home
rea doctor --provider ghidra --json   # 全绿
```

## 2. 零依赖侦察：REA 自带检查器

不需要任何反汇编器的命令先跑一遍：

```bash
rea inspect-artifact ~/.local/bin/agy --json    # 文件身份:Mach-O arm64, SHA-256
rea inspect-macho ~/.local/bin/agy --json       # 段/节布局、加载命令、依赖
```

`inspect-macho` 的输出暴露了一个关键异常：这不是标准 Go 链接器产物——

- `__lrodata_gopcln`(62.5MB)而不是标准的 `__gopclntab`
- `__go_buildinfo`、`__go_fipsinfo`(FIPS/boringcrypto 指纹）、`__go_module`、`__go_type`、`__go_func`

`go version -m` 确认：`go1.28-20260921-RC02 cl/985595000 +644cd61a4a X:fieldtrack,boringcrypto,simd` —— **Google 内部工具链**（带 fieldtrack/boringcrypto 实验特性，公网根本没有这个版本）。

（题外话：REA 文档里的 `inspect-go-binary` 命令能直接读 Go buildinfo，但它只在 main 分支，npm 6.4.0 还没发布——读文档记得对版本。)

## 3. 转折点：符号剥了，但 pclntab 在

`go tool nm` 空空如也——符号表剥了。但 Go 二进制的 pclntab( panic/栈展开表）里**保留着每个函数的入口地址和名字**。gore/redress 这类现成工具全部失灵，因为它们只认 `__gopclntab` 这个 section 名，而内部工具链把它叫 `__lrodata_gopcln`。

绕过办法：自己按偏移把 section 抠出来，喂给 Go 标准库 `debug/gosym`（本仓库的 `main.go` 就是这件事的完整实现，Mach-O/ELF/PE 都支持）:

```go
data, _ := os.ReadFile("/tmp/pclntab.bin")   // 按 Mach-O section 偏移抠出
lt := gosym.NewLineTable(data, 0x1000011c0)  // __text 起始地址
t, _ := gosym.NewTable(nil, lt)
for _, fn := range t.Funcs { fmt.Printf("%x %s\n", fn.Entry, fn.Name) }
```

**15 秒，157,992 个函数名全部恢复。**包路径直接暴露内部仓库结构：`google3/third_party/jetski/cli/...`、`google3/google/internal/cloud/code/v1internal/...`。

## 4. 绝杀：把函数名当"种子"喂回 REA

REA 的 Ghidra provider 支持 `REA_GHIDRA_SEED_FILE`——一个 `地址<TAB>function<TAB>名字` 的文本文件，导入时由预分析脚本批量创建并命名函数（上限 100 万条 / 32MB)。用本仓库工具一条命令生成（154,155 条，18MB):

```bash
go run . ~/.local/bin/agy ./out
# out/seeds.txt 即 REA 种子文件:
# 0x1000011c0	function	internal/abi.(*RegArgs).Dump
# 0x1000013b0	function	internal/abi.(*RegArgs).IntRegArgAddr
# ...
export REA_GHIDRA_SEED_FILE=$PWD/out/seeds.txt
```

于是这个"符号已剥"的 180MB 二进制，在 Ghidra 里以**全量真实函数名**打开。逆向 Go 二进制的体验从"猜 FUN_0012ab34 是干嘛的"变成"直接搜 waitForEligibility"。

## 5. 深度分析：REA 的 MCP 会话模式

REA 的 CLI 每次调用都是一个新进程（Ghidra 分析要重跑），所以交互式深挖要走 MCP——`rea --mcp` 起 stdio server，一个会话里 `open_binary` 一次，然后随便查：

- `search_procedures` / `search_strings` —— 全局搜函数名/字符串
- `procedure_pseudo_code` —— 反编译单个函数
- `procedure_callers` / `procedure_callees` / `xrefs` —— 调用关系
- `annotate_native_function` —— 写注释/改名

我们对登录链、资格链、端点构造函数做了定点反编译。46 分钟全量分析（154k 函数种子 + 自动分析）之后，单个函数的伪代码查询都是秒回。

## 6. 发现了什么

### 6.1 登录判定链（反编译实证）

`auth.RunLogin` 的逻辑树：

1. 先看环境变量 `AGY_ADC_AUTH=true` → 走 Application Default Credentials(`FindDefaultCredentialsWithParams`)，完全绕开浏览器；
2. 否则 `defaultTokenStorage()` 拿存储层，`ValidateStoredToken` 校验存量 token——**能验过就直接成功，不开浏览器**;
3. 都不行才走交互式 OAuth,token 到手后 `ExtractOIDCClaims` 取邮箱，打印 `Successfully authenticated as %s`。

`getOauthParams` 按 auth method 硬编码两套 OAuth client:`consumer`（个人号，client id 1071006060591-…）和 `gcp`(884354919052-…)；其他方法名直接报 `unknown auth method`。两套 client 的 secret 都嵌在二进制里（桌面应用惯例，此处不引用）。

存储层是 `compositeTokenStorage`：钥匙串优先、文件兜底，外加一套熔断——`shouldBypassKeyring` 用 500ms 探测钥匙串可用性，`keyringRecentlyUnavailable` + `keyring-marker-*` 标记文件（原子 temp+rename+chmod 写入）让后续启动跳过会卡死的钥匙串。**这就是"macOS 钥匙串弹窗卡死 CLI"这个已知坑的官方熔断机制**，黑盒永远看不到。

### 6.2 资格检查（ineligibleTiers）的位置与真实语义

- proto 字段：`LoadCodeAssistResponse.GetIneligibleTiers`
- 消费点：`cli/backend` 的 `AuthStatus.EligibilityError` → `IneligibilityFromResult`，按 ineligible tier 的类型映射成四条用户文案：
  - `Your account is blocked due to a Terms of Service violation.`
  - `Verification required. Please complete verification in your browser to continue.`
  - `Unable to verify account eligibility.`
  - `Unknown error. Please try signing in again.`
- 两个等待点都是**超时放行**:
  - `store.waitForEligibility`:30s 超时 + 50ms 轮询，超时日志 `Timed out waiting for the account-eligibility verdict; sending anyway`
  - `printmode.waitForEligibility`:60s 超时，日志 `Print mode: eligibility verdict did not settle within %s; proceeding`

**结论：资格判定是客户端异步自查，拿不到结论就放行**——这和我们黑盒观察到的"Google 服务端不拦、App 不拦、只有 CLI 自己拦"完全互证。

### 6.3 藏起来的东西

- **四套 Cloud Code 环境**:prod / daily / autopush / preprod 全在二进制里；`CLOUD_CODE_URL` 环境变量可以整个换掉 base URL（二进制内有对应日志）。CLI 默认打 daily——这是黑盒先发现、二进制后确认的闭环案例。
- **自动更新器**：独立 Cloud Run 服务 `antigravity-cli-auto-updater-974169037036.us-central1.run.app`,manifest 格式 `%s/manifests/%s_%s%s.json`（按 OS/arch 分发）。
- **11 个对外隐藏的 flag**(`main.hideFlagsForExternal` 逐个置 Hidden):`cdp_port`、`local_chrome_user_data_dir`、`local_chrome_headless`、`use_local_chrome`（本地 Chrome CDP 控制）、`csrf_token`、`host_bridge_url`、`read_host_bridge_token_from_stdin`、`use_host_auth`(host bridge 鉴权通道）、`app_data_dir`、`gemini_dir`、`bg-updater`；同一函数把 `remote-control` 反向设为可见。
- **特性开关走 Unleash**：端点 `antigravity-unleash.goog`(.goog 内部域名）。
- **未发布模型代号**:RIFTRUNNER / GENTLEISLAND / HORIZONDAWN / COSMICFORGE / INFINITYBLOOM / NEMOSREEF / ORIONFIRE / PUREPRISM / RAINSONG，以及 gemini-3.5/3.6/3.7/3.8-flash 系列型号字符串，1650+ 个 MODEL_PLACEHOLDER 占位枚举，甚至混着 Claude/GPT-OSS 的枚举值。
- **内嵌 Google 内部 agent 安全监控配置**:`agy_tool_safety`、`sir_flash_lite`、`severe_internal_risks_agy` 三套监控器的完整 prompt 和 allowlist 直接打在二进制里（标着 experimental_only)。

### 6.4 REA/Ghidra 的诚实局限

- `search_strings` 只覆盖 Ghidra 认定的字符串（以 NUL 结尾的 Data);Go 字符串大量缺席——`CLOUD_CODE_URL` 在 REA 里搜不到，得用 `strings(1)` 兜底。两者要搭配用。
- 反编译窗口里字符串字面量的地址偶尔解析错乱（显示成一大坨相邻 rodata)；按地址手动回读文件即可（这个二进制 addr−0x100000000=file offset 恒成立）。
- CLI 每次调用都重跑分析，180MB 二进制约 46 分钟——交互式深挖必须走 MCP 长会话，这正是 REA 的主设计形态（`npx rea-agents setup` 注册给 agent 用）。

## 7. 运行态验证：capture-process

REA 还能用 PTY 抓进程行为（终端帧、退出码、文件系统前后快照、定时输入事件）:

```json
{"executable": "~/.local/bin/agy", "arguments": ["--version"],
 "terminal": {"columns": 100, "rows": 30}, "timeout_ms": 15000}
```

`rea capture-process scenario.json --json` 返回完整证据链，可以两次运行做 diff(compare-process-captures)。

也有一个 LLDB 函数跟踪器（observe-native-calls)，但它的断点按符号名创建——对剥了符号的 Go 二进制没用，除非先重符号。这是诚实的局限。

## 8. 黑盒抓包 vs 二进制逆向：心得对比

我们之前对 agy 做过完整的黑盒抓包（本地 TLS 记录器对比 JA3/请求头/请求体）。这次二进制逆向下来，两种方法的互补关系非常清晰：

**黑盒能回答的，二进制不一定能**:TLS 指纹、请求头顺序、实际发送时机——这些是运行态事实，二进制里只能看到"代码 capability"，看不到"这次运行走没走到"。比如我们抓到 CLI 默认打 `daily-cloudcode-pa`，二进制里 prod/daily/autopush 三个端点都在，不跑不知道默认选谁。

**二进制能回答的，黑盒永远看不到**:
- **决策逻辑**。`ineligibleTiers` 黑盒只能看到"返回了什么";二进制里能看到它在哪个函数被消费、什么条件下拦用户、超时兜底是什么。
- **没走到的路径**。企业 SSO 登录（externalCorpLoginAuth)、ADC 兜底、gatewayAuth——这些代码路径我们账号永远触发不到，抓包永远抓不到。
- **隐藏开关**。`main.hideFlagsForExternal` 说明存在"只给内部用户看的 flag";1650 个 MODEL_PLACEHOLDER 和未发布模型代号，抓包一个都看不到。
- **设计意图**。钥匙串超时标记文件（keyringRecentlyUnavailable/memoizeKeyringTimeout）说明作者踩过"钥匙串卡死拖垮 CLI"的坑，专门做了熔断——这种工程决策只有读代码才看得见。

**效率对比**：抓包适合"验证行为"，逆向适合"理解意图"。而 REA 这类工具的价值在于把逆向的边际成本降到接近抓包——agent 直接问"搜所有引用 CLOUD_CODE_URL 的地方"，不用自己学 Ghidra 脚本。

**最佳实践是闭环**：二进制里发现假设（"这个 flag 控制 X")→ 黑盒实验验证（"设上它，抓包看行为变了没")→ 再回到二进制确认实现。单靠任何一边都容易自欺。

## 9. 坑位速查表

| 坑 | 解法 |
|---|---|
| npm 裸装到旧版 4.0.1 | `rea-agents@6.4.0` 显式指定 |
| Node 25 不支持 | 22/24/26;CLI 实际可用但 doctor 报错 |
| Ghidra 无 macOS decompiler | `support/gradle ./gradlew buildNatives`(需 JDK 21) |
| gore/redress 不认内部工具链 | section 改名了；手动抠 `__lrodata_gopcln` + debug/gosym |
| 文档里的 inspect-go-binary 不存在 | main 分支功能，6.4.0 未发布 |
| CLI 每次调用重跑分析 | 交互深挖用 `rea --mcp` 长会话 |
