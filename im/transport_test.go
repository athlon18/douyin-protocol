package im

import (
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
	if got.Get("Content-Type") != "application/x-protobuf" || got.Get("Cookie") != "sid=1" || got.Get("User-Agent") != UA {
		t.Errorf("headers wrong: ct=%q cookie=%q ua=%q", got.Get("Content-Type"), got.Get("Cookie"), got.Get("User-Agent"))
	}
}
