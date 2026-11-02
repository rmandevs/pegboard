#!/bin/sh
# Install the latest pegboard release on Linux or macOS:
#
#   curl -fsSL https://raw.githubusercontent.com/rmandevs/pegboard/main/install.sh | sh
#
# Environment:
#   PEGBOARD_VERSION      version to install, e.g. v0.1.0 (default: latest)
#   PEGBOARD_INSTALL_DIR  where to put the binary (default: ~/.local/bin)

set -eu

REPO_URL="https://github.com/rmandevs/pegboard"

fail() {
	echo "pegboard: $*" >&2
	exit 1
}

detect_os() {
	case "$(uname -s)" in
	Linux) echo linux ;;
	Darwin) echo darwin ;;
	*) fail "unsupported system $(uname -s); download a binary from $REPO_URL/releases" ;;
	esac
}

detect_arch() {
	case "$(uname -m)" in
	x86_64 | amd64) echo amd64 ;;
	aarch64 | arm64) echo arm64 ;;
	*) fail "unsupported architecture $(uname -m); download a binary from $REPO_URL/releases" ;;
	esac
}

# Follows the /releases/latest redirect, which unlike the API has no rate limit.
latest_version() {
	url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$REPO_URL/releases/latest") ||
		fail "could not find the latest release at $REPO_URL/releases"
	version=${url##*/}
	case "$version" in
	v[0-9]*) echo "$version" ;;
	*) fail "could not find the latest release at $REPO_URL/releases" ;;
	esac
}

verify_checksum() {
	dir=$1
	archive=$2
	grep "  $archive\$" "$dir/checksums.txt" >"$dir/expected.txt" ||
		fail "$archive is not listed in checksums.txt"
	if command -v sha256sum >/dev/null 2>&1; then
		(cd "$dir" && sha256sum -c expected.txt >/dev/null 2>&1)
	elif command -v shasum >/dev/null 2>&1; then
		(cd "$dir" && shasum -a 256 -c expected.txt >/dev/null 2>&1)
	else
		fail "sha256sum or shasum is required to verify the download"
	fi || fail "checksum mismatch for $archive"
}

# Everything runs from here so a download cut short by the pipe executes nothing.
main() {
	command -v curl >/dev/null 2>&1 || fail "curl is required"
	command -v tar >/dev/null 2>&1 || fail "tar is required"

	os=$(detect_os)
	arch=$(detect_arch)
	tag=${PEGBOARD_VERSION:-$(latest_version)}
	case "$tag" in
	v*) ;;
	*) tag="v$tag" ;;
	esac
	install_dir=${PEGBOARD_INSTALL_DIR:-${HOME:?HOME is not set}/.local/bin}

	archive="pegboard_${tag#v}_${os}_${arch}.tar.gz"
	base_url="$REPO_URL/releases/download/$tag"

	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT
	trap 'exit 1' HUP INT TERM

	echo "Downloading pegboard $tag ($os/$arch)..."
	curl -fsSL -o "$tmp/$archive" "$base_url/$archive" ||
		fail "could not download $base_url/$archive"
	curl -fsSL -o "$tmp/checksums.txt" "$base_url/checksums.txt" ||
		fail "could not download $base_url/checksums.txt"
	verify_checksum "$tmp" "$archive"

	tar -xzf "$tmp/$archive" -C "$tmp" pegboard
	mkdir -p "$install_dir"
	# Move into place from the same directory so a running pegboard is replaced atomically.
	cp "$tmp/pegboard" "$install_dir/.pegboard.new"
	chmod 755 "$install_dir/.pegboard.new"
	mv -f "$install_dir/.pegboard.new" "$install_dir/pegboard"

	echo "Installed pegboard $tag to $install_dir/pegboard"
	case ":${PATH:-}:" in
	*":$install_dir:"*) ;;
	*)
		echo
		echo "$install_dir is not on your PATH. Add it with:"
		echo "  export PATH=\"$install_dir:\$PATH\""
		;;
	esac
}

main
