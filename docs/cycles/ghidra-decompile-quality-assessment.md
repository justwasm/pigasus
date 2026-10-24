# 1.3.3 / 1.3.4 Ghidra decompile 质量评估

## TL;DR
**有核心逻辑代码 (control flow + function call), 但没有 Go 语义**. Ghidra 反汇编 C 伪码是机械变换,
不是 idiomatic Go. 可以用, 但要人工理解 + 大量重写.

## 1.3.3 抽 860 .c (22 packages)

### 包分布 (22 packages, 全 jetski/cli/*)
- `google3/third_party/jetski/cli/backend/auth/auth` (auth 流程, OAuth + ADC)
- `google3/third_party/jetski/cli/backend/backend` (AgentModel, ContextWindow)
- `google3/third_party/jetski/cli/commands/commands` (命令注册)
- `google3/third_party/jetski/cli/printmode/printmode`
- 等等, 全是 Antigravity 业务代码

### 单文件示例 (`fetchAdcUserInfo` - ADC 用户信息获取)
```c
// Source: 0x86a62c0  google3/third_party/jetski/cli/backend/auth/auth.fetchAdcUserInfo
void google3_third_party_jetski_cli_backend_auth_auth_fetchAdcUserInfo(uint param_1, uint param_2)
{
  int iVar1;
  char cVar2;
  undefined8 extraout_RAX;
  ...
  param_1 = param_1 & 0xffffffffffffff48;
  while( true ) {
    param_2 = param_2 + 1;
    if (in_stack_00000050 < param_2) { ... }
  }
}
```

**特征**:
- ✅ 有 control flow (while / if / for)
- ✅ 有函数 call (`func_0x05f99480()` 是 Ghidra unknown call stub)
- ✅ 有 parameter 跟 local var
- ❌ **无类型**: 全 `int / uint / undefined8` (Ghidra "未识别" 标识)
- ❌ **无 Go 语义**: interface / channel / goroutine / slice 全没了
- ❌ 大量 **未解析的 stack var**: `in_stack_00000050`, `in_stack_000000a8` 之类
- ❌ **Google 内部名 sanitized**: `fetchAdcUserInfo` -> `google3_third_party_jetski_cli_backend_auth_auth_fetchAdcUserInfo`

## 1.3.4 抽 1,289 .c (52 packages)

### 0 个 jetski/cli/* 包!
- Cycle 25 抽 1.3.4 priority 改为 **grpc + aiplatform** (不再 jetski/cli/* 业务代码)
- 主要包:
  - `google3_security_credentials_proto_authenticator_go_proto` (355 .c, largest)
  - `google3_third_party_golang_grpc_internal_transport_transport` (190 .c)
  - `google3_google_api_client_go_proto` (130 .c)
  - `google3_google_cloud_aiplatform_master_auth_go_proto` (69 .c)
  - `google3_third_party_golang_grpc_internal_channelz_channelz` (63 .c)
  - 等等

### 大部分是 **proto getter/setter** (样板代码)
```c
// Source: 0x6458520  google3/security/credentials/proto/authenticator_go_proto.GaiaAuxCookiesProto_CookieOrigin.Enum
void google3_security_credentials_proto_authenticator_go_proto_GaiaAuxCookiesProto_CookieOrigin_Enum(void) {
  return;
}
```
**9 行 1 个 return, 64 byte 二进制 protobuf enum stub**.

### 少量真有逻辑的 (channelz.String 168 行)
```c
void google3_third_party_golang_grpc_internal_channelz_channelz___Channel__String
               (undefined8 param_1, int param_2, int param_3, int param_4, uint param_5) {
  // ... 168 行 gRPC channel state 序列化逻辑
  // 包括 state traversal, child channel recursion
}
```
**gRPC 内部状态机**, 不是 Antigravity 自身业务.

## 跟 recovered 1:1 字节级对比

cycle 25-26 我们做 recovered 1:1 byte-for-byte 对比:
- `recovered` 不基于 ghidra decompile .c (那是反汇编 C 伪码, 不是 Go)
- 而基于 **strings cache + pclntab funcs + REA context** (REPL 真实测试 + 7 AuthProvider + mTLS live test)
- recovered 有完整 Go 语义 (interface / channel / slice), 跟 v4 binary 1:1 byte-for-byte CLI surface
- ghidra decompile .c 是 **辅助分析** 工具, 不是 recovered 工程的直接输入

## 结论
- **有** 核心控制流 (有 while / if / 函数 call)
- **有** 一些真实业务逻辑 (1.3.3 fetchAdcUserInfo OAuth 流程, 1.3.4 channelz gRPC 状态)
- **没有** Go 语义 (type / interface / channel)
- **没有** 完全可读的代码 (需要 Ghidra 经验 + Go 经验 + Google 内部 knowledge)
- **不能** 直接编译 .c 文件 (没 stdlib include, 没真实类型)
- **不能** 作为 recovered 工程输入 (recovered 走 strings + pclntab 路线, 不走 ghidra)

## 1.3.5 计划
- TODO.md 已 commit: 走跟 1.3.4 同样 direct analyzeHeadless 路线, ~10 min 跑 1,289 .c (52 packages)
- 抽 priority funcs 跟 1.3.4 同 (grpc + aiplatform + clouds)
- 但 **必须新增 jetski/cli/* priority** (1.3.4 cycle 25 漏抽 882 个 jetski .c, 1.3.5 应该有 1000+)
