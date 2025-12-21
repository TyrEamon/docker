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
	"net/http"
	"regexp"
	"strings"
	"time"

	"my-bot-go/internal/config"
	"my-bot-go/internal/database"
	"my-bot-go/internal/pixiv"
	"my-bot-go/internal/manyacg"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/nfnt/resize"
)

type BotHandler struct {
	API             *bot.Bot
	Cfg             *config.Config
	DB              *database.D1Client
	Forwarding      bool
	ForwardTitle    string
	ForwardTags     string // ✅ 新增字段
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

	// ✅ Pixiv Link
	b.RegisterHandler(bot.HandlerTypeMessageText, "pixiv.net/artworks/", bot.MatchTypeContains, h.handlePixivLink)

	// ✅ 新增：监听 ManyACG 链接
    b.RegisterHandler(bot.HandlerTypeMessageText, "manyacg.top/artwork/", bot.MatchTypeContains, h.handleManyacgLink)

	// ✅ /forward_start & /forward_end
	b.RegisterHandler(bot.HandlerTypeMessageText, "/forward_start", bot.MatchTypePrefix, h.handleForwardStart)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/forward_end", bot.MatchTypeExact, h.handleForwardEnd)

	// ✅ Default handler
	b.RegisterHandler(bot.HandlerTypeMessageText, "", bot.MatchTypePrefix, func(ctx context.Context, b *bot.Bot, update *models.Update) {
		if update.Message == nil {
			return
		}
		if h.Forwarding {
			if len(update.Message.Photo) > 0 && h.ForwardPreview == nil {
				h.ForwardPreview = update.Message
				log.Printf("🖼 收到预览(Photo): %d", update.Message.ID)
				b.SendMessage(ctx, &bot.SendMessageParams{
					ChatID:          update.Message.Chat.ID,
					Text:            "✅ 已获取预览图，请发送原图文件。",
					ReplyParameters: &models.ReplyParameters{MessageID: update.Message.ID},
				})
				return
			}
			if update.Message.Document != nil {
				if h.ForwardPreview == nil {
					h.ForwardPreview = update.Message
					log.Printf("📄 收到预览(Document): %d", update.Message.ID)
					b.SendMessage(ctx, &bot.SendMessageParams{
						ChatID:          update.Message.Chat.ID,
						Text:            "✅ 已获取预览图，请发送原图文件。",
						ReplyParameters: &models.ReplyParameters{MessageID: update.Message.ID},
					})
				}
				if h.ForwardOriginal == nil && h.ForwardPreview != update.Message {
					h.ForwardOriginal = update.Message
					log.Printf("📄 收到原图(Document): %d", update.Message.ID)
					b.SendMessage(ctx, &bot.SendMessageParams{
						ChatID:          update.Message.Chat.ID,
						Text:            "✅ 已获取原图。",
						ReplyParameters: &models.ReplyParameters{MessageID: update.Message.ID},
					})
				}
			}
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

func (h *BotHandler) ProcessAndSend(ctx context.Context, imgData []byte, postID, tags, caption, source string, width, height int) {
	if h.DB.History[postID] {
		log.Printf("⏭️ Skip %s: already in history", postID)
		return
	}
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

	err = h.DB.SaveImage(postID, fileID, originFileID, caption, tags, source, width, height)
	if err != nil {
		log.Printf("❌ D1 Save Failed: %v", err)
	} else {
		log.Printf("✅ Saved: %s (Preview + Origin)", postID)
	}
}

func (h *BotHandler) handleSave(ctx context.Context, b *bot.Bot, update *models.Update) {
	userID := update.Message.From.ID
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
	width := photo.Width
	height := photo.Height
	h.DB.SaveImage(postID, finalFileID, "", caption, "TG-forward", "TG-C", width, height)
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:          update.Message.Chat.ID,
		Text:            "✅ handleManual Saved to D1!",
		ReplyParameters: &models.ReplyParameters{MessageID: update.Message.ID},
	})
}

// ✅ 修改后的 handleForwardStart
func (h *BotHandler) handleForwardStart(ctx context.Context, b *bot.Bot, update *models.Update) {
	msg := update.Message
	if msg == nil {
		return
	}
	userID := msg.From.ID
	if userID != 8040798522 && userID != 6874581126 {
		log.Printf("⛔ Unauthorized /forward_start from UserID: %d", userID)
		return
	}

	// 1. 获取命令后的原始文本
	rawText := ""
	if len(msg.Text) > len("/forward_start") {
		rawText = strings.TrimSpace(msg.Text[len("/forward_start"):])
	}

	// 2. 智能分离 Title 和 Tags (#)
	title := rawText
	tags := ""
	firstHashIndex := strings.Index(rawText, "#")
	if firstHashIndex != -1 {
		title = strings.TrimSpace(rawText[:firstHashIndex])
		tags = strings.TrimSpace(rawText[firstHashIndex:])
	}

	h.Forwarding = true
	h.ForwardTitle = title
	h.ForwardTags = tags // 存起来
	h.ForwardPreview = nil
	h.ForwardOriginal = nil

	// 反馈信息
	info := "✅ 进入转发模式"
	if title != "" {
		info += fmt.Sprintf("\n📝 标题: %s", title)
	}
	if tags != "" {
		info += fmt.Sprintf("\n🏷 标签: %s", tags)
	}
	info += "\n请发送预览图..."

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:          msg.Chat.ID,
		Text:            info,
		ReplyParameters: &models.ReplyParameters{MessageID: msg.ID},
	})
}

