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
	"my-bot-go/internal/yande"
	//"my-bot-go/internal/fanbox"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/nfnt/resize"
)

type BotHandler struct {
	API             *bot.Bot
	Cfg             *config.Config
	DB              *database.D1Client
	Forwarding      bool
	ForwardBaseID   string          // 基础ID (例如 manual_1338)	
	ForwardIndex    int             // 当前是第几张 (0, 1, 2...)
	ForwardTitle    string
	ForwardTags     string // ✅ 新增字段
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

	// ✅ /save
	b.RegisterHandler(bot.HandlerTypeMessageText, "/save", bot.MatchTypeExact, h.handleSave)

	// ✅ Pixiv Link
	b.RegisterHandler(bot.HandlerTypeMessageText, "pixiv.net/artworks/", bot.MatchTypeContains, h.handlePixivLink)

	// ✅ 新增：监听 ManyACG 链接
    b.RegisterHandler(bot.HandlerTypeMessageText, "manyacg.top/artwork/", bot.MatchTypeContains, h.handleManyacgLink)

	// ✅ 新增：监听 Yande 链接
    // 匹配如 https://yande.re/post/show/1179601
    b.RegisterHandler(bot.HandlerTypeMessageText, "yande.re/post/show/", bot.MatchTypeContains, h.handleYandeLink)

	// 在 NewBot() 注册
    //b.RegisterHandler(bot.HandlerTypeMessageText, "fanbox.cc/@", bot.MatchTypeContains, h.handleFanboxLink)


	// ✅ /forward_start & /forward_end
	b.RegisterHandler(bot.HandlerTypeMessageText, "/forward_start", bot.MatchTypePrefix, h.handleForwardStart)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/forward_continue", bot.MatchTypeExact, h.handleForwardContinue) // 新增
	b.RegisterHandler(bot.HandlerTypeMessageText, "/forward_end", bot.MatchTypeExact, h.handleForwardEnd)

	// ✅ Default handler
	b.RegisterHandler(bot.HandlerTypeMessageText, "", bot.MatchTypePrefix, func(ctx context.Context, b *bot.Bot, update *models.Update) {
		if update.Message == nil {
			return
		}
// 1. 如果处于转发模式，拦截图片
		if h.Forwarding {
			msg := update.Message
			
			// 处理图片 (Preview)
			if len(msg.Photo) > 0 {
				h.CurrentPreview = msg
				// 如果是新的一张，清空可能残留的原图
				h.CurrentOriginal = nil 
				
				log.Printf("🖼 [Forward] 收到 P%d 预览图", h.ForwardIndex)
				b.SendMessage(ctx, &bot.SendMessageParams{
					ChatID:          msg.Chat.ID,
					Text:            fmt.Sprintf("✅ 已获取 P%d 预览图，请发送原图文件(Document)。", h.ForwardIndex),
					ReplyParameters: &models.ReplyParameters{MessageID: msg.ID},
				})
				return
			}

			// 处理文件 (Original)
			if msg.Document != nil {
				if h.CurrentPreview == nil {
					// 如果没发预览图直接发文件，把文件同时作为预览和原图
					h.CurrentPreview = msg
					h.CurrentOriginal = msg
				} else {
					h.CurrentOriginal = msg
				}
				
				log.Printf("📄 [Forward] 收到 P%d 原图", h.ForwardIndex)
				b.SendMessage(ctx, &bot.SendMessageParams{
					ChatID:          msg.Chat.ID,
					Text:            fmt.Sprintf("✅ P%d 就绪。\n请输入 /forward_continue 发布并继续下一张\n或 /forward_end 发布并结束。", h.ForwardIndex),
					ReplyParameters: &models.ReplyParameters{MessageID: msg.ID},
				})
				return
			}
			return
		}

		// 2. 非转发模式的手动处理 (handleManual)
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

// ==================== 转发/父子图 核心逻辑 ====================

// 1. 开始会话
func (h *BotHandler) handleForwardStart(ctx context.Context, b *bot.Bot, update *models.Update) {
	msg := update.Message
	userID := msg.From.ID
	if userID != 8040798522 && userID != 6874581126 { // 鉴权
		return
	}

	// 解析标题和标签
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
	h.Forwarding = true
	h.ForwardBaseID = fmt.Sprintf("manual_%d", msg.ID) // 只有 Start 时生成一次 BaseID
	h.ForwardIndex = 0
	h.ForwardTitle = title
	h.ForwardTags = tags
	h.CurrentPreview = nil
	h.CurrentOriginal = nil

	info := fmt.Sprintf("✅ **转发模式已启动**\n🆔 BaseID: `%s`\n📝 标题: %s\n🏷 标签: %s\n\n👉 请发送 **P0 预览图**", 
		h.ForwardBaseID, title, tags)

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    msg.Chat.ID,
		Text:      info,
		///ParseMode: models.ParseModeMarkdown,
	})
}

// 2. 辅助函数：发布当前缓存的那一张 (BaseID_pX)
func (h *BotHandler) publishCurrentItem(ctx context.Context, b *bot.Bot, chatID int64) bool {
	if h.CurrentPreview == nil {
		b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: "⚠️ 错误：当前没有待发布的图片，无法继续。"})
		return false
	}

	// 构造 ID: manual_1001_p0
	postID := fmt.Sprintf("%s_p%d", h.ForwardBaseID, h.ForwardIndex)
	
	// 构造标题
	caption := h.ForwardTitle
	if caption == "" { caption = "MtcACG:TG" }
	// 添加页码显示，方便查看
	caption = fmt.Sprintf("%s [P%d]", caption, h.ForwardIndex)
	if h.ForwardTags != "" {
		caption = caption + "\n" + h.ForwardTags
	}

	dbTags := h.ForwardTags
	if dbTags == "" { dbTags = "TG-Forward" }

	var previewFileID, originFileID string
	var width, height int

	// 发送预览图到频道
	if len(h.CurrentPreview.Photo) > 0 {
		srcPhoto := h.CurrentPreview.Photo[len(h.CurrentPreview.Photo)-1]
		fwdMsg, err := b.SendPhoto(ctx, &bot.SendPhotoParams{
			ChatID:  h.Cfg.ChannelID,
			Photo:   &models.InputFileString{Data: srcPhoto.FileID},
			Caption: caption,
		})
		if err != nil {
			log.Printf("❌ P%d Preview Send Failed: %v", h.ForwardIndex, err)
			return false
		}
		previewFileID = fwdMsg.Photo[len(fwdMsg.Photo)-1].FileID
		width = srcPhoto.Width
		height = srcPhoto.Height
		
		if h.CurrentOriginal != nil && h.CurrentOriginal.Document != nil {
			originFileID = h.CurrentOriginal.Document.FileID
		}
	} else if h.CurrentPreview.Document != nil {
		// Document 模式
		srcDoc := h.CurrentPreview.Document
		fwdMsg, err := b.SendDocument(ctx, &bot.SendDocumentParams{
			ChatID:   h.Cfg.ChannelID,
			Document: &models.InputFileString{Data: srcDoc.FileID},
			Caption:  caption,
		})
		if err != nil {
			log.Printf("❌ P%d Doc Send Failed: %v", h.ForwardIndex, err)
			return false
		}
		previewFileID = fwdMsg.Document.FileID
		originFileID = fwdMsg.Document.FileID // 文档模式原图即预览图
		if fwdMsg.Document.Thumbnail != nil {
			width = fwdMsg.Document.Thumbnail.Width
			height = fwdMsg.Document.Thumbnail.Height
		}
	}

	// 补发原图 (如果存在且不同)
	if originFileID != "" && originFileID != previewFileID {
		docMsg, err := b.SendDocument(ctx, &bot.SendDocumentParams{
			ChatID:   h.Cfg.ChannelID,
			Document: &models.InputFileString{Data: originFileID},
			Caption:  fmt.Sprintf("⬇️ %s P%d Original", h.ForwardTitle, h.ForwardIndex),
		})
		if err == nil {
			originFileID = docMsg.Document.FileID
		}
	}

	// 存入数据库
	err := h.DB.SaveImage(postID, previewFileID, originFileID, caption, dbTags, "TG-Forward", width, height)
	if err != nil {
		log.Printf("❌ P%d DB Save Failed: %v", h.ForwardIndex, err)
		b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: "❌ 数据库保存失败，流程暂停。"})
		return false
	}
	
	log.Printf("✅ Published: %s", postID)
	return true
}

