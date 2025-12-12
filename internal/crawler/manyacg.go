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
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

// ManyACGResponse 对应 https://manyacg.top/api/v1/artwork/random 的返回结构
type ManyACGResponse struct {
	Data []struct {
		// ✅ 修正：ID 改为 string 类型，因为 API 返回的是 "67838d..." 这种字符串
		ID       string `json:"id"` 
		Title    string `json:"title"`
		Artist   struct {
			Name string `json:"name"`
		} `json:"artist"`
		Pictures []struct {
			Regular string `json:"regular"`
		} `json:"pictures"`
		Tags []string `json:"tags"`
		R18  bool     `json:"r18"`
	} `json:"data"`
}

func StartManyACG(ctx context.Context, cfg *config.Config, db *database.D1Client, botHandler *telegram.BotHandler) {
	client := resty.New()
	client.SetTimeout(60 * time.Second)
	client.SetRetryCount(3)
	client.SetHeader("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0")

	for {
		select {
		case <-ctx.Done():
			return
		default:
			log.Println("🎲 Checking ManyACG (Random)...")

			url := "https://manyacg.top/api/v1/artwork/random"

			resp, err := client.R().Get(url)
			if err != nil {
				log.Printf("ManyACG API Error: %v", err)
				time.Sleep(3 * time.Minute)
				continue
			}

			var result ManyACGResponse
			if err := json.Unmarshal(resp.Body(), &result); err != nil {
				log.Printf("ManyACG JSON Error: %v", err)
				time.Sleep(1 * time.Minute)
				continue
			}

			for _, item := range result.Data {
				// ✅ 修正：因为 ID 是 string，这里格式化用 %s
				pid := fmt.Sprintf("manyacg_%s", item.ID)

				if db.History[pid] {
					// ✅ 修正：日志里 ID 也是 string
					log.Printf("⏭️ ManyACG %s 已存在，跳过", item.ID)
					continue
				}

				if len(item.Pictures) == 0 {
					continue
				}
				imgURL := item.Pictures[0].Regular

				// ✅ 修正：日志里 ID 也是 string
				log.Printf("⬇️ Downloading ManyACG: %s", item.ID)

				imgResp, err := client.R().Get(imgURL)
				if err != nil {
					log.Printf("Failed to download image: %v", err)
					continue
				}

				width, height := 0, 0
				if cfg, _, err := image.DecodeConfig(bytes.NewReader(imgResp.Body())); err == nil {
					width = cfg.Width
					height = cfg.Height
				} else {
					// ✅ 修正：日志里 ID 也是 string
					log.Printf("⚠️ 无法解析图片宽高 (ID: %s): %v", item.ID, err)
				}

				tags := item.Tags
				if item.R18 {
					tags = append(tags, "R-18")
				}
				tagsStr := strings.Join(tags, " ")
				formattedTags := strings.ReplaceAll(tagsStr, " ", " #")

				caption := fmt.Sprintf("MtcACG: %s\nArtist: %s\nTags: #%s",
					item.Title,
					item.Artist.Name,
					formattedTags,
				)

				botHandler.ProcessAndSend(ctx, imgResp.Body(), pid, tagsStr, caption, "manyacg", width, height)

				db.PushHistory()

				time.Sleep(3 * time.Second)
			}

			log.Println("😴 ManyACG Done. Sleeping 5m...")
			time.Sleep(5 * time.Minute)
		}
	}
}
