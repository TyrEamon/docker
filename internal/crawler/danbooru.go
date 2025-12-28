package crawler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"my-bot-go/internal/config"
	"my-bot-go/internal/database"
	"my-bot-go/internal/telegram"
	"net/url" // ✅ 必须加这个包
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

// DanbooruPost 对应 /posts.json 返回的字段
type DanbooruPost struct {
	ID           int    `json:"id"`
	ImageWidth   int    `json:"image_width"`
	ImageHeight  int    `json:"image_height"`
	TagString    string `json:"tag_string"`
	FileURL      string `json:"file_url"`
	LargeFileURL string `json:"large_file_url"`
	FileExt      string `json:"file_ext"` // jpg, png, mp4, webm...
}

// StartDanbooru 自动按标签巡逻 Danbooru
func StartDanbooru(ctx context.Context, cfg *config.Config, db *database.D1Client, botHandler *telegram.BotHandler) {
	if cfg.DanbooruTags == "" || cfg.DanbooruLimit <= 0 {
		log.Println("Danbooru disabled (no tags or limit).")
		return
	}

	client := resty.New().
		SetTimeout(60 * time.Second). // 超时设长一点
		SetRetryCount(2)

	// ✅ 使用 Config 中的配置进行认证
	if cfg.DanbooruUsername != "" && cfg.DanbooruAPIKey != "" {
		client.SetBasicAuth(cfg.DanbooruUsername, cfg.DanbooruAPIKey)
		log.Println("🔑 Danbooru API Key enabled")
	} else {
		log.Println("⚠️ Danbooru API Key missing (Cloudflare might block requests)")
	}
	
	// 设置 User-Agent 和 Accept 头
	client.SetHeader("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	client.SetHeader("Accept", "application/json")

	for {
		select {
		case <-ctx.Done():
			return
		default:
			log.Println("🔍 Checking Danbooru...")

			// ✅ 关键修正：对 Tags 进行 URL 编码，防止空格导致 URL 断裂
			encodedTags := url.QueryEscape(cfg.DanbooruTags)

			// 构造查询 URL
			targetURL := fmt.Sprintf(
				"https://danbooru.donmai.us/posts.json?limit=%d&tags=%s",
				cfg.DanbooruLimit,
				encodedTags,
			)

			resp, err := client.R().Get(targetURL)
			if err != nil {
				log.Printf("Danbooru Error: %v", err)
				time.Sleep(1 * time.Minute)
				continue
			}

			// 如果遇到非 200 状态码 (比如 403 Forbidden)，打印 Body 方便调试
			if resp.StatusCode() != 200 {
				log.Printf("⚠️ Danbooru API Status: %d | Body: %s", resp.StatusCode(), string(resp.Body()))
				time.Sleep(1 * time.Minute)
				continue
			}

			var posts []DanbooruPost
			if err := json.Unmarshal(resp.Body(), &posts); err != nil {
				log.Printf("Danbooru JSON Error: %v", err)
				time.Sleep(1 * time.Minute)
				continue
			}

			for _, post := range posts {
				// 跳过无图 / 视频 / zip 等
				if post.FileURL == "" || post.LargeFileURL == "" {
					continue
				}
				ext := strings.ToLower(post.FileExt)
				if ext == "mp4" || ext == "webm" || ext == "zip" || ext == "swf" {
					continue
				}

				pid := fmt.Sprintf("danbooru_%d", post.ID)
				if db.History[pid] {
					continue
				}

				// 下载图片
				imgURL := post.FileURL
				log.Printf("⬇️ Downloading Danbooru: %d", post.ID)

				imgResp, err := client.R().Get(imgURL)
				if err != nil || imgResp.StatusCode() != 200 {
					log.Printf("Danbooru download error: %v", err)
					continue
				}

				tagsStr := post.TagString
				caption := fmt.Sprintf(
					"Danbooru: %d\nTags: #%s",
					post.ID,
					strings.ReplaceAll(tagsStr, " ", " #"),
				)

				// 发送
				botHandler.ProcessAndSend(
					ctx,
					imgResp.Body(),
					pid,
					tagsStr,
					caption,
					"",
					"danbooru",
					post.ImageWidth,
					post.ImageHeight,
				)

				// 每发完一张图，立刻同步到云端
				db.PushHistory()

				time.Sleep(3 * time.Second)
			}

			log.Println("😴 Danbooru Done. Sleeping 10m...")
			time.Sleep(60 * time.Minute)
		}
	}
}
