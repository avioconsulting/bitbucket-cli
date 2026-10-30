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
	for _, version := range []string{"0.37.0-avio-SNAPSHOT", "0.37.0-avio"} {
		channel, expectedPuts := "snapshot", 3
		if !strings.HasSuffix(version, "-SNAPSHOT") {
			channel, expectedPuts = "stable", 4
		}
		build := "jenkins-1-" + strings.Repeat("a", 40)
		for _, scenario := range []string{"success-and-retry", "conflict", "authentication", "missing-receipt", "corrupt-upload", "corrupt-remote-receipt", "redirect"} {
			t.Run(version+"/"+scenario, func(t *testing.T) {
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
					if r.Method == "GET" && strings.HasSuffix(r.URL.Path, ".sha256") {
						data, ok := stored[strings.TrimSuffix(r.URL.Path, ".sha256")]
						if !ok {
							w.WriteHeader(404)
							return
						}
						if scenario == "corrupt-remote-receipt" {
							data = []byte("wrong")
						}
						// Artifactory generates this body without a checksum-of-checksum header.
						io.WriteString(w, digest(data))
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
					if strings.HasSuffix(r.URL.Path, "channels/"+channel+".properties") && len(stored) != expectedPuts-1 {
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
					if puts != expectedPuts {
						t.Fatalf("retry wrote artifacts: %d PUTs", puts)
					}
					if !strings.Contains(string(stored["/channels/"+channel+".properties"]), "path=builds/"+version+"/"+build+"\n") {
						t.Fatal("channel does not pin build")
					}
					if channel == "stable" {
						before := string(stored["/channels/stable.properties"])
						if err := p.publish(dir, version, "jenkins-2-"+strings.Repeat("b", 40)); err == nil {
							t.Fatal("stable version reused by another build")
						}
						if string(stored["/channels/stable.properties"]) != before {
							t.Fatal("conflicting release promoted")
						}
					}
				} else {
					if err == nil {
						t.Fatal("expected publication failure")
					}
					if _, exists := stored["/channels/"+channel+".properties"]; exists {
						t.Fatal("failed publication promoted channel")
					}
					if scenario != "corrupt-upload" && scenario != "corrupt-remote-receipt" && puts != 0 {
						t.Fatal("upload after prerequisite failure")
					}
				}
			})
		}
	}
}
