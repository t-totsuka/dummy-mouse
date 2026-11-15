# 調査と設計判断の記録

## Summary
- **Feature**: `idle-mouse-mover`
- **Discovery Scope**: New Feature（新規開発。既存コードはない）
- **Key Findings**:
  - `fyne.io/systray` v1.12.2 は、Windows 向けなら `CGO_ENABLED=0` でもビルドできる（2026-11-15 に実際に確認。`-H=windowsgui` 付きで exe は約 4.2MB）。ドキュメントには「cgo が必要」とあるが、これは macOS と Linux の話だった
  - 要件 1.1 はクリックとホイールもユーザーの操作として数える。カーソル位置のポーリングだけでは、カーソルが動かないクリックやホイールを検知できない。そこで低レベルマウスフック（`WH_MOUSE_LL`）を主な検知手段にする
  - 自分が送った入力は、`SendInput` の `dwExtraInfo` に付けた固有の署名で見分ける。`LLMHF_INJECTED` フラグで見分けると、他のソフトが送った入力（リモート操作ツールなど）も「自分の入力」として除外してしまうため、使わない

## Research Log

### タスクトレイのライブラリ
- **Context**: 要件 3（タスクトレイ常駐）。steering で「cgo を使わず、単一の静的 exe にする」と決めている
- **Sources Consulted**: pkg.go.dev/fyne.io/systray。スクラッチ領域でのビルド確認
- **Findings**:
  - 最新は v1.12.2（2026-06-09 公開）。使う API は `Run(onReady, onExit)`、`SetIcon([]byte)`、`SetTooltip(string)`、`AddMenuItem(title, tooltip)`、`Quit()`
  - Windows ではアイコンは `.ico` 形式のバイト列で渡す必要がある
  - ほかのメソッドはどの goroutine から呼んでもよい。`Run` はイベントループが終わるまで戻らない
  - Windows で `CGO_ENABLED=0` のビルドが通ることを確認した。依存として `golang.org/x/sys` も一緒に入る
- **Implications**: 外部依存は `x/sys/windows` と `fyne.io/systray` の 2 つになる（steering では 1 つとしていたため、更新が必要）。アイコンは `.ico` ファイルを `go:embed` で exe に埋め込む

### 低レベルマウスフック
- **Context**: 要件 1.1（クリック・ホイールも数える）、1.4（自分の移動は除外する）、2.4（1 周の途中のユーザー操作を検知する）
- **Sources Consulted**: Microsoft Learn「MSLLHOOKSTRUCT」
- **Findings**:
  - `MSLLHOOKSTRUCT` には `pt`（Per-Monitor DPI 基準の座標）、`flags`（`LLMHF_INJECTED` = 0x1）、`dwExtraInfo` が入っている
  - フックの処理は速く返す必要がある。返すのが遅いと、OS がフックを外してしまう（LowLevelHooksTimeout）
  - フックを登録したスレッドで、メッセージループを回す必要がある
- **Implications**: フック専用の OS スレッドを用意し（`runtime.LockOSThread` + `GetMessage` ループ）、コールバックでは時刻を記録するだけにする。終了するときは `PostThreadMessage(WM_QUIT)` を送る

### カーソルの移動方法
- **Context**: 要件 2.1、2.2。業務アプリやチャットツールに「操作あり」と判定させる必要がある
- **Findings**:
  - `SendInput` を使うと、本物の入力と同じ流れで処理され、OS のアイドル計測もリセットされる。`SetCursorPos` はカーソル位置を変えるだけで、入力として扱われないことがある
  - 相対移動（`MOUSEEVENTF_MOVE` だけ）だと、マウスの加速（「ポインターの精度を高める」設定）の影響で、実際の移動量が 10px にならない
  - 絶対座標（`MOUSEEVENTF_ABSOLUTE | MOUSEEVENTF_VIRTUALDESK`）で指定する場合、座標を 0〜65535 に正規化する。このとき丸め誤差で 1px ずれることがある
  - DPI を認識しないプロセスでは、`GetCursorPos` が仮想化された座標を返す。そのため、プロセスを Per-Monitor DPI Aware V2 に設定する
- **Implications**: 移動は絶対座標の `SendInput` で行う。最後の 1 歩のあと実際の位置を確かめ、ずれていたら `SetCursorPos` で補正する。補正によるフックイベントがユーザーの操作と見なされないよう、「最後に指示した座標と同じ位置への、注入された入力」も自分の入力として除外する

### UIPI（整合性レベル）による制約
- **Context**: 要件 2.5（OS が移動を受け付けない場合）。Adjacent expectations
- **Findings**:
  - 管理者権限で動くウィンドウが前面にあると、一般権限のプロセスが送った `SendInput` は黙って捨てられることがある（エラーにならない）
  - 同じ状況では、一般権限のフックにイベントが届かないこともある
- **Implications**: フックで取りこぼしても検知できるよう、1 秒ごとにカーソル位置を確認する補助の仕組みを残す（予想外の位置に変わっていたら、ユーザーの操作と見なす）。`SendInput` が黙って捨てられる場合は検知しない。要件の Adjacent expectations で許容されている範囲

## Architecture Pattern Evaluation

