package releasetest

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReleaseFinalization(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix Jenkins release helper")
	}
	script, err := os.ReadFile("../../scripts/finalize-avio-release.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"success", "advanced-master", "conflicting-tag"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			git := func(args ...string) string {
				t.Helper()
				c := exec.Command("git", args...)
				c.Dir = dir
				out, err := c.CombinedOutput()
				if err != nil {
					t.Fatalf("git %v: %s: %v", args, out, err)
				}
				return strings.TrimSpace(string(out))
			}
			git("init", "--bare", filepath.Join(dir, "remote.git"))
			git("init", "-b", "master")
			git("config", "user.name", "Test")
			git("config", "user.email", "test@example.invalid")
			os.Mkdir(filepath.Join(dir, "scripts"), 0755)
			os.WriteFile(filepath.Join(dir, "scripts/finalize-avio-release.sh"), script, 0755)
			os.WriteFile(filepath.Join(dir, "AVIO_VERSION"), []byte("0.37.0-avio\n"), 0644)
			git("add", "AVIO_VERSION", "scripts")
			git("commit", "-m", "Release")
			source := git("rev-parse", "HEAD")
			git("remote", "add", "origin", filepath.Join(dir, "remote.git"))
			git("push", "-u", "origin", "master")
			if scenario != "success" {
				git("commit", "--allow-empty", "-m", "Concurrent change")
				if scenario == "advanced-master" {
					git("push", "origin", "master")
				} else {
					git("tag", "v0.37.0-avio")
					git("push", "origin", "v0.37.0-avio")
				}
				git("checkout", "--detach", source)
			}
			c := exec.Command("bash", "scripts/finalize-avio-release.sh")
			c.Dir = dir
			c.Env = append(os.Environ(), "SOURCE_COMMIT="+source)
			out, err := c.CombinedOutput()
			if scenario != "success" {
				if err == nil {
					t.Fatalf("expected rejection: %s", out)
				}
				if got := git("show", "origin/master:AVIO_VERSION"); got != "0.37.0-avio" {
					t.Fatal("unexpected version bump")
				}
				return
			}
			if err != nil {
				t.Fatalf("finalize: %s: %v", out, err)
			}
			if got := git("rev-parse", "v0.37.0-avio^{commit}"); got != source {
				t.Fatal("tag does not point at built commit")
			}
			git("fetch", "origin", "master")
			if got := git("show", "origin/master:AVIO_VERSION"); got != "0.38.0-avio-SNAPSHOT" {
				t.Fatal(got)
			}
			if got := git("rev-parse", "origin/master^"); got != source {
				t.Fatal("bump is not a child of release")
			}
		})
	}
}
