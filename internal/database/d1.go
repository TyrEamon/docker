package database

import (
	"fmt"
	"log"
	"my-bot-go/internal/config"
	"strings"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
)

type D1Client struct {
	client  *resty.Client
	cfg     *config.Config
	History map[string]bool
	mu       sync.RWMutex
	lastPush  time.Time
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
	d.mu.Lock() // <---  加写锁
	for _, id := range ids {
		if strings.TrimSpace(id) != "" {
			d.History[id] = true
		}
	}
	log.Printf("🧠 Synced %d items from history", len(d.History))
	d.mu.Unlock() // <--- 解写锁
}

func (d *D1Client) PushHistory() {
	if d.cfg.WorkerURL == "" {
		return
	}
	
	if time.Since(d.lastPush) < 10*time.Second {
		return
	}

	d.mu.RLock() // <--- 加读锁 (只读不写)
	var idList []string
	for id := range d.History {
		idList = append(idList, id)
	}
	d.mu.RUnlock() // <--- 解读锁
	
	data := strings.Join(idList, ",")
	
	_, err := d.client.R().
		SetBody(data).
		Post(d.cfg.WorkerURL + "/api/update_history")
		
	if err != nil {
		log.Printf("⚠️ Push history failed: %v", err)
	} else {
		d.lastPush = time.Now()
		log.Println("☁️ History updated to cloud")
	}
}

func (d *D1Client) SaveImage(postID, fileID, originID, caption, artist, tags, source string, width, height int) error {
	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/d1/database/%s/query",
		d.cfg.CF_AccountID, d.cfg.D1_DatabaseID)
	
	finalTags := fmt.Sprintf("%s %s", tags, source)
	
	sql := "INSERT OR IGNORE INTO images (id, file_name, origin_id, caption, artist, tags, created_at, width, height) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)"
	params := []interface{}{postID, fileID, originID, caption, artist, finalTags, time.Now().Unix(), width, height}
	
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

	d.mu.Lock() // <--- 加写锁
	d.History[postID] = true
	d.mu.Unlock() // <--- 解写锁
	return nil
}

func (d *D1Client) CheckExists(postID string) bool {
	// 1. 第一道防线：查内存 (速度快)
	d.mu.RLock() // <--- 加读锁
	exists := d.History[postID]
	d.mu.RUnlock() // <--- 解读锁
	
	if exists {
		return true
	}

	//实时查 D1 数据库
	// 构造查询 SQL
	sql := "SELECT 1 FROM images WHERE id = ? LIMIT 1"
	body := map[string]interface{}{
		"sql":    sql,
		"params": []interface{}{postID},
	}

	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/d1/database/%s/query",
		d.cfg.CF_AccountID, d.cfg.D1_DatabaseID)

	resp, err := d.client.R().
		SetHeader("Authorization", "Bearer "+d.cfg.CF_APIToken).
		SetHeader("Content-Type", "application/json").
		SetBody(body).
		Post(url)

	if err != nil {
		log.Printf("⚠️ D1 Check Error: %v", err)
		// return false // “宁可发重，不可漏发”
		// return true  // “宁可漏发，不可发重”
		return false 
	}

	respStr := resp.String()
	
	cleanStr := strings.ReplaceAll(respStr, " ", "")
	
	if strings.Contains(cleanStr, "\"results\":[]") {
		return false
	}

	if strings.Contains(cleanStr, "\"success\":true") {
		d.mu.Lock() // <--- 加写锁 
		d.History[postID] = true
		d.mu.Unlock() // <--- 解写锁
		return true
	}

	return false
}

// DeleteImage 从数据库中删除指定 ID 的图片记录
func (d *D1Client) DeleteImage(postID string) error {
    url := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/d1/database/%s/query",
        d.cfg.CF_AccountID, d.cfg.D1_DatabaseID)

    // 构造 DELETE SQL
    sql := "DELETE FROM images WHERE id = ?"
    params := []interface{}{postID}

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
        return fmt.Errorf("D1 API Error: %s", resp.String())
    }

	d.mu.Lock() // <--- 加写锁
    delete(d.History, postID)
	d.mu.Unlock() // <--- 解写锁
    
    // d.PushHistory()     // 可选：立即同步一次历史记录

    return nil
}
