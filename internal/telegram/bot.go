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
	"sync"
	"time"

	"my-bot-go/internal/config"
	"my-bot-go/internal/database"
	"my-bot-go/internal/manyacg"
	"my-bot-go/internal/pixiv"
	"my-bot-go/internal/yande"
	// "my-bot-go/internal/fanbox"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/nfnt/resize"
)

type BotHandler struct {
	API             *bot.Bot
	Cfg             *config.Config
	DB              *database.D1Client
	mu              sync.RWMutex // 🔴 新增互斥锁
	Forwarding      bool
	ForwardBaseID   string
	ForwardIndex    int
	ForwardTitle    string
	ForwardTags     string
	CurrentPreview  *models.Message
	CurrentOriginal *models.Message
}

func NewBot(cfg *config.Config, db *database.D1Client) (*BotHandler, error) {
	h := &BotHandler{Cfg: cfg, DB: db}

	b, err := bot.New(cfg.BotToken)
	if err != nil {
		return nil, err
	}

	h.API = b

	// /save
	b.RegisterHandler(bot.HandlerTypeMessageText, "/save", bot.MatchTypeExact, h.handleSave)

	// /delete
	b.RegisterHandler(bot.HandlerTypeMessageText, "/delete", bot.MatchTypePrefix, h.handleDelete)

	// Pixiv Link
	b.RegisterHandler(bot.HandlerTypeMessageText, "pixiv.net/artworks/", bot.MatchTypeContains, h.handlePixivLink)

	// ManyACG Link
	b.RegisterHandler(bot.HandlerTypeMessageText, "manyacg.top/artwork/", bot.MatchTypeContains, h.handleManyacgLink)

	// Yande Link
	b.RegisterHandler(bot.HandlerTypeMessageText, "yande.re/post/show/", bot.MatchTypeContains, h.handleYandeLink)

	//b.RegisterHandler(bot.HandlerTypeMessageText, "fanbox.cc/@", bot.MatchTypeContains, h.handleFanboxLink)

	// Forward Commands
	b.RegisterHandler(bot.HandlerTypeMessageText, "/forward_start", bot.MatchTypePrefix, h.handleForwardStart)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/forward_continue", bot.MatchTypeExact, h.handleForwardContinue)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/forward_end", bot.MatchTypeExact, h.handleForwardEnd)

	// Universal Handler
	b.RegisterHandler(bot.HandlerTypeMessageText, "", bot.MatchTypePrefix, func(ctx context.Context, b *bot.Bot, update *models.Update) {
		if update.Message == nil {
			return
		}

		// 指令消息跳过
		if strings.HasPrefix(update.Message.Text, "/") {
			return
		}

		// 加读锁检查状态
		h.mu.RLock()
		isForwarding := h.Forwarding
		h.mu.RUnlock()

		// 转发模式，拦截图片
		if isForwarding {
			go func() {
				// 加写锁，因为内部可能修改 CurrentPreview
				h.mu.Lock()
				defer h.mu.Unlock()

				// double check
				if !h.Forwarding {
					return
				}

				msg := update.Message
				bgCtx := context.Background()

				// 处理图片 (Preview)
				if len(msg.Photo) > 0 {
					h.CurrentPreview = msg
					h.CurrentOriginal = nil

					log.Printf("🖼 [Forward] 收到 P%d 预览图", h.ForwardIndex)
					b.SendMessage(bgCtx, &bot.SendMessageParams{
						ChatID:          msg.Chat.ID,
						Text:            fmt.Sprintf("✅ yukiyuki获取到 P%d 预览图啦，主人请发送原图文件(Document)吧，喵~🐱", h.ForwardIndex),
						ReplyParameters: &models.ReplyParameters{MessageID: msg.ID},
					})
					return
				}

				// 处理文件 (Original)
				if msg.Document != nil {
					if h.CurrentPreview == nil {
						h.CurrentPreview = msg
						h.CurrentOriginal = msg
					} else {
						h.CurrentOriginal = msg
					}

					log.Printf("📄 [Forward] 收到 P%d 原图", h.ForwardIndex)
					b.SendMessage(bgCtx, &bot.SendMessageParams{
						ChatID:          msg.Chat.ID,
						Text:            fmt.Sprintf("✅ P%d 就绪了喵~🐱。\n请输入 /forward_continue 发布并继续下一张\n或 /forward_end 发布并结束（^v^）。", h.ForwardIndex),
						ReplyParameters: &models.ReplyParameters{MessageID: msg.ID},
					})
					return
				}
			}()
			return
		}

		// 2. 非转发模式的手动处理
		if len(update.Message.Photo) > 0 {
			go func() {
				h.handleManual(context.Background(), b, update)
			}()
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
	shouldCompress := int64(len(imgData)) > MaxPhotoSize || (width > 4950 || height > 4950)
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

func (h *BotHandler) handleForwardStart(ctx context.Context, b *bot.Bot, update *models.Update) {
	go func() {
		bgCtx := context.Background()
		msg := update.Message
		userID := msg.From.ID
		if userID != 8040798522 && userID != 6874581126 {
			return
		}

		rawText := ""
		if len(msg.Text) > len("/forward_start") {
			rawText = strings.TrimSpace(msg.Text[len("/forward_start"):])
		}
		title := rawText
		tags := ""
		firstHashIndex := strings.Index(rawText, "#")
		if firstHashIndex != -1 {
			title = strings.TrimSpace(rawText[:firstHashIndex])
			tags = strings.TrimSpace(rawText[firstHashIndex:])
		}

		// 初始化状态
		h.mu.Lock()
		h.Forwarding = true
		h.ForwardBaseID = fmt.Sprintf("manual_%d", msg.ID)
		h.ForwardIndex = 0
		h.ForwardTitle = title
		h.ForwardTags = tags
		h.CurrentPreview = nil
		h.CurrentOriginal = nil
		h.mu.Unlock()

		info := fmt.Sprintf("✅ **转发模式已启动**\n🆔 BaseID: `%s`\n📝 标题: %s\n🏷 标签: %s\n\n🐱 请发送 **首张预览图**吧,喵~(^v^)",
			h.ForwardBaseID, title, tags)

		b.SendMessage(bgCtx, &bot.SendMessageParams{
			ChatID: msg.Chat.ID,
			Text:   info,
		})
	}()
}

func (h *BotHandler) publishCurrentItem(ctx context.Context, b *bot.Bot, chatID int64) bool {
	// 🔴 1. 快速读取所有需要的状态
	h.mu.RLock()
	preview := h.CurrentPreview
	original := h.CurrentOriginal
	baseID := h.ForwardBaseID
	index := h.ForwardIndex
	title := h.ForwardTitle
	tags := h.ForwardTags
	h.mu.RUnlock()

	if preview == nil {
		b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: "⚠️ 嗷，出错啦：当前没有等待发布的图片哦，没办法继续了喵~。"})
		return false
	}

	postID := fmt.Sprintf("%s_p%d", baseID, index)

	caption := title
	if caption == "" {
		caption = "MtcACG:TG"
	}
	caption = fmt.Sprintf("%s [P%d]", caption, index)
	if tags != "" {
		caption = caption + "\n" + tags
	}

	dbTags := tags
	if dbTags == "" {
		dbTags = "TG-Forward"
	}

	var previewFileID, originFileID string
	var width, height int

	// 发送预览图
	if len(preview.Photo) > 0 {
		srcPhoto := preview.Photo[len(preview.Photo)-1]
		fwdMsg, err := b.SendPhoto(ctx, &bot.SendPhotoParams{
			ChatID:  h.Cfg.ChannelID,
			Photo:   &models.InputFileString{Data: srcPhoto.FileID},
			Caption: caption,
		})
		if err != nil {
			log.Printf("❌ P%d Preview Send Failed: %v", index, err)
			return false
		}
		previewFileID = fwdMsg.Photo[len(fwdMsg.Photo)-1].FileID
		width = srcPhoto.Width
		height = srcPhoto.Height

		if original != nil && original.Document != nil {
			originFileID = original.Document.FileID
		}
	} else if preview.Document != nil {
		srcDoc := preview.Document
		fwdMsg, err := b.SendDocument(ctx, &bot.SendDocumentParams{
			ChatID:   h.Cfg.ChannelID,
			Document: &models.InputFileString{Data: srcDoc.FileID},
			Caption:  caption,
		})
		if err != nil {
			log.Printf("❌ P%d Doc Send Failed: %v", index, err)
			return false
		}
		previewFileID = fwdMsg.Document.FileID
		originFileID = fwdMsg.Document.FileID
		if fwdMsg.Document.Thumbnail != nil {
			width = fwdMsg.Document.Thumbnail.Width
			height = fwdMsg.Document.Thumbnail.Height
		}
	}

	// 补发原图
	if originFileID != "" && originFileID != previewFileID {
		docMsg, err := b.SendDocument(ctx, &bot.SendDocumentParams{
			ChatID:   h.Cfg.ChannelID,
			Document: &models.InputFileString{Data: originFileID},
			Caption:  fmt.Sprintf("⬇️ %s P%d Original", title, index),
		})
		if err == nil {
			originFileID = docMsg.Document.FileID
		}
	}

	// 存入数据库
	err := h.DB.SaveImage(postID, previewFileID, originFileID, caption, dbTags, "TG-Forward", width, height)
	if err != nil {
		log.Printf("❌ P%d DB Save Failed: %v", index, err)
		b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: "❌ 糟了！数据库保存失败，流程暂停。喵呜(^x_x^)"})
		return false
	}

	log.Printf("✅ Published: %s", postID)
	return true
}

