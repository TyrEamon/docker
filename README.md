# 🌸 MtcACG - Personal ACG Gallery Ecosystem

<div align="center">
  <img src="https://pub-d07d03b8c35d40309ce9c6d8216e885b.r2.dev/ACGg.png" width="120" alt="MtcACG Logo">
  <br>
  <h3>采集 · 整理 · 展示</h3>
  <p>一个基于 Cloudflare Workers 和 Telegram Bot 的二次元插画聚合与展示平台。</p>
</div>
<div align="center">
  <table>
    <tr>
      <td width="30%" align="center" valign="top">
        <img src="https://pub-d07d03b8c35d40309ce9c6d8216e885b.r2.dev/phone.jpg" alt="Mobile View" width="100%" style="border-radius: 10px;">
      </td>
      <td width="70%" align="center" valign="top">
        <img src="https://pub-d07d03b8c35d40309ce9c6d8216e885b.r2.dev/1765473925321.png" alt="Desktop Home" width="100%" style="border-radius: 10px; margin-bottom: 10px;">
        <img src="https://pub-d07d03b8c35d40309ce9c6d8216e885b.r2.dev/1765474149513.png" alt="Desktop Detail" width="100%" style="border-radius: 10px; margin-bottom: 10px;">
        <img src="https://pub-d07d03b8c35d40309ce9c6d8216e885b.r2.dev/1765474175927.png" alt="Desktop About" width="100%" style="border-radius: 10px;">
      </td>
    </tr>
  </table>
</div>
---

## 📖 项目简介 | Introduction

**MtcACG** 是一个围绕“收集、整理与展示二次元插画”打造的私有化生态系统。

不同于无差别的全网爬虫，MtcACG 旨在构建一个“有温度”的数字画廊。它通过 Telegram Bot 从 Pixiv、Yande等高质量源头采集图片，自动处理去重、压缩与存储，最终通过 Cloudflare Workers 构建的现代化前端进行优雅展示。

既是你的个人图床，也是对外分享的精美图站。

### ✨ 核心特性

*   **多源采集**: 支持 Pixiv (Cookie模式/去重)、Yande等多源抓取。
*   **智能处理**: 自动识别 R-18 内容打标，超大图片自动压缩至 Telegram 限制范围内。
*   **云端记忆**: Bot 与 Worker 联动，通过 API 维护已发送图库，杜绝重复采集。
*   **无服务器架构**: 前端与 API 完全基于 Cloudflare Workers + D1 数据库，低成本、高并发。
*   **沉浸式体验**: 
    *   响应式布局：手机端原生瀑布流，电脑端错落砖墙 (Masonry) 网格。
    *   视觉设计：深色模式、磨砂玻璃 (Glassmorphism)、动态模糊背景。
    *   功能完善：R-18 过滤器、随机抽卡、标签搜索、详情页推荐。

---

## 🏗️ 技术架构 | Architecture

1.  **Collector (Python Bot)**: 
    *   使用 `aiogram` 和 `aiohttp` 进行异步抓取。
    *   使用 `Pillow` 进行图片压缩。
    *   将图片发送至 Telegram 频道作为存储后端 (CDN)。
    *   将元数据 (FileID, Tags, Caption) 写入 Cloudflare D1 数据库。
2.  **Storage (Database)**: 
    *   **Cloudflare D1 (SQLite)**: 存储图片索引和元数据。
    *   **Telegram**: 存储实际的图片文件。
3.  **Frontend (Cloudflare Worker)**: 
    *   提供 HTTP API (搜索/随机图)。
    *   服务端渲染 (SSR) HTML 页面。
    *   处理图床代理 (Telegram File Proxy)。

---

## 🚀 部署指南 | Deployment

### 前置准备

*   一个 **Telegram Bot Token** 和一个 **频道 ID (Channel ID)**。
*   一个 **Cloudflare 账号**。
*   一台可以运行 Python 脚本的服务器 (或本地电脑)。

### 第一步：配置 Cloudflare D1 数据库

1.  在 Cloudflare 控制台或使用 `wrangler` 创建一个 D1 数据库：
    ```bash
    npx wrangler d1 create mtcacg-db
    ```
2.  执行初始化 SQL 创建表结构：
    ```sql
    CREATE TABLE IF NOT EXISTS images (
        id TEXT PRIMARY KEY,
        file_name TEXT,
        caption TEXT,
        tags TEXT,
        created_at INTEGER
    );
    ```
    *你可以在 Cloudflare Dashboard 的 D1 控制台中直接执行此 SQL。*

### 第二步：部署 Cloudflare Worker (前端)

1.  将 `worker.js` 代码准备好。
2.  配置 `wrangler.toml`：
    ```toml
    name = "mtcacg"
    main = "worker.js"
    compatibility_date = "2023-12-01"

    [[d1_databases]]
    binding = "DB"
    database_name = "mtcacg-db"
    database_id = "你的_DATABASE_ID"
    ```
3.  部署 Worker：
    ```bash
    npx wrangler deploy
    ```
4.  记录下你的 Worker 域名 (例如 `https://mtcacg.yourname.workers.dev`)，后续 Bot 需要用到。

### 第三步：运行 Python 采集机器人

1.  克隆仓库并安装依赖：
    ```bash
    pip install -r requirements.txt
    ```
    *`requirements.txt` 内容: `aiogram`, `aiohttp`, `Pillow`, `python-dotenv`*

2.  创建 `.env` 文件，填入配置：
    ```ini
    # Telegram 配置
    BOT_TOKEN=你的BotToken
    CHANNEL_ID=你的频道ID(如 -100xxxxxxxx)

    # Cloudflare 配置 (用于Bot直接写入D1和同步历史)
    CLOUDFLARE_ACCOUNT_ID=你的CF账户ID
    CLOUDFLARE_API_TOKEN=你的CF_API_Token
    D1_DATABASE_ID=你的D1数据库ID

    # Worker 地址 (用于去重同步)
    WORKER_URL=https://你的Worker域名.workers.dev

    # 爬虫配置
    YANDE_LIMIT=1
    PIXIV_PHPSESSID=你的PixivCookie
    PIXIV_ARTIST_IDS=画师ID1,画师ID2
    ```

3.  启动 Bot：
    ```bash
    python bot.py
    ```

---

## 🔌 API 接口 | API Usage

本站对外开放简单的随机图接口，欢迎友站调用。

**获取随机图片**
```http
GET /api/posts?q=random
```

**响应示例:**
```json
[
  {
    "id": "pixiv_123456",
    "file_name": "AgACAgEAA...",
    "caption": "Pixiv: Title...",
    "tags": "R-18 original 1girl",
    "created_at": 1700000000
  }
]
```

---

## 📝 待办事项 | Todo

- [x] 基础采集与展示
- [x] R-18 过滤器
- [x] 移动端/桌面端响应式布局优化
- [x] 自动压缩图片防止发送失败
- [x] About 页面与随机背景
- [ ] 增加更多图源 (Danbooru, Twitter)
- [ ] 增加按热度排序功能

---

## 🤝 贡献与许可 | License

本项目仅供学习交流使用。请遵守各图站的 Robots 协议与版权规定。

Powered by **Cloudflare Workers** & **Telegram**.
Made with ❤️ by [TyrEamon](https://github.com/TyrEamon).
