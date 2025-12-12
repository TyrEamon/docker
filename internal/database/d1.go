package database

import (
	"fmt"
	"log"
	"my-bot-go/internal/config"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

type D1Client struct {
	client  *resty.Client
	cfg     *config.Config
	History map[string]bool
}

func NewD1Client(cfg *config.Config) *D1Client {
	return &D1Client{
		client:  resty.New(),
		cfg:     cfg,
		History: make(map[string]bool),
	}
}

func (d *D1Client) SyncHistory() {
	if d.cfg.WorkerURL == "" {
		return
	}
	resp, err := d.client.R().Get(d.cfg.WorkerURL + "/api/get_history")
	if err != nil {
		log.Printf("⚠️ Sync history failed: %v", err)
		return
	}
	
	ids := strings.Split(string(resp.Body()), ",")
	for _, id := range ids {
		if strings.TrimSpace(id) != "" {
			d.History[id] = true
		}
	}
	log.Printf("🧠 Synced %d items from history", len(d.History))
}

func (d *D1Client) PushHistory() {
	if d.cfg.WorkerURL == "" {
		return
	}
	var idList []string
	for id := range d.History {
		idList = append(idList, id)
	}
	data := strings.Join(idList, ",")
	
	_, err := d.client.R().
		SetBody(data).
		Post(d.cfg.WorkerURL + "/api/update_history")
		
	if err != nil {
		log.Printf("⚠️ Push history failed: %v", err)
	} else {
		log.Println("☁️ History updated to cloud")
	}
}

// SaveImage 支持 width 和 height
func (d *D1Client) SaveImage(postID, fileID, caption, tags, source string, width, height int) error {
	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/d1/database/%s/query", 
		d.cfg.CF_AccountID, d.cfg.D1_DatabaseID)
	
	finalTags := fmt.Sprintf("%s %s", tags, source)
	
	// ⚠️ 请确保你在 D1 执行了: ALTER TABLE images ADD COLUMN width INTEGER; ALTER TABLE images ADD COLUMN height INTEGER;
	sql := "INSERT OR IGNORE INTO images (id, file_name, caption, tags, created_at, width, height) VALUES (?, ?, ?, ?, ?, ?, ?)"
	params := []interface{}{postID, fileID, caption, finalTags, time.Now().Unix(), width, height}
	
	body := map[string]interface{}{
		"sql":    sql,
		"params": params,
	}

	resp, err := d.client.R().
		SetHeader("Authorization", "Bearer "+d.cfg.CF_APIToken).
		SetHeader("Content-Type", "application/json").
		SetBody(body).
		Post(url)

	if err != nil {
		return err
	}
	if resp.IsError() {
		return fmt.Errorf("D1 Error: %s", resp.String())
	}
	
	d.History[postID] = true
	return nil
}
