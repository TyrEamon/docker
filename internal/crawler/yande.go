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
	// ✅ 1. 设置超时为 90秒
	client.SetTimeout(90 * time.Second)
	client.SetRetryCount(3)
	client.SetRetryWaitTime(4 * time.Second)
	// ✅ 3. 伪装 User-Agent 为 Chrome 浏览器
	client.SetHeader("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	// 🛠️ 预处理：将标签字符串按逗号分割成多个任务组
	// 例如: "tag1+order:score, tag2+order:score" -> ["tag1+order:score", " tag2+order:score"]
	tagGroups := strings.Split(cfg.YandeTags, ",")

	for {
		select {
		case <-ctx.Done():
			return
		default:
			log.Println("🔄 Starting Yande Loop...")

			// 🔄 遍历每一组标签任务
			for _, tags := range tagGroups {
				currentTags := strings.TrimSpace(tags)
				if currentTags == "" {
					continue
				}

				log.Printf("🔍 Checking Yande Tags: [%s] ...", currentTags)

				// 构造 URL，使用当前这组标签
				url := fmt.Sprintf("https://yande.re/post.json?limit=%d&tags=%s", cfg.YandeLimit, currentTags)

				resp, err := client.R().Get(url)
				if err != nil {
					log.Printf("Yande API Error (%s): %v", currentTags, err)
					time.Sleep(10 * time.Second) // 出错后小憩
					continue
				}

				var posts []YandePost
				if err := json.Unmarshal(resp.Body(), &posts); err != nil {
					log.Printf("Yande JSON Error (%s): %v", currentTags, err)
					time.Sleep(10 * time.Second)
					continue
				}

				if len(posts) == 0 {
					log.Printf("⚠️ No posts found for tags: %s", currentTags)
					continue
				}

				processedInLoop := make(map[int]bool)
				for _, post := range posts {
					if processedInLoop[post.ID] {
						continue
					}

					pid := fmt.Sprintf("yande_%d", post.ID)
					// ✅ 核心去重：先查内存，再查 D1
					if db.CheckExists(pid) {
						continue
					}

					targetID := post.ID
					if post.ParentID != 0 {
						targetID = post.ParentID
					}

					// ✅ 改动1：改用 fetchFamilyWithParent 确保包含父图
					familyPosts := fetchFamilyWithParent(client, targetID)
					if len(familyPosts) == 0 {
						// 兜底：如果 API 查不到，至少处理自己
						familyPosts = []YandePost{post}
					}

					// 处理单图或套图
					if len(familyPosts) == 1 {
						p := familyPosts[0]
						processSingleImage(ctx, client, p, db, botHandler)
						processedInLoop[p.ID] = true
						// 单图也存入历史，防止重复
						db.History[fmt.Sprintf("yande_%d", p.ID)] = true
					} else {
						// ✅ 改动2：传入 targetID (父ID) 用于生成统一格式的 ID
						processMediaGroup(ctx, client, familyPosts, targetID, db, botHandler)
						for _, p := range familyPosts {
							processedInLoop[p.ID] = true
							// 标记子图为已处理
							db.History[fmt.Sprintf("yande_%d", p.ID)] = true
						}
					}
					
					// ✅ 每处理完一组图（无论是单张还是套图），立即保存历史到云端
					// 避免程序意外中断导致重复
					db.PushHistory()
					
					// 处理完一张/组图后稍微休息一下，避免刷屏
					time.Sleep(3 * time.Second)
				}

				// ✅ 一组标签任务跑完后，休息 10 秒再跑下一组标签
				log.Printf("✅ Task [%s] finished. Cooldown 10s...", currentTags)
				time.Sleep(10 * time.Second)
			}

			// ✅ 所有标签组都轮询了一遍，开始长睡眠
			log.Println("😴 All Yande Tasks Done. Sleeping 80m...") 
			time.Sleep(80 * time.Minute)
		}
	}
}

// ✅ 改动3：重构 fetchFamily，先查父图再查子图
func fetchFamilyWithParent(client *resty.Client, parentID int) []YandePost {
	var finalFamily []YandePost

	// 1. 尝试获取父图本身 (如果父ID确实存在)
	// 有些老图 ParentID 可能已被删除，但这步通常能保证父图在列
	urlParent := fmt.Sprintf("https://yande.re/post.json?tags=id:%d", parentID)
	respP, errP := client.R().Get(urlParent)
	var parents []YandePost
	if errP == nil {
		_ = json.Unmarshal(respP.Body(), &parents)
		if len(parents) > 0 {
			finalFamily = append(finalFamily, parents[0])
		}
	}

	// 2. 获取所有子图
	urlChildren := fmt.Sprintf("https://yande.re/post.json?tags=parent:%d", parentID)
	respC, errC := client.R().Get(urlChildren)
	var children []YandePost
	if errC == nil {
		_ = json.Unmarshal(respC.Body(), &children)
		finalFamily = append(finalFamily, children...)
	}

	return finalFamily
}

// processSingleImage 保持不变
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

// ✅ 改动4：增加 parentID 参数，并修改 ID 生成逻辑
func processMediaGroup(ctx context.Context, client *resty.Client, posts []YandePost, parentID int, db *database.D1Client, botHandler *telegram.BotHandler) {
	log.Printf("📦 Processing Family Group: %d (Count: %d)", parentID, len(posts))

	for i, p := range posts {
		if i >= 10 {
			break
		}

		imgURL := selectBestImageURL(p)
		imgResp, err := client.R().Get(imgURL)
		if err != nil {
			continue
		}

		// 格式化 Caption
		tags := strings.Split(p.Tags, " ")
		firstTag := ""
		if len(tags) > 0 {
			firstTag = tags[0]
		}
		caption := fmt.Sprintf("Yande Set: %d [%d/%d]\nTags: #%s", parentID, i+1, len(posts), firstTag)

		// ✅ 核心改动：ID 统一为 yande_{父ID}_p{序号}
		// 这样前端 Worker 就可以识别出它们是一组
		pid := fmt.Sprintf("yande_%d_p%d", parentID, i)

		botHandler.ProcessAndSend(ctx, imgResp.Body(), pid, p.Tags, caption, "yande", p.Width, p.Height)
		time.Sleep(1 * time.Second)
	}
}

// selectBestImageURL 保持不变
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
