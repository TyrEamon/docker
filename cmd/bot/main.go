package main

import (
	"context"
	"log"
	"time"
	"my-bot-go/internal/config"
	"my-bot-go/internal/crawler"
	"my-bot-go/internal/database"
	"my-bot-go/internal/telegram"
	"os"
	"os/signal"
)

func main() {
	log.Println("🚀 Starting Go-MtcACG Bot...")
	
	cfg := config.Load()
	if cfg.BotToken == "" {
		log.Fatal("❌ BOT_TOKEN is missing")
	}

	db := database.NewD1Client(cfg)
	db.SyncHistory() 

	
	botHandler, err := telegram.NewBot(cfg, db)
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	
	go crawler.StartYande(ctx, cfg, db, botHandler)
	
    go func() {
        time.Sleep(5 * time.Minute)
        crawler.StartPixiv(ctx, cfg, db, botHandler)
    }()

	//其他爬虫脚本，还为完善。
	///go crawler.StartDanbooru(ctx, cfg, db, botHandler)///
	///go crawler.StartKemono(ctx, cfg, db, botHandler)///

    go func() {
        time.Sleep(10 * time.Minute)
        crawler.StartCosineTag(ctx, cfg, db, botHandler)
    }()

    go func() {
        time.Sleep(15 * time.Minute)
        crawler.StartManyACGAll(ctx, cfg, db, botHandler)
    }()

	//没必要开了
	///go crawler.StartManyACGSese(ctx, cfg, db, botHandler)///

    go func() {
        time.Sleep(20 * time.Minute)
        crawler.StartManyACG(ctx, cfg, db, botHandler)
    }()

	log.Println("👂 Bot is listening...")
	botHandler.Start(ctx)

	log.Println("🛑 Shutting down... Saving history...")
	db.PushHistory()
	log.Println("👋 Bye!")
}
