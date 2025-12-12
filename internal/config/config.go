package config

import (
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	BotToken       string
	ChannelID      int64
	CF_AccountID   string
	CF_APIToken    string
	D1_DatabaseID  string
	WorkerURL      string
	PixivPHPSESSID string
	PixivLimit     int
	YandeLimit     int
	YandeTags      string
	PixivArtistIDs []string
}

func Load() *Config {
	// 尝试加载 .env 文件，如果不存在也没关系（生产环境直接读环境变量）
	_ = godotenv.Load()

	channelIDStr := getEnv("CHANNEL_ID", "")
	channelID, err := strconv.ParseInt(channelIDStr, 10, 64)
	if err != nil {
		log.Printf("⚠️ Warning: Invalid CHANNEL_ID: %v", err)
	}

	pixivLimit, _ := strconv.Atoi(getEnv("PIXIV_LIMIT", "3"))
	yandeLimit, _ := strconv.Atoi(getEnv("YANDE_LIMIT", "1"))

	artistIDsStr := getEnv("PIXIV_ARTIST_IDS", "")
	var artistIDs []string
	if artistIDsStr != "" {
		// 支持逗号或换行符分隔
		parts := strings.FieldsFunc(artistIDsStr, func(r rune) bool {
			return r == ',' || r == '\n'
		})
		for _, p := range parts {
			if strings.TrimSpace(p) != "" {
				artistIDs = append(artistIDs, strings.TrimSpace(p))
			}
		}
	}

	return &Config{
		BotToken:       getEnv("BOT_TOKEN", ""),
		ChannelID:      channelID,
		CF_AccountID:   getEnv("CLOUDFLARE_ACCOUNT_ID", ""),
		CF_APIToken:    getEnv("CLOUDFLARE_API_TOKEN", ""),
		D1_DatabaseID:  getEnv("D1_DATABASE_ID", ""),
		WorkerURL:      getEnv("WORKER_URL", ""),
		PixivPHPSESSID: getEnv("PIXIV_PHPSESSID", ""),
		PixivLimit:     pixivLimit,
		YandeLimit:     yandeLimit,
		YandeTags:      getEnv("YANDE_TAGS", "order:random"),
		PixivArtistIDs: artistIDs,
	}
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	// 兼容带空格的key或者旧的命名习惯
	if value, exists := os.LookupEnv(strings.ReplaceAll(key, "_", " ")); exists {
		return value
	}
	return fallback
}
