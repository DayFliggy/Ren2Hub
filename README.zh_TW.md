# Ren2Hub

Ren2Hub / DayFliggy 是基於 [NewAPI](https://github.com/QuantumNous/new-api) 的單節點 AI 閘道，使用 Vue 控制台。NewAPI 是唯一上游，目前核心原始碼基線為 `09109ec315a5086366f94bcce00f177ec831dbe1`。

錢包計費、預設模型價格、協定、任務外掛與權限沿用 NewAPI。業務群組已移除；每個 Key 使用自動或有序手動渠道路由，支援渠道倍率，以及不排隊的使用者與渠道併發限制。保留工單、GMPay、統一模型目錄、統計與邀請回饋。站內訂閱、官方訂閱帳號、type57、io.net、Playground、React 和 Electron 已退役。

本輪使用獨立新資料庫，不匯入舊業務資料或支付商戶。新程式在遷移前拒絕舊 Ren2Hub 帳務庫。請讀[文件入口](docs/README.md)與[安裝說明](docs/installation/DOCKER.md)；原始碼、候選驗證和正式部署分別記錄。

保留 NewAPI、One API 的來源與版權聲明，以 [GNU AGPL v3](LICENSE) 分發。另見 [NOTICE](NOTICE) 與[第三方授權](THIRD-PARTY-LICENSES.md)。

[简体中文](README.zh_CN.md) · [English](README.md)
