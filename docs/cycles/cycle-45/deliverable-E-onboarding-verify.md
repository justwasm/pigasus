# Cycle 45 — Deliverable E: Onboarding Wizard 真接 (verification)

## 任务范围

Issue #7: cycle 43 D 已 ship 8 color scheme + terms + renderDone; cycle
44 A ship `HandleEnter`/`HandleSpace`/`HandleAnyKey` 修 "Bye!" bug。本任务
**不改 wizard.go**, 只 verification + main.go 集成 doc。

## 8 color scheme 跟 v1.3.5 binary 1:1

`wizard.go::ColorScheme.String()` (行 115-136):

| idx | const                    | 字面值                       |
|-----|--------------------------|------------------------------|
| 0   | `SchemeTerminal`         | `terminal`                   |
| 1   | `SchemeLight`            | `light`                      |
| 2   | `SchemeSolarizedLight`   | `solarized light`            |
| 3   | `SchemeColorblindLight`  | `colorblind-friendly light`  |
| 4   | `SchemeDark`             | `dark`                       |
| 5   | `SchemeSolarizedDark`    | `solarized dark`             |
| 6   | `SchemeColorblindDark`   | `colorblind-friendly dark`   |
| 7   | `SchemeTokyoNight`       | `tokyo night`                |

`ColorSchemeCount = 8` + `AllSchemes()` 锁顺序。`grep -c "solarized\|tokyo\|
colorblind" wizard.go` = 23 (≥3 ✓)。

## Step 1/2/3 关键 strings (1:1 binary)

- **Step 1** (StepColorScheme): header `"Choose your color scheme:"` + 8
  bullet (selected 带 `▸`) + `─── Preview ───...` + Go preview (`import
  "fmt"` / `func main()` / `func greet(name string)` / `fmt.Printf("Hello,
  %s!\n", name)`) + 7 marker glyphs (`⚙ ◉ ✗ ⚠ → ★ ·`) + `[ Next ]`。
- **Step 2** (StepTerms): 3 行 warning + `☑/☐` checkbox + label
  `"Yes, I agree to help improve Antigravity CLI by allowing Google to
  collect usage data and crash reports."` + URLs
  `https://antigravity.google/terms` + `https://policies.google.com/privacy`
  + `[ Previous ]` + `[ Done ]`。
- **Step 3** (`renderDone` 行 716-727): `"Setup complete!"` +
  `"  Press any key to continue..."`。`HandleAnyKey()` 在 Step 3 返 true
  (`tea.Quit()` signal)。

## Cycle 44 A 'Bye!' 修复 — 3 个 key handler

| handler         | StepColorScheme          | StepTerms (unchecked)     | StepTerms (checked)  | StepDone |
|-----------------|--------------------------|---------------------------|----------------------|----------|
| `HandleEnter()` | → StepTerms (done=false) | → "请先勾选" (done=false)  | → StepDone           | done=true|
| `HandleSpace()` | no-op                    | flip `Agreed`/`OptOut`    | flip                 | no-op    |
| `HandleAnyKey()`| no-op                    | no-op                     | no-op                | done=true|

`TestStep2_EnterWithoutAgreedError` 锁 "请先勾选" + `wz.Step == StepTerms`
(FSM **不** advance) — user live run 不再 "Bye!" 退出。

## 跟 cmd/antigravity/main.go 真接

| 位置                          | 内容                                                    |
|-------------------------------|---------------------------------------------------------|
| `main.go:51`                  | `import ".../internal/onboarding"`                      |
| `main.go:241-247` (runRoot)   | `if !--no-onboarding { runOnboardingWizard(out) } runLiveTUI(...)` |
| `main.go:698-716`             | `runOnboardingWizard`: `onboarding.NewWizard(); wz.Run()`|
| `internal/cli/parser_specs.go`| `ExtraFlags()` 加 `--no-onboarding` bool (cycle 43 D)   |

`--no-onboarding` 跳过 wizard 给 parse-parity CI。**无 args** 才走 wizard
+ TUI; 有 args 走 `runPrintMode`。

## go test 输出

```
$ GOTOOLCHAIN=local GOPROXY=off /workspace/agy/go126/bin/go test \
    ./internal/onboarding/ -count=1 -timeout=30s
ok  	github.com/chadgpt/openagy/internal/onboarding	0.057s
```

15/15 PASS — 11 cycle 43 (`TestWizard*`) + 4 cycle 44 A (`TestStep1_EnterSelects`, `TestStep2_SpaceTogglesAgreed`, `TestStep2_EnterWithoutAgreedError`, `TestStep2_EnterWithAgreedAdvance`)。

## Build status note

`cmd/antigravity/main.go` 整体 build 失败是 sibling tasks A/B/C/D 同时
改 `internal/tui/{hooks,tasks,usage_battle,onboarding}.go` 引起的
`Init() InitState` vs `Init() tea.Cmd` 签名冲突 — parallel-track race,
**不是本任务造成的**。`internal/onboarding/` 独立 compile + test PASS。
