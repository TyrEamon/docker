package telegram

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"my-bot-go/internal/config"
	"my-bot-go/internal/database"
	"net/http"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type BotHandler struct {
	API *bot.Bot
	Cfg *config.Config
	DB  *database.D1Client
}

func NewBot(cfg *config.Config, db *database.D1Client) (*BotHandler, error) {
	// 修复：WithHTTPClient 可能需要两个参数 (Duration, HttpClient)
	// 根据报错提示：want (time.Duration, bot.HttpClient)
	// 我们直接传入超时时间和 Client
	opts := []bot.Option{
		bot.WithHTTPClient(90*time.Second, &http.Client{
			Timeout: 90 * time.Second,
		}),
		bot.WithDefaultHandler(func(ctx context.Context, b *bot.Bot, update *models.Update) {
			// 默认不处理
		}),
	}

	b, err := bot.New(cfg.BotToken, opts...)
	if err != nil {
		return nil, err
	}

	h := &BotHandler{API: b, Cfg: cfg, DB: db}

	// 注册指令处理
	b.RegisterHandler(bot.HandlerTypeMessageText, "/manual", bot.MatchTypePrefix, h.handleManual)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/forward", bot.MatchTypePrefix, func(ctx context.Context, b *bot.Bot, update *models.Update) {
		if update.Message != nil && len(update.Message.Photo) > 0 {
			h.handleManual(ctx, b, update)
		}
	})

	return h, nil
}

func (h *BotHandler) Start(ctx context.Context) {
	h.API.Start(ctx)
}

// ProcessAndSend 核心发送逻辑 (带重试和数据库记录)
func (h *BotHandler) ProcessAndSend(
	ctx context.Context,
	imgData []byte,
	postID, tags, caption, source string,
	width, height int,
) {
	params := &bot.SendPhotoParams{
		ChatID:  h.Cfg.ChannelID,
		Photo:   &models.InputFileUpload{Filename: source + ".jpg", Data: bytes.NewReader(imgData)},
		Caption: caption,
	}

	var msg *models.Message
	var err error

	// ♻️ 重试机制：最多重试 3 次
	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		// 使用单独的超时 Context，防止主 Context 意外取消
		sendCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		msg, err = h.API.SendPhoto(sendCtx, params)
		cancel()

		if err == nil {
			break // 发送成功
		}

		// 如果是超时错误或网络错误，等待后重试
		log.Printf("⚠️ Telegram Send Failed [%s] (Attempt %d/%d): %v", postID, i+1, maxRetries, err)
		time.Sleep(time.Duration(3*(i+1)) * time.Second) // 递增等待
	}

	if err != nil {
		log.Printf("❌ Telegram Send Failed Final [%s]: %v", postID, err)
		return
	}

	if len(msg.Photo) == 0 {
		return
	}

	// 获取最高清的 FileID
	fileID := msg.Photo[len(msg.Photo)-1].FileID

	// 保存到 D1 数据库
	err = h.DB.SaveImage(postID, fileID, caption, tags, source, width, height)
	if err != nil {
		log.Printf("⚠️ D1 Save Failed: %v", err)
	} else {
		log.Printf("✅ Saved %s [%dx%d]", postID, width, height)
	}

	// ⏳ 强制限流：保护 Bot 不被 Telegram 限制
	time.Sleep(3 * time.Second)
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

	// 转发到频道
	msg, err := b.SendPhoto(ctx, &bot.SendPhotoParams{
		ChatID:  h.Cfg.ChannelID,
		Photo:   &models.InputFileString{Data: photo.FileID},
		Caption: caption,
	})

	if err != nil {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   "Forward failed: " + err.Error(),
		})
		return
	}

	finalFileID := msg.Photo[len(msg.Photo)-1].FileID

	// 存入数据库
	h.DB.SaveImage(postID, finalFileID, caption, "manual forwarded", "manual", 0, 0)

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:          update.Message.Chat.ID,
		Text:            "Saved to D1!",
		ReplyParameters: &models.ReplyParameters{MessageID: update.Message.ID},
	})
}
