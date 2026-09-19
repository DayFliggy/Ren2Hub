<div align="center">

![new-api](/frontend/public/logo.png)

# New API

🍥 **新一代大模型網關與AI資產管理系統**

<p align="center">
  繁體中文 |
  <a href="./README.zh_CN.md">简体中文</a> |
  <a href="./README.md">English</a> |
  <a href="./README.fr.md">Français</a> |
  <a href="./README.ja.md">日本語</a>
</p>

<p align="center">
  <a href="https://raw.githubusercontent.com/Calcium-Ion/new-api/main/LICENSE">
    <img src="https://img.shields.io/github/license/Calcium-Ion/new-api?color=brightgreen" alt="license">
  </a>
  <a href="https://github.com/Calcium-Ion/new-api/releases/latest">
    <img src="https://img.shields.io/github/v/release/Calcium-Ion/new-api?color=brightgreen&include_prereleases" alt="release">
  </a>
  <a href="https://hub.docker.com/r/CalciumIon/new-api">
    <img src="https://img.shields.io/badge/docker-dockerHub-blue" alt="docker">
  </a>
  <a href="https://atomgit.com/QuantumNous/new-api" target="_blank">
    <img alt="AtomGit G-Star" src="https://atomgit.com/QuantumNous/new-api/star/badge.svg"/>
  </a>
</p>

<p align="center">
  <a href="https://trendshift.io/repositories/20180" target="_blank">
    <img src="https://trendshift.io/api/badge/repositories/20180" alt="QuantumNous%2Fnew-api | Trendshift" style="width: 250px; height: 55px;" width="250" height="55"/>
  </a>
  <br>
  <a href="https://hellogithub.com/repository/QuantumNous/new-api" target="_blank">
    <img src="https://api.hellogithub.com/v1/widgets/recommend.svg?rid=539ac4217e69431684ad4a0bab768811&claim_uid=tbFPfKIDHpc4TzR" alt="Featured｜HelloGitHub" style="width: 250px; height: 54px;" width="250" height="54" />
  </a>
  <a href="https://atomgit.com/QuantumNous/new-api" target="_blank">
    <img alt="AtomGit G-Star" src="https://atomgit.com/QuantumNous/new-api/star/new_badge.svg" width="250" height="55" />
  </a>
</p>

<p align="center">
  <a href="#-快速開始">快速開始</a> •
  <a href="#-主要特性">主要特性</a> •
  <a href="#-部署">部署</a> •
  <a href="#-文件">文件</a> •
  <a href="#-幫助支援">幫助</a>
</p>

</div>

## 📝 項目說明

目前檢出的專案為 Ren2Hub，路由、准入、API 與發布契約請從[本機文件](docs/README.md)進入。下文保留上游專案介紹和署名；部署以 [Ren2Hub 安裝文件](docs/installation/DOCKER.md)為準，上游映像不包含本次檢出的修改。Ren2Hub 採用單一程序本機 FIFO 准入，新舊版本必須依序切換。RenAI 工作區使用獨立部署資產和 PostgreSQL。

計費使用錢包餘額，Token 額度作為附加消費限制。本站訂閱與方案管理已移除，歷史帳務資料持續保留並受保護；專屬提交與驗證邊界見[現行驗收紀錄](docs/routing/SINGLE_INSTANCE_ACCEPTANCE.md)。

