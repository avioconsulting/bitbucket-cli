# AVIO Jenkins snapshots and releases

The AVIO fork builds Linux x64 and macOS ARM64 on the same standalone Jenkins
agents as MDX. `AVIO_VERSION` identifies the fork snapshot independently of the
upstream release metadata. The Go toolchain is pinned in `scripts/ci-avio.sh`.

The Jenkins job uses shared GitHub App credential `github-avio` and this
repository's `Jenkinsfile`, accepts GitHub push webhooks, polls `master` every five
minutes, and pins both hosts to one commit. Each host runs the full Go tests,
checks generated documentation, builds, packages, and smoke-tests its extracted
binary. macOS uses cgo, the macOS 13 deployment baseline, and the existing ad-hoc
signing helper. Archives and checksums are retained as Jenkins artifacts.

Only the current `master` commit publishes. Feature-branch validation builds
archive their results without receiving publication credentials. Publication uses
the Jenkins username/password credential `artifactory-devops`, scoped to that
stage, as `JFROG_USERNAME` and `JFROG_ACCESS_TOKEN`.

## Artifact layout

Default repository: `https://avio.jfrog.io/artifactory/tools`.

```text
bkt/builds/<AVIO_VERSION>/jenkins-<BUILD_NUMBER>-<COMMIT>/
  bkt-native-linux-x64-<AVIO_VERSION>.tar.gz
  bkt-native-linux-x64-<AVIO_VERSION>.tar.gz.sha256
  bkt-native-darwin-arm64-<AVIO_VERSION>.tar.gz
  bkt-native-darwin-arm64-<AVIO_VERSION>.tar.gz.sha256
bkt/channels/snapshot.properties
bkt/channels/stable.properties
bkt/releases/<release-version>.properties
```

Each archive contains `bkt-<AVIO_VERSION>/bin/bkt`, bundled skills, license,
README, changelog, and `package.properties` with version, target, and commit.
Install by extracting the archive and copying `bin/bkt` to a directory on PATH.

The channel uses MDX's plain `key=value` format: `version`, build-relative `path`,
and `<archive>.sha256=<digest>` for both targets. Consumers fetch the channel once,
download from `bkt/<path>/<archive>`, and verify its pinned SHA-256 before extracting.
Artifactory downloads use the consumer's read token. Do not execute the manifest
as shell code. The public bootstrap website is deployed separately below.

The publisher validates both local checksum receipts before any upload. Existing
build artifacts with matching remote checksums are skipped; conflicting bytes
fail. Only 404 permits creation. Redirects, authentication failures, other HTTP
errors, and failed remote checksum verification stop publication. Artifactory
serves the `.sha256` URLs automatically; the publisher reads and verifies their
contents rather than uploading separate checksum objects. The mutable snapshot
channel is updated only after both archives and checksum responses are verified.
Grant read/deploy on `tools/bkt/builds/**` and overwrite only on
`tools/bkt/channels/**`; Artifactory permissions enforce immutability against
concurrent publishers. A failed upload can leave an unpromoted partial build.

## Local verification and recovery

```sh
bash scripts/ci-avio.sh linux    # Linux x64 host
bash scripts/ci-avio.sh darwin   # macOS ARM64 host
go test ./cmd/avio-publish      # local HTTP tests; no JFrog writes
```

To retry publication, restore the **same** archived files from both hosts into
`dist/avio/`, set the original `BKT_RELEASE_BUILD`, and run
`go run ./cmd/avio-publish` with publication credentials in the environment.
`JFROG_URL` and `JFROG_REPOSITORY` optionally override the defaults. Do not rebuild
over an existing build ID. Run a new Jenkins build for newly built bytes.

## Stable AVIO releases

Merge a PR changing `AVIO_VERSION` from `X.Y.Z-avio-SNAPSHOT` to `X.Y.Z-avio`.
Jenkins builds the committed release version on both platforms, publishes verified
archives, creates an immutable `releases/X.Y.Z-avio.properties` manifest, and
promotes `channels/stable.properties`. A different build cannot reuse that version.
After publication, it creates annotated tag `vX.Y.Z-avio` on the built commit and
commits `X.(Y+1).0-avio-SNAPSHOT`. The tag and version commit use one atomic,
non-forced Git push. Master advancement or tag conflicts stop finalization.
The subsequent snapshot build cannot repeat the release flow.

If publication succeeds but Git finalization fails, recover using the exact
release commit and `scripts/finalize-avio-release.sh` with `SOURCE_COMMIT` and
fresh App credentials after resolving the cause. Do not rebuild/re-publish the
same stable version. Archived artifacts can retry publication with their original
build ID. Grant read/deploy (without overwrite) to `tools/bkt/releases/**` too.

Upstream release metadata, unqualified tags, and GitHub releases continue through
the separate process in [RELEASE.md](RELEASE.md).

Reference: MDX `origin/main` at `ae06b4e`, inspected October 29, 2026.

## Public landing page and installer

Visit **https://tools.avio.dev/bkt**, or run:

```sh
curl -fsSL https://tools.avio.dev/bkt.sh | sh
# Stable is the default; select it explicitly:
curl -fsSL https://tools.avio.dev/bkt.sh | sh -s -- --channel stable
# Opt into the latest development snapshot:
curl -fsSL https://tools.avio.dev/bkt.sh | sh -s -- --channel snapshot
# Custom location (manage PATH yourself); flags can be combined:
curl -fsSL https://tools.avio.dev/bkt.sh | sh -s -- --channel stable --bin-dir "$HOME/bin"
# Show installer options without installing (--help also accepts -h):
curl -fsSL https://tools.avio.dev/bkt.sh | sh -s -- --help
```

Supply `JFROG_ACCESS_TOKEN` to the **sh process**, or enter it at the hidden
terminal prompt. The installer defaults to the stable release channel, pins one
manifest from the selected channel, verifies its archive
digest, rejects unsafe archive entries, checks package target/commit/version,
and smoke-tests the binary before atomically replacing `~/.local/bin/bkt`.
Rerunning updates the installation. Existing Bitbucket configuration is retained.
The default install adds PATH configuration for Bash, Zsh, Fish, or POSIX shells
when needed. A downloaded script accepts `--bin-dir DIRECTORY` for a custom
location (manage PATH yourself in that case).

Source: `scripts/install/index.html` and `scripts/install/bkt.sh`. The page follows
the MDX landing page and reuses the existing `/avio-logo.png` site asset. Installer
reference: MDX `origin/main` at `9fb4ee0`, inspected October 29, 2026.

The existing Cloud Storage bucket is `gs://avio-tools` in GCP project
`public-sites-0570`. Deploy only the two bkt objects, using the active gcloud
account with write access to that bucket:

```sh
bash scripts/deploy-install-page.sh             # preview
bash scripts/deploy-install-page.sh --apply     # upload bkt.sh, then bkt
```

The extensionless `bkt` object has HTML content type; `bkt.sh` is plain text.
Both have a 60-second public cache lifetime. Verify the deployed responses at
`https://tools.avio.dev/bkt` and `https://tools.avio.dev/bkt.sh` after upload.
Installer tests run with the regular Go suite (`go test ./internal/installtest`),
using isolated home directories and mocked downloads. Live installation checks
should use a temporary home or `--bin-dir` to avoid changing the operator's CLI.
