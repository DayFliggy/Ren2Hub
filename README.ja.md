# Ren2Hub

Ren2Hub / DayFliggy は Vue 管理画面を備えた単一ノードの AI ゲートウェイです。[NewAPI](https://github.com/QuantumNous/new-api) を唯一の上流とし、現在のコアは `09109ec315a5086366f94bcce00f177ec831dbe1` に基づきます。

自動ルーティング、既定価格、グループ倍率、ウォレット課金、プロトコル、タスクプラグインと権限は NewAPI を使用します。最小限の拡張はサポートチケット、Token ごとの順序付きプライベートチャネル一覧、GMPay 入金です。サブスクリプション、type57、io.net、Playground、React、Electron は廃止しました。

独立した新規データベースが必要です。旧 Ren2Hub 会計データベースは移行前に拒否され、旧業務データや加盟店設定は取り込みません。[文書](docs/README.md)と[インストール手順](docs/installation/DOCKER.md)を参照してください。ソース更新と検証、実運用への公開は別々に記録します。

NewAPI と One API の由来および著作権表示を保持し、[GNU AGPL v3](LICENSE) で配布します。[NOTICE](NOTICE) と[第三者ライセンス](THIRD-PARTY-LICENSES.md)も参照してください。

[English](README.md) · [简体中文](README.zh_CN.md)
