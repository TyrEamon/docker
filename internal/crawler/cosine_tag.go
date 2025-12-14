package crawler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"my-bot-go/internal/config"
	"my-bot-go/internal/database"
	"my-bot-go/internal/telegram"

	"github.com/go-resty/resty/v2"
)

// CosineImage 对应 pic.cosine.ren API 返回的单个图片结构
type CosineImage struct {
	ID        int      `json:"id"`
	PID       string   `json:"pid"`
	Title     string   `json:"title"`
	Author    string   `json:"author"`
	RawURL    string   `json:"rawurl"`
	ThumbURL  string   `json:"thumburl"`
	Extension string   `json:"extension"`
	Filename  string   `json:"filename"` // ✅ 已补全
	Tags      []string `json:"tags"`
	Width     int      `json:"width"`
	Height    int      `json:"height"`
}

func StartCosineTag(ctx context.Context, cfg *config.Config, db *database.D1Client, botHandler *telegram.BotHandler) {
	// ===============================================
	// ❌ 这里原本是你硬编码的区域，已经不需要了，直接删掉！
	// ===============================================

	// 🚀 使用配置中的 Tags，如果为空则直接退出，避免空跑
	// 这里的 cfg.CosineTags 就是从环境变量 COSINE_TAGS 里读出来的
	if len(cfg.CosineTags) == 0 {
		log.Printf("⚠️ No CosineTags configured. Skipping Cosine Crawler.")
		return
	}

	client := resty.New()
	client.SetTimeout(30 * time.Second)

	indexHeaders := map[string]string{
		"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
		"Referer":    "https://pic.cosine.ren/",
	}

	pixivHeaders := map[string]string{
		"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
		"Referer":    "https://www.pixiv.net/",
	}

	log.Println("🚀 Starting Cosine Tag Crawler...")
	log.Printf("🎯 Target Tags: %v", cfg.CosineTags)
	log.Printf("📊 Limit Per Tag: %d", cfg.CosineLimitPerTag)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			// ✅ 这里改成了 range cfg.CosineTags
			for _, tag := range cfg.CosineTags {
				log.Printf("🏷️  Scanning Tag: %s", tag)
				
				processedCount := 0
				start := 0
				limit := 32

				// ✅ 这里改成了 cfg.CosineLimitPerTag
				for processedCount < cfg.CosineLimitPerTag {
					apiURL := "https://pic.cosine.ren/api/tag"

					resp, err := client.R().
						SetHeaders(indexHeaders).
						SetQueryParams(map[string]string{
							"tag":   tag,
							"start": fmt.Sprintf("%d", start),
							"limit": fmt.Sprintf("%d", limit),
						}).
						Get(apiURL)

					if err != nil || resp.StatusCode() != 200 {
						log.Printf("❌ API Request Failed for tag %s: %v", tag, err)
						break
					}

					var images []CosineImage
					if err := json.Unmarshal(resp.Body(), &images); err != nil {
						log.Printf("❌ JSON Unmarshal Failed: %v", err)
						break
					}

					if len(images) == 0 {
						log.Println("🏁 No more images for this tag.")
						break
					}

					log.Printf("📄 Fetched %d images (start=%d)", len(images), start)

					for _, img := range images {
						if processedCount >= cfg.CosineLimitPerTag {
							break
						}

						// 构造去重 Key
						dbKey := strings.TrimSuffix(img.Filename, "." + img.Extension) 
						if !strings.HasPrefix(dbKey, "pixiv_") {
                             dbKey = "pixiv_" + dbKey
                        }

						if db.CheckExists(dbKey) {
							continue
						}

						downloadURL := img.RawURL
						if downloadURL == "" {
							downloadURL = img.ThumbURL
						}

						log.Printf("⬇️  Downloading: %s (%s)", img.Title, dbKey)

						dlHeaders := indexHeaders
						if strings.Contains(downloadURL, "pximg.net") {
							dlHeaders = pixivHeaders
						}

						imgResp, err := client.R().
							SetHeaders(dlHeaders).
							Get(downloadURL)

						if err != nil || imgResp.StatusCode() != 200 {
							log.Printf("⚠️  Download Failed: %s", downloadURL)
							continue
						}

						cleanTitle := strings.TrimSpace(img.Title)
						tagsStr := strings.Join(img.Tags, " #")
						caption := fmt.Sprintf("Title: %s\nArtist: %s\nTags: #%s\nSource: %s",
							cleanTitle, img.Author, tagsStr, "pic.cosine.ren")

						// 发送
						// 直接调用，不接收返回值
                        botHandler.ProcessAndSend(ctx, imgResp.Body(), dbKey, strings.Join(img.Tags, " "), caption, "pixiv", img.Width, img.Height)
                        
                         // ✅ 直接执行成功逻辑（删掉了 else 分支）
                        db.History[dbKey] = true
                        db.PushHistory()
                        processedCount++
                        time.Sleep(4 * time.Second) // 建议稍微慢点
					}
					
					start += limit
					time.Sleep(2 * time.Second)
				}
			}

			// 爬完一轮休息 4 小时
			log.Println("😴 Cosine Crawler Cycle Done. Sleeping 2 h...")
			time.Sleep(2 * time.Hour)
		}
	}
}
