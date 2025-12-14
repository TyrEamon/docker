package crawler

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"  // 支持 GIF
	_ "image/jpeg" // 支持 JPG
	_ "image/png"// 支持 PNG
	_ "golang.org/x/image/webp"
	"log"
	"my-bot-go/internal/config"
	"my-bot-go/internal/database"
	"my-bot-go/internal/telegram"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

// StartManyACGSese 专门爬取 /sese 接口
// 策略：每 10 分钟爬 10 张
func StartManyACGSese(ctx context.Context, cfg *config.Config, db *database.D1Client, botHandler *telegram.BotHandler) {
	client := resty.New()
	client.SetTimeout(60 * time.Second)
	client.SetHeader("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0")

	for {
		select {
		case <-ctx.Done():
			return
		default:
			log.Println("🎲 Starting Batch Sese (10 Pics)...")

			// ✅ 内部循环：一次爬 10 张
			for i := 0; i < 10; i++ {
				// 1. 请求跳转接口
				url := "https://manyacg.top/sese"
				
				resp, err := client.R().Get(url)
				if err != nil {
					log.Printf("❌ ManyACG Sese Request Failed: %v", err)
					time.Sleep(2 * time.Second)
					continue
				}

				if resp.StatusCode() != 200 {
					log.Printf("❌ ManyACG Sese HTTP Error: %d", resp.StatusCode())
					time.Sleep(2 * time.Second)
					continue
				}

				// 2. 拿到图片数据
				imgData := resp.Body()
				if len(imgData) == 0 {
					continue
				}

				// 3. 解析宽高
				imgConfig, format, err := image.DecodeConfig(bytes.NewReader(imgData))
				if err != nil {
					// log.Printf("⚠️ Sese Decode Failed: %v", err)
					continue 
				}
				width := imgConfig.Width
				height := imgConfig.Height

				// 4. 生成唯一 ID (sese_文件名)
				finalURL := resp.RawResponse.Request.URL.String()
				parts := strings.Split(finalURL, "/")
				fileName := parts[len(parts)-1] 
				
				pid := fmt.Sprintf("sese_%s", fileName)

				// 5. 查重
				if db.CheckExists(pid) {
					// 遇到重复的就跳过，不计入成功次数，直接继续下一次循环
					// 也可以选择在这里 i-- 强行凑够10张，但容易死循环，建议直接跳过
					time.Sleep(1 * time.Second)
					continue
				}

				// 6. 构造数据
				title := "MtcACG: SESE"
				tagsStr := "#R18 #Sese #ManyACG" 
				caption := fmt.Sprintf("%s\nFormat: %s (%dx%d)\nTags: %s", 
					title, strings.ToUpper(format), width, height, tagsStr)

				log.Printf("⬇️ Got Sese [%d/10]: %s (%dx%d)", i+1, fileName, width, height)

				// 7. 发送并保存
				botHandler.ProcessAndSend(ctx, imgData, pid, tagsStr, caption, "manyacg_sese", width, height)
				db.PushHistory()

				// 每张图之间间隔 3 秒，防止 Telegram 发太快限流
				time.Sleep(3 * time.Second)
			}

			// ✅ 批次结束后，休息 10 分钟
			log.Println("😴 Sese Batch Done. Sleeping 30m...")
			time.Sleep(30 * time.Minute)
		}
	}
}
