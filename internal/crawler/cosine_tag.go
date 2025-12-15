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
	Platform  string   `json:"platform"`
}

func StartCosineTag(ctx context.Context, cfg *config.Config, db *database.D1Client, botHandler *telegram.BotHandler) {
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

				for processedCount < cfg.CosineLimitPerTag {
					apiURL := "https://pic.cosine.ren/api/tag"
					resp, err := client.R().
						SetHeaders(indexHeaders).
						SetQueryParams(map[string]string{
							"tag":   tag,
							"start": fmt.Sprintf("%d", start),
							"limit": fmt.Sprintf("%d", limit),
						}).Get(apiURL)

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

						// ================= ID 生成修正逻辑 =================
						// 强制使用标准格式：pixiv_{PID}_p{Page}
						pidStr := img.PID
						pagePart := "_p0" // 默认为 p0
						
						// 尝试从文件名解析 _p1, _p2 等
						if strings.Contains(img.Filename, "_p") {
							start := strings.LastIndex(img.Filename, "_p")
							if start != -1 {
								rest := img.Filename[start:]
								if dot := strings.Index(rest, "."); dot != -1 {
									pagePart = rest[:dot]
								} else {
									pagePart = rest
								}
							}
						}

						// 构造标准 DB Key (无后缀)
						dbKey := fmt.Sprintf("pixiv_%s%s", pidStr, pagePart)

						// 🛡️ 超级去重防御 (同时查带后缀和不带后缀的)
						if db.History[dbKey] || 
						   db.History[dbKey+".jpg"] || 
						   db.History[dbKey+".png"] || 
						   db.History[dbKey+".webp"] {
							// log.Printf("♻️ Skip %s (Already in DB)", dbKey)
							continue
						}

						// ================= 下载逻辑 =================
						
						var imgData []byte
						var finalExt string = ".jpg"

						// 1. 优先尝试 Pixiv 原链
						downloadURL := img.RawURL
						if downloadURL == "" {
							downloadURL = img.ThumbURL
						}

						// 修正 extension
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

						// 2. 🚨 备用方案 (偷家战术)
						if !success {
							log.Printf("⚠️ Primary Source Failed, trying Cosine Backup...")
							
							platformDir := "pixiv"
							if strings.Contains(img.RawURL, "twimg.com") || img.Platform == "twitter" {
								platformDir = "twitter"
							}

							backupBase := fmt.Sprintf("https://backblaze.cosine.ren/pic/origin/%s/", platformDir)
							
							// 策略 A: 原始文件名
							backupURL := backupBase + img.Filename
							log.Printf("🔄 Trying Backup A: %s", backupURL)
							imgResp, err = client.R().SetHeaders(indexHeaders).Get(backupURL)

							if err == nil && imgResp.StatusCode() == 200 {
								success = true
							} else {
								// 策略 B: 强制 .webp
								nameNoExt := img.Filename
								if idx := strings.LastIndex(img.Filename, "."); idx != -1 {
									nameNoExt = img.Filename[:idx]
								}
								backupURL = backupBase + nameNoExt + ".webp"
								log.Printf("🔄 Trying Backup B: %s", backupURL)
								imgResp, err = client.R().SetHeaders(indexHeaders).Get(backupURL)
								
								if err == nil && imgResp.StatusCode() == 200 {
									success = true
									finalExt = ".webp"
								}
							}
						}

						if !success {
							log.Printf("❌ All sources failed for: %s, Skipping.", dbKey)
							continue
						}
						
						imgData = imgResp.Body()

						// ================= 发送与存储 =================

						cleanTitle := strings.TrimSpace(img.Title)
						tagsStr := strings.Join(img.Tags, " #")
						caption := fmt.Sprintf("Title: %s\nArtist: %s\nTags: #%s\nSource: %s",
							cleanTitle, img.Author, tagsStr, "pic.cosine.ren")
						
						// 构造发给 TG 的文件名 (必须带后缀，骗过 TG)
						sendID := dbKey + finalExt

						// 发送
						botHandler.ProcessAndSend(ctx, imgData, sendID, strings.Join(img.Tags, " "), caption, "pixiv", img.Width, img.Height)
                        
                        // 存库 (存标准 Key，无后缀)
                        // 注意：这里显式调用 PushHistory，防止 ProcessAndSend 没存对
                        db.History[dbKey] = true
                        db.PushHistory()
                        
                        processedCount++
                        time.Sleep(4 * time.Second)
					}
					
					start += limit
					time.Sleep(2 * time.Second)
				}
			}

			log.Println("😴 Cosine Crawler Cycle Done. Sleeping 2 hours...")
			time.Sleep(2 * time.Hour)
		}
	}
}
