package telegram

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"log"
	"my-bot-go/internal/config"
	"my-bot-go/internal/database"
	"net/http"
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

			// 调试日志
			log.Printf("📥 DefaultHandler recv: msgID=%d, hasPhoto=%v, hasDoc=%v",
				msg.ID, len(msg.Photo) > 0, msg.Document != nil)

			// 1. 尝试捕捉预览图 (ForwardPreview)
			if h.ForwardPreview == nil {
				if len(msg.Photo) > 0 {
					h.ForwardPreview = msg
					log.Printf("🖼 [Forward] 预览图已捕获 (Photo): %d", msg.ID)
					return
				}
				// 关键修改：如果没有 Photo，第一张 Document 也算预览（也是原图）
				if msg.Document != nil {
					h.ForwardPreview = msg
					log.Printf("📄 [Forward] 文件已捕获 (将自动用于预览+原图): %d", msg.ID)
					return
				}
			}

			// 2. 尝试捕捉原图 (ForwardOriginal)
			// 只有当你还是发了两条消息时（先图后文），这个才生效
			// 如果你只发了一条 Document，这个就会保持为 nil，我们在 End 里处理
			if h.ForwardOriginal == nil && msg.Document != nil {
				// 防止同一条消息既当预览又当原图
				if h.ForwardPreview != nil && h.ForwardPreview.ID == msg.ID {
					return
				}
				h.ForwardOriginal = msg
				log.Printf("📄 [Forward] 额外原图文件已捕获: %d", msg.ID)
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

	// ✅ /forward_start 和 /forward_end
	b.RegisterHandler(bot.HandlerTypeMessageText, "/forward_start", bot.MatchTypePrefix, h.handleForwardStart)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/forward_end", bot.MatchTypeExact, h.handleForwardEnd)

	// ✅ 手动转存逻辑 (非 forward 模式下生效)
	b.RegisterHandler(bot.HandlerTypeMessageText, "", bot.MatchTypePrefix, func(ctx context.Context, b *bot.Bot, update *models.Update) {
		if update.Message == nil {
			return
		}
		if h.Forwarding {
			return
		}
		if len(update.Message.Photo) > 0 {
			h.handleManual(ctx, b, update)
		}
	})

	return h, nil
}

func (h *BotHandler) Start(ctx context.Context) {
	h.API.Start(ctx)
}

// ⬇️ 辅助：下载文件
func (h *BotHandler) downloadFile(ctx context.Context, fileID string) ([]byte, error) {
	file, err := h.API.GetFile(ctx, &bot.GetFileParams{FileID: fileID})
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", h.Cfg.BotToken, file.FilePath)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// ProcessAndSend (保持不变，省略以节省篇幅，请保留你原来的代码)
// ... 这里粘贴你原本的 ProcessAndSend ...
// 为了方便你复制，我还是把 ProcessAndSend 完整放这里，防止你不小心删了
func (h *BotHandler) ProcessAndSend(ctx context.Context, imgData []byte, postID, tags, caption, source string, width, height int) {
	if h.DB.History[postID] {
		log.Printf("⏭️ Skip %s: already in history", postID)
		return
	}
	const MaxPhotoSize = 9 * 1024 * 1024
	shouldCompress := int64(len(imgData)) > MaxPhotoSize || (width > 9500 || height > 9500)
	finalData := imgData

	if shouldCompress {
		log.Printf("⚠️ Image %s needs processing...", postID)
		compressed, err := compressImage(imgData, MaxPhotoSize)
		if err == nil {
			finalData = compressed
		}
	}

	params := &bot.SendPhotoParams{
		ChatID:  h.Cfg.ChannelID,
		Photo:   &models.InputFileUpload{Filename: source + ".jpg", Data: bytes.NewReader(finalData)},
		Caption: caption,
	}
	msg, err := h.API.SendPhoto(ctx, params)
	if err != nil {
		log.Printf("❌ Telegram Send Failed: %v", err)
		return
	}
	if len(msg.Photo) == 0 {
		return
	}
	fileID := msg.Photo[len(msg.Photo)-1].FileID

	docParams := &bot.SendDocumentParams{
		ChatID:          h.Cfg.ChannelID,
		Document:        &models.InputFileUpload{Filename: source + "_original.jpg", Data: bytes.NewReader(imgData)},
		ReplyParameters: &models.ReplyParameters{MessageID: msg.ID},
		Caption:         "⬇️ Original File",
	}
	msgDoc, errDoc := h.API.SendDocument(ctx, docParams)
	originFileID := ""
	if errDoc == nil {
		originFileID = msgDoc.Document.FileID
	}
	h.DB.SaveImage(postID, fileID, originFileID, caption, tags, source, width, height)
	log.Printf("✅ Saved: %s", postID)
}

// handleSave (保持不变)
func (h *BotHandler) handleSave(ctx context.Context, b *bot.Bot, update *models.Update) {
	userID := update.Message.From.ID
	if userID != 8040798522 && userID != 6874581126 {
		return
	}
	if h.DB != nil {
		h.DB.PushHistory()
		b.SendMessage(ctx, &bot.SendMessageParams{ChatID: update.Message.Chat.ID, Text: "✅ History saved!"})
	}
}

// handleManual (保持不变)
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
		b.SendMessage(ctx, &bot.SendMessageParams{ChatID: update.Message.Chat.ID, Text: "❌ Forward failed: " + err.Error()})
		return
	}
	finalFileID := msg.Photo[len(msg.Photo)-1].FileID
	h.DB.SaveImage(postID, finalFileID, "", caption, "TG-forward", "TG-C", photo.Width, photo.Height)
	b.SendMessage(ctx, &bot.SendMessageParams{ChatID: update.Message.Chat.ID, Text: "✅ Saved (Manual)!"})
}

