# pigasus composer 按 Enter 卡顿 —— 根因调查

## 结论（一句话）

按 Enter 卡的那几秒**不是网络、不是模型、也不是渲染**，而是
`saveSession()` 在 **Update goroutine 上同步**把**整条会话历史**重写进 SQLite：
每条 step 一次 `Exec`（自动提交 → 每条一次 fsync），再叠加 summaries 库的全表自愈
UPDATE、FTS5 全文重建、100 行修复扫描。**耗时随会话长度近似线性增长**，
所以无论有没有任务在跑，都会卡。

## 实测数据

### Enter → Update 返回的耗时（真实数据目录 + 真实 3900 step 会话）

| 会话 step 数 | `Enter` 期间 Update 阻塞 | 紧随的 `View()` |
|---|---|---|
| 20 | 66 ms | 0.57 ms |
| 200 | 175 ms | 0.54 ms |
| 1,000 | 663 ms | 0.67 ms |
| 3,903 | **3.30 s** | 0.53 ms |

> 渲染完全不是瓶颈（亚毫秒级）。卡顿 100% 在 Update 里。

### 每个新回合都会重新付一次这个代价

`TestPerTurnSaveCostTmp`（真实 3900 step 会话，模拟「发送→回合结束→再发送」）：

```
turn #0: Enter Update took 879ms
turn #1: Enter Update took 608ms
turn #2: Enter Update took 422ms
turn #3: Enter Update took 514ms
```

> 注意：只有**新回合**会走 `startModelTurn`；流式中的 Enter 走
> `checkStreamingQueue`，只 push 到内存队列，所以很快。这解释了
> 「空闲时也卡」——空闲时按 Enter 恰恰就是开新回合。

### 3,903 step 会话的 saveSession 内部归因

| 子调用 | 耗时 | 说明 |
|---|---|---|
| `WriteConversation`（3903 条 autocommit `Exec`） | **2.20 s** | 每条一次隐式 fsync |
| `updateConversationSummariesDB` | 353 ms | 全表自愈 UPDATE + FTS5 建 24 MB 全文 + 100 行修复 |
| `WriteSummariesTitle` | 78 ms | 另开连接 + FTS5 行 UPDATE |
| `updateConversationMetadata` | 3.5 ms | 读改写 309 KB JSON |
| `updateTrajectoryMetadataBlob` | 0.6 ms | — |

self-heal 的三条全表 UPDATE 单独在真实 17,620 行的 summaries 库上就是 **110 ms**
（无索引 `WHERE app_data_dir=''` 全扫）。

## 调用链

```
tea.KeyEnter
 └ updateKeyMsg → handleEnterKey           (tui_update_keys_edit.go:104)
    └ submitPrompt                          (tui_prompt_submit.go:663)
       └ startModelTurn                     (:562)   ← 同步，在 Update goroutine 上
          ├ syncAccountIdentity             (server_host.go:145)
          │    └ accounts.Current() → auth.LoadTokenUnified()
          │         └ keychain Get → ~20 ms（每次）
          ├ drainTaskNotifications
          ├ resetLiveTurnState
          ├ appendUserTurn
          └ saveSession                     (:834)   ← 真正的瓶颈
               └ sup.Save()                 (supervisor.go:369)  持有 supervisor 写锁
                    ├ convstore.BuildSteps    全量重建 step 切片
                    └ convstore.SaveConversation   (write.go:120)
                         ├ WriteConversation          ← 2.2 s
                         ├ updateTrajectoryMetadataBlob
                         ├ updateLastConversations
                         ├ updateConversationMetadata
                         ├ updateConversationSummariesDB   ← 353 ms
                         │    ├ 3 条全表自愈 UPDATE
                         │    ├ updateConversationsFTS（全文重建）
                         │    └ repairRecentFTSHeaders（100 行 × 最多 3 条语句）
                         └ WriteSummariesTitle / WriteCacheTitle   ← 78 ms
```

`saveSession` 是**直接调用**，不是 `tea.Cmd`，所以整条链路卡在 UI 线程上；
Bubble Tea 在 Update 返回前不会渲染、不会处理下一个按键。

## 为什么 agy 是「按下回车立刻发送」

agy 是原生二进制，按 Enter 时只把消息投递出去、**状态增量追加**；
它不会在每次发送时把整条 trajectory 重新序列化 + 重写全库。