// ✅ 修改后的 handleForwardEnd
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
	if h.ForwardPreview == nil {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: msg.Chat.ID,
			Text:   "❌ 还没有收到预览消息，请先转发一条图片或文件。",
		})
		h.Forwarding = false
		return
	}

	postID := fmt.Sprintf("manual_%d", h.ForwardPreview.ID)
	
	// 1. 确定 Base Caption
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

	// 2. 将 Tags 拼接到 Caption 显示（可选，如果不想显示可去掉）
	if h.ForwardTags != "" {
		caption = caption + "\n" + h.ForwardTags
	}

	// 3. 确定存入 DB 的 Tags
	finalDBTags := h.ForwardTags
	if finalDBTags == "" {
		finalDBTags = "TG-forward"
	}

	var previewFileID, originFileID string
	var width, height int

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
		if h.ForwardOriginal != nil && h.ForwardOriginal.Document != nil {
			originFileID = h.ForwardOriginal.Document.FileID
		}
	} else if h.ForwardPreview.Document != nil {
		log.Printf("📥 单文件模式触发: %s", h.ForwardPreview.Document.FileName)
		srcDoc := h.ForwardPreview.Document
		fwdMsg, err := b.SendDocument(ctx, &bot.SendDocumentParams{
			ChatID:   h.Cfg.ChannelID,
			Document: &models.InputFileString{Data: srcDoc.FileID},
			Caption:  caption,
		})
		if err == nil && fwdMsg.Document != nil {
			previewFileID = fwdMsg.Document.FileID
			if fwdMsg.Document.Thumbnail != nil {
				width = fwdMsg.Document.Thumbnail.Width
				height = fwdMsg.Document.Thumbnail.Height
			}
			originFileID = fwdMsg.Document.FileID
		} else {
			log.Printf("❌ Document 转发失败: %v", err)
			b.SendMessage(ctx, &bot.SendMessageParams{ChatID: msg.Chat.ID, Text: "❌ 文件转发失败。"})
			h.Forwarding = false
			return
		}
	}

	if originFileID != "" && originFileID != previewFileID {
		docMsg, err := b.SendDocument(ctx, &bot.SendDocumentParams{
			ChatID:   h.Cfg.ChannelID,
			Document: &models.InputFileString{Data: originFileID},
		})
		if err == nil && docMsg.Document != nil {
			originFileID = docMsg.Document.FileID
			log.Printf("✅ 原图已补发到频道，新 ID: %s", originFileID)
		} else {
			log.Printf("⚠️ 原图补发失败: %v", err)
		}
	}

	// 存入 D1，使用解析出来的 Tags
	err := h.DB.SaveImage(postID, previewFileID, originFileID, caption, finalDBTags, "TG-C", width, height)
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
	h.Forwarding = false
	h.ForwardPreview = nil
	h.ForwardOriginal = nil
	h.ForwardTags = "" // 清空
	h.ForwardTitle = ""
}

func compressImage(data []byte, targetSize int64) ([]byte, error) {
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode error: %v", err)
	}
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width > 9500 || height > 9500 {
		log.Printf("📏 Resizing image from %dx%d (Too big for TG)", width, height)
		if width > height {
			img = resize.Resize(9500, 0, img, resize.Lanczos3)
		} else {
			img = resize.Resize(0, 9500, img, resize.Lanczos3)
		}
	}
	log.Printf("📉 Compressing %s image...", format)
	quality := 100
	for {
		buf := new(bytes.Buffer)
		err = jpeg.Encode(buf, img, &jpeg.Options{Quality: quality})
		if err != nil {
			return nil, fmt.Errorf("encode error: %v", err)
		}
		compressedData := buf.Bytes()
		size := int64(len(compressedData))
		if size <= targetSize || quality <= 50 {
			log.Printf("✅ Compressed to %.2f MB (Quality: %d)", float64(size)/1024/1024, quality)
			return compressedData, nil
		}
		quality -= 1
	}
}