// 3. 继续下一张 /forward_continue
func (h *BotHandler) handleForwardContinue(ctx context.Context, b *bot.Bot, update *models.Update) {
	if !h.Forwarding { return }
	chatID := update.Message.Chat.ID

	// 尝试发布当前缓存的图片
	success := h.publishCurrentItem(ctx, b, chatID)
	if !success {
		return
	}

	// 发布成功后：更新索引，清空缓存
	prevIndex := h.ForwardIndex
	h.ForwardIndex++
	h.CurrentPreview = nil
	h.CurrentOriginal = nil

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   fmt.Sprintf("✅ **P%d 已发布** (ID: `%s_p%d`)\n⬇️ 正在等待 **P%d** ...", prevIndex, h.ForwardBaseID, prevIndex, h.ForwardIndex),
		ParseMode: models.ParseModeMarkdown,
	})
}

// 4. 结束会话 /forward_end
func (h *BotHandler) handleForwardEnd(ctx context.Context, b *bot.Bot, update *models.Update) {
	if !h.Forwarding { return }
	chatID := update.Message.Chat.ID

	// 检查是否还有最后一张未发布 (用户发了图直接按end的情况)
	if h.CurrentPreview != nil {
		success := h.publishCurrentItem(ctx, b, chatID)
		if success {
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID: chatID,
				Text:   fmt.Sprintf("✅ **P%d (尾图) 已发布**", h.ForwardIndex),
				ParseMode: models.ParseModeMarkdown,
			})
		}
	}

	// 清理状态
	h.Forwarding = false
	h.ForwardBaseID = ""
	h.ForwardIndex = 0
	h.CurrentPreview = nil
	h.CurrentOriginal = nil
	h.ForwardTitle = ""
	h.ForwardTags = ""

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   "🏁 **转发会话结束**",
		ParseMode: models.ParseModeMarkdown,
	})
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

