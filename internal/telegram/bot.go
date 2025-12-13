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

// 状态常量
const (
	StateNone = iota
	StateWaitingTitle    // 等待用户确认标题
	StateWaitingTag      // 等待用户选择标签
)

// 用户会话，用于暂存转发图片的信息
type UserSession struct {
	State       int
	PhotoFileID string
	Width       int
	Height      int
	Caption     string // 图片原本的 caption 或者用户自定义的
	MessageID   int    // 原消息 ID (方便引用回复)
}

type BotHandler struct {
	API *bot.Bot
	Cfg *config.Config
	DB  *database.D1Client
	Sessions map[int64]*UserSession // ✅ 新增：用户 ID -> 会话状态
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
	
	h := &BotHandler{API: b, Cfg: cfg, DB: db, Sessions: make(map[int64]*UserSession),}
	
	// ✅ 注册 /save 命令
	b.RegisterHandler(bot.HandlerTypeMessageText, "/save", bot.MatchTypeExact, h.handleSave)

	// ✅ 注册 CallbackQuery (按钮点击)
	b.RegisterHandler(bot.HandlerTypeCallbackQuery, "", bot.MatchTypePrefix, h.handleCallback)

		// ✅ 新增：监听所有文本消息，用于处理交互式问答
	b.RegisterHandler(bot.HandlerTypeMessageText, "", bot.MatchTypePrefix, h.handleTextReply)


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
	userID := update.Message.From.ID

	// 获取最大尺寸图片
	photo := update.Message.Photo[len(update.Message.Photo)-1]

	// 默认标题处理
	caption := update.Message.Caption
	if caption == "" {
		caption = "MtcACG:TG" // 默认标题
	}

	// 保存会话状态
	h.Sessions[userID] = &UserSession{
		State:       StateWaitingTitle,
		PhotoFileID: photo.FileID,
		Width:       photo.Width,
		Height:      photo.Height,
		Caption:     caption,
		MessageID:   update.Message.ID,
	}

	// 询问用户
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		// 修改点：文案更新为 /title 和 /no
		Text:   fmt.Sprintf("📩 收到图片了,Daishiki喵~🐱！\n\n当前标题：\n%s\n\n🐱主人要自定义标题吗,喵？\n1️🐱和我说 `/title 就可以使用新标题了喵`\n2️⃣ 🐱说 `/no` 那就只能使用原标题的说,喵", caption),
		ReplyParameters: &models.ReplyParameters{
			MessageID: update.Message.ID,
		},
        // 注意：这里不要加 ParseModeMarkdown，防止原标题里有特殊字符报错
	})

}

func (h *BotHandler) handleTextReply(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}
	userID := update.Message.From.ID
	session, exists := h.Sessions[userID]

	// 如果该用户没有正在进行的会话，直接忽略
	if !exists || session.State == StateNone {
		return
	}

	text := update.Message.Text

	// 状态机判断
	switch session.State {

	// 阶段 1: 确认标题
	case StateWaitingTitle:
        // 修改点：这里把判断条件改成 "/no"
		if text == "/no" {
			// 使用默认标题，不做修改
		} else if len(text) > 7 && text[:7] == "/title " {
			// 提取 /title 后面的内容
			session.Caption = text[7:]
		} else {
			// 格式错误，拦截并提示
            // 修改点：提示语也改了
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID: update.Message.Chat.ID,
				Text:   "⚠️ 格式错误,喵~！\n- 确认原标题请回复 `/no`喵~\n- 自定义标题请回复 `/title 新标题`喵~",
				ParseMode: models.ParseModeMarkdown,
			})
			return
		}

		// 更新状态 -> 等待选标签
		session.State = StateWaitingTag

		// 发送 Inline 按钮供选择
		kb := &models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{
				{
					{Text: "🟢 TGC-SFW", CallbackData: "tag_sfw"},
					{Text: "🔞 TGC-NSFW", CallbackData: "tag_nsfw"},
				},
			},
		}

		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      update.Message.Chat.ID,
			Text:        fmt.Sprintf("✅ 狗修金,标题确认好了喵~: `%s`\n请主人狠狠点击下方按钮选择标签,打上只属于主人的标记吧。：", session.Caption),
			ParseMode:   models.ParseModeMarkdown,
			ReplyMarkup: kb,
		})
        
        // 注意：这里删除了 case StateWaitingTag 的逻辑，因为这部分逻辑移到 callback 处理了
	}
}

// ✅ 处理 Inline 按钮点击的回调
func (h *BotHandler) handleCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	// 必须回答 CallbackQuery
	defer b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: update.CallbackQuery.ID,
	})

	userID := update.CallbackQuery.From.ID
	session, exists := h.Sessions[userID]
	
	if !exists || session.State != StateWaitingTag {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: userID, 
			Text:   "⚠️ 哎哟,会话已过期，请重新转发图片,喵~。",
		})
		return
	}

	data := update.CallbackQuery.Data
	tag := ""
	if data == "tag_sfw" {
		tag = "#TGC #SFW"
	} else if data == "tag_nsfw" {
		tag = "#TGC #NSFW #R18"
	} else {
		return
	}

    // ✅ 针对 InaccessibleMessage 的修正
    // 直接访问结构体字段
    chatID := update.CallbackQuery.Message.Chat.ID
    messageID := update.CallbackQuery.Message.MessageID

	// 1. 发送到频道
	msg, err := b.SendPhoto(ctx, &bot.SendPhotoParams{
		ChatID:  h.Cfg.ChannelID,
		Photo:   &models.InputFileString{Data: session.PhotoFileID},
		Caption: fmt.Sprintf("%s\nTags: %s", session.Caption, tag),
	})

	if err != nil {
		b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: "❌ 发送失败: " + err.Error()})
		return
	}

	// 2. 存入 D1
	postID := fmt.Sprintf("manual_%d", msg.ID)
	finalFileID := msg.Photo[len(msg.Photo)-1].FileID
	err = h.DB.SaveImage(postID, finalFileID, session.Caption, tag, "manual", session.Width, session.Height)

	// 3. 反馈
	resultText := "上传成功，喵~ 🐱"
	if err != nil {
		resultText = "图片已发，但数据库保存失败。"
	}

    // 4. 编辑消息
    b.EditMessageText(ctx, &bot.EditMessageTextParams{
        ChatID:    chatID,
        MessageID: messageID, 
        Text:      resultText,
    })

	// 清除会话
	delete(h.Sessions, userID)
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