// handleForwardStart (保持不变)
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
		Text:            "✅ 转发模式开启。\n支持：\n1. 发送单张原图文件（自动生成预览）\n2. 发送预览图 + 原图文件\n完成后发送 /forward_end",
		ReplyParameters: &models.ReplyParameters{MessageID: msg.ID},
	})
}

// ✅ 核心修改：handleForwardEnd
func (h *BotHandler) handleForwardEnd(ctx context.Context, b *bot.Bot, update *models.Update) {
	msg := update.Message
	if msg == nil {
		return
	}

	if !h.Forwarding {
		b.SendMessage(ctx, &bot.SendMessageParams{ChatID: msg.Chat.ID, Text: "ℹ️ 请先 /forward_start"})
		return
	}

	if h.ForwardPreview == nil {
		b.SendMessage(ctx, &bot.SendMessageParams{ChatID: msg.Chat.ID, Text: "❌ 没收到任何图片或文件。"})
		h.Forwarding = false
		return
	}

	// 准备数据
	postID := fmt.Sprintf("manual_%d", h.ForwardPreview.ID)
	caption := h.ForwardTitle
	if caption == "" {
		caption = h.ForwardPreview.Caption
	}
	if caption == "" {
		caption = "MtcACG:TG"
	}

	var previewFileID, originFileID string
	var width, height int

	// 情况 A: 预览已经是 Photo (说明用户手动发了两条，或者是发的压缩图)
	if len(h.ForwardPreview.Photo) > 0 {
		srcPhoto := h.ForwardPreview.Photo[len(h.ForwardPreview.Photo)-1]
		fwdMsg, err := b.SendPhoto(ctx, &bot.SendPhotoParams{
			ChatID:  h.Cfg.ChannelID,
			Photo:   &models.InputFileString{Data: srcPhoto.FileID},
			Caption: caption,
		})
		if err != nil || len(fwdMsg.Photo) == 0 {
			b.SendMessage(ctx, &bot.SendMessageParams{ChatID: msg.Chat.ID, Text: "❌ 预览转存失败"})
			h.Forwarding = false
			return
		}
		previewFileID = fwdMsg.Photo[len(fwdMsg.Photo)-1].FileID
		width = srcPhoto.Width
		height = srcPhoto.Height
		
		// 只有在这种情况下，我们才去检查有没有 ForwardOriginal
		if h.ForwardOriginal != nil && h.ForwardOriginal.Document != nil {
			originFileID = h.ForwardOriginal.Document.FileID
		}

	} else if h.ForwardPreview.Document != nil {
		// 情况 B: 用户发的是文件 (Document)
		// 策略：自动下载该文件，尝试转成 Photo 发给频道作为预览图
		
		log.Printf("📥 正在处理单文件模式，下载中: %s", h.ForwardPreview.Document.FileName)
		b.SendMessage(ctx, &bot.SendMessageParams{ChatID: msg.Chat.ID, Text: "⏳ 正在处理单文件(下载+转换)..."})
		
		fileData, err := h.downloadFile(ctx, h.ForwardPreview.Document.FileID)
		
		// 默认不管成不成功，原图 ID 肯定就是这个文件的 ID
		originFileID = h.ForwardPreview.Document.FileID

		if err == nil {
			// 下载成功，尝试作为 Photo 发送
			fwdMsg, err := b.SendPhoto(ctx, &bot.SendPhotoParams{
				ChatID:  h.Cfg.ChannelID,
				Photo:   &models.InputFileUpload{Filename: "preview.jpg", Data: bytes.NewReader(fileData)},
				Caption: caption,
			})
			
			if err == nil && len(fwdMsg.Photo) > 0 {
				// 成功生成了预览图！
				log.Printf("✅ 自动生成预览图成功")
				previewFileID = fwdMsg.Photo[len(fwdMsg.Photo)-1].FileID
				width = fwdMsg.Photo[len(fwdMsg.Photo)-1].Width
				height = fwdMsg.Photo[len(fwdMsg.Photo)-1].Height
			} else {
				// 转 Photo 失败 (可能不是图片)，那预览图也只能用文件 ID 了
				log.Printf("⚠️ 自动转换失败 (可能非图片): %v", err)
				previewFileID = originFileID
			}
		} else {
			// 下载失败，没办法，预览图只好也用原图 ID
			log.Printf("❌ 下载失败: %v", err)
			previewFileID = originFileID
		}
	}

	// 存入数据库
	err := h.DB.SaveImage(postID, previewFileID, originFileID, caption, "TG-forward", "TG-C", width, height)
	if err != nil {
		log.Printf("❌ D1 Save Failed: %v", err)
		b.SendMessage(ctx, &bot.SendMessageParams{ChatID: msg.Chat.ID, Text: "❌ 存库失败"})
	} else {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:          msg.Chat.ID,
			Text:            "✅ 发布成功！(已自动关联预览图与原图)",
			ReplyParameters: &models.ReplyParameters{MessageID: msg.ID},
		})
	}

	// 重置
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
		if w > h {
			img = resize.Resize(9500, 0, img, resize.Lanczos3)
		} else {
			img = resize.Resize(0, 9500, img, resize.Lanczos3)
		}
	}

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
			return compressedData, nil
		}
		quality -= 5
	}
}
