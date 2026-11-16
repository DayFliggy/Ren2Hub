# Ren2Hub

Ren2Hub / DayFliggy は Vue 管理画面を備えた単一ノードの AI ゲートウェイです。[NewAPI](https://github.com/QuantumNous/new-api) を唯一の上流とします。現在のコアの基準コミットは[英語版 README](README.md) に記載しています。

ウォレット課金、プロトコル、タスクプラグインと権限は NewAPI を使用します。モデルの既定価格と組み込み課金式は含みません。汎用プロバイダープラグインはログインと認証情報の更新に対応します。業務グループは廃止しました。各 Key は自動または順序付き手動チャネルルーティングを使用し、チャネル倍率と待機しないユーザー・チャネル同時実行制限を適用します。工单、GMPay、モデルカタログ、統計、紹介報酬を提供します。サイト内サブスクリプション、旧 Codex/type 57 専用連携、io.net、Playground、React、Electron は廃止しました。

初回インストールには独立した新規データベースが必要です。旧 Ren2Hub 会計データベースは移行前に拒否され、旧業務データや加盟店設定は取り込みません。[文書](docs/README.md)と[インストール手順](docs/installation/DOCKER.md)を参照してください。ソース更新と検証、実運用への公開は別々に記録します。

NewAPI と One API の由来および著作権表示を保持し、[GNU AGPL v3](LICENSE) で配布します。[NOTICE](NOTICE) と[第三者ライセンス](THIRD-PARTY-LICENSES.md)も参照してください。

[English](README.md) · [简体中文](README.zh_CN.md)