| Option | Description | Strengths | Risks / Limitations | Notes |
|--------|-------------|-----------|---------------------|-------|
| Ports & Adapters（採用） | ドメイン（判定ループと 1 周の座標計算）が `Mouse` / `Clock` インターフェースに依存し、Win32 の実装を `platform` に置く | 判定ロジックを偽の時計でテストできる。steering の構成方針と一致する | アダプター層の分だけファイルが増える | structure.md の依存の向き `cmd → domain ← platform` に従う |
| main にまとめて書く | Win32 を直接呼ぶループを main に書く | コード量が最も少ない | 時間に関する要件（5 分、繰り返し、途中の中断）を自動テストできない | 採用しない |

## Design Decisions

### Decision: アイドル検知はフックを主、ポーリングを補助にする
- **Context**: 要件 1.1、1.4、2.4
- **Alternatives Considered**:
  1. カーソル位置のポーリングだけ — クリックとホイールを検知できない
  2. `GetLastInputInfo` — キーボード入力も数えてしまい、要件 1.5 に違反する
  3. `WH_MOUSE_LL` フック + ポーリング（採用）
- **Selected Approach**: フックで、自分以外のマウスイベントをすべて「操作あり」として記録する。1 秒ごとのポーリングでは、カーソル位置が予想外に変わっていないかを確認して、フックの取りこぼしを補う
- **Rationale**: 要件 1.1 を満たせる。UIPI でフックに届かない場面にも対応できる
- **Trade-offs**: フック専用スレッドと、そのメッセージループの実装が必要になる
- **Follow-up**: `SetCursorPos` で補正したときにフックへ注入イベントが届くかを、実機で確かめる（届いても除外条件で無視されることを確認する）

### Decision: 次の 1 周までの時間は「最後の操作」と「最後の 1 周」の遅い方から測る
- **Context**: 要件 2.1（最後の操作から 5 分で 1 周）、2.3（その後は 5 分ごとに繰り返す）
- **Selected Approach**: 基準時刻 = max(最後のユーザー操作, 最後の 1 周を終えた時刻) とし、`現在時刻 − 基準時刻 ≥ アイドル時間` になったら 1 周させる
- **Rationale**: 2 つの要件を 1 つの条件式で表せる（一般化）。自分の移動は「操作」として記録しないので、要件 1.4 とも矛盾しない
- **Trade-offs**: なし

### Decision: 画面の端では座標を画面内に収める
- **Context**: 要件 2.1、2.2。カーソルが画面の上端や右端にあると、10px 動かせない方向がある
- **Alternatives Considered**:
  1. 動かせないときは逆向きに動かす — 要件の「上 → 右 → 下 → 左」の順番が変わってしまう
  2. 各目標座標を仮想スクリーンの範囲内に収める（採用）
- **Selected Approach**: 4 つの目標座標をそれぞれ画面の範囲内に収める。最後の目標は常に開始位置にする
- **Rationale**: 順番が保たれ、必ず開始位置に戻る。どこか 1 方向には必ず動くので、「操作あり」と判定される効果も残る

### Decision: 設定は起動オプションで渡し、時間は Go の duration 形式にする
- **Context**: 要件 4
- **Selected Approach**: `-idle`（例: `5m`、`90s`）と `-step`（正の整数、px）。値がおかしいとき、または知らないオプションがあるときは、`MessageBoxW` で理由を表示し、終了コード 2 で終わる
- **Rationale**: 標準の `flag` パッケージだけで実装できる。`-H=windowsgui` でビルドするとコンソールが出ないので、エラーはメッセージボックスで見せる

### Build vs Adopt
- タスクトレイ: **採用**（fyne.io/systray）。`Shell_NotifyIcon` とメニュー用ウィンドウを自作すると、数百行の Win32 コードが必要になる
- マウスフック・移動・DPI: **自作**。数個の API を呼ぶだけで、ラッパーライブラリを入れるほどではない

### Simplification
- 設定を保持する専用の層や、ログの仕組みは作らない（要件にない）。ツールチップの文言は起動時に一度だけ設定する
- アイドル時刻の記録（Tracker）は、atomic な int64 を 1 つ持つだけの小さな型にする。判定ループと同じ `jiggle` パッケージにまとめ、パッケージを増やさない

## Risks & Mitigations
- 管理者権限のウィンドウが前面にあると移動が効かない — 許容する（Adjacent expectations）。ツールは異常終了しない
- フックのコールバックが遅いと、OS にフックを外される — コールバックでは atomic な書き込みだけを行う。ポーリングの補助もある
- ウイルス対策ソフトや EDR が、入力を注入するツールとして警告する可能性がある — 自分専用なので許容する。exe 1 つで、ネットワークに接続しない構成にしておく
- 絶対座標の丸め誤差 — 最後に位置を確かめ、`SetCursorPos` で補正する

## References
- [fyne.io/systray](https://pkg.go.dev/fyne.io/systray) — タスクトレイの API。Windows のアイコン形式
- [MSLLHOOKSTRUCT](https://learn.microsoft.com/en-us/windows/win32/api/winuser/ns-winuser-msllhookstruct) — フックで受け取る構造体の flags と dwExtraInfo
- 参考にしたスキル: kiro-spec-design の design-principles、design-discovery-full、design-synthesis、design-review-gate
