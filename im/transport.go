package im

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Transport 打一次 imapi 读接口（x-protobuf，仅 Cookie 鉴权）。
// HTTPClient 可选：注入带 Chrome-TLS 指纹的客户端抗风控；nil=内置带 15s 超时客户端。
// 库不持有、不落盘 Cookie；由调用方逐请求传入。
type Transport struct{ HTTPClient *http.Client }

func (t *Transport) Post(ctx context.Context, host, path string, body []byte, cookie string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, host+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("Accept", "application/x-protobuf")
	req.Header.Set("User-Agent", UA)
	req.Header.Set("Referer", "https://www.douyin.com/")
	req.Header.Set("Origin", "https://www.douyin.com")
	req.Header.Set("Cookie", cookie)
	client := t.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("imapi %s 返回 %d", path, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