// ✅ 新增处理函数
func (h *BotHandler) handleYandeLink(ctx context.Context, b *bot.Bot, update *models.Update) {
    if h.Forwarding {
        return
    }

    text := update.Message.Text
    // 正则匹配 ID
    re := regexp.MustCompile(`post/show/(\d+)`)
    matches := re.FindStringSubmatch(text)
    if len(matches) < 2 {
        return
    }

    postID := matches[1]
    
    // 构造 PID (先构造出来去查重)
    // 注意：ID是字符串转int，这里我们假设正则抓到的数字是合法的
    // 最好还是转一下 int 保持一致性，虽然字符串拼接也行
    pid := fmt.Sprintf("yande_%s", postID)

    // ✅ 1. 先查重
    if h.DB.CheckExists(pid) {
        b.SendMessage(ctx, &bot.SendMessageParams{
            ChatID:             update.Message.Chat.ID,
            Text:               "⏭️ 这张图已经发过了 (ID: " + pid + ")，跳过。",
            ReplyParameters:    &models.ReplyParameters{MessageID: update.Message.ID},
        })
        return // 直接结束
    }

    // 提示正在抓取
    loadingMsg, _ := b.SendMessage(ctx, &bot.SendMessageParams{
        ChatID:             update.Message.Chat.ID,
        Text:               "⏳ 正在抓取 Yande ID: " + postID + " ...",
        ReplyParameters:    &models.ReplyParameters{MessageID: update.Message.ID},
    })

    // 2. 获取详情
    post, err := yande.GetYandePost(postID)
    if err != nil {
        b.SendMessage(ctx, &bot.SendMessageParams{
            ChatID: update.Message.Chat.ID,
            Text:   "❌ 获取失败: " + err.Error(),
        })
        // 删掉 loading 消息
        if loadingMsg != nil {
            b.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: update.Message.Chat.ID, MessageID: loadingMsg.ID})
        }
        return
    }

    // 3. 下载图片
    imgURL := yande.SelectBestURL(post)
    imgData, err := yande.DownloadYandeImage(imgURL)
    if err != nil {
        b.SendMessage(ctx, &bot.SendMessageParams{
            ChatID: update.Message.Chat.ID,
            Text:   "❌ 下载图片失败: " + err.Error(),
        })
        if loadingMsg != nil {
            b.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: update.Message.Chat.ID, MessageID: loadingMsg.ID})
        }
        return
    }

    // 4. 构造发送参数
    tags := strings.ReplaceAll(post.Tags, " ", " #")
    caption := fmt.Sprintf("Yande: %d\nSize: %dx%d\nTags: #%s", 
        post.ID, post.Width, post.Height, tags)

    // 5. 发送并保存
    h.ProcessAndSend(ctx, imgData, pid, post.Tags, caption, "yande", post.Width, post.Height)

    // 6. 完成反馈
    if loadingMsg != nil {
        b.DeleteMessage(ctx, &bot.DeleteMessageParams{
            ChatID:    update.Message.Chat.ID,
            MessageID: loadingMsg.ID,
        })
    }
    
    b.SendMessage(ctx, &bot.SendMessageParams{
        ChatID: update.Message.Chat.ID,
        Text:   "✅ 处理完成！",
        ReplyParameters: &models.ReplyParameters{MessageID: update.Message.ID},
    })
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
//}


