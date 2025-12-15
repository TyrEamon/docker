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
	Filename  string   `json:"filename"`
	Tags      []string `json:"tags"`
	Width     int      `json:"width"`
	Height    int      `json:"height"`
	Platform  string   `json:"platform"` // 你的 API JSON 里其实有这个字段，虽然你可能没用到
}

func StartCosineTag(ctx context.Context, cfg *config.Config, db *database.D1Client, botHandler *telegram.BotHandler) {
	// 🚀 使用配置中的 Tags，如果为空则直接退出，避免空跑
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
			for _, tag := range cfg.CosineTags {
				log.Printf("🏷️  Scanning Tag: %s", tag)
				
				processedCount := 0
				start := 0
				limit := 32

				// 循环每一页
				for processedCount < cfg.CosineLimitPerTag {
					// 注意：tag 需要 URL 编码
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

						// 构造去重 Key (去除后缀)
						// 注意：有些 filename 本身没后缀，需要防范
						dbKey := img.Filename
						if idx := strings.LastIndex(img.Filename, "."); idx != -1 {
							dbKey = img.Filename[:idx]
						}
						
						// 加上前缀以兼容旧系统
						if !strings.HasPrefix(dbKey, "pixiv_") {
                             dbKey = "pixiv_" + dbKey
                        }

						if db.History[dbKey] {
							continue
						}

						// ================= 下载逻辑开始 =================
						
						var imgData []byte
						var finalExt string = ".jpg" // 默认给个 jpg 后缀，防止 wav 干扰

						// 1. 优先尝试 Pixiv 原链
						downloadURL := img.RawURL
						if downloadURL == "" {
							downloadURL = img.ThumbURL
						}

						// 修正 extension (如果 API 给的是 png/jpg)
						if img.Extension != "" {
							finalExt = "." + img.Extension
						}

						log.Printf("⬇️  Downloading: %s (%s)", img.Title, dbKey)

						dlHeaders := indexHeaders
						if strings.Contains(downloadURL, "pximg.net") {
							dlHeaders = pixivHeaders
						}

						imgResp, err := client.R().SetHeaders(dlHeaders).Get(downloadURL)
						
						success := (err == nil && imgResp.StatusCode() == 200)

						// 2. 🚨 偷家战术：如果原链失败，尝试 Cosine 备份站
						if !success {
							log.Printf("⚠️ Primary Source Failed, trying Cosine Backup...")
							
							// 确定 platform 路径
							// 简单起见，如果 rawurl 包含 twitter，就用 twitter，否则 pixiv
							platformDir := "pixiv"
							if strings.Contains(img.RawURL, "twimg.com") || img.Platform == "twitter" {
								platformDir = "twitter"
							}

							backupBase := fmt.Sprintf("https://backblaze.cosine.ren/pic/origin/%s/", platformDir)
							
							// 策略 A: 原始文件名 (e.g. 120975361_p0.jpg)
							backupURL := backupBase + img.Filename
							log.Printf("🔄 Trying Backup A: %s", backupURL)
							imgResp, err = client.R().SetHeaders(indexHeaders).Get(backupURL)

							if err == nil && imgResp.StatusCode() == 200 {
								success = true
								// 如果备份站的图本身是 jpg，那就用原来的后缀
							} else {
								// 策略 B: 强制改 .webp (e.g. 120975361_p0.webp)
								// 很多图床会转存 webp
								nameNoExt := dbKey
								if strings.HasPrefix(nameNoExt, "pixiv_") {
									nameNoExt = strings.TrimPrefix(nameNoExt, "pixiv_")
								}
								// 注意：有些 Key 可能是 12345_p0，有些是 12345
								// 最保险是用 img.Filename 去掉后缀
								if idx := strings.LastIndex(img.Filename, "."); idx != -1 {
									nameNoExt = img.Filename[:idx]
								}

								backupURL = backupBase + nameNoExt + ".webp"
								log.Printf("🔄 Trying Backup B: %s", backupURL)
								imgResp, err = client.R().SetHeaders(indexHeaders).Get(backupURL)
								
								if err == nil && imgResp.StatusCode() == 200 {
									success = true
									finalExt = ".webp" // 这是一个 WebP
								}
							}
						}

						// 3. 最终检查
						if !success {
							log.Printf("❌ All sources failed for: %s, Skipping.", dbKey)
							continue
						}
						
						imgData = imgResp.Body()

						// ================= 下载逻辑结束 =================

						cleanTitle := strings.TrimSpace(img.Title)
						tagsStr := strings.Join(img.Tags, " #")
						caption := fmt.Sprintf("Title: %s\nArtist: %s\nTags: #%s\nSource: %s",
							cleanTitle, img.Author, tagsStr, "pic.cosine.ren")
						
						// 🛠️ 强制伪装文件名
						// 如果 finalExt 是 .webp，Telegram 可能会把它当 Sticker。
						// 如果它是 webp，我们可以尝试用 .jpg 后缀骗 TG，或者保留 .webp 看 TG 怎么处理。
						// 稳妥起见：对于 pixiv 图，通常都是 jpg/png。
						// 即使下载下来是 webp 数据，把文件名改成 xxx.jpg 发给 TG，TG 也许能识别。
						// 但如果不行，就老老实实传 .webp。
						// 这里的关键是：绝对不能传 .wav！
						
						// 构造发给 TG 的文件名 ID
						// 假设 ProcessAndSend 会用这个 sendID 当作文件名
						// 我们强制加一个图片后缀
						sendID := dbKey + finalExt

						// 发送
						botHandler.ProcessAndSend(ctx, imgData, sendID, strings.Join(img.Tags, " "), caption, "pixiv", img.Width, img.Height)
                        
                        // 成功逻辑
                        db.History[dbKey] = true
                        db.PushHistory()
                        processedCount++
                        time.Sleep(4 * time.Second)
					}
					
					start += limit
					time.Sleep(2 * time.Second)
				}
			}

			log.Println("😴 Cosine Crawler Cycle Done. Sleeping 4 hours...")
			time.Sleep(4 * time.Hour)
		}
	}
}
