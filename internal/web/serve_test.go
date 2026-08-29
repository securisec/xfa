package web

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestServeBindsLocalhostAndShutsDown(t *testing.T) {
	s := openTemp(t)
	var buf strings.Builder
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	// synchronize on the banner: Serve must print the URL before serving
	urlCh := make(chan string, 1)
	go func() {
		done <- Serve(ctx, s, Options{Out: &bannerTee{&buf, urlCh}})
	}()
	var url string
	select {
	case url = <-urlCh:
	case <-time.After(5 * time.Second):
		t.Fatal("no URL banner")
	}
	m := regexp.MustCompile(`http://127\.0\.0\.1:\d+/`).FindString(url)
	if m == "" {
		t.Fatalf("banner without local URL: %q", url)
	}
	resp, err := http.Get(m + "api/me")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("live server: %v %v", err, resp)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not shut down on ctx cancel")
	}
}

// bannerTee forwards writes and signals the first line.
type bannerTee struct {
	w  io.Writer
	ch chan string
}

func (b *bannerTee) Write(p []byte) (int, error) {
	select {
	case b.ch <- string(p):
	default:
	}
	return fmt.Fprintf(b.w, "%s", p)
}
