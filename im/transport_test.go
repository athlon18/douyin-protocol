package im

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTransportPostHeaders(t *testing.T) {
	var got http.Header
	var bodySeen []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		bodySeen, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	// 注入指向测试服务
	host := "http://" + srv.Listener.Addr().String()
	out, err := (&Transport{}).Post(context.Background(), host, "/x", []byte("body"), "sid=1")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "ok" || string(bodySeen) != "body" {
		t.Errorf("body/out mismatch: %q", out)
	}
	// 逐个断言官方口径头（回归任一头都要被抓到）
	checks := []struct {
		key, want string
	}{
		{"Content-Type", "application/x-protobuf"},
		{"Accept", "application/x-protobuf"},
		{"Cookie", "sid=1"},
		{"User-Agent", UA},
		{"Referer", "https://www.douyin.com/"},
		{"Origin", "https://www.douyin.com"},
	}
	for _, c := range checks {
		if v := got.Get(c.key); v != c.want {
			t.Errorf("header %s = %q, want %q", c.key, v, c.want)
		}
	}
}

// recordingRoundTripper 记录自己是否被调用，并合成一个响应。
// 用它包出的 *http.Client 一旦被 Transport.Post 使用，calls 必然 +1；
// 若 Post 忽略注入的 HTTPClient、改用内置默认客户端，则此 RoundTripper
// 永不被调用（且默认客户端会真的去连不可达 host 而报错），本用例即失败。
type recordingRoundTripper struct {
	calls   int
	lastReq *http.Request
}

func (rt *recordingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.calls++
	// RoundTripper 契约：不得改动入参 req，克隆一份留存。
	rt.lastReq = req.Clone(req.Context())
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader([]byte("injected-ok"))),
		Request:    req,
	}, nil
}

// TestTransportUsesInjectedHTTPClient 证明注入的 HTTPClient 真正生效，
// 而非只是字段存在。回归点：若 Post 里写成 client := &http.Client{...}
// （忽略 t.HTTPClient），本用例必红。
func TestTransportUsesInjectedHTTPClient(t *testing.T) {
	rt := &recordingRoundTripper{}
	injected := &http.Client{Transport: rt}
	tr := &Transport{HTTPClient: injected}

	// 用一个必然不可达的 host：只有走注入的 RoundTripper 才不会真发起网络请求。
	out, err := tr.Post(context.Background(), "https://injected.invalid", "/x", []byte("body"), "sid=9")
	if err != nil {
		t.Fatalf("Post 报错，说明未走注入客户端（默认客户端连不可达 host 会失败）: %v", err)
	}
	if rt.calls != 1 {
		t.Fatalf("注入的 HTTPClient 未被使用：RoundTrip 调用次数 = %d, want 1（回归：Post 忽略了 t.HTTPClient）", rt.calls)
	}
	if string(out) != "injected-ok" {
		t.Errorf("响应体应来自注入客户端: got %q, want %q", out, "injected-ok")
	}
	// 注入的 RoundTripper 必须看到 Post 组装好的完整请求（含头与目标 URL）。
	if rt.lastReq == nil {
		t.Fatal("注入客户端未观察到请求")
	}
	if rt.lastReq.URL.String() != "https://injected.invalid/x" {
		t.Errorf("注入客户端看到的 URL = %q, want %q", rt.lastReq.URL.String(), "https://injected.invalid/x")
	}
	if rt.lastReq.Method != http.MethodPost {
		t.Errorf("注入客户端看到的方法 = %q, want POST", rt.lastReq.Method)
	}
	if got := rt.lastReq.Header.Get("Cookie"); got != "sid=9" {
		t.Errorf("注入客户端看到的 Cookie = %q, want %q", got, "sid=9")
	}
	if got := rt.lastReq.Header.Get("Content-Type"); got != "application/x-protobuf" {
		t.Errorf("注入客户端看到的 Content-Type = %q, want application/x-protobuf", got)
	}
}
