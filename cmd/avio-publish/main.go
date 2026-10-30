// avio-publish uploads three-platform builds and promotes their channel.
package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type publisher struct {
	base, username, token string
	client                *http.Client
}

func (p publisher) request(method, path string, data []byte) (*http.Response, error) {
	req, err := http.NewRequest(method, p.base+"/"+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if p.username != "" {
		req.SetBasicAuth(p.username, p.token)
	} else {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}
	if method == http.MethodPut {
		req.Header.Set("X-Checksum-Sha256", digest(data))
		req.Header.Set("Content-Type", "application/octet-stream")
	}
	return p.client.Do(req)
}

func digest(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

// Artifactory serves checksum URLs dynamically after an archive is uploaded.
// These responses need not have a checksum header of their own.
func (p publisher) verifyReceipt(path, name, hash string) error {
	res, err := p.request(http.MethodGet, path+".sha256", nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 4096))
	if err != nil {
		return err
	}
	fields := strings.Fields(string(data))
	if res.StatusCode != http.StatusOK || len(fields) < 1 || len(fields) > 2 ||
		!strings.EqualFold(fields[0], hash) || (len(fields) == 2 && fields[1] != name) {
		return fmt.Errorf("remote checksum receipt verification failed: %s.sha256 (HTTP %d)", path, res.StatusCode)
	}
	return nil
}

func (p publisher) upload(path string, data []byte, mutable bool) error {
	res, err := p.request(http.MethodHead, path, nil)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode == http.StatusOK {
		if res.Header.Get("X-Checksum-Sha256") == digest(data) {
			return nil
		}
		if !mutable {
			return fmt.Errorf("immutable artifact conflict: %s", path)
		}
	} else if res.StatusCode != http.StatusNotFound {
		return fmt.Errorf("HEAD %s: HTTP %d", path, res.StatusCode)
	}
	res, err = p.request(http.MethodPut, path, data)
	if err != nil {
		return err
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusNoContent {
		return fmt.Errorf("PUT %s: HTTP %d", path, res.StatusCode)
	}
	res, err = p.request(http.MethodHead, path, nil)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK || res.Header.Get("X-Checksum-Sha256") != digest(data) {
		return fmt.Errorf("remote checksum verification failed: %s", path)
	}
	return nil
}

func (p publisher) publish(dir, version, build string) error {
	if !regexp.MustCompile(`^\d+\.\d+\.\d+-avio(-SNAPSHOT)?$`).MatchString(version) ||
		!regexp.MustCompile(`^jenkins-\d+-[a-f0-9]{40}$`).MatchString(build) {
		return fmt.Errorf("invalid AVIO version or Jenkins build ID")
	}
	path := "builds/" + version + "/" + build
	manifest := fmt.Sprintf("version=%s\npath=%s\n", version, path)
	type artifact struct {
		name string
		data []byte
	}
	var artifacts []artifact
	// Validate all hosts' receipts before the first upload.
	for _, target := range []struct{ name, extension string }{
		{"linux-x64", ".tar.gz"},
		{"darwin-arm64", ".tar.gz"},
		{"windows-x64", ".zip"},
	} {
		name := "bkt-native-" + target.name + "-" + version + target.extension
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		receipt := []byte(digest(data) + "  " + name + "\n")
		actual, err := os.ReadFile(filepath.Join(dir, name+".sha256"))
		if err != nil {
			return err
		}
		if !bytes.Equal(actual, receipt) {
			return fmt.Errorf("invalid checksum receipt: %s", name)
		}
		artifacts = append(artifacts, artifact{name, data})
		manifest += name + ".sha256=" + digest(data) + "\n"
	}
	for _, a := range artifacts {
		if err := p.upload(path+"/"+a.name, a.data, false); err != nil {
			return err
		}
		if err := p.verifyReceipt(path+"/"+a.name, a.name, digest(a.data)); err != nil {
			return err
		}
		fmt.Printf("Verified %s/%s/%s\n", p.base, path, a.name)
	}
	channel := "snapshot"
	if !strings.HasSuffix(version, "-SNAPSHOT") {
		// A version can identify only one verified build, even across CI retries.
		if err := p.upload("releases/"+version+".properties", []byte(manifest), false); err != nil {
			return err
		}
		channel = "stable"
	}
	return p.upload("channels/"+channel+".properties", []byte(manifest), true)
}

func run() error {
	base := strings.TrimRight(os.Getenv("JFROG_URL"), "/")
	if base == "" {
		base = "https://avio.jfrog.io/artifactory"
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("JFROG_URL must be an HTTPS Artifactory base URL")
	}
	repo := os.Getenv("JFROG_REPOSITORY")
	if repo == "" {
		repo = "tools"
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9_-]+$`).MatchString(repo) {
		return fmt.Errorf("invalid JFROG_REPOSITORY")
	}
	token := os.Getenv("JFROG_ACCESS_TOKEN")
	username, supplied := os.LookupEnv("JFROG_USERNAME")
	if token == "" || (supplied && username == "") {
		return fmt.Errorf("set JFROG_ACCESS_TOKEN and, if supplied, a nonempty JFROG_USERNAME")
	}
	version, err := os.ReadFile("AVIO_VERSION")
	if err != nil {
		return err
	}
	p := publisher{base + "/" + repo + "/bkt", username, token, &http.Client{
		Timeout:       2 * time.Minute,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
	return p.publish("dist/avio", strings.TrimSpace(string(version)), os.Getenv("BKT_RELEASE_BUILD"))
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
