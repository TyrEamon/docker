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

				// 2. 从最终跳转 URL 里提取 picture id
				finalURL := resp.RawResponse.Request.URL.String()
				// 例子: https://cdn.manyacg.top/regular/twitter/.../67009d8d4e0a5f427e928347_regular.webp
				parts := strings.Split(finalURL, "/")
				fileName := parts[len(parts)-1] // 67009d8d4e0a5f427e928347_regular.webp

				// 去掉结尾的 "_regular..."，只保留中间那段 id
				idPart := fileName
				if idx := strings.Index(idPart, "_regular"); idx != -1 {
					idPart = idPart[:idx]
				}

				// 3. 使用原图接口下载真正原图
				originURL := fmt.Sprintf("https://api.manyacg.top/v1/picture/file/%s", idPart)
				originResp, err := client.R().Get(originURL)
				if err != nil || originResp.StatusCode() != 200 {
					log.Printf("❌ Sese Origin Download Failed: %v (status=%d)", err, originResp.StatusCode())
					continue
				}

				// 4. 拿到原图数据
				imgData := originResp.Body()
				if len(imgData) == 0 {
					continue
				}

				// 5. 解析宽高
				imgConfig, format, err := image.DecodeConfig(bytes.NewReader(imgData))
				if err != nil {
					// log.Printf("⚠️ Sese Decode Failed: %v", err)
					continue
				}
				width := imgConfig.Width
				height := imgConfig.Height

				// 6. 生成唯一 ID (sese_文件名)
				pid := fmt.Sprintf("sese_%s", fileName)

				// 7. 查重
				if db.CheckExists(pid) {
					time.Sleep(1 * time.Second)
					continue
				}

				// 8. 构造数据
				title := "MtcACG: SESE"
				tagsStr := "#R18 #Sese #ManyACG"
				caption := fmt.Sprintf("%s\nFormat: %s (%dx%d)\nTags: %s",
					title, strings.ToUpper(format), width, height, tagsStr)

				log.Printf("⬇️ Got Sese [%d/10]: %s (%dx%d)", i+1, fileName, width, height)

				// 9. 发送并保存（用原图数据）
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
