package yande

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type YandePostLink struct {
	ID        int    `json:"id"`
	ParentID  int    `json:"parent_id"`
	SampleURL string `json:"sample_url"`
	FileURL   string `json:"file_url"`
	FileSize  int    `json:"file_size"`
	Tags      string `json:"tags"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

//根据 ID 获取图片详情
func GetYandePost(id string) (*YandePostLink, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	
	url := fmt.Sprintf("https://yande.re/post.json?tags=id:%s", id)
	
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API status code: %d", resp.StatusCode)
	}

	var posts []YandePostLink
	if err := json.NewDecoder(resp.Body).Decode(&posts); err != nil {
		return nil, err
	}

	if len(posts) == 0 {
		return nil, fmt.Errorf("post not found: %s", id)
	}

	return &posts[0], nil
}

// 下载图片数据
func DownloadYandeImage(url string) ([]byte, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", "https://yande.re/")
	
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("download status %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

func SelectBestURL(post *YandePostLink) string {
	const MaxSize = 10 * 1024 * 1024 
	
	// 如果原图小于限制，优先原图
	if post.FileSize > 0 && post.FileSize < MaxSize {
		return post.FileURL
	}
	// 否 用大图
	if post.SampleURL != "" {
		return post.SampleURL
	}
	return post.FileURL
}
