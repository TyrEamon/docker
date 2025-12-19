package telegram

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg" 
	_ "image/png" 
	"log"
	"my-bot-go/internal/config"
	"my-bot-go/internal/database"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/nfnt/resize"
)

type BotHandler struct {
	API *bot.Bot
	Cfg *config.Config
	DB  *database.D1Client
	// ✅ 新增：转发会话状态
    Forwarding      bool             
    ForwardTitle    string          
    ForwardPreview  *models.Message  
    ForwardOriginal *models.Message 
}

func NewBot(cfg *config.Config, db *database.D1Client) (*BotHandler, error) {
    h := &BotHandler{Cfg: cfg, DB: db}

    opts := []bot.Option{
        bot.WithDefaultHandler(func(ctx context.Context, b *bot.Bot, update *models.Update) {
            if update.Message == nil {
                return
            }
            // 只有在 forward 模式下才收集预览/原图
            if h.Forwarding {
                if len(update.Message.Photo) > 0 && h.ForwardPreview == nil {
                    h.ForwardPreview = update.Message
                    log.Printf("🖼 收到预览图消息: %d", update.Message.ID)
                }
                if update.Message.Document != nil && h.ForwardOriginal == nil {
                    h.ForwardOriginal = update.Message
                    log.Printf("📄 收到原图文件消息: %d", update.Message.ID)
                }
            }
        }),
    }

    b, err := bot.New(cfg.BotToken, opts...)
    if err != nil {
        return nil, err
    }

    h.API = b

    // ✅ /save
    b.RegisterHandler(bot.HandlerTypeMessageText, "/save", bot.MatchTypeExact, h.handleSave)

    // ✅ 新增：/forward_start 和 /forward_end
    b.RegisterHandler(bot.HandlerTypeMessageText, "/forward_start", bot.MatchTypePrefix, h.handleForwardStart)
    b.RegisterHandler(bot.HandlerTypeMessageText, "/forward_end",   bot.MatchTypeExact,  h.handleForwardEnd)

    // ✅ 保留原来的手动转存逻辑（老的转发方式）
    //    但是在 forward 模式下不处理，避免和 /forward_start 流程冲突
    b.RegisterHandler(bot.HandlerTypeMessageText, "", bot.MatchTypePrefix, func(ctx context.Context, b *bot.Bot, update *models.Update) {
        if update.Message == nil {
            return
        }
        // 如果当前在 forward 模式，交给 default handler 收集，不用老逻辑
        if h.Forwarding {
            return
        }
        // 非 forward 模式，走原来的 handleManual
        if len(update.Message.Photo) > 0 {
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
    shouldCompress := int64(len(imgData)) > MaxPhotoSize || (width > 9500 || height > 9500)
	finalData := imgData

    if shouldCompress {
        log.Printf("⚠️ Image %s needs processing (Size: %.2f MB, Dim: %dx%d)...", postID, float64(len(imgData))/1024/1024, width, height)
	   compressed, err := compressImage(imgData, MaxPhotoSize)
		if err != nil {
			log.Printf("❌ Compression failed: %v. Trying original...", err)
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

// 4. ✨ 新增：发送原文件 (SendDocument) - 点击下载时给这张
	docParams := &bot.SendDocumentParams{
		ChatID: h.Cfg.ChannelID,
		Document: &models.InputFileUpload{
			Filename: source + "_original.jpg", // 文件名
			Data:     bytes.NewReader(imgData), // ⚠️ 必须用原始数据
		},
		ReplyParameters: &models.ReplyParameters{
			MessageID: msg.ID, // 回复上一条消息，保持整洁
		},
		Caption: "⬇️ Original File",
	}

	var originFileID string
	msgDoc, errDoc := h.API.SendDocument(ctx, docParams)
	if errDoc != nil {
		log.Printf("⚠️ SendDocument Failed (Will only save preview): %v", errDoc)
		originFileID = "" // 失败了就留空，不影响预览
	} else {
		originFileID = msgDoc.Document.FileID
	}

	// 5. 存入 D1 数据库 (传入 previewID 和 originID)
	err = h.DB.SaveImage(postID, fileID, originFileID, caption, tags, source, width, height)
	if err != nil {
		log.Printf("❌ D1 Save Failed: %v", err)
	} else {
		log.Printf("✅ Saved: %s (Preview + Origin)", postID)
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

    // 中间增加了一个 "" 空字符串，作为 originID 的占位符
    h.DB.SaveImage(postID, finalFileID, "", caption, "TG-forward", "TG-C", width, height)


    b.SendMessage(ctx, &bot.SendMessageParams{
        ChatID: update.Message.Chat.ID,
        Text:   "✅ handleManual Saved to D1!",
        ReplyParameters: &models.ReplyParameters{
            MessageID: update.Message.ID,
        },
    })
 }

// ✅ /forward_start [可选标题]
func (h *BotHandler) handleForwardStart(ctx context.Context, b *bot.Bot, update *models.Update) {
    msg := update.Message
    if msg == nil {
        return
    }

    userID := msg.From.ID
    // 🔒 鉴权：只允许这几个 ID 触发
    if userID != 8040798522 && userID != 6874581126 {
        log.Printf("⛔ Unauthorized /forward_start from UserID: %d", userID)
        return
    }

    // 解析命令后的文本作为“本次会话标题”
    text := msg.Text
    title := ""
    if len(text) > len("/forward_start") {
        title = strings.TrimSpace(text[len("/forward_start"):])
    }

    h.Forwarding = true
    h.ForwardTitle = title
    h.ForwardPreview = nil
    h.ForwardOriginal = nil

    b.SendMessage(ctx, &bot.SendMessageParams{
        ChatID: msg.Chat.ID,
        Text:   "✅ 已进入转发模式，请先转发预览图片，再转发原图文件。完成后发送 /forward_end",
        ReplyParameters: &models.ReplyParameters{
            MessageID: msg.ID,
        },
    })
}

// ✅ /forward_end：根据规则生成 caption + 存库
func (h *BotHandler) handleForwardEnd(ctx context.Context, b *bot.Bot, update *models.Update) {
    msg := update.Message
    if msg == nil {
        return
    }

    if !h.Forwarding {
        b.SendMessage(ctx, &bot.SendMessageParams{
            ChatID: msg.Chat.ID,
            Text:   "ℹ️ 当前没有进行中的转发会话，请先发送 /forward_start",
        })
        return
    }

    // 必须有预览图
    if h.ForwardPreview == nil || len(h.ForwardPreview.Photo) == 0 {
        b.SendMessage(ctx, &bot.SendMessageParams{
            ChatID: msg.Chat.ID,
            Text:   "❌ 还没有收到预览图片，请先转发一条带图片的消息。",
        })
        h.Forwarding = false
        return
    }

    // 原图文件可选
    hasOrigin := h.ForwardOriginal != nil && h.ForwardOriginal.Document != nil

    // 生成 postID
    postID := fmt.Sprintf("manual_%d", h.ForwardPreview.ID)

    // 计算 caption：
    // 有自定义标题 -> 用自定义；没有 -> 用预览图 caption；都没有 -> 默认
    caption := h.ForwardTitle
    if caption == "" {
        caption = h.ForwardPreview.Caption
    }
    if caption == "" {
        caption = "MtcACG:TG"
    }

    // 先把预览图转存到图床频道
    srcPhoto := h.ForwardPreview.Photo[len(h.ForwardPreview.Photo)-1]
    fwdMsg, err := b.SendPhoto(ctx, &bot.SendPhotoParams{
        ChatID:  h.Cfg.ChannelID,
        Photo:   &models.InputFileString{Data: srcPhoto.FileID},
        Caption: caption,
    })
    if err != nil || len(fwdMsg.Photo) == 0 {
        log.Printf("❌ Forward preview failed: %v", err)
        b.SendMessage(ctx, &bot.SendMessageParams{
            ChatID: msg.Chat.ID,
            Text:   "❌ 预览图转存失败。",
        })
        h.Forwarding = false
        return
    }
    previewFileID := fwdMsg.Photo[len(fwdMsg.Photo)-1].FileID
    width := srcPhoto.Width
    height := srcPhoto.Height

    // 决定 originID
    originFileID := ""
    if hasOrigin {
        originFileID = h.ForwardOriginal.Document.FileID
    }

    // 存入 D1
    err = h.DB.SaveImage(postID, previewFileID, originFileID, caption, "TG-forward", "TG-C", width, height)
    if err != nil {
        log.Printf("❌ D1 Save Failed (forward): %v", err)
        b.SendMessage(ctx, &bot.SendMessageParams{
            ChatID: msg.Chat.ID,
            Text:   "❌ 保存到数据库失败。",
        })
    } else {
        b.SendMessage(ctx, &bot.SendMessageParams{
            ChatID: msg.Chat.ID,
            Text:   "✅ 已发布到图床（预览 + 原图）。",
            ReplyParameters: &models.ReplyParameters{
                MessageID: msg.ID,
            },
        })
    }

    // 重置会话状态
    h.Forwarding = false
    h.ForwardTitle = ""
    h.ForwardPreview = nil
    h.ForwardOriginal = nil
}


// compressImage 尝试把图片压缩到指定大小以下
func compressImage(data []byte, targetSize int64) ([]byte, error) {
	// 解码图片
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode error: %v", err)
	}

    // 2. ✅ 新增：检查分辨率 (Telegram 限制宽+高 ≤ 10000，这里限制单边 4000 最稳)
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	
	if width > 9500 || height > 9500 {
		log.Printf("📏 Resizing image from %dx%d (Too big for TG)", width, height)
		// 保持比例缩放，最大边长设为 4000
		if width > height {
			img = resize.Resize(9500, 0, img, resize.Lanczos3)
		} else {
			img = resize.Resize(0, 9500, img, resize.Lanczos3)
		}
	}
	
    log.Printf("📉 Compressing %s image...", format)

	// 循环尝试压缩，降低质量
	quality := 99 // 初始质量
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
