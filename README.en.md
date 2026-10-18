# Ren2Hub

Ren2Hub / DayFliggy is a single-node AI gateway with a Vue console, based on [NewAPI](https://github.com/QuantumNous/new-api). NewAPI is the only upstream; the current core source baseline is `09109ec315a5086366f94bcce00f177ec831dbe1`.

Wallet billing, default model prices, protocols, task plugins and permissions follow NewAPI. Business groups are removed. Each API token uses automatic or ordered manual channel routing, with channel price ratios and immediate user/channel concurrency limits. Extensions include support tickets, GMPay top-ups, a shared model catalog, usage reporting and referral rewards. The wallet is the only funding source. Site subscription plans, provider subscription accounts, type 57, io.net, Playground, React and Electron are outside this build.

Start with the [documentation](docs/README.md) and [source installation guide](docs/installation/DOCKER.md). This core requires a separate new database; it rejects the retired Ren2Hub billing database before migration. Old business data and merchant configuration are not imported. Deployment and verification status are recorded separately from source changes in the owning workspace.

Ren2Hub retains the NewAPI and One API origins and copyright notices. Distributed under the [GNU AGPL v3](LICENSE); see [NOTICE](NOTICE) and [third-party licenses](THIRD-PARTY-LICENSES.md). Upstream images do not include this checkout's Vue interface or extensions.

[简体中文](README.zh_CN.md) · [繁體中文](README.zh_TW.md) · [English](README.md) · [Français](README.fr.md) · [日本語](README.ja.md)
