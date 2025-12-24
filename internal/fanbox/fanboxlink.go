package fanbox

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// FanboxPost 结构
type FanboxPost struct {
	ID      string
	Title   string
	Images  []FanboxImage
	Tags    []string
	Author  string
}

// FanboxImage 图片信息
type FanboxImage struct {
	URL    string
	Width  int
	Height int
}

// GetFanboxPost 获取 Fanbox 帖子详情
func GetFanboxPost(postID string, cookie string) (*FanboxPost, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	// Fanbox API: https://api.fanbox.cc/post.info?postId=1234567890
	url := fmt.Sprintf("https://api.fanbox.cc/post.info?postId=%s", postID)
	
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Cookie", cookie) // 需要 Fanbox Cookie
	req.Header.Set("Origin", "https://*.fanbox.cc")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var apiResp struct {
		Body struct {
			Title      string   `json:"title"`
			Images     []struct {
				Extension string `json:"extension"`
				Path      string `json:"path"`
			} `json:"images"`
			Tags  []string `json:"tags"`
			Creator struct {
				Name string `json:"name"`
			} `json:"creator"`
		} `json:"body"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}

	post := &FanboxPost{
		ID:     postID,
		Title:  apiResp.Body.Title,
		Author: apiResp.Body.Creator.Name,
		Tags:   apiResp.Body.Tags,
	}

	// 解析图片 URL
	for _, img := range apiResp.Body.Images {
		post.Images = append(post.Images, FanboxImage{
			URL:    fmt.Sprintf("https://storage.fanbox.cc/%s.%s", img.Path, img.Extension),
			Width:  0,  // Fanbox API 不提供，需要下载后获取或估算
			Height: 0,
		})
	}

	return post, nil
}

// DownloadFanboxImage 下载图片
func DownloadFanboxImage(url, cookie string) ([]byte, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", "https://*.fanbox.cc/")
	req.Header.Set("Cookie", cookie)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}