func (h *BotHandler) handlePixivLink(ctx context.Context, b *bot.Bot, update *models.Update) {
	if h.Forwarding {
		return
	}
	text := update.Message.Text
	re := regexp.MustCompile(`artworks/(\d+)`)
	matches := re.FindStringSubmatch(text)
	if len(matches) < 2 {
		return
	}
	illustID := matches[1]

	loadingMsg, _ := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:          update.Message.Chat.ID,
		Text:            "⏳ 正在抓取 Pixiv ID: " + illustID + " ...",
		ReplyParameters: &models.ReplyParameters{MessageID: update.Message.ID},
	})

	illust, err := pixiv.GetIllust(illustID, h.Cfg.PixivPHPSESSID)
	if err != nil {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   "❌ 获取失败: " + err.Error(),
		})
		return
	}

	successCount := 0
	skippedCount := 0

	for i, page := range illust.Pages {
		imgData, err := pixiv.DownloadImage(page.Urls.Original, h.Cfg.PixivPHPSESSID)
		if err != nil {
			fmt.Printf("❌ Pixiv Download Failed: %v\n", err)
			continue
		}
		pid := fmt.Sprintf("pixiv_%s_p%d", illust.ID, i)
		caption := fmt.Sprintf("Pixiv: %s [P%d/%d]\nArtist: %s\nTags: #%s",
			illust.Title, i+1, len(illust.Pages),
			illust.Artist,
			strings.ReplaceAll(illust.Tags, " ", " #"))

		if h.DB.CheckExists(pid) {
			skippedCount++
			continue
		}
		h.ProcessAndSend(ctx, imgData, pid, illust.Tags, caption, "pixiv", page.Width, page.Height)
		successCount++
		time.Sleep(1 * time.Second)
	}

	finalText := fmt.Sprintf("✅ 处理完成！\n成功发送: %d 张\n跳过重复: %d 张", successCount, skippedCount)
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   finalText,
	})

	if loadingMsg != nil {
		b.DeleteMessage(ctx, &bot.DeleteMessageParams{
			ChatID:    update.Message.Chat.ID,
			MessageID: loadingMsg.ID,
		})
	}
}

func (h *BotHandler) handleManyacgLink(ctx context.Context, b *bot.Bot, update *models.Update) {
	// ✅ 关键修改：如果当前正在转发模式，忽略链接，防止冲突
	if h.Forwarding {
		return
	}

	text := update.Message.Text

	// 1. 提取 ManyACG 链接
	re := regexp.MustCompile(`manyacg\.top/artwork/[a-zA-Z0-9]+`)
	matches := re.FindStringSubmatch(text)
	if len(matches) < 1 {
		return
	}
	artworkURL := matches[0]

	// 提示用户正在处理
	loadingMsg, _ := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   "⏳ 正在抓取 ManyACG 链接...",
		ReplyParameters: &models.ReplyParameters{MessageID: update.Message.ID},
	})

	// 2. 调用 manyacg 包获取信息
	artwork, err := manyacg.GetArtworkInfo(artworkURL)
	if err != nil {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   "❌ 获取失败: " + err.Error(),
		})
		return
	}

	// 3. 循环发送每一张图
	successCount := 0
	skippedCount := 0

	for i, pic := range artwork.Pictures {
		// 下载原图
		imgData, err := manyacg.DownloadOriginal(ctx, pic.ID)
		if err != nil {
			fmt.Printf("❌ ManyACG Download Failed: %v\n", err)
			continue
		}

		// 构造唯一的 PID: mtcacg_123456_p0
		pid := fmt.Sprintf("mtcacg_%s_p%d", artwork.ID, i)

		// 构造标题
		caption := fmt.Sprintf("MtcACG: %s [P%d/%d]\nArtist: %s\nTags: %s",
			artwork.Title, i+1, len(artwork.Pictures),
			artwork.Artist,
			manyacg.FormatTags(artwork.Tags))

		// 检查数据库去重
		if h.DB.CheckExists(pid) {
			skippedCount++
			continue
		}

		// 发送
		h.ProcessAndSend(ctx, imgData, pid, manyacg.FormatTags(artwork.Tags), caption, "manyacg", pic.Width, pic.Height)
		successCount++

		// 稍微歇一下
		time.Sleep(1 * time.Second)
	}

	// 4. 反馈结果
	finalText := fmt.Sprintf("✅ 处理完成！\n成功发送: %d 张\n跳过重复: %d 张", successCount, skippedCount)
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   finalText,
	})

	// 删掉那个“正在抓取”的提示（可选）
	if loadingMsg != nil {
		b.DeleteMessage(ctx, &bot.DeleteMessageParams{
			ChatID:    update.Message.Chat.ID,
			MessageID: loadingMsg.ID,
		})
	}
}

