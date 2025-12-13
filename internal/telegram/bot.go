package telegram

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"log"
	"strings"

	"my-bot-go/internal/config"
	"my-bot-go/internal/database"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// 状态常量
const (
	StateNone = iota
	StateWaitingTitle    // 等待用户确认标题
	StateWaitingTag      // 等待用户选择标签
)

// 用户会话
type UserSession struct {
	State       int
	PhotoFileID string
	Width       int
	Height      int
	Caption     string
	MessageID   int
}

type BotHandler struct {
	API      *bot.Bot
	Cfg      *config.Config
	DB       *database.D1Client
	Sessions map[int64]*UserSession
}

func NewBot(cfg *config.Config, db *database.D1Client) (*BotHandler, error) {
	opts := []bot.Option{
		bot.WithDefaultHandler(func(ctx context.Context, b *bot.Bot, update *models.Update) {
            // 默认不做任何事
		}),
	}

	b, err := bot.New(cfg.BotToken, opts...)
	if err != nil {
		return nil, err
	}

	h := &BotHandler{API: b, Cfg: cfg, DB: db, Sessions: make(map[int64]*UserSession)}

    // ---------------------------------------------------------
    // ✅ Handler 注册
    // ---------------------------------------------------------

	// 1. 优先处理按钮回调 (Inline Button)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "", bot.MatchTypePrefix, h.handleTagCallback)

	// 2. 注册具体指令 /save
	b.RegisterHandler(bot.HandlerTypeMessageText, "/save", bot.MatchTypeExact, h.handleSave)

	// 3. 统一消息入口：处理 图片 OR 文本回复
	b.RegisterHandler(bot.HandlerTypeMessageText, "", bot.MatchTypePrefix, h.handleMainRouter)

	return h, nil
}

func (h *BotHandler) Start(ctx context.Context) {
	h.API.Start(ctx)
}

// =====================================================================================
// ✅ 核心逻辑路由
// =====================================================================================

func (h *BotHandler) handleMainRouter(ctx context.Context, b *bot.Bot, update *models.Update) {
    if update.Message == nil {
        return
    }

    // A. 如果是图片 -> 进入新图片处理流程
    if len(update.Message.Photo) > 0 {
        h.handleNewPhoto(ctx, b, update)
        return
    }

    // B. 如果是文本 -> 分发给文本处理
    if update.Message.Text != "" {
        h.handleTextReply(ctx, b, update)
        return
    }
}

// 处理新收到的图片
func (h *BotHandler) handleNewPhoto(ctx context.Context, b *bot.Bot, update *models.Update) {
	userID := update.Message.From.ID
	photo := update.Message.Photo[len(update.Message.Photo)-1]

	caption := update.Message.Caption
	if caption == "" {
		caption = "MtcACG:TG"
	}

	h.Sessions[userID] = &UserSession{
		State:       StateWaitingTitle,
		PhotoFileID: photo.FileID,
		Width:       photo.Width,
		Height:      photo.Height,
		Caption:     caption,
		MessageID:   update.Message.ID,
	}

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   fmt.Sprintf("📩 收到图片了,Daishiki喵！\n\n当前标题：\n%s\n\n主人要自定义标题吗,喵？\n1️和我说 `/title` 就可以使用新标题了喵\n2️说 `/no` 那就只能使用原标题的说,喵", caption),
		ReplyParameters: &models.ReplyParameters{
			MessageID: update.Message.ID,
		},
        // 这里顺手移除可能残留的旧键盘（ReplyKeyboardRemove）
        // 如果想保险一点，可以在发每条消息时都带上 ReplyKeyboardRemove，但这和 InlineKeyboard 冲突
        // 既然现在都转 Inline 了，我们可以在这里先尝试清一次
	})
}

