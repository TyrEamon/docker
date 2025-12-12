package crawler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"my-bot-go/internal/config"
	"my-bot-go/internal/database"
	"my-bot-go/internal/telegram"
	"path"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

type KemonoPostResp struct {
	Post struct {
		ID          string   `json:"id"`
		User        string   `json:"user"`
		Service     string   `json:"service"`
		Title       string   `json:"title"`
		Content     string   `json:"content"`
		Tags        []string `json:"tags"`
		Attachments []struct {
			Path string `json:"path"`
			Name string `json:"name"`
		} `json:"attachments"`
	} `json:"post"`
	Previews []struct {
		Type   string `json:"type"`   // "thumbnail"
		Server string `json:"server"` // e.g. "https://n4.kemono.cr"
		Path   string `json:"path"`   // same as attachment.Path
	} `json:"previews"`
}

func StartKemono(ctx context.Context, cfg *config.Config, db *database.D1Client, botHandler *telegram.BotHandler) {
	if len(cfg.KemonoCreators) == 0 {
		log.Println("Kemono disabled (no creators configured)")
		return
	}

	client := resty.New().
		SetTimeout(60 * time.Second).
		SetRetryCount(3)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			log.Println("🧩 Checking Kemono...")
			// 移除 hasNew 变量，改为即时保存

			for _, creator := range cfg.KemonoCreators {
				service := strings.TrimSpace(creator.Service)
				for _, rawUID := range creator.UserIDs {
					uid := strings.TrimSpace(rawUID)
					if uid == "" {
						continue
					}

					listURL := fmt.Sprintf("https://kemono.cr/api/v1/%s/user/%s/posts", service, uid)
					resp, err := client.R().Get(listURL)
					if err != nil {
						log.Printf("⚠️ Kemono list error (%s/%s): %v", service, uid, err)
						continue
					}

					var posts []struct {
						ID string `json:"id"`
					}
					if err := json.Unmarshal(resp.Body(), &posts); err != nil {
						log.Printf("⚠️ Kemono list JSON error: %v", err)
						continue
					}

					// 最新的在前面，一次只抓前 N 个防止刷屏
					maxPosts := 5
					for i, p := range posts {
						if i >= maxPosts {
							break
						}
						pid := fmt.Sprintf("kemono_%s_%s_%s", service, uid, p.ID)
						// 粗略过滤，防止同一个 Post 反复进 fetchKemonoPost
						if db.History[pid] {
							continue
						}
						
						// 进入详情抓取
						err := fetchKemonoPost(ctx, client, service, uid, p.ID, pid, db, botHandler)
						if err != nil {
							log.Printf("❌ Failed to fetch post %s: %v", p.ID, err)
						} else {
							// 如果整个 Post 处理成功，把 Post ID 标记为已完成
							db.History[pid] = true
						}

						// ✅ 【关键修改】每处理完一个 Post，立刻推送到 D1
						db.PushHistory()
						
						time.Sleep(3 * time.Second)
					}
				}
			}

			// 循环结束后休息
			log.Println("😴 Kemono Done. Sleeping 10m...")
			time.Sleep(10 * time.Minute)
		}
	}
}

func fetchKemonoPost(
	ctx context.Context,
	client *resty.Client,
	service, uid, postID, basePID string,
	db *database.D1Client,
	botHandler *telegram.BotHandler,
) error {
	apiURL := fmt.Sprintf("https://kemono.cr/api/v1/%s/user/%s/post/%s", service, uid, postID)
	resp, err := client.R().SetContext(ctx).Get(apiURL)
	if err != nil {
		return err
	}

	var kResp KemonoPostResp
	if err := json.Unmarshal(resp.Body(), &kResp); err != nil {
		return err
	}

	// 构建 path -> server 映射
	cdnMap := make(map[string]string)
	for _, p := range kResp.Previews {
		if p.Type != "thumbnail" {
			continue
		}
		cdnMap[p.Path] = p.Server
	}

	// 下载每一张图
	for idx, att := range kResp.Post.Attachments {
		ext := strings.ToLower(path.Ext(att.Path))
		if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".webp" {
			continue
		}

		// 构建唯一的子图 ID
		subPID := fmt.Sprintf("%s_p%d", basePID, idx)
		
		// 检查子图是否发过（断点续传的关键）
		if db.History[subPID] {
			continue
		}

		server := cdnMap[att.Path]
		if server == "" {
			server = "https://n4.kemono.cr"
		}
		imgURL := server + "/data" + att.Path

		log.Printf("⬇️ Downloading Kemono: %s", imgURL)
		imgResp, err := client.R().SetContext(ctx).Get(imgURL)
		if err != nil || imgResp.StatusCode() != 200 {
			log.Printf("❌ Kemono image error: %v", err)
			continue
		}
		data := imgResp.Body()

		// 解码宽高
		width, height := 0, 0
		if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
			width, height = cfg.Width, cfg.Height
		}

		caption := fmt.Sprintf("Kemono: %s\nService: %s\nUser: %s\nPost: %s",
			kResp.Post.Title, kResp.Post.Service, kResp.Post.User, kResp.Post.ID)
		tagsStr := strings.Join(kResp.Post.Tags, " ")

		botHandler.ProcessAndSend(ctx, data, subPID, tagsStr, caption, "kemono", width, height)
		
		// ✅ 【关键修改】每张子图发完，立刻推送到 D1
		// 这样如果图片很多，下载到一半挂了，下次也不会重复发前几张
		db.PushHistory()
	}

	return nil
}
