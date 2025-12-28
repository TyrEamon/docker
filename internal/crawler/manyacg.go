package crawler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"my-bot-go/internal/config"
	"my-bot-go/internal/database"
	"my-bot-go/internal/manyacg"
	"my-bot-go/internal/telegram"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

// ManyACGResponse 对应 https://manyacg.top/api/v1/artwork/random 的返回结构
type ManyACGResponse struct {
	Data []struct {
		ID       string `json:"id"` 
		Title    string `json:"title"`
		Artist   struct {
			Name string `json:"name"`
		} `json:"artist"`
		Pictures []struct {
			ID     string `json:"id"`
			Regular string `json:"regular"`
			Width   int    `json:"width"` 
			Height  int    `json:"height"` 
			Index   int    `json:"index"`
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
			log.Println("🎲 Starting Batch ManyACG (10 Pics)...")

			//  批量抽 10 次
			for i := 0; i < 10; i++ {
				url := "https://manyacg.top/api/v1/artwork/random"

				resp, err := client.R().Get(url)
				if err != nil {
					log.Printf("ManyACG API Error: %v", err)
					continue
				}

				var result ManyACGResponse
				if err := json.Unmarshal(resp.Body(), &result); err != nil {
					log.Printf("ManyACG JSON Error: %v", err)
					continue
				}

                for _, item := range result.Data {
                    // 1) 先检查第一张图（p0）是否存在，避免重复整个图集
                    firstPid := fmt.Sprintf("mtcacg_%s_p0", item.ID)

                    if db.CheckExists(firstPid) {
                        log.Printf("♻️ MtcACG random skip (already in mtcacg_all): %s [p0 exists]", item.ID)
                        continue
                    }

                    if len(item.Pictures) == 0 {
                        continue
                    }

                    // 2) 遍历所有子图
                    for _, pic := range item.Pictures {
                        // 构造每张子图的 pid: mtcacg_{artworkID}_p{index}
                        pid := fmt.Sprintf("mtcacg_%s_p%d", item.ID, pic.Index)

                        // 3) 单张子图去重检查
                        if db.CheckExists(pid) {
                            log.Printf("♻️ MtcACG random skip duplicate: %s", pid)
                            continue
                        }

                        imgData, err := manyacg.DownloadOriginal(ctx, pic.ID)
                        if err != nil || len(imgData) == 0 {
                            log.Printf("❌ MtcACGR original failed: %v (picID=%s)", err, pic.ID)
                            continue
                        }

                        // 直接从 JSON 获取宽高
                        width := pic.Width
                        height := pic.Height

                        // 1. 截断 tags（避免 caption 太长）
                        maxTags := 20
                        tags := item.Tags
                        if item.R18 {
                            tags = append(tags, "R-18")
                        }
                        if len(tags) > maxTags {
                            tags = tags[:maxTags]
                        }


                         // 2. 压缩图片尺寸（避免 Telegram 尺寸超限）
                        maxSize := 4000
                        if width > maxSize || height > maxSize {
							longest := width
                            if height > longest {
                               longest = height
                            }
                            scale := float64(maxSize) / float64(longest)
                            width = int(float64(width) * scale)
                            height = int(float64(height) * scale)
                            }


                        log.Printf("⬇️ MtcACG random [%s] P%d (%dx%d, pid=%s)", item.Title, pic.Index, width, height, pid)

                        tagsStr := strings.Join(tags, " ")
                        hashTags := ""
                        if len(tags) > 0 {
                            hashTags = "#" + strings.Join(tags, " #")
                        }

                        caption := fmt.Sprintf(
                            "MtcACG: %s [P%d/%d]\nArtist: %s\nTags: %s",
                            item.Title,
                            pic.Index+1, len(item.Pictures),
                            item.Artist.Name,
                            hashTags,
                        )

                        botHandler.ProcessAndSend(ctx, imgData, pid, tagsStr, caption, item.Artist.Name, "mtcacg", width, height)
                        db.History[pid] = true
                        db.PushHistory()

                        time.Sleep(8 * time.Second)
                    }
                }


			            time.Sleep(3 * time.Second)
			    }

			log.Println("😴 ManyACG Batch Done. Sleeping 37m...")
			time.Sleep(37 * time.Minute)
		}
	}
}
