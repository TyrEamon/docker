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

			// 如果不在转发模式，直接忽略
			if !h.Forwarding {
				return
			}

			msg := update.Message

			// 调试日志：看看收到了什么
			log.Printf("📥 DefaultHandler recv: msgID=%d, hasPhoto=%v, hasDoc=%v",
				msg.ID, len(msg.Photo) > 0, msg.Document != nil)

			// 1. 尝试捕捉预览图 (ForwardPreview)
			if h.ForwardPreview == nil {
				if len(msg.Photo) > 0 {
					h.ForwardPreview = msg
					log.Printf("🖼 [Forward] 预览图已捕获 (Photo): %d", msg.ID)
					return
				}
				// 关键修改：如果没有 Photo，第一张 Document 也算预览
				if msg.Document != nil {
					h.ForwardPreview = msg
					log.Printf("🖼 [Forward] 预览图已捕获 (Document): %d", msg.ID)
					return
				}
			}

			// 2. 尝试捕捉原图 (ForwardOriginal)
			// 如果预览图已经有了，再来一张 Document，就当它是原图
			if h.ForwardOriginal == nil && msg.Document != nil {
				// 防止同一条消息既当预览又当原图 (虽然前面的 return 已经防住了)
				if h.ForwardPreview != nil && h.ForwardPreview.ID == msg.ID {
					return
				}
				h.ForwardOriginal = msg
				log.Printf("📄 [Forward] 原图文件已捕获: %d", msg.ID)
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
	b.RegisterHandler(bot.HandlerTypeMessageText, "/forward_end", bot.MatchTypeExact, h.handleForwardEnd)

	// ✅ 手动转存逻辑 (非 forward 模式下生效)
	b.RegisterHandler(bot.HandlerTypeMessageText, "", bot.MatchTypePrefix, func(ctx context.Context, b *bot.Bot, update *models.Update) {
		if update.Message == nil {
			return
		}
		// 如果当前在 forward 模式，交给 default handler 收集，不走老逻辑
		if h.Forwarding {
			return
		}
		// 非 forward 模式，且有图片，走原来的 handleManual
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
	// 1. 先检查内存历史
	if h.DB.History[postID] {
		log.Printf("⏭️ Skip %s: already in history", postID)
		return
	}

	// 2. 检查图片大小，如果超过 9MB 则压缩
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

	// 3. 发送到 Telegram (预览)
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

	// 4. 发送原文件
	docParams := &bot.SendDocumentParams{
		ChatID: h.Cfg.ChannelID,
		Document: &models.InputFileUpload{
			Filename: source + "_original.jpg",
			Data:     bytes.NewReader(imgData),
		},
		ReplyParameters: &models.ReplyParameters{
			MessageID: msg.ID,
		},
		Caption: "⬇️ Original File",
	}

	var originFileID string
	msgDoc, errDoc := h.API.SendDocument(ctx, docParams)
	if errDoc != nil {
		log.Printf("⚠️ SendDocument Failed (Will only save preview): %v", errDoc)
		originFileID = ""
	} else {
		originFileID = msgDoc.Document.FileID
	}

	// 5. 存入 D1 数据库
	err = h.DB.SaveImage(postID, fileID, originFileID, caption, tags, source, width, height)
	if err != nil {
		log.Printf("❌ D1 Save Failed: %v", err)
	} else {
		log.Printf("✅ Saved: %s (Preview + Origin)", postID)
	}
}

// handleSave 手动保存历史记录
func (h *BotHandler) handleSave(ctx context.Context, b *bot.Bot, update *models.Update) {
	userID := update.Message.From.ID
	if userID != 8040798522 && userID != 6874581126 {
		return
	}
	if h.DB != nil {
		h.DB.PushHistory()
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   "✅ History successfully saved to Cloudflare D1!",
		})
	}
}

// handleManual 老的手动转存逻辑
func (h *BotHandler) handleManual(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil || len(update.Message.Photo) == 0 {
		return
	}

	photo := update.Message.Photo[len(update.Message.Photo)-1]
	postID := fmt.Sprintf("manual_%d", update.Message.ID)
	caption := update.Message.Caption
	if caption == "" {
		caption = "MtcACG:TG"
	}

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
	h.DB.SaveImage(postID, finalFileID, "", caption, "TG-forward", "TG-C", photo.Width, photo.Height)

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:          update.Message.Chat.ID,
		Text:            "✅ Saved to D1! (Manual Mode)",
		ReplyParameters: &models.ReplyParameters{MessageID: update.Message.ID},
	})
}

// handleForwardStart
func (h *BotHandler) handleForwardStart(ctx context.Context, b *bot.Bot, update *models.Update) {
	msg := update.Message
	if msg == nil {
		return
	}
	userID := msg.From.ID
	if userID != 8040798522 && userID != 6874581126 {
		return
	}

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
		ChatID:          msg.Chat.ID,
		Text:            "✅ 已进入转发模式。\n请发送预览图（可以是图片或文件），再发送原图文件。\n完成后发送 /forward_end",
		ReplyParameters: &models.ReplyParameters{MessageID: msg.ID},
	})
}

