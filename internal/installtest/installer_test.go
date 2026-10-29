package installtest

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBootstrap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix installer")
	}
	script, err := filepath.Abs("../../scripts/install/bkt.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"install-update", "checksum", "metadata", "duplicate-manifest", "traversal", "symlink", "download-failure"} {
		t.Run(scenario, func(t *testing.T) {
			work := t.TempDir()
			home := filepath.Join(work, "home with spaces")
			mock := filepath.Join(work, "mock")
			for _, dir := range []string{home, mock, filepath.Join(home, ".local/bin")} {
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
			}
			write := func(path, content string, mode os.FileMode) {
				t.Helper()
				if err := os.WriteFile(path, []byte(content), mode); err != nil {
					t.Fatal(err)
				}
			}
			write(filepath.Join(mock, "uname"), "#!/bin/sh\ncase $1 in -s) echo Linux ;; -m) echo x86_64 ;; esac\n", 0755)
			write(filepath.Join(mock, "curl"), `#!/bin/sh
cat >/dev/null
out=''
while [ "$#" -gt 0 ]; do
    case "$1" in --output) out=$2; shift 2 ;; *) url=$1; shift ;; esac
done
[ "$SCENARIO" != download-failure ] || exit 22
case "$url" in
    */snapshot.properties) cp "$FIXTURES/channel" "$out" ;;
    *.tar.gz) cp "$FIXTURES/archive.tar.gz" "$out" ;;
    *) exit 1 ;;
esac
`, 0755)
			const version = "0.37.0-avio-SNAPSHOT"
			commit := strings.Repeat("a", 40)
			root := "bkt-" + version + "/"
			archive := filepath.Join(work, "archive.tar.gz")
			file, err := os.Create(archive)
			if err != nil {
				t.Fatal(err)
			}
			gz := gzip.NewWriter(file)
			tw := tar.NewWriter(gz)
			add := func(name, content string, mode int64) {
				t.Helper()
				if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(content))}); err != nil {
					t.Fatal(err)
				}
				if _, err := tw.Write([]byte(content)); err != nil {
					t.Fatal(err)
				}
			}
			add(root+"bin/bkt", "#!/bin/sh\necho 'bkt version "+version+"'\n", 0755)
			target := "linux-x64"
			if scenario == "metadata" {
				target = "darwin-arm64"
			}
			add(root+"package.properties", "version="+version+"\nplatform="+target+"\ncommit="+commit+"\n", 0644)
			if scenario == "traversal" {
				add(root+"../outside", "bad", 0644)
			}
			if scenario == "symlink" {
				if err := tw.WriteHeader(&tar.Header{Name: root + "link", Typeflag: tar.TypeSymlink, Linkname: "/tmp", Mode: 0777}); err != nil {
					t.Fatal(err)
				}
			}
			if err := tw.Close(); err != nil {
				t.Fatal(err)
			}
			if err := gz.Close(); err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(archive)
			if err != nil {
				t.Fatal(err)
			}
			hash := fmt.Sprintf("%x", sha256.Sum256(data))
			if scenario == "checksum" {
				hash = strings.Repeat("0", 64)
			}
			manifest := "version=" + version + "\npath=builds/" + version + "/jenkins-1-" + commit + "\nbkt-native-linux-x64-" + version + ".tar.gz.sha256=" + hash + "\n"
			if scenario == "duplicate-manifest" {
				manifest += "version=" + version + "\n"
			}
			write(filepath.Join(work, "channel"), manifest, 0644)
			installed := filepath.Join(home, ".local/bin/bkt")
			write(installed, "existing binary", 0755)
			run := func() ([]byte, error) {
				cmd := exec.Command("sh", script)
				cmd.Env = append(os.Environ(), "HOME="+home, "PATH="+mock+":"+os.Getenv("PATH"), "SHELL=/bin/bash", "JFROG_ACCESS_TOKEN=test-token", "FIXTURES="+work, "SCENARIO="+scenario)
				return cmd.CombinedOutput()
			}
			out, err := run()
			if strings.Contains(string(out), "test-token") {
				t.Fatal("token leaked")
			}
			if scenario == "install-update" {
				if err != nil {
					t.Fatalf("%v: %s", err, out)
				}
				if out, err := run(); err != nil {
					t.Fatalf("update: %v: %s", err, out)
				}
				if out, err := exec.Command(installed, "--version").Output(); err != nil || !strings.Contains(string(out), version) {
					t.Fatalf("installed binary: %v %s", err, out)
				}
				profile, err := os.ReadFile(filepath.Join(home, ".bashrc"))
				if err != nil || strings.Count(string(profile), "# AVIO bkt") != 1 {
					t.Fatalf("PATH was not configured exactly once: %v", err)
				}
			} else {
				if err == nil {
					t.Fatalf("expected refusal: %s", out)
				}
				old, err := os.ReadFile(installed)
				if err != nil || string(old) != "existing binary" {
					t.Fatal("failed update changed existing installation")
				}
			}
		})
	}
}