func (h *BotHandler) handleForwardContinue(ctx context.Context, b *bot.Bot, update *models.Update) {
	go func() {
		bgCtx := context.Background()
		h.mu.RLock()
		if !h.Forwarding {
			h.mu.RUnlock()
			return
		}
		h.mu.RUnlock()
		chatID := update.Message.Chat.ID

		success := h.publishCurrentItem(bgCtx, b, chatID)
		if !success {
			return
		}

		h.mu.Lock()
		prevIndex := h.ForwardIndex
		h.ForwardIndex++
		h.CurrentPreview = nil
		h.CurrentOriginal = nil
		h.mu.Unlock()

		b.SendMessage(bgCtx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   fmt.Sprintf("✅ **P%d 已发布** (ID: `%s_p%d`)\n⬇️ 正在等待 **P%d** ...", prevIndex, h.ForwardBaseID, prevIndex, h.ForwardIndex),
		})
	}()
}

func (h *BotHandler) handleForwardEnd(ctx context.Context, b *bot.Bot, update *models.Update) {
	go func() {
		bgCtx := context.Background()
		h.mu.RLock()
		if !h.Forwarding {
			h.mu.RUnlock()
			return
		}
		h.mu.RUnlock()

		chatID := update.Message.Chat.ID

		if h.CurrentPreview != nil {
			success := h.publishCurrentItem(bgCtx, b, chatID)
			if success {
				h.mu.RLock()
				idx := h.ForwardIndex
				h.mu.RUnlock()
				b.SendMessage(bgCtx, &bot.SendMessageParams{
					ChatID: chatID,
					Text:   fmt.Sprintf("✅ **P%d (尾图) 已发布**", idx),
				})
			}
		}

		h.mu.Lock()
		h.Forwarding = false
		h.ForwardBaseID = ""
		h.ForwardIndex = 0
		h.CurrentPreview = nil
		h.CurrentOriginal = nil
		h.ForwardTitle = ""
		h.ForwardTags = ""
		h.mu.Unlock()

		b.SendMessage(bgCtx, &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      "🏁 🐱好耶（^-^）**任务完成喵~** 🐱",
			ParseMode: models.ParseModeMarkdown,
		})
	}()
}

