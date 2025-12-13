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
		IllustType int    `json:"illustType"` // 2=动图
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

				// 提取 ID 并倒序排列 (最新的在前)
				var ids []int
				for k := range profile.Body.Illusts {
					if id, err := strconv.Atoi(k); err == nil {
						ids = append(ids, id)
					}
				}
				sort.Sort(sort.Reverse(sort.IntSlice(ids)))

				// 限制处理数量
				count := 0
				for _, id := range ids {
					if count >= cfg.PixivLimit {
						break
					}
					
					// 基础去重 (只要发过第一张，就算这个ID处理过了)
					// 注意：如果是多图，我们在下面会处理，这里只防重复抓同一个作品
					mainPid := fmt.Sprintf("pixiv_%d_p0", id)
					if db.History[mainPid] {
						continue
					}

					log.Printf("🔍 Processing Pixiv ID: %d", id)

					// 2. 获取详情 (主要为了拿标题、Tags、动图判断)
					detailResp, err := client.R().Get(fmt.Sprintf("https://www.pixiv.net/ajax/illust/%d", id))
					if err != nil { continue }

					var detail PixivDetailResp
					if err := json.Unmarshal(detailResp.Body(), &detail); err != nil {
						continue
					}
					
					// 如果是动图 (IllustType == 2)，暂时跳过，或者你可以以后加动图逻辑
					if detail.Body.IllustType == 2 {
						log.Printf("⚠️ Skip Ugoira (GIF): %d", id)
						// 标记为已处理，防止反复检查
						db.History[mainPid] = true
						continue 
					}

					// Tags 拼接
					var tagStrs []string
					for _, t := range detail.Body.Tags.Tags {
						tagStrs = append(tagStrs, t.Tag)
					}
					tagsStr := strings.Join(tagStrs, " ")
					
					// 3. ✨ 关键升级：获取 Pages (多图+宽高)
					pagesResp, err := client.R().Get(fmt.Sprintf("https://www.pixiv.net/ajax/illust/%d/pages?lang=zh", id))
					if err != nil { continue }

					var pages PixivPagesResp
					json.Unmarshal(pagesResp.Body(), &pages)

					if len(pages.Body) == 0 {
						continue
					}

					// 4. 开始处理每一张图 (支持多图发送)
					// 这里我们简化逻辑：循环发每一张图，或者你可以改成 MediaGroup
					// 为了数据库 FileID 的准确性，我们采用“带页码标记”的单发模式
					
					// 限制一下多图数量，防止一个作品 200 张图刷屏
					maxPages := 5 
					
					for i, page := range pages.Body {
						if i >= maxPages { break }

						// 构造唯一的 PID: pixiv_12345_p0, pixiv_12345_p1
						subPid := fmt.Sprintf("pixiv_%d_p%d", id, i)
						
						// 双重检查：防止中断后重启重复发后面几张
						if db.History[subPid] {
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

						// ProcessAndSend 内部会用 subPid 作为 ID 存入 D1
						botHandler.ProcessAndSend(ctx, imgResp.Body(), subPid, tagsStr, caption, "pixiv", page.Width, page.Height)
						
						time.Sleep(4 * time.Second) // 慢一点，防止被 ban
					}
					
					if db.CheckExists(mainPid) {
					
					count++
				}
			}

			
			log.Println("😴 Pixiv Done. Sleeping 180m...")
			time.Sleep(180 * time.Minute)
		}
	}
}