// handleForwardEnd
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

	// 1. 检查有没有预览图 (ForwardPreview 不为空即可，不用管它是 Photo 还是 Document)
	if h.ForwardPreview == nil {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: msg.Chat.ID,
			Text:   "❌ 还没有收到预览消息，请先转发一条图片或文件。",
		})
		h.Forwarding = false
		return
	}

	// 2. 准备数据
	postID := fmt.Sprintf("manual_%d", h.ForwardPreview.ID)
	caption := h.ForwardTitle
	if caption == "" {
		caption = h.ForwardPreview.Caption
	}
	if caption == "" {
		caption = "MtcACG:TG"
	}

	// 3. 转存预览图到频道
	var previewFileID string
	var width, height int

	if len(h.ForwardPreview.Photo) > 0 {
		// 情况 A: 预览是 Photo
		srcPhoto := h.ForwardPreview.Photo[len(h.ForwardPreview.Photo)-1]
		fwdMsg, err := b.SendPhoto(ctx, &bot.SendPhotoParams{
			ChatID:  h.Cfg.ChannelID,
			Photo:   &models.InputFileString{Data: srcPhoto.FileID},
			Caption: caption,
		})
		if err != nil || len(fwdMsg.Photo) == 0 {
			log.Printf("❌ Forward preview(Photo) failed: %v", err)
			b.SendMessage(ctx, &bot.SendMessageParams{ChatID: msg.Chat.ID, Text: "❌ 预览图转存失败。"})
			h.Forwarding = false
			return
		}
		previewFileID = fwdMsg.Photo[len(fwdMsg.Photo)-1].FileID
		width = srcPhoto.Width
		height = srcPhoto.Height

	} else if h.ForwardPreview.Document != nil {
		// 情况 B: 预览是 Document (文件)
		srcDoc := h.ForwardPreview.Document
		fwdMsg, err := b.SendDocument(ctx, &bot.SendDocumentParams{
			ChatID:   h.Cfg.ChannelID,
			Document: &models.InputFileString{Data: srcDoc.FileID},
			Caption:  caption,
		})
		if err != nil || fwdMsg.Document == nil {
			log.Printf("❌ Forward preview(Doc) failed: %v", err)
			b.SendMessage(ctx, &bot.SendMessageParams{ChatID: msg.Chat.ID, Text: "❌ 预览图转存失败。"})
			h.Forwarding = false
			return
		}
		previewFileID = fwdMsg.Document.FileID
		// 文件通常没有宽高信息，或者在 Thumbnail 里，这里简化处理设为 0
		width = 0
		height = 0
	}

	// 4. 处理原图 (如果有)
	originFileID := ""
	if h.ForwardOriginal != nil && h.ForwardOriginal.Document != nil {
		originFileID = h.ForwardOriginal.Document.FileID
	}

	// 5. 存入数据库
	err := h.DB.SaveImage(postID, previewFileID, originFileID, caption, "TG-forward", "TG-C", width, height)
	if err != nil {
		log.Printf("❌ D1 Save Failed (forward): %v", err)
		b.SendMessage(ctx, &bot.SendMessageParams{ChatID: msg.Chat.ID, Text: "❌ 保存到数据库失败。"})
	} else {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:          msg.Chat.ID,
			Text:            "✅ 已发布到图床（预览 + 原图）。",
			ReplyParameters: &models.ReplyParameters{MessageID: msg.ID},
		})
	}

	// 6. 重置状态
	h.Forwarding = false
	h.ForwardTitle = ""
	h.ForwardPreview = nil
	h.ForwardOriginal = nil
}

// compressImage (保持不变)
func compressImage(data []byte, targetSize int64) ([]byte, error) {
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode error: %v", err)
	}

	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()

	if w > 9500 || h > 9500 {
		log.Printf("📏 Resizing image from %dx%d", w, h)
		if w > h {
			img = resize.Resize(9500, 0, img, resize.Lanczos3)
		} else {
			img = resize.Resize(0, 9500, img, resize.Lanczos3)
		}
	}

	log.Printf("📉 Compressing %s image...", format)
	quality := 99
	for {
		buf := new(bytes.Buffer)
		err = jpeg.Encode(buf, img, &jpeg.Options{Quality: quality})
		if err != nil {
			return nil, fmt.Errorf("encode error: %v", err)
		}
		compressedData := buf.Bytes()
		size := int64(len(compressedData))
		if size <= targetSize || quality <= 40 {
			log.Printf("✅ Compressed to %.2f MB (Quality: %d)", float64(size)/1024/1024, quality)
			return compressedData, nil
		}
		quality -= 5
	}
}
