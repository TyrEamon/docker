package twitter

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

type Tweet struct {
	ID       string
	Text     string
	ImageURL string
	Width    int
	Height   int
}

// 内部结构体，用于解析 GraphQL JSON
type tweetDetailResp struct {
	Data struct {
		TweetResult struct {
			Result struct {
				Legacy struct {
					FullText string `json:"full_text"`
					Entities struct {
						Media []struct {
							MediaURLHTTPS string `json:"media_url_https"`
							Type          string `json:"type"`
							OriginalInfo  struct {
								Width  int `json:"width"`
								Height int `json:"height"`
							} `json:"original_info"`
						} `json:"media"`
					} `json:"entities"`
				} `json:"legacy"`
				// 有时候结构在 NoteTweet 里（长推文）
				NoteTweet struct {
					NoteTweetResults struct {
						Result struct {
							Text string `json:"text"`
						} `json:"result"`
					} `json:"note_tweet_results"`
				} `json:"note_tweet"`
			} `json:"result"`
		} `json:"tweetResult"`
	} `json:"data"`
}

// GetTweetWithCookie 通过 X 的内部 GraphQL API 获取推文信息
// ✅ 修改：增加 ct0 参数，用于通过 API 的 CSRF 校验
func GetTweetWithCookie(url string, cookie string, ct0 string) (*Tweet, error) {
	// 1. 从 URL 提取推文 ID
	re := regexp.MustCompile(`status/(\d+)`)
	matches := re.FindStringSubmatch(url)
	if len(matches) < 2 {
		return nil, fmt.Errorf("invalid twitter url")
	}
	tweetID := matches[1]

	// 2. 构造 GraphQL API 请求
	// 这是 X 网页版通用的 TweetDetail 接口参数
	apiURL := "https://x.com/i/api/graphql/s-C-O-qC8fqNkQ8qV_JgNA/TweetDetail?variables=%7B%22focalTweetId%22%3A%22" + tweetID + "%22%2C%22with_rux_injections%22%3Afalse%2C%22includePromotedContent%22%3Atrue%2C%22withCommunity%22%3Atrue%2C%22withQuickPromoteEligibilityTweetFields%22%3Atrue%2C%22withBirdwatchNotes%22%3Atrue%2C%22withVoice%22%3Atrue%2C%22withV2Timeline%22%3Atrue%7D&features=%7B%22rweb_lists_timeline_redesign_enabled%22%3Atrue%2C%22responsive_web_graphql_exclude_directive_enabled%22%3Atrue%2C%22verified_phone_label_enabled%22%3Afalse%2C%22creator_subscriptions_tweet_preview_api_enabled%22%3Atrue%2C%22responsive_web_graphql_timeline_navigation_enabled%22%3Atrue%2C%22responsive_web_graphql_skip_user_profile_image_extensions_enabled%22%3Afalse%2C%22tweetypie_unmention_optimization_enabled%22%3Atrue%2C%22responsive_web_edit_tweet_api_enabled%22%3Atrue%2C%22graphql_is_translatable_rweb_tweet_is_translatable_enabled%22%3Atrue%2C%22view_counts_everywhere_api_enabled%22%3Atrue%2C%22longform_notetweets_consumption_enabled%22%3Atrue%2C%22responsive_web_twitter_article_tweet_consumption_enabled%22%3Afalse%2C%22tweet_awards_web_tipping_enabled%22%3Afalse%2C%22freedom_of_speech_not_reach_fetch_enabled%22%3Atrue%2C%22standardized_nudges_misinfo%22%3Atrue%2C%22tweet_with_visibility_results_prefer_gql_limited_actions_policy_enabled%22%3Atrue%2C%22longform_notetweets_rich_text_read_enabled%22%3Atrue%2C%22longform_notetweets_inline_media_enabled%22%3Atrue%2C%22responsive_web_media_download_video_enabled%22%3Afalse%2C%22responsive_web_enhance_cards_enabled%22%3Afalse%7D"

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	// 清理 Cookie
	cleanCookie := strings.TrimSpace(cookie)
	req.Header.Set("Cookie", cleanCookie)
	// 伪装成浏览器
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36")
	
	// ⚠️ 必须带 Authorization 和 X-Csrf-Token
	// 这是一个通用的 Guest Token (长期有效)
	req.Header.Set("Authorization", "Bearer AAAAAAAAAAAAAAAAAAAAANRILgAAAAAAnNwIzUejRCOuH5E6I8xnZz4puTs%3D1Zv7ttfk8LF81IUq16cHjhLTvJu4FA33AGWWjCpTnA")
	
	// ✅ 使用传入的 ct0，不再自动提取，防止 403
	if ct0 != "" {
		req.Header.Set("x-csrf-token", ct0)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("api status: %d", resp.StatusCode)
	}

	// 3. 解析 JSON
	var data tweetDetailResp
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	// 空指针保护
	if data.Data.TweetResult.Result.Legacy.FullText == "" && data.Data.TweetResult.Result.NoteTweet.NoteTweetResults.Result.Text == "" {
		// 有可能遇到敏感内容被折叠，或者是结构体层级不对
		// 这里简单处理，如果没数据就报错
		// fmt.Println("Debug JSON:", data) // 调试用
		return nil, fmt.Errorf("api returned empty result (possibly suspended or sensitive content?)")
	}

	result := data.Data.TweetResult.Result
	
	// 提取文本
	text := result.Legacy.FullText
	if text == "" && result.NoteTweet.NoteTweetResults.Result.Text != "" {
		text = result.NoteTweet.NoteTweetResults.Result.Text
	}

	// 4. 提取第一张图片
	var imageURL string
	var width, height int
	
	if len(result.Legacy.Entities.Media) > 0 {
		for _, m := range result.Legacy.Entities.Media {
			if m.Type == "photo" {
				imageURL = m.MediaURLHTTPS
				width = m.OriginalInfo.Width
				height = m.OriginalInfo.Height
				break // 目前代码逻辑只支持单图，取第一张
			}
		}
	}

	if imageURL == "" {
		return nil, fmt.Errorf("no image found in API response")
	}

	return &Tweet{
		ID:       tweetID,
		Text:     text,
		ImageURL: imageURL,
		Width:    width,
		Height:   height,
	}, nil
}

// DownloadImage 下载图片，强制使用 :orig 获取最高清原图
func DownloadImage(imageURL string, cookie string) ([]byte, error) {
	if imageURL == "" {
		return nil, fmt.Errorf("imageURL is empty")
	}

	// 🎨 优化：强制请求原图 (:orig)
    // 1. 如果 URL 已经包含参数（比如 ?format=jpg&name=xxx），先尝试去掉参数拿到纯净的 .jpg 结尾
    if strings.Contains(imageURL, "?") {
        parts := strings.Split(imageURL, "?")
        imageURL = parts[0]
    }
    
    // 2. 如果 URL 结尾没有 :orig，就加上它
    if !strings.HasSuffix(imageURL, ":orig") {
        imageURL = imageURL + ":orig"
    }

	req, err := http.NewRequest("GET", imageURL, nil)
	if err != nil {
		return nil, err
	}
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
