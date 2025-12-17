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
	
	if time.Since(d.lastPush) < 10*time.Second {
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
		d.lastPush = time.Now()
		//log.Println("☁️ History updated to cloud")
	}
}

// SaveImage 支持 width 和 height
func (d *D1Client) SaveImage(postID, fileID, originID, caption, tags, source string, width, height int) error {
	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/d1/database/%s/query",
		d.cfg.CF_AccountID, d.cfg.D1_DatabaseID)
	
	finalTags := fmt.Sprintf("%s %s", tags, source)
	
	// ⚠️ 请确保你在 D1 执行了: ALTER TABLE images ADD COLUMN width INTEGER; ALTER TABLE images ADD COLUMN height INTEGER;
	sql := "INSERT OR IGNORE INTO images (id, file_name, origin_id, caption, tags, created_at, width, height) VALUES (?, ?, ?, ?, ?, ?, ?, ?)"
	params := []interface{}{postID, fileID, originID, caption, finalTags, time.Now().Unix(), width, height}
	
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

// CheckExists 检查图片是否存在 (内存缓存 -> D1 实时查询)
// ✅ 请把这个方法加到 d1.go 的最后面
func (d *D1Client) CheckExists(postID string) bool {
	// 1. 第一道防线：查内存 (速度快)
	if d.History[postID] {
		return true
	}

	// 2. 第二道防线：实时查 D1 数据库 (准确)
	// 构造查询 SQL：只查是否存在，不查具体数据，效率高
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
		// 网络错误时，为了防止重复发送，这里怎么处理取决于你：
		// return false // 倾向于“宁可发重，不可漏发”
		// return true  // 倾向于“宁可漏发，不可发重”
		return false 
	}

	// 3. 解析结果
	// Cloudflare D1 的返回结果中，如果没有找到数据，results 字段是空的： "results":[]
	// 我们简单粗暴判断字符串即可，不用额外引入 encoding/json
	respStr := resp.String()
	
	// 去除空格防止格式差异
	cleanStr := strings.ReplaceAll(respStr, " ", "")
	
	// 如果包含 "results":[] 说明数据库里也没有 -> 返回 false
	if strings.Contains(cleanStr, "\"results\":[]") {
		return false
	}

	// 如果包含 "success":true 且 results 不为空 -> 说明数据库里有！
	if strings.Contains(cleanStr, "\"success\":true") {
		// 查到了！赶紧补回内存，下次就不用查网路了
		d.History[postID] = true
		return true
	}

	return false
}

