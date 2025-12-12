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

	// ✨ 新增：Kemono 支持
	KemonoCreators []KemonoCreator
}

func Load() *Config {
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

	cfg := &Config{
		BotToken:       getEnv("BOT_TOKEN", ""),
		ChannelID:      channelID,
		CF_AccountID:   getEnv("CLOUDFLARE_ACCOUNT_ID", ""),
