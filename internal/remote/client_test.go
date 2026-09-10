package remote

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestForwardRoundTrip(t *testing.T) {
	var got Request
	var gotPath, gotCT, gotOrigin string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotCT, gotOrigin = r.URL.Path, r.Header.Get("Content-Type"), r.Header.Get("Origin")
		json.NewDecoder(r.Body).Decode(&got)
		json.NewEncoder(w).Encode(Response{Stdout: "hi\n", Code: 3})
	}))
	defer srv.Close()
	resp, err := Forward(srv.URL, "read", Request{Args: []string{"--json"}, Cwd: "/p", Handle: "amber-otter-1", Stdin: []byte("x")}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/read" || gotCT != "application/json" || gotOrigin != "" {
		t.Fatalf("path=%q ct=%q origin=%q", gotPath, gotCT, gotOrigin)
	}
	if got.Cwd != "/p" || got.Handle != "amber-otter-1" || string(got.Stdin) != "x" || len(got.Args) != 1 {
		t.Fatalf("request = %+v", got)
	}
	if resp.Stdout != "hi\n" || resp.Code != 3 {
		t.Fatalf("response = %+v", resp)
	}
	// A trailing slash on the base must not yield "//v1/read": the real mux
	// 307s that to the clean form, and Forward never follows redirects.
	if _, err := Forward(srv.URL+"/", "read", Request{Cwd: "/p"}, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/read" {
		t.Fatalf("trailing-slash base path = %q", gotPath)
	}
}

func TestForwardErrors(t *testing.T) {
	_, err := Forward("http://127.0.0.1:1", "read", Request{}, 500*time.Millisecond)
	if err == nil || !strings.HasPrefix(err.Error(), "cannot reach xfa server http://127.0.0.1:1") {
		t.Fatalf("dead: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		json.NewEncoder(w).Encode(map[string]string{"error": `does not accept "nope"`})
	}))
	defer srv.Close()
	_, err = Forward(srv.URL, "nope", Request{}, time.Second)
	if err == nil || err.Error() != "xfa server "+srv.URL+`: does not accept "nope"` {
		t.Fatalf("404: %v", err)
	}
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(502)
		w.Write([]byte("<html>bad gateway</html>"))
	}))
	defer srv2.Close()
	_, err = Forward(srv2.URL, "read", Request{}, time.Second)
	if err == nil || err.Error() != "xfa server "+srv2.URL+": unexpected status 502" {
		t.Fatalf("502: %v", err)
	}
	srv3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"stdout":"abc`))
	}))
	defer srv3.Close()
	_, err = Forward(srv3.URL, "read", Request{}, time.Second)
	if err == nil || !strings.HasPrefix(err.Error(), "cannot reach xfa server "+srv3.URL) {
		t.Fatalf("truncated: %v", err)
	}
	srv4 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1:1/", http.StatusFound)
	}))
	defer srv4.Close()
	_, err = Forward(srv4.URL, "read", Request{}, time.Second)
	if err == nil || err.Error() != "xfa server "+srv4.URL+": unexpected status 302" {
		t.Fatalf("redirect: %v", err)
	}
}

func TestIsVerb(t *testing.T) {
	for _, v := range []string{"read", "hook", "project-register", "session"} {
		if !IsVerb(v) {
			t.Errorf("%s should be a verb", v)
		}
	}
	for _, v := range []string{"init", "uninstall", "reset", "tui", "serve", "help", "completion", "__complete", ""} {
		if IsVerb(v) {
			t.Errorf("%s must not be a verb", v)
		}
	}
}
