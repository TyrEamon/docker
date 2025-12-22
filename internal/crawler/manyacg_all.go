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
	"my-bot-go/internal/manyacg"
	"my-bot-go/internal/telegram"

	"github.com/go-resty/resty/v2"
)

// ManyACGArtwork 对应 /v1/artwork/list 的单条数据
type ManyACGArtwork struct {
	ID        string   `json:"id"`
	CreatedAt string   `json:"created_at"`
	Title     string   `json:"title"`
	Desc      string   `json:"description"`
	SourceURL string   `json:"source_url"`
	R18       bool     `json:"r18"`
	LikeCount int      `json:"like_count"`
	Tags      []string `json:"tags"`
	Artist    struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Type     string `json:"type"`
		UID      string `json:"uid"`
		Username string `json:"username"`
	} `json:"artist"`
	SourceType string `json:"source_type"`
	Pictures   []struct {
		ID       string `json:"id"`
		Width    int    `json:"width"`
		Height   int    `json:"height"`
		Index    int    `json:"index"`
		FileName string `json:"file_name"`
		Hash      string `json:"hash"`
		ThumbHash string `json:"thumb_hash"`
		Thumbnail string `json:"thumbnail"`
		Regular   string `json:"regular"`
	} `json:"pictures"`
}

type ManyACGListResp struct {
	Status  int              `json:"status"`
	Message string           `json:"message"`
	Data    []ManyACGArtwork `json:"data"`
}

// StartManyACGAll 通过 /v1/artwork/list 按页爬 ManyACG 全站
func StartManyACGAll(ctx context.Context, cfg *config.Config, db *database.D1Client, botHandler *telegram.BotHandler) {
	client := resty.New()
	client.SetTimeout(60 * time.Second)
	client.SetHeader("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	// 起始页，可以以后做成配置
	page := 1

	// r18 参数：0=非R18，1=R18，2=全部
	r18Param := "2"
	if cfg.ManyACGR18Mode != "" {
		r18Param = cfg.ManyACGR18Mode
	}

	log.Println("🚀 Starting MtcACG All Crawler...")

	for {
		select {
		case <-ctx.Done():
			return

		default:
			log.Printf("📜 MtcACG list page=%d, r18=%s ...", page, r18Param)

			apiURL := "https://api.manyacg.top/v1/artwork/list"
			resp, err := client.R().
				SetQueryParams(map[string]string{
					"page":      fmt.Sprintf("%d", page),
					"page_size": "20",
					"r18":       r18Param,
					"limit":     "20",
				}).
				Get(apiURL)

			if err != nil || resp.StatusCode() != 200 {
				log.Printf("❌ ManyACG list error: %v (status=%d)", err, resp.StatusCode())
				time.Sleep(15 * time.Second)
				continue
			}

			var list ManyACGListResp
			if err := json.Unmarshal(resp.Body(), &list); err != nil {
				log.Printf("❌ ManyACG list JSON error: %v", err)
				time.Sleep(15 * time.Second)
				continue
			}

			if len(list.Data) == 0 {
				log.Printf("🏁 ManyACG page %d has no data, reset to page=1 and sleep 30m...", page)
				page = 1
				time.Sleep(30 * time.Minute)
				continue
			}

			for _, aw := range list.Data {
				if len(aw.Pictures) == 0 {
					continue
				}

				maxPages := len(aw.Pictures)
				if maxPages > 50 {
					maxPages = 50
				}

				for _, pic := range aw.Pictures {
					if pic.Index >= maxPages {
						continue
					}

					// 1) 唯一 PID: mtcacg_{artworkID}_p{index}
					pid := fmt.Sprintf("mtcacg_%s_p%d", aw.ID, pic.Index)

                    if db.CheckExists(pid) {
                       // 可选：加一行提示
                      log.Printf("♻️ MtcACG_all skip duplicate: %s", pid)
                      continue
                    }

					// 3) 用 picture id 下载原图
					imgData, err := manyacg.DownloadOriginal(ctx, pic.ID)
					if err != nil || len(imgData) == 0 {
						log.Printf("❌ MtcACG original failed: %v (picID=%s)", err, pic.ID)
						continue
					}

					width := pic.Width
					height := pic.Height

					// 4) 组装标签 / caption
					tagsStr := strings.Join(aw.Tags, " ")
					hashTags := ""
					if len(aw.Tags) > 0 {
						hashTags = "#" + strings.Join(aw.Tags, " #")
					}

					source := "manyacg"
					if aw.SourceType != "" {
						source = aw.SourceType
					}

					caption := fmt.Sprintf(
						"ManyACG: %s [P%d/%d]\nArtist: %s\nSource: %s\nPlatform: %s\nTags: %s",
						strings.TrimSpace(aw.Title),
						pic.Index+1, len(aw.Pictures),
						strings.TrimSpace(aw.Artist.Name),
						aw.SourceURL,
						source,
						hashTags,
					)

					log.Printf("⬇️ ManyACG [%s] P%d (%dx%d, pid=%s)", aw.Title, pic.Index, width, height, pid)

					// 5) 发送并存库
					botHandler.ProcessAndSend(ctx, imgData, pid, tagsStr, caption, source, width, height)
					db.History[pid] = true
					db.PushHistory()

					time.Sleep(3 * time.Second)
				}
			}

			page++
			time.Sleep(5 * time.Second)
		}
	}
}
