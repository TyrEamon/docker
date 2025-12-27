c, update *models.Update) {
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
			ParseMode: models.ParseModeMarkdown,
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
