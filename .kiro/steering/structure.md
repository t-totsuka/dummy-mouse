# プロジェクト構成

## 構成の方針

Go の標準的な `cmd/` + `internal/` レイアウトを使う。責務ごとに層を分け、**OS に依存する処理（Win32 API）を 1 か所に閉じ込める**。判定ロジックは OS に依存しないパッケージに置き、インターフェース経由でテストできるようにする。

## ディレクトリのパターン

### エントリーポイント
**場所**: `/cmd/<app>/`
**用途**: `main` パッケージ。依存を組み立てて（実際の Clock とマウスを注入して）、シグナルを受けたら終了するだけにする。ロジックは書かない
**例**: `cmd/mousemover/main.go`

### ドメインロジック
**場所**: `/internal/<domain>/`
**用途**: アイドル判定、移動方向の切り替え、スケジューリングなど、OS に依存しないロジック。外部とのやり取りはインターフェース（`Mouse`, `Clock`）越しに行う
**例**: `internal/idle/`（アイドル判定）、`internal/jiggle/`（移動パターン）

### OS アダプター
**場所**: `/internal/platform/` などのアダプターパッケージ
**用途**: Win32 API 呼び出しの薄いラッパー。ドメイン側のインターフェースを実装する。Windows 専用のファイルには `_windows.go` サフィックスかビルドタグを付ける
**例**: `internal/platform/mouse_windows.go`

### 配布物
**場所**: `/dist/`（git 管理の対象外）
**用途**: ビルドした exe の出力先

## 命名規則

- **ファイル**: `snake_case.go`。OS 専用のファイルは `_windows.go` サフィックス。テストは `_test.go`
- **パッケージ**: 短い小文字の単数形（`idle`, `jiggle`, `platform`）
- **公開している識別子**: `PascalCase`。インターフェースは役割を表す名詞にする（`Mouse`, `Clock`）
- **定数**: 既定値は意味が分かる名前にする（`DefaultIdleThreshold`, `DefaultStepPixels`）

## import の整理

```go
import (
    "context"   // 標準ライブラリ
    "time"

    "golang.org/x/sys/windows"   // 外部依存

    "<module>/internal/idle"   // 自分のモジュール内
)
```

- グループの順番は「標準 → 外部 → 自モジュール」で、`goimports` の形式に合わせる
- 依存の向きは `cmd` → `internal/<domain>` ← `internal/platform`。ドメインパッケージは `platform` も `x/sys/windows` も import しない

## コード構成の原則

- **インターフェースは使う側で定義する**: `Mouse` / `Clock` は、それを使うドメインパッケージで宣言する
- **副作用は端に寄せる**: 実時間・実カーソルに触れるのは `main` で注入した実装だけ
- **設定値は 1 か所にまとめる**: 間隔（5 分）、移動量（10px）、ポーリング周期は 1 つの設定構造体にまとめ、`main` から渡す

---
_ファイルツリーではなくパターンを記録する。パターンに沿って追加したファイルなら、ここを更新する必要はない_
