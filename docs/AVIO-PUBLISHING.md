# AVIO Jenkins snapshots and releases

The AVIO fork builds Linux x64, macOS ARM64 and Windows x64 on standalone Jenkins
agents. `AVIO_VERSION` identifies the fork snapshot independently of the
upstream release metadata. The Go toolchain is pinned in `scripts/ci-avio.sh`.

The Jenkins job uses shared GitHub App credential `github-avio` and this
repository's `Jenkinsfile`, accepts GitHub push webhooks, polls `master` every five
minutes, and pins all hosts to one commit. Each host runs the Go test suite,
builds and packages. Platform-independent skill/generated-documentation checks run
once on Linux. Package extraction and executable smoke checks are implementation-time
validation, not recurring CI stages. macOS uses cgo, the macOS 13 deployment baseline, and the existing ad-hoc
signing helper. Archives and checksums are retained as Jenkins artifacts.

Windows uses `standalone-windows-build && amd64` (AC0087). The agent's MSYS2
installation at `C:\ProgramData\AVIO\Jenkins\msys64` supplies Bash, native
`mingw32-make`, Unix utilities and ZIP. The shared Bash script invokes the upstream
Makefile's native Windows/cmd.exe recipes, with mise-managed Go 1.26.8 and
`CGO_ENABLED=0`. Git, Go, Java and Maven remain mise-managed; MSYS2 packages are
maintained separately with pacman. No WSL or C/C++ compiler is required.

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
  bkt-native-windows-x64-<AVIO_VERSION>.zip
  bkt-native-windows-x64-<AVIO_VERSION>.zip.sha256
bkt/channels/snapshot.properties
bkt/channels/stable.properties
bkt/releases/<release-version>.properties
```

Each archive contains `bkt-<AVIO_VERSION>/bin/bkt` (`bin/bkt.exe` on Windows), bundled skills, license,
README, changelog, and `package.properties` with version, target, and commit.
Install by extracting the archive and copying `bin/bkt` to a directory on PATH.

The channel uses MDX's plain `key=value` format: `version`, build-relative `path`,
and `<archive>.sha256=<digest>` for all three targets. Consumers fetch the channel once,
download from `bkt/<path>/<archive>`, and verify its pinned SHA-256 before extracting.
Artifactory downloads use the consumer's read token. Do not execute the manifest
as shell code. The public bootstrap website is deployed separately below.

The publisher validates all three local checksum receipts before any upload. Existing
build artifacts with matching remote checksums are skipped; conflicting bytes
fail. Only 404 permits creation. Redirects, authentication failures, other HTTP
errors, and failed remote checksum verification stop publication. Artifactory
serves the `.sha256` URLs automatically; the publisher reads and verifies their
contents rather than uploading separate checksum objects. The mutable snapshot
channel is updated only after all three archives and checksum responses are verified.
Grant read/deploy on `tools/bkt/builds/**` and overwrite only on
`tools/bkt/channels/**`; Artifactory permissions enforce immutability against
concurrent publishers. A failed upload can leave an unpromoted partial build.

## Local verification and recovery

```sh
bash scripts/ci-avio.sh linux    # Linux x64 host
bash scripts/ci-avio.sh darwin   # macOS ARM64 host
bash scripts/ci-avio.sh windows # Windows x64 with MSYS2/native Make on PATH
go test ./cmd/avio-publish      # local HTTP tests; no JFrog writes
```

To retry publication, restore the **same** archived files from all three hosts into
`dist/avio/`, set the original `BKT_RELEASE_BUILD`, and run
`go run ./cmd/avio-publish` with publication credentials in the environment.
`JFROG_URL` and `JFROG_REPOSITORY` optionally override the defaults. Do not rebuild
over an existing build ID. Run a new Jenkins build for newly built bytes.

## Stable AVIO releases

Merge a PR changing `AVIO_VERSION` from `X.Y.Z-avio-SNAPSHOT` to `X.Y.Z-avio`.
Jenkins builds the committed release version on all three platforms, publishes verified
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

### Approved Windows backfill for 0.37.0-avio

On 2026-10-30, an explicitly approved, one-time metadata exception added the missing
Windows ZIP to the existing `0.37.0-avio` release. AC0087 built the original tagged
commit `5f58e8869a0bac37c7f87090383d2a147a17e817` using the current Windows packaging
helper. The ZIP was added beneath the original release build path; its checksum
was appended to `releases/0.37.0-avio.properties` and `channels/stable.properties`.
Existing Linux/macOS archives, hashes and the Git tag were preserved.

Windows SHA-256: `233d95a50e2d7257bdbfc48225c8475847ae34c5f12b488862f0fb2f0f88ef52`.
[One-time backfill #1](https://devops.avio.dev/jenkins/job/Admin%20Tools/job/bkt%20Windows%200.37%20Backfill/1/)
archives the original manifests and Windows artifact. The job is disabled after
verification. This exception does not change the normal immutable-release policy.

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

Windows x64 uses PowerShell 5.1 or 7:

```powershell
& ([scriptblock]::Create((irm https://tools.avio.dev/bkt.ps1)))

# Load once when selecting options:
$installBkt = [scriptblock]::Create((irm https://tools.avio.dev/bkt.ps1))
& $installBkt -Channel snapshot
& $installBkt -BinDir "$HOME\bin"
& $installBkt -Help
```

The default directory is `%LOCALAPPDATA%\AVIO\bin`, added to the user PATH.
`-BinDir` leaves PATH management to the caller. The token comes from
`$env:JFROG_ACCESS_TOKEN` or a hidden prompt. The installer validates the manifest,
SHA-256, ZIP entries and package metadata, then replaces the executable with a
same-directory staged file. It requires neither Bash nor administrator rights.
Close running instances before updating. Inspect a stale `.bkt-install.lock`
after an interrupted process before removing it.

Source: `scripts/install/index.html`, `scripts/install/bkt.sh` and
`scripts/install/bkt.ps1`. The page uses the MDX landing page's exact CSS,
tabbed layout, copy-button behavior and grouped examples/parameter format, with
bkt-specific content. It reuses the existing `/avio-logo.png` site asset. Installer
reference: MDX `origin/main` at `9fb4ee0`, inspected October 29, 2026.

The existing Cloud Storage bucket is `gs://avio-tools` in GCP project
`public-sites-0570`. Deploy only the three bkt objects, using the active gcloud
account with write access to that bucket:

```sh
bash scripts/deploy-install-page.sh             # preview
bash scripts/deploy-install-page.sh --apply     # upload bkt.sh/bkt.ps1, then bkt
```

The extensionless `bkt` object has HTML content type; both scripts are plain text.
All have a 60-second public cache lifetime. Verify the deployed responses at
`https://tools.avio.dev/bkt`, `/bkt.sh` and `/bkt.ps1` after upload.
Installer tests run with the regular Go suite (`go test ./internal/installtest`),
using isolated home directories and mocked downloads. Live installation checks
should use a temporary home or `--bin-dir` to avoid changing the operator's CLI.
