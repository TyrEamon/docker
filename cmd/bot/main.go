package main

import (
\t"context"
\t"log"
\t"my-bot-go/internal/config"
\t"my-bot-go/internal/crawler"
\t"my-bot-go/internal/database"
\t"my-bot-go/internal/telegram"
\t"os"
\t"os/signal"
)

func main() {
\tlog.Println("🚀 Starting Go-MtcACG Bot...")
\t
\t// 1. 加载配置
\tcfg := config.Load()
\tif cfg.BotToken == "" {
\t\tlog.Fatal("❌ BOT_TOKEN is missing")
\t}

\t// 2. 初始化数据库客户端
\tdb := database.NewD1Client(cfg)
\tdb.SyncHistory() // 启动时同步一次

\t// 3. 初始化 Bot
\tbotHandler, err := telegram.NewBot(cfg, db)
\tif err != nil {
\t\tlog.Fatal(err)
\t}

\tctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
\tdefer cancel()

\t// 4. 启动爬虫 (并发运行)
\tgo crawler.StartYande(ctx, cfg, db, botHandler)
\tgo crawler.StartPixiv(ctx, cfg, db, botHandler)

\t// 5. 启动 Bot 监听 (阻塞主线程)
\tlog.Println("👂 Bot is listening...")
\tbotHandler.Start(ctx)
}