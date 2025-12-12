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

func StartPixiv(ctx context.Context, cfg *config.Config, db *database.D1Client, bot *telegram.BotHandler) {
	client := resty.New()
	client.SetHeader("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	client.SetHeader("Referer", "https://www.pixiv.net/")
	client.SetHeader("Cookie", "PHPSESSID="+cfg.PixivPHPSESSID)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			log.Println("🍪 Checking Pixiv (Cookie Mode)...")
			hasNew := false

			for _, uid := range cfg.PixivArtistIDs {
				// 1. 获取画师作品
				resp, err := client.R().Get(fmt.Sprintf("https://www.pixiv.net/ajax/user/%s/profile/all", uid))
				if err != nil || resp.StatusCode() != 200 {
					log.Printf("⚠️ Pixiv User %s Error", uid)
					continue
				}

				var profile struct {
					Body struct {
						Illusts map[string]interface{} `json:"illusts"`
					} `json:"body"`
				}
				json.Unmarshal(resp.Body(), &profile)

				// 提取 ID 并排序
				var ids []int
				for k := range profile.Body.Illusts {
					if id, err := strconv.Atoi(k); err == nil {
						ids = append(ids, id)
					}
				}
				// 降序排列 (最新的在前)
				sort.Sort(sort.Reverse(sort.IntSlice(ids)))

				// 取前 N 个
				count := 0
				for _, id := range ids {
					if count >= cfg.PixivLimit {
						break
					}
					pid := fmt.Sprintf("pixiv_%d", id)

					// 去重检查
					if db.History[pid] {
						continue
					}

					// 2. 获取详情
					detailResp, err := client.R().Get(fmt.Sprintf("https://www.pixiv.net/ajax/illust/%d", id))
					if err != nil {
						continue
					}

					// 解析 JSON (这里用 map 偷懒，不用定义超长结构体)
					var detail map[string]interface{}
					json.Unmarshal(detailResp.Body(), &detail)
					
					body, ok := detail["body"].(map[string]interface{})
					if !ok { continue }

					title := body["illustTitle"].(string)
					userName := body["userName"].(string)
					urls := body["urls"].(map[string]interface{})
					originalURL := urls["original"].(string)

					// Tags 处理
					tagsObj := body["tags"].(map[string]interface{})
					tagsList := tagsObj["tags"].([]interface{})
					var tagStrs []string
					for _, t := range tagsList {
						tData := t.(map[string]interface{})
						tagStrs = append(tagStrs, tData["tag"].(string))
					}
					tagsStr := strings.Join(tagStrs, " ")

					// 下载
					log.Printf("⬇️ Downloading Pixiv: %s", title)
					imgResp, err := client.R().Get(originalURL)
					if err == nil && imgResp.StatusCode() == 200 {
						caption := fmt.Sprintf("Pixiv: %s\nArtist: %s\nTags: #%s", title, userName, strings.ReplaceAll(tagsStr, " ", " #"))
						bot.ProcessAndSend(ctx, imgResp.Body(), pid, tagsStr, caption, "pixiv")
						hasNew = true
						count++
					}
					time.Sleep(2 * time.Second)
				}
			}

			if hasNew {
				db.PushHistory()
			}
			
			log.Println("😴 Pixiv Done. Sleeping 10m...")
			time.Sleep(10 * time.Minute)
		}
	}
}
