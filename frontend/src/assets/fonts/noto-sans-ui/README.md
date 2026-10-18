# 中文界面补充字体

现有 Noto Sans SC 分片没有覆盖全部中文界面。本目录的 `ui-supplement.woff2` 使用 Google Fonts 官方 CSS2 `text` 子集接口，仅补现有分片缺少的字形，文件保留 100–900 的可变字重；CSS 与既有分片一致，分别声明 400、500、700，避免同一字体族混用范围字重与静态字重导致浏览器排除部分分片。浏览器直接加载本地文件，不请求外部字体服务。展示字体继续优先使用现有 Serif；缺字时回退到自托管 Sans。

覆盖语料包含 `src` 下全部生产 TypeScript、Vue、CSS、HTML、JSON、SVG，包括集中翻译、组件内 `messages`、固定中文及注释。测试文件与字体来源记录不计入语料。检查汉字、中文标点及全角字符；不承诺覆盖外部用户名或服务器内容中的任意 Unicode。

`source.json` 记录官方 CSS 请求 URL、返回字体 URL、User-Agent 和 SHA-256；`source-response.txt` 保留官方原始响应。`coverage.json` 由 fontTools 读取每个真实字体的 cmap 生成，并绑定字体 SHA-256。检查同时考虑 CSS `unicode-range`，声明范围不能替代实际字形。

字体遵循 SIL Open Font License 1.1，完整条款及 Adobe 原始版权归属见 [OFL.txt](./OFL.txt)。官方来源是 [Google Fonts Noto Sans SC](https://github.com/google/fonts/tree/main/ofl/notosanssc)，子集请求格式见 [CSS2 API 文档](https://developers.google.com/fonts/docs/css2)。没有修改字体内的名称或字形。`public/licenses/noto-sans-sc-OFL.txt` 携带相同的完整许可，Vite 会将它复制到分发产物；离线回归核对两份字节一致。

在独立 Python 环境安装工具依赖；应用本身不增加运行时依赖：

```sh
python3 -m venv /tmp/ren2hub-font-tools
/tmp/ren2hub-font-tools/bin/pip install 'fonttools[woff]==4.60.1'
cd frontend
# 离线读取真实 cmap 并验证当前生产语料
/tmp/ren2hub-font-tools/bin/python scripts/verify-font-coverage.py
# 新文案引入缺字时，从官方重新生成最小补充子集并更新来源及 cmap 记录
/tmp/ren2hub-font-tools/bin/python scripts/verify-font-coverage.py --update
# 只更新已验证的本地 cmap 记录，不访问网络
/tmp/ren2hub-font-tools/bin/python scripts/verify-font-coverage.py --write-manifest
bun run test:run scripts/tests/fontCoverage.spec.ts
```

Google Fonts 的动态服务可能更新字体版本，重新生成后应审查 `source.json` 中的 URL 和哈希变化；需要重取同一版本时使用已记录的 `font_url` 并验证 `font_sha256`。普通 Vitest 回归离线核对当前语料、字体哈希、cmap 记录、CSS 范围及回退链，无需 Python。