pigasus 选择了「会话文件即状态、整份重写」的持久化模型
（`supervisor.Save` 里 `BuildSteps(facts)` → `SaveConversation` 全量 upsert）。
语义上等价于 agy 的最终落盘结果，但代价是 **O(全部历史)**，
而 agy 的发送路径是 **O(1)**。

## 修复方案（按性价比排序）

### P0 — 把 `WriteConversation` 的 3903 次 autocommit 包进一个事务

这是最大的一块（2.2 s），也是最好改的：`write.go:374` 的 for 循环里每条 step
一次 `Exec`，每条都触发隐式提交 + fsync。

实测（同一份 3903 step 数据）：

| 实现 | 耗时 |
|---|---|
| 现状：逐条 autocommit | 3.30 s |
| `BEGIN … COMMIT` 包住循环 | **132 ms** |

**约 16× 提速**，改动 10 行以内。

### P1 — 改「全量重写」为「增量追加」

`convstore.AppendConversationSteps`（append.go:35）**已经存在**，
但 TUI 的保存路径没用它。实测：

| 实现 | 耗时 |
|---|---|
| 全量重写 3903 step | 3.11 s |
| 增量追加 2 条新 step | **54 ms** |

**约 57× 提速**。`Supervisor` 已经有 `trajLoaded` / `trajSeededLen` 跟踪
「已落盘的边界」，改成「只写 `Session[trajSeededLen:]` + `trajCalls`」即可，
语义正好对应 Append 的设计意图。

### P2 — summaries 库的热路径瘦身（353 ms → 期望 < 40 ms）

三处独立浪费，都可单改：

1. **全表自愈 UPDATE**（`summary_schema.go` 里 5 条无条件 `UPDATE ... WHERE x=''`）
   每回合都跑一遍全表扫描。→ 只在 schema 迁移时执行一次，用
   `user_version` / `search_schema_version` 做门闩。
2. **`repairRecentFTSHeaders`** 每回合 `LIMIT 100` 拉 100 行、逐行
   `QueryRow` + 最多 2 条 `Exec` = 最多 300 条语句。→ 同样加迁移门闩，
   或改成 `WHERE last_modified_time IS NULL` 的条件查询。
3. **FTS5 全文重建**：每回合把全部 3903 条 user/agent 文本重新灌进
   `conversations_fts`（24 MB）。→ 改成只重建 `chunk_index=0` 的增量 chunk，
   或按 chunk 分片索引而非整篇重灌。

### P3 — 让持久化离开 UI 线程

即使上面都做了，落盘仍是几十毫秒的同步 IO。正确做法是：

- `saveSession()` 改成**派发 `tea.Cmd`** 在后台 goroutine 执行；
  用「写入序号 + 完成后回投 `sessionSavedMsg`」保证顺序与幂等。
- 回合开始时的 `saveSession` 其实只需要把**用户消息**落盘（供崩溃恢复），
  完全可以让它在后台跑，Enter 立刻返回。
- `Supervisor.Save` 目前持写锁跑完整条 IO，会顺带阻塞 tick 路径读锁；
  应改为「快照 + 释放锁 + 后台写」。

### P4 — 顺手的小项

- `syncAccountIdentity` 每次 Enter 都 `LoadTokenUnified()`，
  macOS keychain 读约 **20 ms**，且 `HasOAuthToken()` 内部还会再读一次。
  应缓存 email/token 快照，只在 tick 或账号切换时失效。
- `WriteSummariesTitle` 每次都 `sql.Open` 一个新连接池并执行 FTS5 行 UPDATE；
  复用 summaries 库的长连接，并在标题未变时跳过。

## 预期收益（累加）

| 阶段 | 3900 step 会话的 Enter 阻塞 |
|---|---|
| 现状 | ~3.30 s |
| +P0（事务） | ~1.2 s |
| +P1（增量追加） | ~0.25 s |
| +P2（summaries 瘦身） | ~0.06 s |
| +P3（后台落盘） | **Enter 立即返回（<1 ms）** |

## 复现方式

```bash
# 1) 把一份长会话转成可用于 benchmark 的 store
cp ~/.gemini/antigravity-cli/conversations/<长会话>.db /tmp/pigbench/big.db

# 2) 跑 Enter 路径计时（临时测试，跑完删除）
go test ./internal/tui/ -run TestEnterKeyLatencyTmp -v
go test ./internal/app/session/convstore/ -run TestAttributeSave -v
```

> 调查中临时加的 `zz_*_test.go` 已全部删除，`git status` 干净。
> 中途写入真实数据目录的 `bench-id.db` 也已清理。
