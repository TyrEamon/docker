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

type YandePost struct {
	ID        int    `json:"id"`
	SampleURL string `json:"sample_url"`
	FileURL   string `json:"file_url"`
	Tags      string `json:"tags"`
}

func StartYande(ctx context.Context, cfg *config.Config, db *database.D1Client, bot *telegram.BotHandler) {
	client := resty.New()
	
	for {
		select {
		case <-ctx.Done():
			return
		default:
			log.Println("🔍 Checking Yande...")
			url := fmt.Sprintf("https://yande.re/post.json?limit=%d&tags=%s", cfg.YandeLimit, cfg.YandeTags)
			
			resp, err := client.R().Get(url)
			if err != nil {
				log.Printf("Yande Error: %v", err)
				time.Sleep(1 * time.Minute)
				continue
			}

			var posts []YandePost
			if err := json.Unmarshal(resp.Body(), &posts); err != nil {
				log.Printf("Yande JSON Error: %v", err)
				continue
			}

			for _, post := range posts {
				pid := fmt.Sprintf("yande_%d", post.ID)
				
				// 简单的内存去重检查 (可选)
				if db.History[pid] {
					continue
				}

				imgURL := post.SampleURL
				if imgURL == "" {
					imgURL = post.FileURL
				}

				log.Printf("Downloading Yande: %d", post.ID)
				imgResp, err := client.R().Get(imgURL)
				if err != nil {
					continue
				}

				caption := fmt.Sprintf("Yande: %d\nTags: #%s", post.ID, strings.ReplaceAll(post.Tags, " ", " #"))
				
				bot.ProcessAndSend(ctx, imgResp.Body(), pid, post.Tags, caption, "yande")
				time.Sleep(2 * time.Second)
			}

			log.Println("😴 Yande Done. Sleeping 10m...")
			time.Sleep(10 * time.Minute)
		}
	}
}
