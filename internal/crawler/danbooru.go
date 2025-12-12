package crawler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"my-bot-go/internal/config"
	"my-bot-go/internal/database"
	"my-bot-go/internal/telegram"
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
		SetTimeout(30 * time.Second).
		SetRetryCount(2)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			log.Println("🔍 Checking Danbooru...")

			// 构造查询 URL
			url := fmt.Sprintf(
				"https://danbooru.donmai.us/posts.json?limit=%d&tags=%s",
				cfg.DanbooruLimit,
				cfg.DanbooruTags,
			)

			resp, err := client.R().Get(url)
			if err != nil {
				log.Printf("Danbooru Error: %v", err)
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
					// 建议：如果你不想让日志一直刷 "skip"，可以把这些视频也加入 history 屏蔽掉
					// db.History[fmt.Sprintf("danbooru_%d", post.ID)] = true
					continue
				}

				pid := fmt.Sprintf("danbooru_%d", post.ID)
				if db.History[pid] {
					continue
				}

				// ⬇️ 这里补回了下载逻辑
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
					"danbooru",
					post.ImageWidth,
					post.ImageHeight,
				)

				// ✅ 【关键修正】每发完一张图，立刻同步到云端
				db.PushHistory()

				time.Sleep(3 * time.Second)
			}

			log.Println("😴 Danbooru Done. Sleeping 10m...")
			time.Sleep(10 * time.Minute)
		}
	}
}