func compressImage(data []byte, targetSize int64) ([]byte, error) {
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode error: %v", err)
	}
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width > 4950 || height > 4950 {
		log.Printf("📏 Resizing image from %dx%d (Too big for TG)", width, height)
		if width > height {
			img = resize.Resize(4950, 0, img, resize.Lanczos3)
		} else {
			img = resize.Resize(0, 4950, img, resize.Lanczos3)
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

	go func() {
		bgCtx := context.Background()

		text := update.Message.Text
		re := regexp.MustCompile(`artworks/(\d+)`)
		matches := re.FindStringSubmatch(text)
		if len(matches) < 2 {
			return
		}
		illustID := matches[1]

		loadingMsg, _ := b.SendMessage(bgCtx, &bot.SendMessageParams{
			ChatID:          update.Message.Chat.ID,
			Text:            "⏳ 正在抓取 Pixiv ID 了喵~🐱: " + illustID + " ...",
			ReplyParameters: &models.ReplyParameters{MessageID: update.Message.ID},
		})

		illust, err := pixiv.GetIllust(illustID, h.Cfg.PixivPHPSESSID)
		if err != nil {
			b.SendMessage(bgCtx, &bot.SendMessageParams{
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
			h.ProcessAndSend(bgCtx, imgData, pid, illust.Tags, caption, "pixiv", page.Width, page.Height)
			successCount++
			time.Sleep(1 * time.Second)
		}

		finalText := fmt.Sprintf("✅ 处理完成了喵~🐱！\n成功发送: %d 张\n跳过重复: %d 张", successCount, skippedCount)
		b.SendMessage(bgCtx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   finalText,
		})

		if loadingMsg != nil {
			b.DeleteMessage(bgCtx, &bot.DeleteMessageParams{
				ChatID:    update.Message.Chat.ID,
				MessageID: loadingMsg.ID,
			})
		}
	}()
}

func (h *BotHandler) handleManyacgLink(ctx context.Context, b *bot.Bot, update *models.Update) {
	if h.Forwarding {
		return
	}

	go func() {
		bgCtx := context.Background()

		text := update.Message.Text
		re := regexp.MustCompile(`manyacg\.top/artwork/[a-zA-Z0-9]+`)
		matches := re.FindStringSubmatch(text)
		if len(matches) < 1 {
			return
		}
		artworkURL := matches[0]

		loadingMsg, _ := b.SendMessage(bgCtx, &bot.SendMessageParams{
			ChatID:          update.Message.Chat.ID,
			Text:            "⏳ 正在抓取 ManyACG 链接...了 喵~🐱",
			ReplyParameters: &models.ReplyParameters{MessageID: update.Message.ID},
		})

		artwork, err := manyacg.GetArtworkInfo(artworkURL)
		if err != nil {
			b.SendMessage(bgCtx, &bot.SendMessageParams{
				ChatID: update.Message.Chat.ID,
				Text:   "❌ 获取失败: " + err.Error(),
			})
			return
		}

		successCount := 0
		skippedCount := 0

		for i, pic := range artwork.Pictures {
			imgData, err := manyacg.DownloadOriginal(bgCtx, pic.ID)
			if err != nil {
				fmt.Printf("❌ ManyACG Download Failed: %v\n", err)
				continue
			}

			pid := fmt.Sprintf("mtcacg_%s_p%d", artwork.ID, i)
			caption := fmt.Sprintf("MtcACG: %s [P%d/%d]\nArtist: %s\nTags: %s",
				artwork.Title, i+1, len(artwork.Pictures),
				artwork.Artist,
				manyacg.FormatTags(artwork.Tags))

			if h.DB.CheckExists(pid) {
				skippedCount++
				continue
			}

			h.ProcessAndSend(bgCtx, imgData, pid, manyacg.FormatTags(artwork.Tags), caption, "manyacg", pic.Width, pic.Height)
			successCount++
			time.Sleep(1 * time.Second)
		}

		finalText := fmt.Sprintf("✅ 处理完成了喵~🐱！\n成功发送: %d 张\n跳过重复: %d 张", successCount, skippedCount)
		b.SendMessage(bgCtx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   finalText,
		})

		if loadingMsg != nil {
			b.DeleteMessage(bgCtx, &bot.DeleteMessageParams{
				ChatID:    update.Message.Chat.ID,
				MessageID: loadingMsg.ID,
			})
		}
	}()
}

func (h *BotHandler) handleYandeLink(ctx context.Context, b *bot.Bot, update *models.Update) {
	if h.Forwarding {
		return
	}

	go func() {
		bgCtx := context.Background()

		text := update.Message.Text
		re := regexp.MustCompile(`post/show/(\d+)`)
		matches := re.FindStringSubmatch(text)
		if len(matches) < 2 {
			return
		}

		postID := matches[1]
		pid := fmt.Sprintf("yande_%s", postID)

		if h.DB.CheckExists(pid) {
			b.SendMessage(bgCtx, &bot.SendMessageParams{
				ChatID:          update.Message.Chat.ID,
				Text:            "⏭️ 这张图已经发过了哦 (ID: " + pid + ")，跳过。",
				ReplyParameters: &models.ReplyParameters{MessageID: update.Message.ID},
			})
			return
		}

		loadingMsg, _ := b.SendMessage(bgCtx, &bot.SendMessageParams{
			ChatID:          update.Message.Chat.ID,
			Text:            "⏳ 正在抓取 Yande ID 了喵~🐱: " + postID + " ...",
			ReplyParameters: &models.ReplyParameters{MessageID: update.Message.ID},
		})

		post, err := yande.GetYandePost(postID)
		if err != nil {
			b.SendMessage(bgCtx, &bot.SendMessageParams{
				ChatID: update.Message.Chat.ID,
				Text:   "❌ 获取失败: " + err.Error(),
			})
			if loadingMsg != nil {
				b.DeleteMessage(bgCtx, &bot.DeleteMessageParams{ChatID: update.Message.Chat.ID, MessageID: loadingMsg.ID})
			}
			return
		}

		imgURL := yande.SelectBestURL(post)
		imgData, err := yande.DownloadYandeImage(imgURL)
		if err != nil {
			b.SendMessage(bgCtx, &bot.SendMessageParams{
				ChatID: update.Message.Chat.ID,
				Text:   "❌ 下载图片失败: " + err.Error(),
			})
			if loadingMsg != nil {
				b.DeleteMessage(bgCtx, &bot.DeleteMessageParams{ChatID: update.Message.Chat.ID, MessageID: loadingMsg.ID})
			}
			return
		}

		tags := strings.ReplaceAll(post.Tags, " ", " #")
		caption := fmt.Sprintf("Yande: %d\nSize: %dx%d\nTags: #%s",
			post.ID, post.Width, post.Height, tags)

		h.ProcessAndSend(bgCtx, imgData, pid, post.Tags, caption, "yande", post.Width, post.Height)

		if loadingMsg != nil {
			b.DeleteMessage(bgCtx, &bot.DeleteMessageParams{
				ChatID:    update.Message.Chat.ID,
				MessageID: loadingMsg.ID,
			})
		}

		b.SendMessage(bgCtx, &bot.SendMessageParams{
			ChatID:          update.Message.Chat.ID,
			Text:            "✅ 处理完成！",
			ReplyParameters: &models.ReplyParameters{MessageID: update.Message.ID},
		})
	}()
}

func (h *BotHandler) handleDelete(ctx context.Context, b *bot.Bot, update *models.Update) {
	go func() {
		bgCtx := context.Background()

		userID := update.Message.From.ID
		if userID != 8040798522 && userID != 6874581126 {
			b.SendMessage(bgCtx, &bot.SendMessageParams{
				ChatID: update.Message.Chat.ID,
				Text:   "⛔ 你没有权限执行删除操作喵~",
			})
			return
		}

		text := update.Message.Text
		parts := strings.Fields(text)
		if len(parts) < 2 {
			b.SendMessage(bgCtx, &bot.SendMessageParams{
				ChatID: update.Message.Chat.ID,
				Text:   "⚠️ 格式不对喵🐱！~请输入：/delete <ID>\n例如：/delete pixiv_114514_p0。再输错，小心本喵帮你格式化🐱嗷~",
			})
			return
		}

		targetID := strings.TrimSpace(parts[1])

		err := h.DB.DeleteImage(targetID)
		if err != nil {
			log.Printf("❌ Delete Failed: %v", err)
			b.SendMessage(bgCtx, &bot.SendMessageParams{
				ChatID: update.Message.Chat.ID,
				Text:   fmt.Sprintf("🐱不好了喵~❌ 删除失败: %v", err),
			})
			return
		}

		log.Printf("🗑️ Image deleted: %s", targetID)
		b.SendMessage(bgCtx, &bot.SendMessageParams{
			ChatID:    update.Message.Chat.ID,
			Text:      fmt.Sprintf("🗑️🐱Yuki猫猫已经帮主人清理干净了喵~!🐱图片 `%s` 已从数据库移除。", targetID),
			//ParseMode: models.ParseModeMarkdown,
		})
	}()
}

//func (h *BotHandler) handleFanboxLink(ctx context.Context, b *bot.Bot, update *models.Update) {
//    if h.Forwarding {
//        return
//    }

 //   text := update.Message.Text
 //   re := regexp.MustCompile(`fanbox\.cc/@[\w-]+/posts/(\d+)`)
//    matches := re.FindStringSubmatch(text)
//    if len(matches) < 2 {
//        return
//    }

//    postID := matches[1]
//    pid := fmt.Sprintf("fanbox_%s", postID)

    // ✅ 先查重
//    if h.DB.CheckExists(pid) {
//        b.SendMessage(ctx, &bot.SendMessageParams{
//            ChatID:             update.Message.Chat.ID,
 //           Text:               "⏭️ Fanbox 这张已经发过了，跳过。",
 //           ReplyParameters:    &models.ReplyParameters{MessageID: update.Message.ID},
  //      })
  //      return
//    }

//    loadingMsg, _ := b.SendMessage(ctx, &bot.SendMessageParams{
//        ChatID:             update.Message.Chat.ID,
//        Text:               "⏳ 正在抓取 Fanbox ID: " + postID + " ...",
//        ReplyParameters:    &models.ReplyParameters{MessageID: update.Message.ID},
//    })

    // 获取详情
//    post, err := fanbox.GetFanboxPost(postID, h.Cfg.FanboxCookie)
//    if err != nil {
//        b.SendMessage(ctx, &bot.SendMessageParams{
//            ChatID: update.Message.Chat.ID,
//           Text:   "❌ Fanbox 获取失败: " + err.Error(),
//        })
//        return
//    }

    // 处理多图
//    successCount := 0
//    for i, img := range post.Images {
//        imgData, err := fanbox.DownloadFanboxImage(img.URL, h.Cfg.FanboxCookie)
//        if err != nil {
//            continue
//        }
//
//        caption := fmt.Sprintf("Fanbox: %s [P%d/%d]\nAuthor: %s\nTags: #%s",
//            post.Title, i+1, len(post.Images),
//            post.Author,
//            strings.Join(post.Tags, " #"))
//
//        h.ProcessAndSend(ctx, imgData, fmt.Sprintf("%s_p%d", pid, i), 
//            strings.Join(post.Tags, " "), caption, "fanbox", img.Width, img.Height)
//        successCount++
//        time.Sleep(1 * time.Second)
//    }

    // 6. 完成反馈
//    if loadingMsg != nil {
//        b.DeleteMessage(ctx, &bot.DeleteMessageParams{
//            ChatID:    update.Message.Chat.ID,
//            MessageID: loadingMsg.ID,
//        })
//    }
	
//    b.SendMessage(ctx, &bot.SendMessageParams{
//        ChatID: update.Message.Chat.ID,
//        Text:   fmt.Sprintf("✅ Fanbox 处理完成！发送 %d 张", successCount),
//    })