> [!IMPORTANT]
> - 本專案僅面向合法授權的 AI API 閘道、組織內部鑑權、多模型管理、用量統計、成本核算和私有化部署場景。
> - 使用者必須合法取得上游 API Key、帳號、模型服務或介面權限，並遵守上游服務條款及適用法律法規。
> - 使用者應確保其使用方式符合上游服務條款及適用法律法規。
> - 面向公眾提供生成式人工智慧服務時，使用者應遵守[《生成式人工智慧服務管理暫行辦法》](http://www.cac.gov.cn/2023-07/13/c_1690898327029107.htm)等監管要求，自行完成所在司法轄區要求的備案、許可、內容安全、實名、日誌留存、稅務和上游授權等合規義務。

---

## 🤝 我們信任的合作伙伴

<p align="center">
  <em>排名不分先後</em>
</p>

<p align="center">
  <a href="https://www.cherry-ai.com/" target="_blank">
    <img src="./docs/images/cherry-studio.png" alt="Cherry Studio" height="80" />
  </a><!--
  --><a href="https://github.com/iOfficeAI/AionUi/" target="_blank">
    <img src="./docs/images/aionui.png" alt="Aion UI" height="80" />
  </a><!--
  --><a href="https://bda.pku.edu.cn/" target="_blank">
    <img src="./docs/images/pku.png" alt="北京大學" height="80" />
  </a><!--
  --><a href="https://www.compshare.cn/?ytag=GPU_yy_gh_newapi" target="_blank">
    <img src="./docs/images/ucloud.png" alt="UCloud 優刻得" height="80" />
  </a><!--
  --><a href="https://www.aliyun.com/" target="_blank">
    <img src="./docs/images/aliyun.png" alt="阿里雲" height="80" />
  </a><!--
  --><a href="https://io.net/" target="_blank">
    <img src="./docs/images/io-net.png" alt="IO.NET" height="80" />
  </a>
</p>

---

## 🙏 特別鳴謝

<p align="center">
  <a href="https://www.jetbrains.com/?from=new-api" target="_blank">
    <img src="https://resources.jetbrains.com/storage/products/company/brand/logos/jb_beam.png" alt="JetBrains Logo" width="120" />
  </a>
</p>

<p align="center">
  <strong>感謝 <a href="https://www.jetbrains.com/?from=new-api">JetBrains</a> 為本項目提供免費的開源開發許可證</strong>
</p>

---

## 🚀 快速開始

依[單一實例 Docker 安裝文件](docs/installation/DOCKER.md)部署目前的 Ren2Hub 原始碼。建置包含 Go 後端和 Vue 前端，使用 PostgreSQL 與 Redis；先填寫必要密鑰再啟動。應用程式只繫結 `127.0.0.1:3000`，容器不設定 CPU 或記憶體配額。

---

## 📚 文件

<div align="center">

### 📖 [官方文件](https://docs.newapi.pro/zh/docs) | [![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/QuantumNous/new-api)

</div>

**快速導航：**

| 分類 | 連結 |
|------|------|
| 🚀 部署指南 | [安裝文件](https://docs.newapi.pro/zh/docs/installation) |
| ⚙️ 環境配置 | [環境變數](https://docs.newapi.pro/zh/docs/installation/config-maintenance/environment-variables) |
| 📡 接口文件 | [API 文件](https://docs.newapi.pro/zh/docs/api) |
| ❓ 常見問題 | [FAQ](https://docs.newapi.pro/zh/docs/support/faq) |
| 💬 社群交流 | [交流管道](https://docs.newapi.pro/zh/docs/support/community-interaction) |

---

## ✨ 主要特性

> 詳細特性請參考 [特性說明](https://docs.newapi.pro/zh/docs/guide/wiki/basic-concepts/features-introduction)

### 🎨 核心功能

| 特性 | 說明 |
|------|------|
| 🎨 全新 UI | 現代化的用戶界面設計 |
| 🌍 多語言 | Vue 介面支援簡體中文、英文 |
| 🔄 數據兼容 | 支援 SQLite、MySQL、PostgreSQL；升級遵守本機遷移與發布契約 |
| 📈 數據看板 | 視覺化控制檯與統計分析 |
| 🔒 權限管理 | 令牌分組、模型限制、用戶管理 |

### 💰 授權用量與成本管理

- ✅ 合法授權場景下的內部儲值與額度分配（易支付、Stripe）
- ✅ 組織內按次、按量或快取命中成本核算
- ✅ 支援 OpenAI、Azure、DeepSeek、Claude、Qwen 等模型的快取計費統計
- ✅ 面向內部管理或企業客戶的靈活計費策略配置

### 🔐 授權與安全

- 😈 Discord 授權登錄
- 🤖 LinuxDO 授權登錄
- 📱 Telegram 授權登錄
- 🔑 OIDC 統一認證
- 🔍 Key 查詢使用額度（配合 [new-api-key-tool](https://github.com/Calcium-Ion/new-api-key-tool)）

### 🚀 高級功能

**API 格式支援：**
- ⚡ [OpenAI Responses](https://docs.newapi.pro/zh/docs/api/ai-model/chat/openai/create-response)
- ⚡ [OpenAI Realtime API](https://docs.newapi.pro/zh/docs/api/ai-model/realtime/create-realtime-session)（含 Azure）
- ⚡ [Claude Messages](https://docs.newapi.pro/zh/docs/api/ai-model/chat/create-message)
- ⚡ [Google Gemini](https://doc.newapi.pro/api/google-gemini-chat)
- 🔄 [Rerank 模型](https://docs.newapi.pro/zh/docs/api/ai-model/rerank/create-rerank)（Cohere、Jina）

**智慧路由：**
- ⚖️ 管道加權隨機
- 🔄 失敗自動重試
- 🚦 用戶級別模型限流

**格式轉換：**

Chat、Responses、Claude、Gemini 使用統一轉換規劃。已支援的工具、媒體、引用、終態語意及明確拒絕邊界集中維護於[協定支援矩陣](docs/routing/GATEWAY_SUPPORT_MATRIX.md)。

**Reasoning Effort 支援：**

<details>
<summary>查看詳細配置</summary>

**OpenAI 系列模型：**
- `o3-mini-high` - High reasoning effort
- `o3-mini-medium` - Medium reasoning effort
- `o3-mini-low` - Low reasoning effort
- `gpt-5-high` - High reasoning effort
- `gpt-5-medium` - Medium reasoning effort
- `gpt-5-low` - Low reasoning effort

**Claude 思考模型：**
- `claude-3-7-sonnet-20250219-thinking` - 啟用思考模式

**Google Gemini 系列模型：**
- `gemini-2.5-flash-thinking` - 啟用思考模式
- `gemini-2.5-flash-nothinking` - 禁用思考模式
- `gemini-2.5-pro-thinking` - 啟用思考模式
- `gemini-2.5-pro-thinking-128` - 啟用思考模式，並設置思考預算為128tokens
- 也可以直接在 Gemini 模型名稱後追加 `-low` / `-medium` / `-high` 來控制思考力道（無需再設置思考預算後綴）

</details>

---

## 🤖 模型支援

> 詳情請參考 [接口文件 - 閘道接口](https://docs.newapi.pro/zh/docs/api)

| 模型類型 | 說明 | 文件 |
|---------|------|------|
| 🤖 OpenAI-Compatible | OpenAI 兼容模型 | [文件](https://docs.newapi.pro/zh/docs/api/ai-model/chat/openai/createchatcompletion) |
| 🤖 OpenAI Responses | OpenAI Responses 格式 | [文件](https://docs.newapi.pro/zh/docs/api/ai-model/chat/openai/createresponse) |
| 🎨 Midjourney-Proxy | [Midjourney-Proxy(Plus)](https://github.com/novicezk/midjourney-proxy) | [文件](https://doc.newapi.pro/api/midjourney-proxy-image) |
| 🎵 Suno-API | [Suno API](https://github.com/Suno-API/Suno-API) | [文件](https://doc.newapi.pro/api/suno-music) |
| 🔄 Rerank | Cohere、Jina | [文件](https://docs.newapi.pro/zh/docs/api/ai-model/rerank/create-rerank) |
| 💬 Claude | Messages 格式 | [文件](https://docs.newapi.pro/zh/docs/api/ai-model/chat/createmessage) |
| 🌐 Gemini | Google Gemini 格式 | [文件](https://docs.newapi.pro/zh/docs/api/ai-model/chat/gemini/geminirelayv1beta) |
| 🔧 Dify | ChatFlow 模式 | - |
| 🎯 自訂上游 | 支援配置合法授權的上游介面位址 | - |

### 📡 支援的接口

<details>
<summary>查看完整接口列表</summary>

- [聊天接口 (Chat Completions)](https://docs.newapi.pro/zh/docs/api/ai-model/chat/openai/createchatcompletion)
- [響應接口 (Responses)](https://docs.newapi.pro/zh/docs/api/ai-model/chat/openai/createresponse)
- [圖像接口 (Image)](https://docs.newapi.pro/zh/docs/api/ai-model/images/openai/post-v1-images-generations)
- [音訊接口 (Audio)](https://docs.newapi.pro/zh/docs/api/ai-model/audio/openai/create-transcription)
- [影片接口 (Video)](https://docs.newapi.pro/zh/docs/api/ai-model/audio/openai/createspeech)
- [嵌入接口 (Embeddings)](https://docs.newapi.pro/zh/docs/api/ai-model/embeddings/createembedding)
- [重排序接口 (Rerank)](https://docs.newapi.pro/zh/docs/api/ai-model/rerank/creatererank)
- [即時對話 (Realtime)](https://docs.newapi.pro/zh/docs/api/ai-model/realtime/createrealtimesession)
- [Claude 聊天](https://docs.newapi.pro/zh/docs/api/ai-model/chat/createmessage)
- [Google Gemini 聊天](https://docs.newapi.pro/zh/docs/api/ai-model/chat/gemini/geminirelayv1beta)

</details>

---

## 🚢 部署

使用維護中的 [Compose 設定](docker-compose.yml)與[安裝文件](docs/installation/DOCKER.md)。前端開發使用 [docker-compose.dev.yml](docker-compose.dev.yml)，面板環境見[寶塔說明](docs/installation/BT.md)。上游 New-API 映像不包含目前檢出的修改。

Ren2Hub 的使用者與管道並行額度由單一程序持有。先排空並退出舊閘道，再啟動新閘道，遵守[發布與回復流程](docs/routing/GATEWAY_RELEASE.md)，不得滾動多副本。PostgreSQL、Redis 與應用資料必須沿用已確認的資料卷。

環境變數見 [.env.example](.env.example)，認證與代理設定見[登入工作階段契約](docs/authentication.md)。FIFO 限制與共用重試預算以[閘道 API](docs/routing/GATEWAY_API.md)及[統一路由](docs/routing/UNIFIED_ROUTING.md)為準。Redis 繼續負責 Provider 配額、頻率、健康和快取，不再維護使用者或管道並行計數。

---

## 🔗 相關項目

### 上游項目

| 項目 | 說明 |
|------|------|
| [One API](https://github.com/songquanpeng/one-api) | 原版項目基礎 |
| [Midjourney-Proxy](https://github.com/novicezk/midjourney-proxy) | Midjourney 接口支援 |

### 配套工具

| 項目 | 說明 |
|------|------|
| [new-api-key-tool](https://github.com/Calcium-Ion/new-api-key-tool) | Key 額度查詢工具 |
| [new-api-horizon](https://github.com/Calcium-Ion/new-api-horizon) | New API 高性能優化版 |

---

## 💬 幫助支援

### 📖 文件資源

| 資源 | 連結 |
|------|------|
| 📘 常見問題 | [FAQ](https://docs.newapi.pro/zh/docs/support/faq) |
| 💬 社群交流 | [交流管道](https://docs.newapi.pro/zh/docs/support/community-interaction) |
| 🐛 回饋問題 | [問題回饋](https://docs.newapi.pro/zh/docs/support/feedback-issues) |
| 📚 完整文件 | [官方文件](https://docs.newapi.pro/zh/docs) |

### 🤝 貢獻指南

歡迎各種形式的貢獻！

- 🐛 報告 Bug
- 💡 提出新功能
- 📝 改進文件
- 🔧 提交程式碼

---

## 📜 許可證

本項目採用 [GNU Affero 通用公共許可證 v3.0 (AGPLv3)](./LICENSE) 授權。

本項目為開源項目，在 [One API](https://github.com/songquanpeng/one-api)（MIT 許可證）的基礎上進行二次開發。

如果您所在的組織政策不允許使用 AGPLv3 許可的軟體，或您希望規避 AGPLv3 的開源義務，請發送郵件至：[support@quantumnous.com](mailto:support@quantumnous.com)

---

## 🌟 Star History

<div align="center">

[![Star History Chart](https://api.star-history.com/svg?repos=Calcium-Ion/new-api&type=Date)](https://star-history.com/#Calcium-Ion/new-api&Date)

</div>

---

<div align="center">

### 💖 感謝使用 New API

如果這個項目對你有幫助，歡迎給我們一個 ⭐️ Star！

**[官方文件](https://docs.newapi.pro/zh/docs)** • **[問題回饋](https://github.com/Calcium-Ion/new-api/issues)** • **[最新發布](https://github.com/Calcium-Ion/new-api/releases)**

<sub>Built with ❤️ by QuantumNous</sub>

</div>