// 处理文本回复
func (h *BotHandler) handleTextReply(ctx context.Context, b *bot.Bot, update *models.Update) {
	userID := update.Message.From.ID
	session, exists := h.Sessions[userID]

	// 1. 如果没有会话
	if !exists {
		return
	}

    text := update.Message.Text

    // ============================================================
    // 兼容逻辑：如果用户点了旧的键盘 (TGC-SFW / TGC-NSFW)
    // 即使在 WaitingTitle 阶段，我们也允许直接通过这个跳过
    // 或者在 WaitingTag 阶段响应这个文本
    // ============================================================
    if text == "TGC-SFW" || text == "TGC-NSFW" || text == "TG-SFW" || text == "TG-NSFW" {
        // 如果当前是 WaitingTag 或者 WaitingTitle (防止用户手快直接点了旧键盘)
        // 我们直接把它当做选择了标签处理
        tag := ""
        if text == "TGC-SFW" || text == "TG-SFW" {
            tag = "#TGC #SFW"
        } else {
            tag = "#TGC #NSFW #R18"
        }
        
        h.processForwardUpload(ctx, b, update.Message.Chat.ID, session, tag)
        delete(h.Sessions, userID)
        
        // 发送一个“移除键盘”的消息，彻底把那个烦人的旧键盘清掉
        b.SendMessage(ctx, &bot.SendMessageParams{
            ChatID: update.Message.Chat.ID,
            Text:   "✅ 识别到标签，已上传喵！（旧键盘已移除）",
            ReplyMarkup: &models.ReplyKeyboardRemove{}, // 👈 这里是关键，清除旧键盘
        })
        return
    }

    // 2. 如果状态不对（比如已经结束了），忽略
    if session.State != StateWaitingTitle {
        return
    }

    // 3. 处理 /no 和 /title 指令
	if text == "/no" || strings.EqualFold(text, "no") { // 兼容大小写 no
		// 确认使用原标题
	} else if strings.HasPrefix(text, "/title ") {
		newTitle := strings.TrimSpace(strings.TrimPrefix(text, "/title "))
		if newTitle != "" {
			session.Caption = newTitle
		} else {
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID: update.Message.Chat.ID,
				Text:   "⚠️ 标题不能为空啊喵，请重新跟我说说吧 `/title 你的标题`",
			})
			return
		}
	} else {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   "⚠️ 格式错误,喵~！\n- 确认原标题请回复 `/no`喵~\n- 自定义标题请回复 `/title 新标题`喵~",
		})
		return
	}

    // 状态流转 -> 等待标签
	session.State = StateWaitingTag

	kb := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "TG-SFW", CallbackData: "tag_sfw"},
				{Text: "TG-NSFW", CallbackData: "tag_nsfw"},
			},
		},
	}

    // 发送带有 Inline 按钮的消息
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      update.Message.Chat.ID,
		Text:        fmt.Sprintf("✅ 狗修金,标题确认好了喵~: \n%s\n\n请主人狠狠点击下方按钮选择标签,打上只属于主人的标记吧。：", session.Caption),
		ReplyMarkup: kb,
	})
}

// 处理按钮回调 (Inline Button)
func (h *BotHandler) handleTagCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	userID := update.CallbackQuery.From.ID
	session, exists := h.Sessions[userID]

	if !exists || session.State != StateWaitingTag {
		b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: update.CallbackQuery.ID,
			Text:            "⚠️ 哎哟,会话已过期，请重新转发图片,喵~。",
		})
		return
	}

	data := update.CallbackQuery.Data
	tag := ""
	if data == "tag_sfw" {
		tag = "#TGC #SFW"
	} else if data == "tag_nsfw" {
		tag = "#TGC #NSFW #R18"
	}

	if tag != "" {
		chatID := update.CallbackQuery.Message.Chat.ID

		h.processForwardUpload(ctx, b, chatID, session, tag)
		delete(h.Sessions, userID) // 上传完清除会话

		// 编辑原消息，去掉按钮，防止重复点击
		b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: update.CallbackQuery.Message.MessageID, 
			Text:      fmt.Sprintf("✅ 已处理: \n%s\n\nTags: %s", session.Caption, tag),
		})
        
        // 【关键】顺便发一条不可见的消息或者小提示，带上 ReplyKeyboardRemove，
        // 试图清除那个顽固的旧键盘（虽然 Inline 回调里不方便直接发新消息清键盘，但在逻辑上旧键盘应该已经没用了）
	}

	b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: update.CallbackQuery.ID,
	})
}

// 核心上传逻辑
func (h *BotHandler) processForwardUpload(ctx context.Context, b *bot.Bot, chatID int64, session *UserSession, tag string) {
	msg, err := b.SendPhoto(ctx, &bot.SendPhotoParams{
		ChatID:  h.Cfg.ChannelID,
		Photo:   &models.InputFileString{Data: session.PhotoFileID},
		Caption: fmt.Sprintf("%s\nTags: %s", session.Caption, tag),
	})

	if err != nil {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "❌ 发送失败，喵~ (" + err.Error() + ")",
		})
		return
	}

	postID := fmt.Sprintf("manual_%d", msg.ID)
	finalFileID := msg.Photo[len(msg.Photo)-1].FileID

	err = h.DB.SaveImage(postID, finalFileID, session.Caption, tag, "manual", session.Width, session.Height)
	if err != nil {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "❌ 图片已发频道，但数据库保存失败，喵~",
		})
	} else {
        // 这里不发消息了，因为 Inline 模式下通常编辑原消息就够了。
        // 或者你可以选择发一条 "上传成功" 的消息，并带上 RemoveKeyboard
        /*
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "上传成功，喵~ 🐱",
            ReplyMarkup: &models.ReplyKeyboardRemove{}, // 尝试清除旧键盘
		})
        */
	}
}

// =====================================================================================
// 辅助方法
// =====================================================================================

func (h *BotHandler) ProcessAndSend(ctx context.Context, imgData []byte, postID, tags, caption, source string, width, height int) {
	if h.DB.History[postID] {
		log.Printf("⏭️ Skip %s: already in history", postID)
		return
	}

	const MaxPhotoSize = 9 * 1024 * 1024 
	finalData := imgData

	if int64(len(imgData)) > MaxPhotoSize {
		log.Printf("⚠️ Image %s is too large (%.2f MB), compressing...", postID, float64(len(imgData))/1024/1024)
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

func compressImage(data []byte, targetSize int64) ([]byte, error) {
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode error: %v", err)
	}
	log.Printf("📉 Compressing %s image...", format)

	quality := 98
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
