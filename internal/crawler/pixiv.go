package crawler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"my-bot-go/internal/config"
	"my-bot-go/internal/database"
	"my-bot-go/internal/telegram"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

// 定义更严谨的结构体，方便解析 pages 接口
type PixivPage struct {
	Urls struct {
		Original string `json:"original"`
		Small    string `json:"small"`
	} `json:"urls"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type PixivPagesResp struct {
	Body []PixivPage `json:"body"`
}

type PixivDetailResp struct {
	Body struct {
		IllustId   string `json:"illustId"`
		IllustTitle string `json:"illustTitle"`
		UserName   string `json:"userName"`
		IllustType int    `json:"illustType"` 
		Tags       struct {
			Tags []struct {
				Tag string `json:"tag"`
			} `json:"tags"`
		} `json:"tags"`
	} `json:"body"`
}

func StartPixiv(ctx context.Context, cfg *config.Config, db *database.D1Client, botHandler *telegram.BotHandler) {
	client := resty.New()
	client.SetHeader("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	client.SetHeader("Referer", "https://www.pixiv.net/")
	client.SetHeader("Cookie", "PHPSESSID="+cfg.PixivPHPSESSID)
	// 建议把超时设长一点
	client.SetTimeout(60 * time.Second)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			log.Println("🍪 Checking Pixiv (Cookie Mode)...")

			for _, uid := range cfg.PixivArtistIDs {
				// 1. 获取画师所有作品列表
				resp, err := client.R().Get(fmt.Sprintf("https://www.pixiv.net/ajax/user/%s/profile/all", uid))
				if err != nil || resp.StatusCode() != 200 {
					log.Printf("⚠️ Pixiv User %s Error: %v", uid, err)
					continue
				}

				var profile struct {
					Body struct {
						Illusts map[string]interface{} `json:"illusts"`
					} `json:"body"`
				}
				json.Unmarshal(resp.Body(), &profile)

				var ids []int
				for k := range profile.Body.Illusts {
					if id, err := strconv.Atoi(k); err == nil {
						ids = append(ids, id)
					}
				}
				sort.Sort(sort.Reverse(sort.IntSlice(ids)))

				count := 0
				for i, id := range ids {

				// 检查是否超过了回溯范围，太旧了，直接跳出循环
                if cfg.PixivCrawlRange > 0 && i >= cfg.PixivCrawlRange {
                 log.Printf("🛑 触达回溯限制 (%d/%d)，停止处理画师 %s 的旧图", i, cfg.PixivCrawlRange, uid)
                 break 
                 }
					
					if count >= cfg.PixivLimit {
						break
					}
					
					// 基础去重 
					mainPid := fmt.Sprintf("pixiv_%d_p0", id)
					if db.CheckExists(mainPid) {
						continue
					}

					log.Printf("🔍 Processing Pixiv ID: %d", id)

					// 2. 获取详情
					detailResp, err := client.R().Get(fmt.Sprintf("https://www.pixiv.net/ajax/illust/%d", id))
					if err != nil { continue }

					var detail PixivDetailResp
					if err := json.Unmarshal(detailResp.Body(), &detail); err != nil {
						continue
					}
					
					// 如果是动图，暂时跳过
					if detail.Body.IllustType == 2 {
						log.Printf("⚠️ Skip Ugoira (GIF): %d", id)
						db.History[mainPid] = true
						continue 
					}

					// Tags 拼接
					var tagStrs []string
					for _, t := range detail.Body.Tags.Tags {
						tagStrs = append(tagStrs, t.Tag)
					}
					tagsStr := strings.Join(tagStrs, " ")
					
					// 关键升级：获取 Pages
					pagesResp, err := client.R().Get(fmt.Sprintf("https://www.pixiv.net/ajax/illust/%d/pages?lang=zh", id))
					if err != nil { continue }

					var pages PixivPagesResp
					json.Unmarshal(pagesResp.Body(), &pages)

					if len(pages.Body) == 0 {
						continue
					}

					maxPages := 50 
					
					for i, page := range pages.Body {
						if i >= maxPages { break }

						// 构造唯一的PID
						subPid := fmt.Sprintf("pixiv_%d_p%d", id, i)
						
						// 双重检查
						if db.CheckExists(subPid) {
							continue
						}

						log.Printf("⬇️ Downloading Pixiv: %s (P%d)", detail.Body.IllustTitle, i)
						
						imgResp, err := client.R().Get(page.Urls.Original)
						if err != nil || imgResp.StatusCode() != 200 {
							log.Printf("❌ Download failed: %v", err)
							continue
						}

						// 构造标题
						caption := fmt.Sprintf("Pixiv: %s [P%d/%d]\nArtist: %s\nTags: #%s", 
							detail.Body.IllustTitle, i+1, len(pages.Body), 
							detail.Body.UserName, 
							strings.ReplaceAll(tagsStr, " ", " #"))

						botHandler.ProcessAndSend(ctx, imgResp.Body(), subPid, tagsStr, caption, "pixiv", page.Width, page.Height)
						
						time.Sleep(18 * time.Second) // 防被ban
					}
					
					db.PushHistory()
					
					count++
				}
			}

			
			log.Println("😴 Pixiv Done. Sleeping 73m...")
			time.Sleep(73 * time.Minute)
		}
	}
}
