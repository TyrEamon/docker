package twitter

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "log"
    "net/http"
    "regexp"
    "strings"

    "github.com/PuerkitoBio/goquery"
)

type Tweet struct {
    ID       string
    Text     string
    ImageURL string
    Width    int
    Height   int
}

func GetTweetWithCookie(url string, cookie string) (*Tweet, error) {
    req, err := http.NewRequest("GET", url, nil)
    if err != nil {
        return nil, err
    }
    req.Header.Set("Cookie", cookie)

    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    // 解析 HTML
    doc, err := goquery.NewDocumentFromReader(resp.Body)
    if err != nil {
        return nil, err
    }

    // 提取推文文本
    var text string
    doc.Find("meta[property='og:description']").Each(func(i int, s *goquery.Selection) {
        text = s.AttrOr("content", "")
    })

    // 提取图片链接
    var imageURL string
    doc.Find("meta[property='og:image']").Each(func(i int, s *goquery.Selection) {
        imageURL = s.AttrOr("content", "")
    })

    // 提取图片尺寸
    var width, height int
    doc.Find("meta[property='og:image:width']").Each(func(i int, s *goquery.Selection) {
        fmt.Sscanf(s.AttrOr("content", ""), "%d", &width)
    })
    doc.Find("meta[property='og:image:height']").Each(func(i int, s *goquery.Selection) {
        fmt.Sscanf(s.AttrOr("content", ""), "%d", &height)
    })

    // 提取推文 ID
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

func DownloadImage(imageURL string, cookie string) ([]byte, error) {
    req, err := http.NewRequest("GET", imageURL, nil)
    if err != nil {
        return nil, err
    }
    req.Header.Set("Cookie", cookie)

    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    return io.ReadAll(resp.Body)
}
