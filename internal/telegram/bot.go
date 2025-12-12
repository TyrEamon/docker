package telegram

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"my-bot-go/internal/config"
	"my-bot-go/internal/database"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type BotHandler struct {
	API *bot.Bot
	Cfg *config.Config
	DB  *database.D1Client
}

func NewBot(cfg *config.Config, db *database.D1Client) (*BotHandler, error) {
	opts := []bot.Option{
		bot.WithDefaultHandler(func(ctx context.Context, b *bot.Bot, update *models.Update) {
			// 默认不做处理
		}),
	}

	b, err := bot.New(cfg.BotToken, opts...)
	if err != nil {
		return nil, err
	}
	
	h := &BotHandler{API: b, Cfg: cfg, DB: db}
	
	// 注册手动转发监听
	b.RegisterHandler(bot.HandlerTypeMessageText, "", bot.MatchTypePrefix, h.handleManual)
	b.RegisterHandler(bot.HandlerTypeMessageText, "", bot.MatchTypePrefix, func(ctx context.Context, b *bot.Bot, update *models.Update) {
		// Fallback for photos if necessary, usually check update.Message.Photo
		if update.Message != nil && len(update.Message.Photo) > 0 {
			h.handleManual(ctx, b, update)
		}
	})

	return h, nil
}

func (h *BotHandler) Start(ctx context.Context) {
	h.API.Start(ctx)
}

// ProcessAndSend 处理并发送图片
func (h *BotHandler) ProcessAndSend(ctx context.Context, imgData []byte, postID, tags, caption, source string) {
	// 1. 发送图片
	params := &bot.SendPhotoParams{
		ChatID:  h.Cfg.ChannelID,
		Photo:   &models.InputFileUpload{Filename: source + ".jpg", Data: bytes.NewReader(imgData)},
		Caption: caption,
	}

	msg, err := h.API.SendPhoto(ctx, params)
	if err != nil {
		log.Printf("❌ Telegram Send Failed [%s]: %v", postID, err)
		return
	}

	// 2. 获取 FileID (取最大尺寸)
	if len(msg.Photo) == 0 {
		return 
	}
	fileID := msg.Photo[len(msg.Photo)-1].FileID

	// 3. 存库
	err = h.DB.SaveImage(postID, fileID, caption, tags, source)
	if err != nil {
		log.Printf("❌ D1 Save Failed: %v", err)
	} else {
		log.Printf("✅ Saved: %s", postID)
	}
}

func (h *BotHandler) handleManual(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil || len(update.Message.Photo) == 0 {
		return
	}
	
	photo := update.Message.Photo[len(update.Message.Photo)-1]
	postID := fmt.Sprintf("manual_%d", update.Message.ID)
	caption := update.Message.Caption
	if caption == "" {
		caption = "Forwarded Image"
	}

	// 既然已经是 TG 图片，直接用 FileID 发送
	msg, err := b.SendPhoto(ctx, &bot.SendPhotoParams{
		ChatID:  h.Cfg.ChannelID,
		Photo:   &models.InputFileString{Data: photo.FileID},
		Caption: caption,
	})
	
	if err != nil {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   "❌ Forward failed: " + err.Error(),
		})
		return
	}

	finalFileID := msg.Photo[len(msg.Photo)-1].FileID
	h.DB.SaveImage(postID, finalFileID, caption, "manual forwarded", "manual")
	
b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   "✅ Saved to D1!",
		ReplyParameters: &models.ReplyParameters{
			MessageID: update.Message.ID,
		},
	})
}
