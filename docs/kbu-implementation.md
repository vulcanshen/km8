# kbu — Implementation

本文件是 [**VTP** — Vulcan's TUI Design Principle](https://github.com/vulcanshen/thoughts/blob/main/vtp.md) 在
kbu 上的具體 implementation。VTP 是 **interface**、本文件是 **implementation
class** — 每節對應 VTP 同編號的條款、寫 kbu 的具體選擇、案例、hotkey 表。

新加入 kbu 開發的人讀這份文件、能直接知道「kbu 把每條原則 implement 成什
麼樣」。想知道**為什麼**這樣做、回去看通用規範。

本文件**含 popup convention v2 全部 normative 內容**（見 §6）— popup
convention 是 kbu 對通用 §6 的完整實作、`one popup = one file = one
PopupAnimator` 的 invariant 在這裡強制執行。

---

## §A. Implementation in kbu

通用規範 §A.0（設計原則的 spectrum 量化框架）+ §A.1（Contextual track）+ §A.2
（Non-contextual track）在 kbu 的具體實現。

### §A.0 kbu score 對照

kbu 在通用 §A.0 spectrum 上的位置：

| 軸 / 結果 | kbu 值 | 計算 |
|---|---|---|
| **X. 揭露程度** | 1.0（~100%）| Space menu 列出 contextual 動作 100%、`?` help popup 列出 non-contextual 動作 100%、X = (入口列出操作) / (app 全部操作) ≈ 1.0。**以 user 學習單位計算**（通用 §A.0 Action 粒度）：`Y` view YAML 對 30 個 resource type 通用、user 學一次 → 算 **1 個 action**、不是 30 個；同理 `E` edit / `D` delete / `C` compare / `S` shell 各算 1 個、不論底下 code 有幾個 switch case |
| **Y. core-key role 數量** | 5 | 5 個 distinct role：「focus 切換」(`Tab`) / 「確認」(`Enter`) / 「取消」(`Esc`) / 「contextual 入口」(`Space`) / 「non-contextual 入口」(`?`)。**alias 共用 role 算 1 個**：若 kbu 補 `q` 跟 `Esc` 完整 alias 做「取消」（所有 surface 都同樣有效）、仍算 1 個取消 role、Y 不變、仍 = 5。若 `q` 只在某些 surface 取消、其他沒有 — 半套 alias、不算同 role、Y +1 變 6 |
| `min(1, 5/Y)` 係數 | 1.0 | Y = 5、無 penalty |
| **Score** | `1.0 × 1.0 × 100%` = **100%** | user 不靠事先學就能用 |
| 事前認知門檻（反向）| `100% − 100%` = **0** | 不需要看 README |

kbu 是「**高易用性 + Kubernetes 操作工具**」這個 niche 的 prototypical
案例 — 不靠事先學習就能用、靠 Space menu 跟 `?` help popup 走完所有
功能。letter hotkey 是加速捷徑、不是必經之路。

### §A.0.Y kbu core-key 集合（5 個）

| Core-key | kbu 語意 | 對應通用條款 |
|---|---|---|
| `Tab` | focus 切換到下個 panel | §4.1 |
| `Enter` | 確認 / drill-down 進入 | §4.1 |
| `Esc` | 取消 / 關閉最上層浮層（kbu §4.3 取消 key）| §4.3 |
| `Space` | §A.1 contextual 入口（Space menu）| §A.1 |
| `?` | §A.2 non-contextual 入口（help popup）| §A.2 |

5 個、剛好通用 §A.0.Y 上限。letter hotkey（`Y` / `E` / `D` / `C` / `N` /
`Alt-t` / `q` 等）**不算 core-key**、是兩個入口內動作的加速捷徑。

五個 core-key 的語意在 kbu 任意 panel / popup 都不變。

### §A.1 Contextual track in kbu — Space menu

kbu §A.1 入口是 **Space menu**：每個 panel + cursor / popup focus 都接
`Space`、列出當前能做的事。

**Layer 1 — entry key (`Space`) 自身揭露**（強制、通用 §A.1 前提）：kbu
在 **bottom statusline** 顯示 `Space menu`（5 個 core-key 全在 bottom
statusline 揭露：`? help`、`Esc exit`、`Space menu`、`Enter commit/into`、
`Tab cycle panel`）— user 第一次開 app、沒看 README、從 bottom statusline
就知道按 `Space` 會跳東西。沒這條揭露 → X = 0、entry key 等於不存在。

| Contextual 動作類型 | 出現位置 |
|---|---|
| 對 cursor 指的 item 做 YAML / Edit / Delete / Compare / Shell | panel 2 menu（cursor row region） |
| 對當前 panel list 做 Sort / Pin | panel 2 menu（panel operation region） |
| Mark / Unmark Compare anchor | panel 2 menu（cursor row region） |
| 對 Compare popup 內的 hunk 做 layout 切換 / 切顯隱 | comparemenu（compare popup 的子 menu） |

kbu 每個 contextual letter hotkey（`Y` / `E` / `D` / `C` / `S` / `Alt-S` /
`P`）對應的動作、都在對應 focus 的 Space menu 出現 — letter hotkey 是
「給知道的人」的加速捷徑、Space menu 是「給所有人」的完整界面。

**完整性 audit**：新增一條 contextual 動作時、必須同時在 Space menu 加
entry、不能只綁 letter hotkey。否則就是原則破洞（通用 §A.1）。

### §A.2 Non-contextual track in kbu — `?` help popup 入口 + statusbar 揭露

kbu §A.2 入口是 **`?` help popup**：在任何 surface 按 `?` 跳出 app 全
部全域動作的完整清單。

**Layer 1 — entry key (`?`) 自身揭露**（強制、通用 §A.2 前提）：kbu 在
**bottom statusline** 顯示 `? help` — user 第一次開 app、沒看 README、
從 bottom statusline 就知道按 `?` 會跳東西。沒這條揭露 → X = 0、跟 vim
`:` 同處境。

User 知道 `?` 存在後、光按 `?` 就能看到所有全域動作 + 它們對應的 letter
hotkey 加速捷徑。

kbu 的全域動作（都在 help popup 內列出）：

| 全域動作 | letter hotkey 加速 | help popup 內列 |
|---|---|---|
| 切換 namespace | `N` | ✓ |
| 切換 context | `C`（panel 1/3 上） | ✓ |
| 切 Alterm（內嵌 shell） | `Alt-t` | ✓ |
| 切 Compare mode | `C`（panel 2 上） | ✓ |
| Quit | `q` | ✓ |

letter hotkey 是 §A.2 入口內動作的加速捷徑、跟 §A.1 letter hotkey 性質
相同 — 不是另開的功能、新使用者光靠 `?` 就找得到。

**完整性 audit**：新增一條 non-contextual 動作時、必須同時在 help popup
加 entry、不能只綁 letter hotkey。

#### Layer 2 — 個別動作的 ambient 揭露（kbu 加碼、通用 §A.2 optional）

Layer 1 是強制揭露 entry key 自身、Layer 2 是 kbu 自選加碼 — 在
statusbar 用 chip 持續揭露**個別全域動作**、增加 user 對它們的 ambient
awareness。通用規範不強制、kbu 選了做：

| 全域動作 | statusbar chip 揭露 |
|---|---|
| namespace | `[N]amespace: <name>` chip（panel 1/3） |
| context | `[C]ontext: <name>` chip（panel 1/3） |
| Alterm | `[Alt-t]erm` chip（Alterm active 時） |
| Compare mode | `[C]ompare` chip（lock active 時） |
| Quit | bottom statusline |

即使所有 chip 拿掉、user 透過 `?` 仍能找到全部全域動作、§A.2 完整性不
破 — chip 只是讓 user 更快找到、不是 user 唯一的學習路徑。

**`C` 字母 panel-aware**：`C` 同時是 panel 1/3 的 `[C]ontext` picker、
跟 panel 2 的 `[C]ompare`、走通用 §2.6 anti-correlated brightness 消歧
（見 §2.6）。

### kbu contextual / non-contextual 動作邊界（audit 用）

| 動作 | 是否 contextual | track |
|---|---|---|
| 對 cursor 指的 item 做 YAML / Edit / Delete / Compare | ✓ 對 cursor row | §A.1（Space menu） |
| 對當前 panel list 做 Sort / Pin | ✓ 對當前 panel | §A.1 |
| Mark / Unmark Compare anchor | ✓ 對 cursor row | §A.1 |
| 切換 namespace / context | ✗ 全域 | §A.2（? 入口 + statusbar chip） |
| Help / quit | ✗ 全域 | §A.2（? 入口） |
| 切換 Alterm | ✗ 全域 toggle | §A.2（? 入口 + statusbar chip） |

---

## §B. 元素專職化 in kbu

kbu 對 §B 的具體 routing：

| 元素 | 專職語意 | 不准兼職什麼 |
|---|---|---|
| `Lavender` (Catppuccin) | 使用者足跡（Pinned / settings ON / compare anchor / unfocused cursor chip） | 不能做 panel border / popup border / hotkey discoverability signal |
| `Peach` (Catppuccin) | warning override 色（toast warn border / warn badge） | 不參與 popup layer scale |
| `Red` | error override 色 | 同上 |
| Panel border 色 | structural（blue 亮 = focus、blue 暗 = unfocus） | 不能拿去做 user state |
| Popup border 色 | layer 明度（lavenphire25/50/75 → sapphire） | 不能 hardcode |
| `[X]label` bracket 格式 | letter hotkey discoverability | 純 label 不加 bracket |
| `Esc` | 關閉 / 退出（kbu §4.3 取消 key）| 永遠不當「確認」|
| `Space` | §A.1 contextual 入口（Space menu）| 不當別的 letter hotkey |
| `?` | §A.2 non-contextual 入口（help popup）| 不當別的 letter hotkey |
| Glyph (U+f...) | 類型訊號（surface 是哪一類） | 不當熱鍵 signal、不當純裝飾 |

---

## §1. 空間結構 in kbu

### 1.1 窄寬可用

kbu 在窄寬下：sidebar (panel 1) 可隱藏、panel 2 + panel 3 並列、最窄寬度
仍能瀏覽 + drill-down 資源。

### 1.2 Width stability

kbu panel header 的動態元素（context / namespace 名稱）用 `Lavender` chip
固定 slot；statusbar 的 `[Alt-t]erm` / `[C]ompare` chip 寬度與內容無關
（chip 本身是固定 string、不會因為當前 panel 變寬度）。

### 1.3 Statusbar 行數

**kbu 選 N=1**（單行 statusbar）— 通用 §1.3 規定行數一旦選定就鎖死、
kbu 一律單行。

代價自負（kbu 承擔）：

- 內容溢出時截斷、不折行
- 不會臨時擴成兩行容納額外資訊

main canvas 高度 = `視窗高 - 1（statusbar）- N（panel border 之類框）`、
跟 statusbar 行數綁死、不會浮動。

---

## §2. 色彩 in kbu

### 2.1 kbu 配色錨點

kbu 選了 **3 個錨點**：

| 錨點 | kbu 顏色 | 用途 |
|---|---|---|
| `base` | Catppuccin Mocha base `#1e1e2e` | canvas |
| `user footprint` | `Lavender #b4befe` | 使用者足跡 |
| `popup ceiling` | `Sapphire #74c7ec` | 浮層最頂層 |

### 2.2 明度 z-axis

kbu 從 base → Lavender → Sapphire 一路變淺、絕對不逆轉。

### 2.5 kbu layer 插值

kbu 採線性 lightness 插值、N=4、預製離散色階：

| Layer | kbu 色 | 常數 |
|---|---|---|
| 1 | `#A4C0FA` | `theme.Lavenphire25` |
| 2 | `#94C3F5` | `theme.Lavenphire50` |
| 3 | `#84C5F0` | `theme.Lavenphire75` |
| 4+ | `#74c7ec` | `theme.Sapphire` |

`theme.PopupLayerColor(layer int)` 是唯一指派來源、不准 hardcode hex。

**`Lavender` 是 scale 錨點、但 popup border 永遠不直接用** — Lavender 保
留給 in-panel user state（Pinned / settings ON / compare anchor / unfocused
cursor chip）。Popup 從 layer 1 (Lavenphire25) 起算、不踩 Lavender 明度
帶。

### 2.4 kbu override 色

| 用途 | 顏色 | 為什麼跳出 layer scale |
|---|---|---|
| Toast warn border | `Peach #fab387` | warning 優先 |
| Toast error border | `Red` | error 優先 |
| PTY popup border | `Lavenphire25` (layer 1 永遠) | §6.4.1 context-shift target — PTY 取代 popup tree、UX 上只有單層、不跟隨 popupDepth |

### 2.6 kbu panel-aware brightness dimming

kbu 上 `C` 字母同時是 panel 1/3 的 `[C]ontext` picker、跟 panel 2 的
`[C]ompare`。兩個 chip 同時存在 statusbar、用 anti-correlated brightness
消歧：

| Focus | `[C]ontext` 的 `[C]` | `[C]ompare` 的 `[C]` | 訊息 |
|---|---|---|---|
| Panel 2（table） | dim grey | bright blue | C 在這裡 fires compare |
| Panel 1 / 3 | bright blue | dim grey | C 在這裡 fires context picker |

讀者掃一眼 statusbar 就知道「我現在按 C 會打開什麼」、不用想 panel 邏輯。

---

## §3. 符號語彙 in kbu

### 3.1 Nerd Font 是設計

kbu 假設使用者有 JetBrainsMono Nerd Font 或同等字體、不設計降級分支。
README install 提示安裝、未裝者的使用體驗不在 kbu 設計範圍。

### 3.2 kbu glyph 子集

kbu 只用 `U+f...` 開頭區段（Font Awesome / Octicons / Material Design
Icons）、避開 `U+e...`（Devicons / Codicons / Pomicons — 跨字體支援度
低、易掉 box）。

### 3.3 kbu surface 標籤

每個 panel / popup 標題都採 glyph + text：

| Surface | kbu 標題範例 |
|---|---|
| Confirm popup | ` 󰦕 Confirm` |
| Help popup | ` 󰘳 Keybindings` |
| Compare popup | ` 󢢪 <left> vs <right>` |
| PTY popup（Alterm） | ` 󰆍 Alterm:host` |

glyph 缺一不可、text 缺一不可。Toast 例外處理見 §6.5（level-specific
glyph + 固定 text 「kbu」）。

---

## §4. 互動 in kbu

### 4.1 Core 4 鍵語意

見 §A 上面的 core key 表。`Tab` / `Enter` / `Esc` / `Space` 在 kbu 任意
surface 都做同一件事。

### 4.2 Letter hotkey ⊆ Space menu

kbu 每個 contextual letter hotkey 都在對應 Space menu 出現：

| Hotkey | Contextual 動作 | 在哪個 Space menu |
|---|---|---|
| `Y` | YAML view | panel 2 menu |
| `E` | kubectl edit | panel 2 menu |
| `D` | delete confirm | panel 2 menu |
| `C` | Mark / Unmark anchor / Compare to anchor | panel 2 menu |
| `S` | Helm history / kubectl exec shell | panel 2 menu（依 resource type）|
| `Alt-S` | Sort panel 2 | panel 2 menu |

Non-contextual hotkey（`N` namespace / `C` context picker / `Alt-t` Alterm
/ `?` help / `q` quit）走 §A.2 statusbar 揭露、不擠進 Space menu。

### 4.3 kbu 取消 key — 選 `Esc`

通用 §4.3 要求指派一個 core key 當「全 app 取消/關閉」；**kbu 選 `Esc`**：

- 任何浮層按 `Esc` 立即關閉
- auto-dismiss toast 也吃 `Esc` — toast 即使有 dismiss timer、`Esc` 也
  能即時撤
- 沒有任何 surface 用 `Esc` 以外的 key 當取消（跨 surface 一致）

實作分工見 §6.5（blocking 浮層自己接 Esc、non-blocking 浮層由 host 路由）。

### 4.4 kbu hotkey discoverability：bracket `[X]label`

| 形式 | kbu 範例 |
|---|---|
| 單字母 hotkey | `[N]amespace`、`[C]ontext`、`[Y]AML`、`[E]dit` |
| Chord（含 modifier） | `[Alt-t]erm`、`[Alt-S]ort`、`[Ctrl-q]uit` |
| 無 hotkey 的純 label | `Compare`、`Mark anchor` — 不加 bracket |

Chord 寫法用 `[Alt-t]` 而非 `[Alt][T]` 或 `[Alt+T]` — 一個 bracket、
hyphen 連接 modifier 跟字母、跟 bottom statusline 的 `Alt-t` 對齊。

跟 §2.6 配合：bracket 標「這是鍵」、brightness 標「這鍵在這個 focus 會不
會 fire」。

---

## §5. Mouse in kbu

kbu v1.6 引入 mouse 支援、嚴格走通用規範 §5：

| Mouse 動作 | 對應 keyboard |
|---|---|
| 左鍵單擊 | focus panel + 選 row（= keyboard 的 j/k + focus） |
| 左鍵雙擊 | Enter（drill-down） |
| 右鍵 | Space（打開 Space menu） |
| Scroll wheel | `j` / `k` |

沒有「只有 mouse 才能做的事」。

---

## §6. 浮層 in kbu — Popup Convention v2

kbu 採 popup convention v2、本節是通用規範 §6 在 kbu 的**完整 implementation**。
核心 invariant：

> **One popup = one file = one PopupAnimator**、4 類 taxonomy 各有固定
> layout、跨 popup 一套 stack 規則。

緣由：v1.7.4 發現 in-popup compare menu 同時 skip 了動畫 rollout 跟
padding convention — 它是唯一一個住在別人檔案裡的 popup、所以從 filename
audit 看不到。本節的 cross-cutting 規則 + per-category 規格 + inventory
讓 audit 變顯式。

### §6.0 Cross-category invariant（不分 popup 類型一律遵守）

#### 6.0.1 一個 popup 一個檔

每個 popup model 放 `internal/ui/` 下獨立檔案、檔名 lowercase no-separator：

| File | Type |
|---|---|
| `hintpopup.go` | `HintPopupModel` |
| `comparepopup.go` | `CompareYamlPopupModel` |
| `comparemenu.go` | `CompareMenuPopupModel` |
| `panel2menu.go` | `Panel2MenuPopupModel` |
| `helmdocmenu.go` | `HelmDocMenuPopupModel` |
| `ptyview.go` | `PtyView` |

#### 6.0.2 一個 PopupAnimator、Target = filename

每個 popup 擁有自己的 `PopupAnimator`、`Target` 對應檔名（hierarchy 用
`_` 分隔）：

```go
animator: NewPopupAnimator("comparepopup", lipgloss.Color(theme.Periwinkle))
animator: NewPopupAnimator("comparepopup_menu", lipgloss.Color(theme.Periwinkle))
animator: NewPopupAnimator("ptyview_shell", lipgloss.Color(theme.Periwinkle))
animator: NewPopupAnimator("ptyview_tx",    lipgloss.Color(theme.Periwinkle))
```

多 instance popup（如 `PtyView` 有兩 slot）、constructor 收 target 參數、
每個 instance 有 distinct routing key。

#### 6.0.3 Top-level composite 在 `app.go View()`

popup stack 是 `View()` 的最底層、每個 top-level popup 標準 wiring：

```go
if m.XXX.IsActive() {
    m.XXX.SetSize(m.width, m.height)
    mainView = overlay.Composite(m.XXX.RenderPopup(), mainView,
                                  overlay.Center, overlay.Center, 0, 0)
}
```

#### 6.0.4 Sub-popup own file + own animator

Popup 內 host 另一個 popup（如 `comparepopup.go` 用 Space 開 diff menu）、
child 仍要獨立檔 + 自己的 `PopupAnimator`。Parent 在自己的 `renderFrame`
裡 composite：

```go
if m.subPopup.IsActive() {
    frame = m.subPopup.Render(frame)
}
```

Parent 對 child 的義務：

- **Reset** sub-popup state 在 parent 的 `Open()`（`m.menu.Reset()` —
  否則前次 state 跨 open 殘留）
- **Route `HandleTick`** 給 sub-popup（unique target 自己 route AnimTickMsg）
- **Route `Update`** 給 sub-popup while `m.subPopup.IsActive()`、接它的
  action（Cancel / Commit + cursor）

#### 6.0.5 No bare `overlay.Composite` outside `app.go View()`

`app.go View()` 之外的 bare `overlay.Composite(...)` 是 smell — 代表某
個 embedded sub-popup 即將 slip filename audit。唯一例外是 sub-popup
model 的 `Render` 自己走 animator 的 `RenderFrame`。

#### 6.0.6 Title format: glyph + text mandatory

每個 popup title 都是 **`glyph + text`**：

```go
title := " 󰦕 Confirm"
title := " 󰘳 Keybindings"
title := fmt.Sprintf(" \U000f08aa %s vs %s ", left, right)  // compare
title := " " + ptyTitleGlyph + " " + titleText + " "         // pty
```

Toast 的 text 部分固定為 `kbu`（layer-specific glyph：`󰵅 kbu` info、
`󰀦 kbu` warn）— 每個 toast 讀起來都是「你的 app 在說話」、靠 glyph + border
color 區分等級。

---

### §6.1 Popup 4 類 taxonomy（對應通用 §6.1）

kbu 把 popup 分四類、每類有固定 layout 規格：

| Component | **menu** | **message** | **viewport** | **pty** |
|---|:---:|:---:|:---:|:---:|
| PopupAnimator | ✓ | ✓ | ✓ | ✓ |
| Border | ✓ | ✓ | ✓ | ✓ |
| Title (glyph + text) | ✓ | ✓ | ✓ | ✓ |
| Hint bar | ✓ | ✓ | ✓ | ✓ |
| Top padRow | ✓ | ✓ | ✗ | ✗ |
| Bottom padRow | ✓ | ✓ | ✗ | ✗ |
| Body owner | kbu | kbu | kbu | subprocess |

#### menu — 互動選擇器

```
╭─ glyph Title ──────╮
│                    │   ← top padRow
│  > item 1          │
│    item 2          │
│    item 3          │
│                    │   ← bottom padRow
╰─ hint ─────────────╯
```

- Item list + cursor + Enter commit
- 可選 keyboard search（context / namespace）
- 可 host sub-popup（comparepopup → comparemenu）
- Input：`j/k`（或 `↑↓`）移 cursor、`Enter` commit、`Esc` cancel、optional `/` search

#### message — 短文 + 動作

```
╭─ glyph Title ──────╮
│                    │   ← top padRow
│  Are you sure?     │
│                    │   ← bottom padRow
╰─ Enter/Esc ────────╯
```

- 短 body、不需要最大化垂直空間
- Input 視情況：binary（confirm 用 `Enter`/`Esc`）、scroll（applog/help 用
  `j/k`/`u/d`）、auto-dismiss（toast）
- 例：applog、help、confirm、toast

#### viewport — 長內容、垂直最大化

```
╭─ glyph Title ──────╮
│  content row 1     │
│  content row 2     │
│  content row 3     │
│  content row N     │
╰─ hint ─────────────╯
```

- 無 padRow — 每行都是 content space
- Scrollable：`j/k` 行、`u/d` 頁、`gg/G` 跳
- 例：yamlpopup、comparepopup

#### pty — 內嵌 subprocess

```
╭─ glyph Alterm:host ────╮
│ $ kubectl get pods       │   ← body 由 subprocess 渲染
│ NAME    STATUS  AGE      │     (vt10x terminal grid)
│ pod-1   Run     5m       │
╰─ Alt-t:hide ────────────╯
```

- **Frame kbu 渲染**（border + title + hint、無 padRow）
- **Body subprocess 渲染**（vt10x terminal output）
- 本 convention 管 frame；subprocess 控制 body styling
- `IsInteractive()` keys 被 frame 攔截、其他 keys 給 subprocess
- 例：shellPty (Alterm)、txPty (kubectl edit / kubectl exec)

#### Decision tree — 新 popup 分到哪一類

```
此 popup 讓 user 用 j/k + Enter 從清單挑東西嗎？
├ YES → menu
└ NO ↓

此 popup 渲染長內容、會 benefit from 最大化垂直空間（scrollable / multi-screen）嗎？
├ YES → viewport
└ NO ↓

Body 是 kbu 渲染、還是 subprocess 渲染？
├ subprocess → pty
└ kbu       → message
```

message vs viewport 的中間地帶：user 通常看 ≤ 半屏內容、偏好呼吸空間勝過
多幾行 → message；user 預期翻頁式閱讀、每行都重要 → viewport。

---

### §6.2 開關動畫 — PopupAnimator（對應通用 §6.2）

kbu timing：

| 動作 | 時長 |
|---|---|
| Open | ~160ms |
| Close | ~160ms |
| In-place swap | ~120ms |

落在通用 §6.2 推薦的 100-200ms 區間。

---

### §6.3 Border 色 = layer 明度（對應通用 §6.3）

Popup border + animator stroke 從 layer-based gradient 取色（`lavender →
sapphire`）：

| Layer | 色 | 常數 |
|:---:|---|---|
| 1 | `#A4C0FA` | `theme.Lavenphire25` |
| 2 | `#94C3F5` | `theme.Lavenphire50` |
| 3 | `#84C5F0` | `theme.Lavenphire75` |
| 4+ | `#74c7ec` | `theme.Sapphire` (catppuccin Mocha) |

**`theme.Lavender (#b4befe)` 是 scale 錨點、popup 永遠不直接用**。Lavender
保留給 in-panel user state（sidebar Pinned、settings ON toggle、compare
anchor row、unfocused cursor chip）。

`theme.PopupLayerColor(layer int) lipgloss.Color` 是唯一 authoritative
helper、任何位置都用它、絕不 hardcode hex。

**Wiring 規範**：

- 每個 popup model 有 `layer int` + `borderColor lipgloss.Color` 兩個欄
  位、`SetLayer(layer int)` method 同時 stamp 兩者 + 更新 `animator.Color`
  （open/close stroke 跟 final border 對齊）
- `AppModel.popupDepth()` 回傳當前 active popup 數。每次 `Open` / `Show`
  / `Toggle`-to-open 前呼叫 `popup.SetLayer(m.popupDepth() + 1)`
- **Sub-popup**（comparemenu inside comparepopup）用 `parent.layer + 1`、
  不用全域 `popupDepth()` — 一律 stack 在 host 之上一層
- **In-place swap**（listPicker column → direction step）保持原 layer。
  `popupDepth()` 在 popup 已 active 時會把自己也算進去、naive 重新 stamp
  `SetLayer(popupDepth() + 1)` 會 double-count、變比實際深一階。Guard：
  `if !popup.IsActive() { SetLayer(popupDepth() + 1) }` — first open
  stamp、swap path 保留現有 layer
- **Inner accents**（hunk header、column header、search box border 等）
  從 `m.borderColor` 推導、不 hardcode

**Layer scale 例外**（明示 opt-out）：

| Element | 色 | 為什麼 |
|---|---|---|
| Toast warn border | `Peach #fab387` | warning signal 優先於 layer |
| PTY popups（Alterm、kubectl edit、kubectl exec） | Layer 1 永遠 | §6.4.1 context-shift target — PTY 取代 popup tree（`closeAllBlockingPopups` 在 entry handler 執行）、不 stack 在它上面。UX 上 PTY 可見時 user 看到的就是單層 popup、所以所有 PTY 共用 layer 1、不論從哪條鍊啟動 |

**未來擴展**：插值區間可細分（12.5 / 37.5 / …）或把上限拉到 Sapphire
以上（Sky）— 同 `1/2` halving 原則。

---

### §6.4 Stack 預設保留 source（對應通用 §6.4）

popup A 開著時、user 從 A 內觸發 popup B、**A 在 B 底下保持 active**。
B 開在 `SetLayer(m.popupDepth() + 1)`、走 layer-color scale (§6.3) 渲染
在上面。Esc 關 B 只關 B、user 回到 A — A 沒被 dismiss、user 只是在 B 上
做了一次互動。

不論 B 是 sub-popup（§6.0.4 — 在 A 的 animator chain 內、例 comparepopup
→ comparemenu）還是 top-level（§6.0.3 — 在 `app.go View()` composite、例
relatives menu → switch-resource confirm）、A 都**不**因 A 的「open B」
動作而關閉。

**例子**：

| Path | Behavior | Verdict |
|---|---|---|
| Relatives menu → switch-resource confirm | Esc on confirm → 回 Relatives menu | ✓ canonical |
| Compare popup → Space diff menu | Esc on menu → 回 Compare | ✓ canonical |
| Panel 2 menu → Edit YAML / Shell confirm | Panel 2 menu 在 confirm show 之前就關掉 → Esc on confirm 直接掉到 panel | ✗ 違反 §6.4 |

**Anti-pattern** — 從 caller 關 source：

```go
// ✗ wrong — source popup 在 target 出場前已被拆
m.panel2Menu.Close()
return m, m.confirm.Open(...)

// ✓ right — source 保留、target stack 在 parent.layer + 1
m.confirm.SetLayer(m.popupDepth() + 1)
return m, m.confirm.Open(...)
// panel 2 menu 的 Update 路 Esc 給 confirm 優先、user 主動 Esc-cancel
// confirm 時才關 menu
```

**Audit 路徑**：trace popup A 的 Update fire 開 B 的 cmd 的每個位置。
若 A 的 `Close()`（或等效 state reset）出現在那條 path、就違反 §6.4。
Source popup 只能由 user 對 source 的明確動作關閉、不能當 open target 的
副作用。

### §6.4.1 例外：Context-shift target 清掉 source

§6.4 是預設 — target stack 在 source 上、source 留底。這對 **inline action**
target（confirm、yaml viewer、diff、sort picker、helm docs）是對的、秒級
完成、自然把 control 還給 source。

但有些 target 不是 inline、而是**context shift** — user 離開 kbu 的 popup
心智模型、進另一個 mode：

| Target | 為什麼是 context shift |
|---|---|
| `txPty`（kubectl edit / kubectl exec）| 分鐘級 subprocess；user 的 working memory 在 shell 裡打的字、不在啟動它的 menu 上 |
| `shellPty`（Alterm Alt-t） | 同上 — 真正的 shell session、不定長 |
| `enterDrillDown`（Pod→Container、HPA→target、…）| panel 2 換 columns + rows；source popup 浮在已換的內容上是 stale、不是 anchor |

從 context shift 回到 stale source 是反效果 — user 必須先 dismiss 才能看
到已換的 base view。所以對 context-shift target、規則反過來：

> Context-shift target 的 **entry handler** 必須在執行 shift 的 path
> 最頂端呼叫 `AppModel.closeAllBlockingPopups()`。Source 由 target 關、
> 不由 caller 關 — 每個 launch site（直接 hotkey、panel 2 menu commit、
> 未來的新表面）自動拿到正確行為。

**Helper contract**：

- `closeAllBlockingPopups() tea.Cmd` 住在 `app.go`、跟 `popupDepth()` 旁邊。
  Batch `Close()` 給每個當前 active 的 blocking popup（見 §6.8 inventory）、
  **排除** PTY slot 自己跟 toast（toast 非 blocking 且 auto-dismiss；
  PTY-PTY 串接由 dual-slot mutex toast 處理）
- Nothing open 時回 `nil`、caller 可以無條件 `tea.Batch(closeAll, ...)`
- `PopupAnimator.Close()` 已 idempotent（closing/closed state 時 no-op）、
  對即將自己關掉的 popup（如 user 剛按 Enter 的 confirm）再 close 一次安全

**Entry-point inventory** — 每個 context-shift target 的 entry handler、
auditor 對照本表確認 handler 開頭有 `closeAllBlockingPopups()`：

| Handler | Target | Notes |
|---|---|---|
| `case startEditMsg:` in `app.go` | txPty edit | confirm → onConfirm → `startEditMsg` |
| `case startShellExecMsg:` in `app.go` | txPty exec | confirm → onConfirm → `startShellExecMsg` |
| Alt-t handler in `app.go` | shellPty (Alterm) | 實務上 popup-key 已被 blocked；helper 回 nil、call 留著保 rule 對稱 |
| `enterDrillDown()` in `app.go` | drill-down（Pod / Resource 分支）| 從 table Enter + panel 2 menu Enter commit 進來 |

**Anti-pattern** — 從 caller 關 source：

```go
// ✗ wrong — close 邏輯散在 panel2menu 的 Enter case
case "Enter":
    closeCmd := m.panel2Menu.Close()
    return m, tea.Batch(closeCmd, m.enterDrillDown())

// ✓ right — caller 只 dispatch、enterDrillDown 自己關
case "Enter":
    return m, m.enterDrillDown()
```

新 launch site（例如 sidebar hotkey 開 Alterm）會自動拿到 close-all 行為、
不用 case-by-case retrofit。新 context-shift target（例未來 port-forward
viewer 跑分鐘級）出現時、補進 inventory + entry handler 加
`closeAllBlockingPopups()`。

---

### §6.5 取消 key 通殺、含 auto-dismiss（對應通用 §6.5）

任何 popup 都必須接受 `Esc` 立即關閉、含 auto-dismiss 的 toast。User 沒
有等 timer 倒數的義務。

**Implementation 分工**：

- **Blocking popup**（menu / message / viewport / pty）：自己的 `Update`
  攔截 `Esc`。§6.8 inventory 內所有 popup 都是這樣（toast 例外）
- **Non-blocking popup**（toast — keys pass-through 到下面 panel）：自
  己無法攔 `Esc`、由 host 從 panel 路出去：
  ```go
  // panel / app Esc handler 內：
  if m.toast.IsActive() {
      return m, m.toast.Dismiss()   // 先 dismiss
  }
  // ... 再走 panel 自己的 Esc 行為
  ```

Sticky toast 也適用 — `Esc` 必須能把它拿下、不只看「Esc: close」hint
顯示出來。

---

### §6.6 Menu region cursor-first（對應通用 §6.6）

kbu panel 2 menu 的 `buildPanel2MenuItems` 把 cursor 操作 region 排第一、
panel operation region（Sort 等）排第二、每個 region 有自己的 header。

### §6.6.1 Region 內主要意圖優先（kbu 額外規則）

kbu 從 v1.7.6 起追加一條 region 內排序規則 — 通用規範未強制、是 kbu 自
己的設計選擇：

當 menu 開啟時的 app state 暗示 user **此次開 menu 的主要意圖**、該 item
提升到所屬 region 的第一順位。提升要保守（state 明確指向**單一**意圖時
才提升、否則維持原序）、避免每次洗牌破壞 muscle memory。

**kbu 具體案例**：panel 2 menu 的 `C` action

| Menu 開啟時 state | 主要意圖 | 提升項 |
|---|---|---|
| In compare mode + cursor 在 candidate row | 「比對該 row 跟 anchor」幾乎 100% 是意圖 | "Compare to anchor" → item 0 |
| In compare mode + cursor 在 anchor 上 | cleanup verb、不是「來做事」 | "Unmark" 維持原序 |
| 未 lock | 還沒設定 anchor、user 可能要做任何 row action | "Mark" 維持原序 |

**不該提升的反例**：

- panel 2 cursor 在某 pod row → 不提升 `[Y]AML`、因為 user 可能要 Edit /
  Shell / Delete / YAML / drill、意圖不單一
- user 剛用過 Edit → 不提升 Edit、過去用過不等於現在還要

### §6.6.2 Panel border hint 只承載 tab-contextual（kbu 額外規則）

通用規範未明示、是 kbu 自己的設計選擇 — 跟 §6.6.1 同屬「§6.6.x = kbu
額外條款」的編號分支、主題跟 menu region cursor 無關。

**規則**：Panel border 邊角 hint 只列 **tab-contextual** 行為。Core-key
（Tab / Space / Esc / Enter / ?）的 app-wide default 語意由 §A.2 `?`
help popup + statusbar 揭露承擔、不在 panel 邊角重複污染。Enter / Esc
例外寫入 hint 的條件：其在該 tab 的行為跟 core-key 全 app default
**不同**（drill / pop-one-level 等 panel-local 特化動作）。

**kbu 具體例**：

| Surface | Hint | Enter/Esc 為什麼出現？ |
|---|---|---|
| panel 3 Logs tab | `u/d: page  gg: top  G: live` | 沒出現 — 全部是 tab-contextual scroll、Esc/Enter 走全 app default |
| panel 3 Relatives @ depth=1 | `enter: drill` | Enter 是 drill into reference、不是 app default 的「commit / no-op」 |
| panel 3 Relatives @ depth>1 | `enter: drill  esc: back` | Esc 多了 pop-one-drill-level、不是 app default 的 dismiss |
| panel 2 bottom-left | `.: toggle helm` (+ `esc: exit compare` 鎖定中) | `.` 是 contextual letter；compare 鎖定時 Esc 是 panel-2-local「exit compare」、不是 app default |

**反例（不該進 hint）**：

- 寫 `?: help` — `?` 是 core-key、揭露由 statusbar bracket-hotkey 跟全
  app 共通契約承擔、panel 邊角重複等於 noise
- 寫 `esc: close` 在沒 popup 的 tab — Esc 走 app-wide default、沒做任何
  panel-local 事、寫了就是污染
- 寫 `space: open menu` — Space 全 app 一致語意、§A.1 contextual entry
  入口已揭露、不該重複

**為什麼這條重要**：panel border hint 寬度有限（multi-pane narrow 模式
panel innerW = 38–48 cells、扣 scroll indicator 後剩 28–38）。塞
core-key default 會擠掉 tab-contextual 的 discoverability 空間 — panel
hint 是 §A.2 揭露機制的 **panel-local 補充**、不是 `?` help 的 second
copy。

---

### §6.7 錯誤呈現

kbu 用三層：

| 層 | 形式 | kbu 條款 |
|---|---|---|
| 即時警告 | toast（auto-dismiss、Esc 可關）| §6.5 |
| 歷史查閱 | applog popup（scrollable message、Esc 可關） | §6.1 message class |
| Ambient 提示 | statusbar warn / error badge（持續可見） | §1.2 / §A.2 |

---

### §6.8 Inventory by category（audit checklist）

新增 popup 時、append 到對應分類保持 inventory 完整。

#### menu (9)

| File | Type | Notes |
|---|---|---|
| `helmdocmenu.go` | `HelmDocMenuPopupModel` | |
| `panel2menu.go` | `Panel2MenuPopupModel` | |
| `hintpopup.go` | `HintPopupModel` | |
| `listpicker.go` | `ListPickerModel` | |
| `breadcrumb.go` | `BreadcrumbPopupModel` | |
| `settingspopup.go` | `SettingsPopupModel` | |
| `comparemenu.go` | `CompareMenuPopupModel` | sub-popup of comparepopup |
| `context.go` | `ContextPickerModel` | menu + keyboard search |
| `namespace.go` | `NamespacePickerModel` | menu + keyboard search |

#### message (4)

| File | Type | Notes |
|---|---|---|
| `applog.go` | `AppLogModel` | log list、scrollable |
| `help.go` | `HelpModel` | keybinding cheatsheet、scrollable |
| `confirm.go` | `ConfirmModel` | binary Enter / Esc action |
| `toast.go` | `ToastModel` | transient / sticky、固定 title text 「kbu」 |

#### viewport (2)

| File | Type | Notes |
|---|---|---|
| `yamlpopup.go` | `YamlPopupModel` | YAML render、可能跨千行 |
| `comparepopup.go` | `CompareYamlPopupModel` | side-by-side 或 unified diff |

#### pty (1 model、2 instances)

| File | Type | Instances |
|---|---|---|
| `ptyview.go` | `PtyView` | `shellPty` (PtyKindShell、Alterm)、`txPty` (PtyKindEdit / PtyKindExec) |

---

### §6.9 Code idiom — `padRow` recipe

menu + message category 用**named padRow recipe**（Idiom A）。**不要**
用 inline `bodyLines := []string{""}` + `append("")` 模式（Idiom B）—
視覺輸出相同、但讀起來像 ad-hoc、不像「這是 popup convention」。

```go
// ✓ canonical (Idiom A)
left := bStyle.Render("│")
right := bStyle.Render("│")
padRow := left + strings.Repeat(" ", innerW) + right + "\n"
b.WriteString(top)                  // top border
b.WriteString(padRow)               // top padding row
for _, line := range contentLines { // content
    b.WriteString(left + line + pad + right + "\n")
}
b.WriteString(padRow)               // bottom padding row
b.WriteString(bot)                  // bottom border
```

```go
// ✗ anti-pattern (Idiom B) — 視覺一樣、讀起來 ad-hoc
bodyLines := append([]string{""}, strings.Split(body, "\n")...)
bodyLines = append(bodyLines, "")
for _, line := range bodyLines {
    b.WriteString(left + line + pad + right + "\n")
}
```

---

## §7. 時間軸 UX in kbu

### 7.1 kbu source-target 關係 implementation

通用 §7.1（target 完成後 source 是否還有意義）的 kbu 具體實現分兩條路：

| Target 類型 | 是否清除 source | kbu 條款 |
|---|---|---|
| Confirm popup / YAML popup / Compare diff popup | 保留 source（panel 2 menu）| §6.4（預設）|
| Alterm（Alt-t）/ kubectl edit / kubectl exec | 清除 source（`closeAllBlockingPopups`）| §6.4.1（context-shift）|
| Drill-down（Pod→Container / HPA→Deployment）| 清除 source | §6.4.1（context-shift）|

判斷邏輯入口：context-shift target 的 entry handler 在執行 shift 之前
呼叫 `closeAllBlockingPopups()`、不在 caller。

### 7.2 kbu streaming exception：Logs 不退階

kbu panel 3 五個 tab 的 unfocus 處理：

| Panel 3 tab | streaming? | unfocus 處理 |
|---|---|---|
| Logs | ✓ k8s log stream | 不 dim、container/pod hash color 保留 |
| Events | ✗ 靜態 list | dim 至 overlay1 |
| Conditions | ✗ 靜態 table | dim 至 overlay1 |
| Relatives | ✗ 靜態 nav hub | dim 至 overlay1 |
| History | ✗ 靜態 list | dim 至 overlay1 |

實作位置：tab.go render 路徑、依 tab 類型分支 dim / no-dim。

**業界 precedent**：Lens、k9s 對 streaming logs 都不 dim。

---

## §8. Panel chrome in kbu — border title / tab bar / border hint

> 這一節把 kbu 三件 panel 外框裝飾抽成可攜 spec：**border title chip**、
> **panel tab bar**、**border hint**。跟 §6 Popup Convention 平行——§6 講
> 浮層怎麼畫、§8 講 panel 外框怎麼畫。另做工具時整套照搬即可，不用逐項
> 重講。所有顏色引用 §2 錨點、glyph 規範接 §3.2、hint 承載規則接 §6.6.2。

### §8.0 三件套、focus 二態、共用色

kbu 每個 panel 的外框由這三件裝飾 + box 邊框組成：

| 件 | 位置 | 由誰畫 | 出現在 |
|---|---|---|---|
| Border title chip | 上邊框左端 | `focusedPanelTitle` / `plainTitlePrefix` | panel 1 / 2 / 3 |
| Panel tab bar | 上邊框左端（接在 title chip 後）| `DetailModel.TabTitle` | panel 3 |
| Border hint | 上邊框右端 + 下邊框左端 + 下邊框右端 | `renderPanelWithScroll` | panel 2 / 3 |

**Focus 是唯一的二態變數**——一個 panel 只有 focused / unfocused 兩種樣子，
所有裝飾的顏色與字重都由它驅動：

| 角色 | focused | unfocused | 常數 |
|---|---|---|---|
| 邊框 + chip 底色 | Blue `#89b4fa` | Surface2 `#585b70` | `Sidebar.CategoryFg` / `Detail.BorderColor` |
| chip 文字 | base `#1e1e2e` | base `#1e1e2e` | 固定 base（暗字壓在亮底上求可讀）|
| box 字元 | 雙線 `╔═╗╚╝║` | 圓角細線 `╭─╮╰╯│` | 見 §8.4 |
| tab 區底色 | crust `#11111b` | crust `#11111b` | 固定 crust（比 base 低一階、tab 區顯得凹陷）|
| tab chevron divider | surface0 `#313244` | base `#1e1e2e` | focus 差一階、聚焦 panel 的 chevron 略亮 |

focused 的 Blue `#89b4fa` 是**結構色**（focus 訊號），不是 §2.5 的 popup layer
scale，也不是 Lavender（user state）——panel chrome 只表達「哪個 panel 有焦點」，
跟浮層層級、使用者足跡兩條色軸互不干涉。

**Powerline glyph 例外（重要）**：§3.2 定 kbu 只用 `U+f...`、避開 `U+e...`。
但 panel chrome 用的分隔符是 Powerline 私有區 `U+E0B0`–`U+E0B7`——這是
`U+e...` 區段裡**唯一**該破例的子集：它是 powerline 事實標準（starship / tmux /
vim-airline / lualine 全用），每套 Nerd Font 必附、跨終端機寬度穩定（都是 1 cell）。
其餘 `U+e...`（Devicons / Codicons / Pomicons）照舊避開。

本節用到的 Powerline cap：

| Codepoint | 形狀 | kbu 用途 |
|---|---|---|
| `U+E0B6` | 實心左半圓（round 左）| 開 chip（title chip / tab bar 起點）|
| `U+E0B4` | 實心右半圓（round 右）| **收尾** cap（title chip 尾、tab bar 尾）|
| `U+E0B0` | 實心右三角（hard）| tab **之間**的 boundary cap（chip↔底色轉換）|
| `U+E0B1` | 右三角細線（thin chevron）| 兩個 inactive tab 之間的 divider |

**收尾用圓（`E0B4` / `E0B6`）、內部銜接用三角（`E0B0` / `E0B1`）** 是刻意的：
圓角把整條 title 的頭尾收成柔邊，三角負責 chip 對 chip 的 starship 式流動銜接。

### §8.1 Border title — powerline chip

panel 1 / 2 的 title 是**單一 chip**：

```
<E0B6>[N] body<E0B4>
 圓左   ↑     ↑ 圓右
      [N]=panel id   body=內容
```

- `[N]` 是 panel 編號（`[1]` Kinds / `[2]` breadcrumb / `[3]` tab bar），對應
  §4.4 的 `[X]label` hotkey 入口語彙——數字即 focus 該 panel 的鍵。
- chip = `fg base #1e1e2e + bg 邊框色 + bold`；兩端 cap = `fg 邊框色、無底`
  （半圓貼在 panel 底色上）。
- focused / unfocused 只換邊框色（Blue ↔ Surface2），chip 形狀、cell 數不變
  ——換色不換版位，符合 §1.2 width stability。

panel 3 的 title 不自己收尾：`plainTitlePrefix` 只畫 `<E0B6>[3]`（開 chip、
不畫右 cap），把收尾交給緊接的 tab bar（§8.2）。理由是 panel 3 的 body 是一整
條 tab chip chain、自帶收尾系統，共用同一顆 `[N]` chip 讓三個 panel 的視覺語言
一致。

### §8.2 Panel tab bar — starship chip chain（panel 3 專屬）

panel 3 的 title body 是一條 starship 風格的 powerline chip chain。規則：

- **只有 active tab 是亮 chip**（`fg base + bg 邊框色 + bold`）；其餘 tab 坐在
  crust 底 (`fg 邊框色 + bg crust`)。
- **首 tab active 時與 `[N]` chip 合併**：不畫邊界 cap，直接 ` Label` 續在藍
  chip 上，成一段連續藍「`[3] Label`」，避免邊界處雙 chevron。
- **tab 之間的銜接**依前後 tab 狀態選 cap：
  - active→inactive：`E0B0` close cap（`fg 邊框色 + bg crust`）
  - inactive→active：`E0B0` open cap（`fg crust + bg 邊框色`）
  - inactive→inactive：`E0B1` thin chevron divider（focus-tiered 色，見 §8.0 表）
- **尾端收圓** `E0B4`：末 tab 是 active 就用 `fg 邊框色` 的圓、是 inactive 就用
  `fg crust` 的圓（把 crust tab 區以柔邊收進 panel 底色）。
- **Width stability（硬約束）**：tab label 一律 `Label` 無 leading space、
  active/inactive 都同寬；`Logs` / `Events` 的 live/paused glyph（`U+F0753` /
  `U+F0754`）**不論 active 與否恆常渲染**。否則切 tab 時 tab bar 會伸縮 1–2 cell、
  傳導到 panel 3 邊框對齊，popup 疊上去就抖。這是 §1.2 在 tab bar 上的具體落實。

### §8.3 Border hint — 三個邊角

border hint 把「當前 surface 能按什麼」織進邊框，不佔 body 行。三個位置：

| 位置 | 格式 | kbu 內容來源 |
|---|---|---|
| 上邊框右端 | ` <hint>─`（前導空格 + hint + 1 dash 收角）| `BorderTopRightHint()` |
| 下邊框左端 | `─<hint>─`（dash 夾 content）| `BorderBottomLeftHint()` / `tablePanelBottomLeft()` |
| 下邊框右端 | ` X of Y `（收角前）| `ScrollInfo{Position, Total}` |

- **樣式**：hint 文字 = `fg 邊框色 + bold`，dash / 邊框 = `fg 邊框色`——跟 title
  同色系，讀成同一層 chrome。
- **溢位一律靜默丟棄、不截斷**：小終端機寧可退回素邊框也不擠壞版位。下邊框的
  丟棄有**優先序**——scroll 指示器（`X of Y`）比 bottom-left hint 有用，空間不夠
  時先丟 hint、保 scroll。
- **承載規則接 §6.6.2**：border hint **只承載 tab-contextual 鍵**（意義隨當前
  tab 變的鍵）。core-key（Tab / Space / Esc / Enter / ?）語意 app-wide 恆定、
  不重複進 hint、只住在 `?` help。例外：`Enter` / `Esc` 在 Relatives 上行為
  ≠ app 預設（Enter drill 進引用資源、Esc 在 depth>1 退一層 drill）時才現身。
  kbu 現行 hint：Relatives→`enter: drill`（depth>1 疊 `esc: back`）、
  Logs / Events→`u/d: page  gg: top  G: live`（`G` 寫 live 因為它同時重掛
  live tail、行為非預設所以進 hint）。

### §8.4 Focus 訊號 — box 字重 + 色

focus 用**兩個同時的訊號**表達，讀者掃一眼即知焦點在哪：

| 訊號 | focused | unfocused |
|---|---|---|
| box 字元字重 | 雙線 `╔═╗╚╝║` | 圓角細線 `╭─╮╰╯│` |
| 邊框 + chip 色 | Blue `#89b4fa` | Surface2 `#585b70` |

兩套 box 字元**cell 數相同**——只換字形字重、不換寬高，focus 切換零位移
（再次呼應 §1.2）。字重（雙線 vs 細線）是色盲也讀得到的第二訊號，不把 focus
全押在顏色上。

---

## 附錄 — kbu hotkey 全表

### Core key（跨 surface 不變、≤ 5 個）

| 鍵 | 語意 |
|---|---|
| `Tab` | focus → next panel |
| `Enter` | 確認 / drill-down |
| `Esc` | 取消 / 關浮層（kbu §4.3 取消 key） |
| `Space` | Space menu（kbu §A.1 contextual 入口） |
| `?` | help popup（kbu §A.2 non-contextual 入口） |

### Contextual letter hotkey（在 Space menu 中現身）

| 鍵 | 動作 | Focus |
|---|---|---|
| `Y` | YAML view | panel 2 row |
| `E` | kubectl edit | panel 2 row（editable resource） |
| `D` | delete confirm | panel 2 row |
| `C` | Compare anchor 操作 | panel 2 row |
| `S` | Helm history / shell exec | panel 2 row（依 resource） |
| `Alt-S` | Sort panel 2 list | panel 2 |
| `P` | Pin / Unpin | sidebar / panel 2 row |

### Non-contextual letter hotkey（在 `?` help popup 中現身）

| 鍵 | 動作 | Statusbar 額外揭露（optional） |
|---|---|---|
| `N` | namespace picker | `[N]amespace: <name>` chip |
| `C` | context picker（panel 1/3 上） | `[C]ontext: <name>` chip |
| `Alt-t` | Alterm toggle | `[Alt-t]erm` chip |
| `q` | quit confirm | bottom statusline |
| `:` | command palette（future） | bottom statusline |

### Vim-style navigation（跨 surface 同義）

| 鍵 | 動作 |
|---|---|
| `j` / `↓` | cursor down |
| `k` / `↑` | cursor up |
| `h` / `←` | cursor left（panel 內） |
| `l` / `→` | cursor right（panel 內） |
| `g g` | jump to top |
| `G` | jump to bottom |
| `u` / `Ctrl-u` | page up |
| `d` / `Ctrl-d` | page down |
| `/` | search filter |

---

## 結語

本文件捕捉 kbu 對通用規範的每條 implementation 選擇、含 popup convention
v2 完整內容。增加新功能時：

1. **先**回去看通用規範對應條款、確認新功能落在哪條 track（§A.1 contextual
   走 Space menu / §A.2 non-contextual 走 statusbar 揭露）
2. **再**檢查本文件 kbu 是否已有對應 implementation pattern、follow 之
3. 若涉及 popup、走 §6 完整 convention（一個 file、一個 PopupAnimator、
   分類定 layout、stack 走 §6.4 / §6.4.1）
4. 若是新 pattern、補進本文件對應節 + §6.8 inventory

通用規範改動時、本文件相應更新；反過來、kbu 累積的新 pattern 達到通用層
級時、再 promote 進通用規範。

---

*隨 kbu 開發累積會持續修訂。*
