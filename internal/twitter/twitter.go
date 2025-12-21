package twitter

import (
    "fmt"
    "io"
    "net/http"
    "regexp"
    "strings" // 新增引入

    "github.com/PuerkitoBio/goquery"
)

// ... Tweet struct 保持不变 ...
type Tweet struct {
	ID       string
	Text     string
	ImageURL string
	Width    int
	Height   int
}

func GetTweetWithCookie(url string, cookie string) (*Tweet, error) {
    // 1. 构造请求
    req, err := http.NewRequest("GET", url, nil)
    if err != nil {
        return nil, err
    }
    req.Header.Set("Cookie", cookie)
    
    // 💡 关键修改：尝试使用 Facebook 的爬虫 UA，有时 X 会给它完整的 meta 标签
    // 或者保持你原来的 Chrome UA，但要确保 Cookie 是有效的
    req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)") 
    // 或者试下: "facebookexternalhit/1.1 (+http://www.facebook.com/externalhit_uatext.php)"

    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    if resp.StatusCode != 200 {
        return nil, fmt.Errorf("http status: %d", resp.StatusCode)
    }

    // 2. 解析 HTML
    doc, err := goquery.NewDocumentFromReader(resp.Body)
    if err != nil {
        return nil, err
    }

    // 3. 提取推文文本
    var text string
    // 优先尝试 og:description
    doc.Find("meta[property='og:description']").Each(func(i int, s *goquery.Selection) {
        if text == "" { text = s.AttrOr("content", "") }
    })
    // 备选: twitter:description
    if text == "" {
         doc.Find("meta[name='twitter:description']").Each(func(i int, s *goquery.Selection) {
            if text == "" { text = s.AttrOr("content", "") }
        })
    }

    // 4. 提取图片链接 (增强版)
    var imageURL string
    // 尝试 og:image
    doc.Find("meta[property='og:image']").Each(func(i int, s *goquery.Selection) {
        if imageURL == "" { imageURL = s.AttrOr("content", "") }
    })
    // 💡 新增：尝试 twitter:image
    if imageURL == "" {
        doc.Find("meta[name='twitter:image']").Each(func(i int, s *goquery.Selection) {
            if imageURL == "" { imageURL = s.AttrOr("content", "") }
        })
    }
    // 💡 新增：尝试从 Twitter 页面特有的 URL 结构直接拼凑（如果 meta 全挂了）
    // 注意：这招通常只对旧版页面有效，现在全是 React 很难拼，但可以尝试解析 JSON（太复杂了先不加）
    
    // 检查是否拿到了默认头像或者占位图，过滤掉
    if strings.Contains(imageURL, "profile_images") {
        // 有时候提取到的是头像不是推文图，置空重试
        // imageURL = "" 
        // 暂时先不置空，头像也比空强，或者你可以选择严格模式
    }

    if imageURL == "" {
        // 🚨 调试信息：如果还是空，可能是 HTML 根本没渲染
        // 可以让 Bot 返回更详细的错误，比如 title 看看是不是 verify 页面
        title := doc.Find("title").Text()
        return nil, fmt.Errorf("no image found. Page Title: %s", strings.TrimSpace(title))
    }

    // ... 宽高的提取逻辑保持不变 ...
    var width, height int
    doc.Find("meta[property='og:image:width']").Each(func(i int, s *goquery.Selection) {
        if width == 0 { fmt.Sscanf(s.AttrOr("content", ""), "%d", &width) }
    })
    doc.Find("meta[property='og:image:height']").Each(func(i int, s *goquery.Selection) {
        if height == 0 { fmt.Sscanf(s.AttrOr("content", ""), "%d", &height) }
    })

    // ... ID 提取逻辑保持不变 ...
    var id string
    re := regexp.MustCompile(`status/(\d+)`)
    matches := re.FindStringSubmatch(url)
    if len(matches) > 1 {
        id = matches[1]
    }

    return &Tweet{
        ID:       id,
        Text:     text,
        ImageURL: imageURL,
        Width:    width,
        Height:   height,
    }, nil
}

// DownloadImage 保持你原来的样子即可
func DownloadImage(imageURL string, cookie string) ([]byte, error) {
    if imageURL == "" {
        return nil, fmt.Errorf("imageURL is empty")
    }
    req, err := http.NewRequest("GET", imageURL, nil)
    if err != nil {
        return nil, err
    }
    // 下载图片通常不需要 cookie，但带着也无妨，有些图床可能有防盗链
    req.Header.Set("User-Agent", "Mozilla/5.0") 
    
    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    
    if resp.StatusCode != 200 {
         return nil, fmt.Errorf("download status: %d", resp.StatusCode)
    }

    return io.ReadAll(resp.Body)
}
