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
	ParentID  int    `json:"parent_id"`
	SampleURL string `json:"sample_url"`
	FileURL   string `json:"file_url"`
	FileSize  int    `json:"file_size"`
	Tags      string `json:"tags"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

func StartYande(ctx context.Context, cfg *config.Config, db *database.D1Client, botHandler *telegram.BotHandler) {
	client := resty.New()
	
	// ✅ 1. 设置超时为 120秒
	client.SetTimeout(90 * time.Second)
	
	client.SetRetryCount(3)
	client.SetRetryWaitTime(4 * time.Second)

	// ✅ 3. 伪装 User-Agent 为 Chrome 浏览器
	client.SetHeader("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	for {
		select {
		case <-ctx.Done():
			return
		default:
			log.Println("🔍 Checking Yande...")
			url := fmt.Sprintf("https://yande.re/post.json?limit=%d&tags=%s", cfg.YandeLimit, cfg.YandeTags)

			resp, err := client.R().Get(url)
			if err != nil {
				log.Printf("Yande API Error: %v", err)
				time.Sleep(1 * time.Minute)
				continue
			}

			var posts []YandePost
			if err := json.Unmarshal(resp.Body(), &posts); err != nil {
				log.Printf("Yande JSON Error: %v", err)
				time.Sleep(1 * time.Minute)
				continue
			}

			processedInLoop := make(map[int]bool)

			for _, post := range posts {
				if processedInLoop[post.ID] {
					continue
				}

				pid := fmt.Sprintf("yande_%d", post.ID)
				if db.History[pid] {
					continue
				}

				targetID := post.ID
				if post.ParentID != 0 {
					targetID = post.ParentID
				}

				familyPosts := fetchFamily(client, targetID)
				if len(familyPosts) == 0 {
					familyPosts = []YandePost{post}
				}

				// 处理单图或套图
				if len(familyPosts) == 1 {
					p := familyPosts[0]
					processSingleImage(ctx, client, p, db, botHandler)
					processedInLoop[p.ID] = true
				} else {
					processMediaGroup(ctx, client, familyPosts, db, botHandler)
					for _, p := range familyPosts {
						processedInLoop[p.ID] = true
						// 标记子图为已处理
						db.History[fmt.Sprintf("yande_%d", p.ID)] = true
					}
				}

				// ✅ 【关键修正】每处理完一组图，立即保存历史到云端
				db.PushHistory()

				time.Sleep(3 * time.Second)
			}

			log.Println("😴 Yande Done. Sleeping 10m...")
			time.Sleep(180 * time.Minute)
		}
	}
}

func fetchFamily(client *resty.Client, parentID int) []YandePost {
	url := fmt.Sprintf("https://yande.re/post.json?tags=parent:%d", parentID)
	resp, err := client.R().Get(url)
	if err != nil {
		return nil
	}
	var posts []YandePost
	if err := json.Unmarshal(resp.Body(), &posts); err != nil {
		return nil
	}
	return posts
}

func processSingleImage(ctx context.Context, client *resty.Client, post YandePost, db *database.D1Client, botHandler *telegram.BotHandler) {
	imgURL := selectBestImageURL(post)
	log.Printf("⬇️ Downloading Yande: %d", post.ID)

	imgResp, err := client.R().Get(imgURL)
	if err != nil {
		log.Printf("Failed to download image: %v", err)
		return
	}

	pid := fmt.Sprintf("yande_%d", post.ID)
	caption := fmt.Sprintf("Yande: %d\nTags: #%s", post.ID, strings.ReplaceAll(post.Tags, " ", " #"))

	botHandler.ProcessAndSend(ctx, imgResp.Body(), pid, post.Tags, caption, "yande", post.Width, post.Height)
}

func processMediaGroup(ctx context.Context, client *resty.Client, posts []YandePost, db *database.D1Client, botHandler *telegram.BotHandler) {
	log.Printf("📦 Processing MediaGroup for Parent: %d (Count: %d)", posts[0].ParentID, len(posts))

	for i, p := range posts {
		if i >= 10 {
			break
		}

		imgURL := selectBestImageURL(p)
		imgResp, err := client.R().Get(imgURL)
		if err != nil {
			continue
		}

		caption := fmt.Sprintf("Yande Set: %d [%d/%d]\nTags: #%s", p.ParentID, i+1, len(posts), strings.Split(p.Tags, " ")[0])
		pid := fmt.Sprintf("yande_%d", p.ID)

		botHandler.ProcessAndSend(ctx, imgResp.Body(), pid, p.Tags, caption, "yande", p.Width, p.Height)
		time.Sleep(1 * time.Second)
	}
}

func selectBestImageURL(post YandePost) string {
	const MaxSize = 13 * 1024 * 1024
	if post.FileSize > 0 && post.FileSize < MaxSize {
		return post.FileURL
	}
	if post.SampleURL == "" {
		return post.FileURL
	}
	return post.SampleURL
}
