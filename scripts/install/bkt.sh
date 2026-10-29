#!/bin/sh
# Public bootstrap for the AVIO bkt snapshot channel. No elevated privileges needed.
set +x
set -eu

main() {
    die() { printf 'Error: %s\n' "$*" >&2; exit 1; }
    progress() { printf '%s\n' "$*" >&2; }
    bin_dir="$HOME/.local/bin"
    custom_dir=false
    while [ "$#" -gt 0 ]; do
        case "$1" in
            --bin-dir) [ "$#" -ge 2 ] && [ -n "$2" ] || die 'Missing bin directory'; bin_dir=$2; custom_dir=true; shift 2 ;;
            --help|-h) echo 'Usage: sh bkt.sh [--bin-dir DIRECTORY]'; exit 0 ;;
            *) die "Unknown option: $1" ;;
        esac
    done
    case $(uname -s):$(uname -m) in
        Linux:x86_64|Linux:amd64) target=linux-x64 ;;
        Darwin:arm64) target=darwin-arm64 ;;
        *) die 'AVIO snapshots support Linux x64 and macOS Apple Silicon only' ;;
    esac
    for tool in curl tar awk mktemp; do command -v "$tool" >/dev/null 2>&1 || die "Required tool missing: $tool"; done
    if command -v sha256sum >/dev/null 2>&1; then hash=sha256sum
    elif command -v shasum >/dev/null 2>&1; then hash=shasum
    else die 'sha256sum or shasum is required'; fi
    work=$(mktemp -d "${TMPDIR:-/var/tmp}/bkt-bootstrap.XXXXXX")
    tty_state=''; staged=''; lock=''
    cleanup() {
        if [ -n "$tty_state" ]; then stty "$tty_state" < /dev/tty; printf '\n' > /dev/tty; fi
        [ -z "$staged" ] || rm -f "$staged"
        [ -z "$lock" ] || rmdir "$lock"
        rm -rf "$work"
    }
    trap cleanup EXIT
    trap 'exit 130' INT TERM
    token=${JFROG_ACCESS_TOKEN:-}
    unset JFROG_ACCESS_TOKEN
    if [ -z "$token" ]; then
        tty_state=$(stty -g < /dev/tty 2>/dev/null) || die 'Set JFROG_ACCESS_TOKEN or run in an interactive terminal'
        printf 'JFrog access token: ' > /dev/tty
        stty -echo < /dev/tty
        IFS= read -r token < /dev/tty || die 'Could not read token'
        stty "$tty_state" < /dev/tty; tty_state=''; printf '\n' > /dev/tty
    fi
    case $token in ''|*[!A-Za-z0-9._~+/=-]*) die 'Invalid bearer token characters' ;; esac
    repository=https://avio.jfrog.io/artifactory/tools/bkt
    download() {
        if [ -t 2 ]; then curl_progress=--progress-bar; else curl_progress=--silent; fi
        # curl strips Authorization on cross-origin redirects to signed storage URLs.
        # Pass credentials through stdin, never argv, URLs, or disk files.
        printf 'header = "Authorization: Bearer %s"\n' "$token" |
            curl --config - --proto '=https' --proto-redir '=https' --location --max-redirs 5 \
                --tlsv1.2 --fail "$curl_progress" --show-error --output "$2" "$1"
    }
    progress 'Resolving the bkt snapshot channel...'
    download "$repository/channels/snapshot.properties" "$work/channel" || die 'Channel download failed'
    awk -F= 'NF != 2 || $1 !~ /^[A-Za-z0-9_.-]+$/ || $2 !~ /^[A-Za-z0-9_.\/-]+$/ || seen[$1]++ {exit 1}' \
        "$work/channel" || die 'Invalid channel manifest'
    value() { awk -F= -v key="$1" '$1 == key {print $2; found++} END {if (found != 1) exit 1}' "$2"; }
    version=$(value version "$work/channel") || die 'Missing channel version'
    printf '%s\n' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+-avio-SNAPSHOT$' || die 'Invalid snapshot version'
    release_path=$(value path "$work/channel") || die 'Missing channel path'
    build_id=${release_path#"builds/$version/"}
    [ "$release_path" = "builds/$version/$build_id" ] || die 'Invalid channel path'
    printf '%s\n' "$build_id" | grep -Eq '^jenkins-[0-9]+-[a-f0-9]{40}$' || die 'Invalid build ID'
    commit=${build_id##*-}
    name="bkt-native-$target-$version.tar.gz"
    expected=$(value "$name.sha256" "$work/channel") || die 'Target not available in this channel'
    [ "${#expected}" -eq 64 ] || die 'Invalid SHA-256'
    case $expected in *[!a-f0-9]*) die 'Invalid SHA-256' ;; esac
    progress "Downloading bkt $version ($target)..."
    download "$repository/$release_path/$name" "$work/archive.tar.gz" || die 'Archive download failed'
    unset token
    progress 'Verifying the archive...'
    if [ "$hash" = sha256sum ]; then actual=$(sha256sum "$work/archive.tar.gz" | awk '{print $1}')
    else actual=$(shasum -a 256 "$work/archive.tar.gz" | awk '{print $1}'); fi
    [ "$actual" = "$expected" ] || die 'Archive checksum mismatch'
    root="bkt-$version"
    tar -tzf "$work/archive.tar.gz" > "$work/names" || die 'Unreadable archive'
    awk -v root="$root" '
        $0 !~ /^[A-Za-z0-9_.\/-]+$/ {exit 1}
        $0 != root "/" && index($0, root "/") != 1 {exit 1}
        /(^|\/)\.\.?($|\/)/ {exit 1}
        {if (seen[$0]++) exit 1; count++}
        END {if (!count) exit 1}
    ' "$work/names" || die 'Unsafe archive path'
    tar -tvzf "$work/archive.tar.gz" > "$work/types" || die 'Unreadable archive'
    awk 'substr($0,1,1) != "-" && substr($0,1,1) != "d" {exit 1}' "$work/types" || die 'Archive contains links or special files'
    mkdir "$work/payload"
    tar -xzf "$work/archive.tar.gz" -C "$work/payload" || die 'Extraction failed'
    payload="$work/payload/$root"
    [ -f "$payload/package.properties" ] && [ -x "$payload/bin/bkt" ] || die 'Incomplete package'
    [ "$(value version "$payload/package.properties")" = "$version" ] &&
        [ "$(value platform "$payload/package.properties")" = "$target" ] &&
        [ "$(value commit "$payload/package.properties")" = "$commit" ] || die 'Downloaded metadata mismatch'
    binary_version=$("$payload/bin/bkt" --version) || die 'Binary version check failed'
    printf '%s\n' "$binary_version" | grep -F -- "$version" || die 'Binary version mismatch'
    "$payload/bin/bkt" --help >/dev/null || die 'Binary help check failed'
    mkdir -p "$bin_dir"
    [ ! -d "$bin_dir/bkt" ] || die 'The installation target is a directory'
    mkdir "$bin_dir/.bkt-install.lock" 2>/dev/null || die 'Another installer is running (or a stale .bkt-install.lock exists)'
    lock="$bin_dir/.bkt-install.lock"
    staged=$(mktemp "$bin_dir/.bkt.XXXXXX")
    cp "$payload/bin/bkt" "$staged"
    chmod 755 "$staged"
    mv -f "$staged" "$bin_dir/bkt"
    staged=''
    if [ "$custom_dir" = false ]; then
        case ":$PATH:" in
            *":$HOME/.local/bin:"*) ;;
            *)
                line='export PATH="$HOME/.local/bin:$PATH"'
                case ${SHELL:-} in
                    */zsh) profile="$HOME/.zshrc" ;;
                    */bash) case $(uname -s) in Darwin) profile="$HOME/.bash_profile" ;; *) profile="$HOME/.bashrc" ;; esac ;;
                    */fish) profile="$HOME/.config/fish/config.fish"; line='fish_add_path "$HOME/.local/bin"' ;;
                    *) profile="$HOME/.profile" ;;
                esac
                mkdir -p "$(dirname "$profile")"
                if ! grep -Fqx "$line" "$profile" 2>/dev/null; then printf '\n# AVIO bkt\n%s\n' "$line" >> "$profile"; fi
                progress "Added PATH configuration to $profile. Open a new terminal."
                ;;
        esac
    fi
    progress "Installed bkt to $bin_dir/bkt"
    progress 'Run bkt --help, then bkt auth login to connect to Bitbucket.'
}

main "$@"
