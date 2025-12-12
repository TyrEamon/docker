package crawler

import (
\t"context"
\t"encoding/json"
\t"fmt"
\t"log"
\t"my-bot-go/internal/config"
\t"my-bot-go/internal/database"
\t"my-bot-go/internal/telegram"
\t"sort"
\t"strconv"
\t"strings"
\t"time"

\t"github.com/go-resty/resty/v2"
)

func StartPixiv(ctx context.Context, cfg *config.Config, db *database.D1Client, bot *telegram.BotHandler) {
\tclient := resty.New()
\tclient.SetHeader("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
\tclient.SetHeader("Referer", "https://www.pixiv.net/")
\tclient.SetHeader("Cookie", "PHPSESSID="+cfg.PixivPHPSESSID)

\tfor {
\t\tselect {
\t\tcase <-ctx.Done():
\t\t\treturn
\t\tdefault:
\t\t\tlog.Println("🍪 Checking Pixiv (Cookie Mode)...")
\t\t\thasNew := false

\t\t\tfor _, uid := range cfg.PixivArtistIDs {
\t\t\t\t// 1. 获取画师作品
\t\t\t\tresp, err := client.R().Get(fmt.Sprintf("https://www.pixiv.net/ajax/user/%s/profile/all", uid))
\t\t\t\tif err != nil || resp.StatusCode() != 200 {
\t\t\t\t\tlog.Printf("⚠️ Pixiv User %s Error", uid)
\t\t\t\t\tcontinue
\t\t\t\t}

\t\t\t\tvar profile struct {
\t\t\t\t\tBody struct {
\t\t\t\t\t\tIllusts map[string]interface{} `json:"illusts"`
\t\t\t\t\t} `json:"body"`
\t\t\t\t}
\t\t\t\tjson.Unmarshal(resp.Body(), &profile)

\t\t\t\t// 提取 ID 并排序
\t\t\t\tvar ids []int
\t\t\t\tfor k := range profile.Body.Illusts {
\t\t\t\t\tif id, err := strconv.Atoi(k); err == nil {
\t\t\t\t\t\tids = append(ids, id)
\t\t\t\t\t}
\t\t\t\t}
\t\t\t\t// 降序排列 (最新的在前)
\t\t\t\tsort.Sort(sort.Reverse(sort.IntSlice(ids)))

\t\t\t\t// 取前 N 个
\t\t\t\tcount := 0
\t\t\t\tfor _, id := range ids {
\t\t\t\t\tif count >= cfg.PixivLimit {
\t\t\t\t\t\tbreak
\t\t\t\t\t}
\t\t\t\t\tpid := fmt.Sprintf("pixiv_%d", id)

\t\t\t\t\t// 去重检查
\t\t\t\t\tif db.History[pid] {
\t\t\t\t\t\tcontinue
\t\t\t\t\t}

\t\t\t\t\t// 2. 获取详情
\t\t\t\t\tdetailResp, err := client.R().Get(fmt.Sprintf("https://www.pixiv.net/ajax/illust/%d", id))
\t\t\t\t\tif err != nil {
\t\t\t\t\t\tcontinue
\t\t\t\t\t}

\t\t\t\t\t// 解析 JSON (这里用 map 偷懒，不用定义超长结构体)
\t\t\t\t\tvar detail map[string]interface{}
\t\t\t\t\tjson.Unmarshal(detailResp.Body(), &detail)
\t\t\t\t\t
\t\t\t\t\tbody, ok := detail["body"].(map[string]interface{})
\t\t\t\t\tif !ok { continue }

\t\t\t\t\ttitle := body["illustTitle"].(string)
\t\t\t\t\tuserName := body["userName"].(string)
\t\t\t\t\turls := body["urls"].(map[string]interface{})
\t\t\t\t\toriginalURL := urls["original"].(string)

\t\t\t\t\t// Tags 处理
\t\t\t\t\ttagsObj := body["tags"].(map[string]interface{})
\t\t\t\t\ttagsList := tagsObj["tags"].([]interface{})
\t\t\t\t\tvar tagStrs []string
\t\t\t\t\tfor _, t := range tagsList {
\t\t\t\t\t\ttData := t.(map[string]interface{})
\t\t\t\t\t\ttagStrs = append(tagStrs, tData["tag"].(string))
\t\t\t\t\t}
\t\t\t\t\ttagsStr := strings.Join(tagStrs, " ")

\t\t\t\t\t// 下载
\t\t\t\t\tlog.Printf("⬇️ Downloading Pixiv: %s", title)
\t\t\t\t\timgResp, err := client.R().Get(originalURL)
\t\t\t\t\tif err == nil && imgResp.StatusCode() == 200 {
\t\t\t\t\t\tcaption := fmt.Sprintf("Pixiv: %s
Artist: %s
Tags: #%s", title, userName, strings.ReplaceAll(tagsStr, " ", " #"))
\t\t\t\t\t\tbot.ProcessAndSend(ctx, imgResp.Body(), pid, tagsStr, caption, "pixiv")
\t\t\t\t\t\thasNew = true
\t\t\t\t\t\tcount++
\t\t\t\t\t}
\t\t\t\t\ttime.Sleep(2 * time.Second)
\t\t\t\t}
\t\t\t}

\t\t\tif hasNew {
\t\t\t\tdb.PushHistory()
\t\t\t}
\t\t\t
\t\t\tlog.Println("😴 Pixiv Done. Sleeping 10m...")
\t\t\ttime.Sleep(10 * time.Minute)
\t\t}
\t}
}