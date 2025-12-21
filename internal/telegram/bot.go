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

	b, err := bot.New(cfg.BotToken)
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

		// ✅ 把 defaultHandler 的逻辑放在这里
		if h.Forwarding {
			// 1. 如果是 Photo，优先当 Preview
			if len(update.Message.Photo) > 0 && h.ForwardPreview == nil {
				h.ForwardPreview = update.Message
				log.Printf("🖼 收到预览(Photo): %d", update.Message.ID)
				// 添加提示
				b.SendMessage(ctx, &bot.SendMessageParams{
					ChatID: update.Message.Chat.ID,
					Text:   "✅ 已获取预览图，请发送原图文件。",
					ReplyParameters: &models.ReplyParameters{MessageID: update.Message.ID},
				})
				return
			}

			// 2. 如果是 Document
			if update.Message.Document != nil {
				// 如果 Preview 还是空，这个 Document 就是 Preview！
				if h.ForwardPreview == nil {
					h.ForwardPreview = update.Message
					log.Printf("📄 收到预览(Document): %d", update.Message.ID)
					// 添加提示
					b.SendMessage(ctx, &bot.SendMessageParams{
						ChatID: update.Message.Chat.ID,
						Text:   "✅ 已获取预览图，请发送原图文件。",
						ReplyParameters: &models.ReplyParameters{MessageID: update.Message.ID},
					})
				}
				
				// 如果 Original 是空（且不是同一个消息），它也是 Original
				if h.ForwardOriginal == nil && h.ForwardPreview != update.Message {
					h.ForwardOriginal = update.Message
					log.Printf("📄 收到原图(Document): %d", update.Message.ID)
					// 添加提示
					b.SendMessage(ctx, &bot.SendMessageParams{
						ChatID: update.Message.Chat.ID,
						Text:   "✅ 已获取原图。",
						ReplyParameters: &models.ReplyParameters{MessageID: update.Message.ID},
					})
				}
			}
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

// ✅ /forward_end：自动处理单文件或双图模式
func (h *BotHandler) handleForwardEnd(ctx context.Context, b *bot.Bot, update *models.Update) {
	// 如果你没有 mutex，可以把这两行删掉
	// h.mu.Lock()
	// defer h.mu.Unlock()

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

	// 1. 检查有没有预览图
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
	
// 确定 caption
var caption string
if h.ForwardTitle != "" {
    caption = h.ForwardTitle
} else if h.ForwardOriginal != nil && h.ForwardOriginal.Caption != "" {
    caption = h.ForwardOriginal.Caption
} else if h.ForwardPreview.Caption != "" {
    caption = h.ForwardPreview.Caption
} else {
    caption = "MtcACG:TG"
}


	var previewFileID, originFileID string
	var width, height int

	// 情况 A: 预览已经是 Photo (用户发了 Photo，或者发了两条)
	if len(h.ForwardPreview.Photo) > 0 {
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

		// 检查是否有额外的原图文件
		if h.ForwardOriginal != nil && h.ForwardOriginal.Document != nil {
			originFileID = h.ForwardOriginal.Document.FileID
		}

	} else if h.ForwardPreview.Document != nil {
		// 情况 B: 用户只发了一个文件 (Document)
		log.Printf("📥 单文件模式触发: %s", h.ForwardPreview.Document.FileName)
		
		// 1. 先把 Document 转发到频道作为预览 (如果它是图片，TG 会展示缩略图)
		srcDoc := h.ForwardPreview.Document
		fwdMsg, err := b.SendDocument(ctx, &bot.SendDocumentParams{
			ChatID:   h.Cfg.ChannelID,
			Document: &models.InputFileString{Data: srcDoc.FileID},
			Caption:  caption,
		})
		
		if err == nil && fwdMsg.Document != nil {
			previewFileID = fwdMsg.Document.FileID
			// 如果有缩略图尺寸
			if fwdMsg.Document.Thumbnail != nil {
				width = fwdMsg.Document.Thumbnail.Width
				height = fwdMsg.Document.Thumbnail.Height
			} else {
				// 尝试用原图尺寸 (如果能获取到)
				// 注意：Document 里面不一定有 Width/Height 字段，视情况而定
			}
			originFileID = fwdMsg.Document.FileID // 这种情况下，原图就是预览图
		} else {
			log.Printf("❌ Document 转发失败: %v", err)
			b.SendMessage(ctx, &bot.SendMessageParams{ChatID: msg.Chat.ID, Text: "❌ 文件转发失败。"})
			h.Forwarding = false      // ✅ 改成这个
			return
		}
	}

	// 🔥 关键步骤：如果存在独立的 originFileID (情况A)，把它也转发到频道！
	// 这样爬虫 Bot 才能下载它！
	if originFileID != "" && originFileID != previewFileID {
		docMsg, err := b.SendDocument(ctx, &bot.SendDocumentParams{
			ChatID:   h.Cfg.ChannelID,
			Document: &models.InputFileString{Data: originFileID},
			// 不带 Caption 防止刷屏
		})
		if err == nil && docMsg.Document != nil {
			// 更新 originFileID 为频道里的新 ID
			originFileID = docMsg.Document.FileID
			log.Printf("✅ 原图已补发到频道，新 ID: %s", originFileID)
		} else {
			log.Printf("⚠️ 原图补发失败: %v", err)
		}
	}

	// 3. 存入 D1
	// 注意：这里的 SaveImage 参数要和你的 d1.go 匹配
	// 假设你的 d1.go 还是: SaveImage(postID, fileID, originID, caption, tags, width, height)
	// 如果你之前删了 source 参数，这里记得也要去掉！
	err := h.DB.SaveImage(postID, previewFileID, originFileID, caption, "TG-forward", "TG-C", width, height)
	
	if err != nil {
		log.Printf("❌ D1 Save Failed: %v", err)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: msg.Chat.ID,
			Text:   "❌ 保存到数据库失败 (D1 Error)。",
		})
	} else {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:          msg.Chat.ID,
			Text:            fmt.Sprintf("✅ 发布成功！\nPost ID: %s", postID),
			ReplyParameters: &models.ReplyParameters{MessageID: msg.ID},
		})
	}

	// 4. 重置会话
	h.Forwarding = false
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
	quality := 100 // 初始质量
	for {
		buf := new(bytes.Buffer)
		err = jpeg.Encode(buf, img, &jpeg.Options{Quality: quality})
		if err != nil {
			return nil, fmt.Errorf("encode error: %v", err)
		}

		compressedData := buf.Bytes()
		size := int64(len(compressedData))

		// 如果达标了，或者是质量太低了就不压了
		if size <= targetSize || quality <= 50 {
			log.Printf("✅ Compressed to %.2f MB (Quality: %d)", float64(size)/1024/1024, quality)
			return compressedData, nil
		}

		// 否则降低质量继续
		quality -= 1
	}
}
