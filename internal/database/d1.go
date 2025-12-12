package database

import (
\t"fmt"
\t"log"
\t"my-bot-go/internal/config"
\t"strings"
\t"time"

\t"github.com/go-resty/resty/v2"
)

type D1Client struct {
\tclient  *resty.Client
\tcfg     *config.Config
\tHistory map[string]bool // 本地缓存的已发送ID
}

func NewD1Client(cfg *config.Config) *D1Client {
\treturn &D1Client{
\t\tclient:  resty.New(),
\t\tcfg:     cfg,
\t\tHistory: make(map[string]bool),
\t}
}

// SyncHistory 从 Worker 获取历史记录
func (d *D1Client) SyncHistory() {
\tif d.cfg.WorkerURL == "" {
\t\treturn
\t}
\tresp, err := d.client.R().Get(d.cfg.WorkerURL + "/api/get_history")
\tif err != nil {
\t\tlog.Printf("⚠️ Sync history failed: %v", err)
\t\treturn
\t}
\t
\tids := strings.Split(string(resp.Body()), ",")
\tfor _, id := range ids {
\t\tif strings.TrimSpace(id) != "" {
\t\t\td.History[id] = true
\t\t}
\t}
\tlog.Printf("🧠 Synced %d items from history", len(d.History))
}

// PushHistory 上传历史记录到 Worker
func (d *D1Client) PushHistory() {
\tif d.cfg.WorkerURL == "" {
\t\treturn
\t}
\tvar idList []string
\tfor id := range d.History {
\t\tidList = append(idList, id)
\t}
\tdata := strings.Join(idList, ",")
\t
\t_, err := d.client.R().
\t\tSetBody(data).
\t\tPost(d.cfg.WorkerURL + "/api/update_history")
\t\t
\tif err != nil {
\t\tlog.Printf("⚠️ Push history failed: %v", err)
\t} else {
\t\tlog.Println("☁️ History updated to cloud")
\t}
}

// SaveImage 写入 D1 数据库
func (d *D1Client) SaveImage(postID, fileID, caption, tags, source string) error {
\turl := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/d1/database/%s/query", 
\t\td.cfg.CF_AccountID, d.cfg.D1_DatabaseID)
\t
\tfinalTags := fmt.Sprintf("%s %s", tags, source)
\tsql := "INSERT OR IGNORE INTO images (id, file_name, caption, tags, created_at) VALUES (?, ?, ?, ?, ?)"
\tparams := []interface{}{postID, fileID, caption, finalTags, time.Now().Unix()}
\t
\tbody := map[string]interface{}{
\t\t"sql":    sql,
\t\t"params": params,
\t}

\tresp, err := d.client.R().
\t\tSetHeader("Authorization", "Bearer "+d.cfg.CF_APIToken).
\t\tSetHeader("Content-Type", "application/json").
\t\tSetBody(body).
\t\tPost(url)

\tif err != nil {
\t\treturn err
\t}
\tif resp.IsError() {
\t\treturn fmt.Errorf("D1 Error: %s", resp.String())
\t}
\t
\t// 更新本地缓存
\td.History[postID] = true
\treturn nil
}