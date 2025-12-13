package config

import (
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type KemonoCreator struct {
	Service string   // fanbox / patreon / gumroad ...
	UserIDs []string // 该平台下要巡逻的作者 ID
}

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

	// Kemono 支持
	KemonoCreators []KemonoCreator

	// Danbooru 支持
	DanbooruTags  string
	DanbooruLimit int
	DanbooruUsername string 
	DanbooruAPIKey   string 

	CosineTags        []string // 要爬取的标签列表，例如 "原神,崩坏星穹铁道"
	CosineLimitPerTag int      // 每个标签限制爬取的数量
}

func Load() *Config {
	// 本地开发时尝试加载 .env；生产环境直接用环境变量
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
		parts := strings.FieldsFunc(artistIDsStr, func(r rune) bool {
			return r == ',' || r == '\n'
		})
		for _, p := range parts {
			if strings.TrimSpace(p) != "" {
				artistIDs = append(artistIDs, strings.TrimSpace(p))
			}
		}
	}

	// 👇 【新增】读取 Cosine 配置
	cosineLimit, _ := strconv.Atoi(getEnv("COSINE_LIMIT_PER_TAG", "30")) // 默认 50 张
	
	cosineTagsStr := getEnv("COSINE_TAGS", "初音未来") // 默认只爬"原神"
	var cosineTags []string
	if cosineTagsStr != "" {
		// 支持逗号或换行分隔
		parts := strings.FieldsFunc(cosineTagsStr, func(r rune) bool {
			return r == ',' || r == '\n'
		})
		for _, p := range parts {
			if strings.TrimSpace(p) != "" {
				cosineTags = append(cosineTags, strings.TrimSpace(p))
			}
		}
	}

	cfg := &Config{
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
		CosineTags:        cosineTags,
		CosineLimitPerTag: cosineLimit,
	}

	// 解析 Kemono 多平台配置
	// 例子：
	// KEMONO_SERVICES=fanbox,patreon
	// KEMONO_FANBOX_USER_IDS=123,456
	// KEMONO_PATREON_USER_IDS=111,222
	servicesEnv := getEnv("KEMONO_SERVICES", "")
	if servicesEnv != "" {
		services := strings.Split(servicesEnv, ",")
		for _, s := range services {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			key := "KEMONO_" + strings.ToUpper(s) + "_USER_IDS"
			idsEnv := getEnv(key, "")
			if idsEnv == "" {
				continue
			}
			var ids []string
			parts := strings.FieldsFunc(idsEnv, func(r rune) bool {
				return r == ',' || r == '\n'
			})
			for _, p := range parts {
				if strings.TrimSpace(p) != "" {
					ids = append(ids, strings.TrimSpace(p))
				}
			}
			if len(ids) > 0 {
				cfg.KemonoCreators = append(cfg.KemonoCreators, KemonoCreator{
					Service: s,
					UserIDs: ids,
				})
			}
		}
	}

	// 解析 Danbooru 配置
	// 例子：
	// DANBOORU_TAGS=order:rank date:today -animated
	// DANBOORU_LIMIT=5
	danLimit, _ := strconv.Atoi(getEnv("DANBOORU_LIMIT", "3"))
	cfg.DanbooruTags = getEnv("DANBOORU_TAGS", "order:rank -animated")
	cfg.DanbooruLimit = danLimit
	cfg.DanbooruUsername = getEnv("DANBOORU_USERNAME", "")
	cfg.DanbooruAPIKey = getEnv("DANBOORU_APIKEY", "")

	return cfg
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	// 兼容带空格的 key 或旧命名
	if value, exists := os.LookupEnv(strings.ReplaceAll(key, "_", " ")); exists {
		return value
	}
	return fallback
}
