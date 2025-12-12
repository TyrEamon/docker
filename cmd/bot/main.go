package main

import (
	"context"
	"log"
	"my-bot-go/internal/config"
	"my-bot-go/internal/crawler"
	"my-bot-go/internal/database"
	"my-bot-go/internal/telegram"
	"os"
	"os/signal"
)

func main() {
	log.Println("🚀 Starting Go-MtcACG Bot...")
	
	// 1. 加载配置
	cfg := config.Load()
	if cfg.BotToken == "" {
		log.Fatal("❌ BOT_TOKEN is missing")
	}

	// 2. 初始化数据库客户端
	db := database.NewD1Client(cfg)
	
	// ⭐【关键】程序启动时，从 D1 数据库拉取所有旧图片 ID 到内存
	// 这样爬虫启动后，遇到旧 ID 就会自动跳过，实现去重
	db.SyncHistory() 

	// 3. 初始化 Bot
	botHandler, err := telegram.NewBot(cfg, db)
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// 4. 启动爬虫 (并发运行)
	go crawler.StartYande(ctx, cfg, db, botHandler)
	go crawler.StartPixiv(ctx, cfg, db, botHandler)
	go crawler.StartDanbooru(ctx, cfg, db, botHandler) // 如果你有 Danbooru 爬虫
	go crawler.StartKemono(ctx, cfg, db, botHandler)   // 如果你有 Kemono 爬虫

	// 5. 启动 Bot 监听 (阻塞主线程)
	log.Println("👂 Bot is listening...")
	botHandler.Start(ctx)
}
