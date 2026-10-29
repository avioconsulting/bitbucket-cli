package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublication(t *testing.T) {
	const version = "0.37.0-avio-SNAPSHOT"
	build := "jenkins-1-" + strings.Repeat("a", 40)
	for _, scenario := range []string{"success-and-retry", "conflict", "authentication", "missing-receipt", "corrupt-upload", "redirect"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			for _, target := range []string{"linux-x64", "darwin-arm64"} {
				name := "bkt-native-" + target + "-" + version + ".tar.gz"
				data := []byte(target)
				if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
					t.Fatal(err)
				}
				if scenario == "missing-receipt" && target == "darwin-arm64" {
					continue
				}
				if err := os.WriteFile(filepath.Join(dir, name+".sha256"), []byte(digest(data)+"  "+name+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			stored := map[string][]byte{}
			puts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-token" {
					t.Error("missing authentication")
				}
				if scenario == "authentication" {
					w.WriteHeader(403)
					return
				}
				if scenario == "redirect" {
					w.Header().Set("Location", "/unexpected")
					w.WriteHeader(302)
					return
				}
				if r.Method == "HEAD" {
					if scenario == "conflict" {
						w.Header().Set("X-Checksum-Sha256", digest([]byte("different")))
					} else if data, ok := stored[r.URL.Path]; ok {
						w.Header().Set("X-Checksum-Sha256", digest(data))
					} else {
						w.WriteHeader(404)
					}
					return
				}
				puts++
				data, _ := io.ReadAll(r.Body)
				if strings.HasSuffix(r.URL.Path, "snapshot.properties") && len(stored) != 4 {
					t.Error("premature channel promotion")
				}
				if scenario == "corrupt-upload" {
					data = []byte("corrupted")
				}
				stored[r.URL.Path] = data
				w.WriteHeader(201)
			}))
			defer server.Close()
			client := server.Client()
			client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
			p := publisher{server.URL, "", "test-token", client}
			err := p.publish(dir, version, build)
			if scenario == "success-and-retry" {
				if err != nil {
					t.Fatal(err)
				}
				if err := p.publish(dir, version, build); err != nil {
					t.Fatal(err)
				}
				if puts != 5 {
					t.Fatalf("retry wrote artifacts: %d PUTs", puts)
				}
				if !strings.Contains(string(stored["/channels/snapshot.properties"]), "path=builds/"+version+"/"+build+"\n") {
					t.Fatal("channel does not pin build")
				}
			} else {
				if err == nil {
					t.Fatal("expected publication failure")
				}
				if _, exists := stored["/channels/snapshot.properties"]; exists {
					t.Fatal("failed publication promoted channel")
				}
				if scenario != "corrupt-upload" && puts != 0 {
					t.Fatal("upload after prerequisite failure")
				}
			}
		})
	}
}
