package telegram

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg" // ✅ 必须加，用于压缩
	_ "image/png" // ✅ 必须加，支持 PNG 解码
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
	
	// ✅ 注册 /save 命令
	b.RegisterHandler(bot.HandlerTypeMessageText, "/save", bot.MatchTypeExact, h.handleSave)

	// 其他 Handlers
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
	// 1. 先检查内存历史，如果有了就直接跳过
	if h.DB.History[postID] {
		log.Printf("⏭️ Skip %s: already in history", postID)
		return
	}

	// 2. 检查图片大小，如果超过 9MB 则压缩 (Telegram 限制 10MB)
	const MaxPhotoSize = 9 * 1024 * 1024 
	finalData := imgData

	if int64(len(imgData)) > MaxPhotoSize {
		log.Printf("⚠️ Image %s is too large (%.2f MB), compressing...", postID, float64(len(imgData))/1024/1024)
		compressed, err := compressImage(imgData, MaxPhotoSize)
		if err != nil {
			log.Printf("❌ Compression failed: %v. Trying original...", err)
			// 压缩失败，还是试着用原图发一下（虽然大概率失败）
		} else {
			finalData = compressed
		}
	}

	// 3. 发送到 Telegram
	params := &bot.SendPhotoParams{
		ChatID:  h.Cfg.ChannelID,
		Photo:   &models.InputFileUpload{Filename: source + ".jpg", Data: bytes.NewReader(finalData)},
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

	// 4. 存入 D1 数据库
	err = h.DB.SaveImage(postID, fileID, caption, tags, source, width, height)
	if err != nil {
		log.Printf("❌ D1 Save Failed: %v", err)
	} else {
		log.Printf("✅ Saved: %s (%dx%d)", postID, width, height)
	}
}

func (h *BotHandler) PushHistoryToCloud() {
	if h.DB != nil {
		h.DB.PushHistory()
	}
}

// ✅ 手动保存历史记录的 handler
func (h *BotHandler) handleSave(ctx context.Context, b *bot.Bot, update *models.Update) {
    userID := update.Message.From.ID

    // 🔒 鉴权：只允许这几个 ID 触发
    if userID != 8040798522 && userID != 6874581126 {
        log.Printf("⛔ Unauthorized /save attempt from UserID: %d", userID)
        return
    }

    log.Printf("💾 Manual save triggered by UserID: %d", userID)
    
    if h.DB != nil {
        h.DB.PushHistory()
        b.SendMessage(ctx, &bot.SendMessageParams{
            ChatID: update.Message.Chat.ID,
            Text:   "✅ History successfully saved to Cloudflare D1!",
        })
    } else {
        b.SendMessage(ctx, &bot.SendMessageParams{
            ChatID: update.Message.Chat.ID,
            Text:   "❌ Database client is not initialized.",
        })
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
        caption = "MtcACG:TG"
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

    h.DB.SaveImage(postID, finalFileID, caption, "TG-forward", "TG-C", width, height)

    b.SendMessage(ctx, &bot.SendMessageParams{
        ChatID: update.Message.Chat.ID,
        Text:   "✅ Saved to D1!",
        ReplyParameters: &models.ReplyParameters{
            MessageID: update.Message.ID,
        },
    })
}

// compressImage 尝试把图片压缩到指定大小以下
func compressImage(data []byte, targetSize int64) ([]byte, error) {
	// 解码图片
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode error: %v", err)
	}
    log.Printf("📉 Compressing %s image...", format)

	// 循环尝试压缩，降低质量
	quality := 98 // 初始质量
	for {
		buf := new(bytes.Buffer)
		err = jpeg.Encode(buf, img, &jpeg.Options{Quality: quality})
		if err != nil {
			return nil, fmt.Errorf("encode error: %v", err)
		}

		compressedData := buf.Bytes()
		size := int64(len(compressedData))

		// 如果达标了，或者是质量太低了就不压了
		if size <= targetSize || quality <= 40 {
			log.Printf("✅ Compressed to %.2f MB (Quality: %d)", float64(size)/1024/1024, quality)
			return compressedData, nil
		}

		// 否则降低质量继续
		quality -= 5
	}
}
