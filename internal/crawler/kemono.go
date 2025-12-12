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

	// 1. 初始化 Client，添加仿真浏览器 Header
	client := resty.New().
		SetTimeout(60 * time.Second).
		SetRetryCount(3).
		SetHeader("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36").
		SetHeader("Accept", "application/json, text/plain, */*").
		SetHeader("Accept-Language", "en-US,en;q=0.9").
		SetHeader("Referer", "https://kemono.su/") // 使用 .su 或 .cr

	// 如果配置中有 Cookie，可以在这里加上 (需要在 config.go 添加字段)
	// if cfg.KemonoCookie != "" {
	// 	client.SetHeader("Cookie", cfg.KemonoCookie)
	// }

	for {
		select {
		case <-ctx.Done():
			return
		default:
			log.Println("🧩 Checking Kemono...")
			hasNew := false

			for _, creator := range cfg.KemonoCreators {
				service := strings.TrimSpace(creator.Service)
				for _, rawUID := range creator.UserIDs {
					uid := strings.TrimSpace(rawUID)
					if uid == "" {
						continue
					}

					// 注意 API 域名可能变动，目前常用 .su 或 .cr
					listURL := fmt.Sprintf("https://kemono.su/api/v1/%s/user/%s/posts", service, uid)
					resp, err := client.R().Get(listURL)
					if err != nil {
						log.Printf("⚠️ Kemono list error (%s/%s): %v", service, uid, err)
						continue
					}

					// 如果返回 HTML (被 CF 拦截)，Unmarshal 会报错
					if strings.HasPrefix(strings.TrimSpace(string(resp.Body())), "<") {
						log.Printf("⚠️ Kemono blocked by Cloudflare (received HTML instead of JSON)")
						time.Sleep(5 * time.Second)
						continue
					}

					var posts []struct {
						ID string `json:"id"`
					}
					if err := json.Unmarshal(resp.Body(), &posts); err != nil {
						log.Printf("⚠️ Kemono list JSON error: %v (Body start: %s)", err, string(resp.Body())[:50])
						continue
					}

					// 最新的在前面，一次只抓前 N 个防止刷屏
					maxPosts := 5
					for i, p := range posts {
						if i >= maxPosts {
							break
						}
						pid := fmt.Sprintf("kemono_%s_%s_%s", service, uid, p.ID)
						if db.History[pid] {
							continue
						}
						if err := fetchKemonoPost(ctx, client, service, uid, p.ID, pid, db, botHandler); err == nil {
							hasNew = true
						}
						time.Sleep(5 * time.Second) // 增加间隔，减少被封概率
					}
				}
			}

			// db.PushHistory() 已移除，因为 SaveImage 实时写入数据库

			if hasNew {
				log.Println("😴 Kemono Batch Done.")
			}
			log.Println("😴 Kemono Sleeping 10m...")
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
	apiURL := fmt.Sprintf("https://kemono.su/api/v1/%s/user/%s/post/%s", service, uid, postID)
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

		server := cdnMap[att.Path]
		if server == "" {
			// 备用服务器，有时是 .cr 有时是 .su
			server = "https://n4.kemono.su"
		}
		imgURL := server + "/data" + att.Path

		log.Printf("⬇️ Downloading Kemono: %s", imgURL)
		imgResp, err := client.R().SetContext(ctx).Get(imgURL)
		if err != nil || imgResp.StatusCode() != 200 {
			log.Printf("❌ Kemono image error: %v (Status: %d)", err, imgResp.StatusCode())
			continue
		}
		data := imgResp.Body()

		// ✨ 方案 A：解码宽高
		width, height := 0, 0
		if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
			width, height = cfg.Width, cfg.Height
		}

		subPID := fmt.Sprintf("%s_p%d", basePID, idx)
		if db.History[subPID] {
			continue
		}

		caption := fmt.Sprintf("Kemono: %s\nService: %s\nUser: %s\nPost: %s",
			kResp.Post.Title, kResp.Post.Service, kResp.Post.User, kResp.Post.ID)
		tagsStr := strings.Join(kResp.Post.Tags, " ")

		botHandler.ProcessAndSend(ctx, data, subPID, tagsStr, caption, "kemono", width, height)
	}

	return nil
}
