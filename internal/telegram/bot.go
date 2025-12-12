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
		}),
	}

	b, err := bot.New(cfg.BotToken, opts...)
	if err != nil {
		return nil, err
	}
	
	h := &BotHandler{API: b, Cfg: cfg, DB: db}
	
	b.RegisterHandler(bot.HandlerTypeMessageText, "", bot.MatchTypePrefix, h.handleManual)
	b.RegisterHandler(bot.HandlerTypeMessageText, "", bot.MatchTypePrefix, func(ctx context.Context, b *bot.Bot, update *models.Update) {
		if update.Message != nil && len(update.Message.Photo) > 0 {
			h.handleManual(ctx, b, update)
		}
	})

	return h, nil
}

func (h *BotHandler) Start(ctx context.Context) {
	h.API.Start(ctx)
}

// ProcessAndSend 增加了 width, height 参数
func (h *BotHandler) ProcessAndSend(ctx context.Context, imgData []byte, postID, tags, caption, source string, width, height int) {
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

	if len(msg.Photo) == 0 {
		return 
	}
	fileID := msg.Photo[len(msg.Photo)-1].FileID

	err = h.DB.SaveImage(postID, fileID, caption, tags, source, width, height)
	if err != nil {
		log.Printf("❌ D1 Save Failed: %v", err)
	} else {
		log.Printf("✅ Saved: %s (%dx%d)", postID, width, height)
	}
}

func (h *BotHandler) handleManual(ctx context.Context, b *bot.Bot, update *models.Update) {
    if update.Message == nil || len(update.Message.Photo) == 0 {
        return
    }

    // 用户发来的最大尺寸那张图，里面自带宽高
    photo := update.Message.Photo[len(update.Message.Photo)-1]

    postID := fmt.Sprintf("manual_%d", update.Message.ID)
    caption := update.Message.Caption
    if caption == "" {
        caption = "Forwarded Image"
    }

    // 先转存到图床频道
    msg, err := b.SendPhoto(ctx, &bot.SendPhotoParams{
        ChatID: h.Cfg.ChannelID,
        Photo:  &models.InputFileString{Data: photo.FileID},
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

    // 使用原消息里的宽高
    width := photo.Width
    height := photo.Height

    h.DB.SaveImage(postID, finalFileID, caption, "manual forwarded", "manual", width, height)

    b.SendMessage(ctx, &bot.SendMessageParams{
        ChatID: update.Message.Chat.ID,
        Text:   "✅ Saved to D1!",
        ReplyParameters: &models.ReplyParameters{
            MessageID: update.Message.ID,
        },
    })
}
