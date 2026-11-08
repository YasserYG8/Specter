#!/bin/sh
# Specter installer. Installs the latest release of Specter on Linux (and WSL2).
#
#   curl -sSL https://raw.githubusercontent.com/YasserYG8/Specter/main/install.sh | sh
#
# It downloads the release archive and checksums.txt, checks the SHA-256 of the archive, and only then installs
# the binary. It does not install other packages: if strace or lsof is missing, it prints the command for you.
#
# Settings (environment variables):
#   SPECTER_VERSION       install this release tag instead of the latest, for example v0.1.0
#   SPECTER_INSTALL_DIR   install into this directory instead of /usr/local/bin
#   SPECTER_DOWNLOAD_BASE for tests only: download the files from this URL instead of GitHub

set -eu

REPO="YasserYG8/Specter"
tmp=""

fail() {
	printf 'install.sh: %s\n' "$*" >&2
	exit 1
}

cleanup() {
	if [ -n "$tmp" ]; then
		rm -rf "$tmp"
	fi
}

# Everything happens inside main, which is called on the last line. If the download of this script is cut off
# halfway through "curl | sh", nothing runs.
main() {
	trap cleanup EXIT
	trap 'exit 1' INT TERM HUP

	# Specter reads running processes through ptrace and /proc, so it only works on Linux
	case "$(uname -s)" in
	Linux) ;;
	Darwin)
		fail "Specter needs Linux to attach to a process, and macOS does not provide that. Run your server and Specter inside a Linux VM (for example OrbStack, UTM or Lima) or a Docker container."
		;;
	MINGW* | MSYS* | CYGWIN*)
		fail "Specter needs Linux. On Windows, run this installer inside WSL2."
		;;
	*)
		fail "Specter needs Linux (this is $(uname -s))."
		;;
	esac

	machine=$(uname -m)
	case "$machine" in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) fail "unsupported chip '$machine'. Supported: x86_64 (amd64) and aarch64 (arm64)." ;;
	esac

	if command -v curl >/dev/null 2>&1; then
		fetch() { curl -fsSL -o "$2" "$1"; }
	elif command -v wget >/dev/null 2>&1; then
		fetch() { wget -qO "$2" "$1"; }
	else
		fail "needs curl or wget to download Specter."
	fi

	# The checksum is the only proof that the file is the one that was released, so never skip it
	if command -v sha256sum >/dev/null 2>&1; then
		verify() { sha256sum -c -; }
	elif command -v shasum >/dev/null 2>&1; then
		verify() { shasum -a 256 -c -; }
	else
		fail "needs sha256sum or shasum to check the download, and will not install an unchecked file."
	fi

	if [ -n "${SPECTER_DOWNLOAD_BASE:-}" ]; then
		base=$SPECTER_DOWNLOAD_BASE
	elif [ -n "${SPECTER_VERSION:-}" ]; then
		version=$SPECTER_VERSION
		case "$version" in v*) ;; *) version="v$version" ;; esac
		base="https://github.com/$REPO/releases/download/$version"
	else
		base="https://github.com/$REPO/releases/latest/download"
	fi

	archive="specter_linux_$arch.tar.gz"
	tmp=$(mktemp -d)

	printf 'Downloading %s\n' "$archive"
	fetch "$base/$archive" "$tmp/$archive" || fail "could not download $base/$archive"
	fetch "$base/checksums.txt" "$tmp/checksums.txt" || fail "could not download $base/checksums.txt"

	# checksums.txt has lines like "<hash>  specter_linux_amd64.tar.gz"
	line=$(awk -v f="$archive" '$2 == f { print; exit }' "$tmp/checksums.txt")
	[ -n "$line" ] || fail "checksums.txt has no entry for $archive."
	if ! (cd "$tmp" && printf '%s\n' "$line" | verify >/dev/null 2>&1); then
		fail "the checksum of $archive does not match checksums.txt. Nothing was installed."
	fi

	tar -xzf "$tmp/$archive" -C "$tmp" specter || fail "could not unpack $archive."

	# Use sudo only when the directory cannot be written by the current user
	dir=${SPECTER_INSTALL_DIR:-/usr/local/bin}
	use_sudo=no
	if mkdir -p "$dir" 2>/dev/null && [ -w "$dir" ]; then
		:
	elif [ "$(id -u)" -eq 0 ]; then
		fail "cannot write to $dir."
	elif command -v sudo >/dev/null 2>&1; then
		printf 'Installing to %s needs sudo.\n' "$dir"
		use_sudo=yes
		sudo mkdir -p "$dir"
	else
		fail "$dir is not writable and sudo is not available. Set SPECTER_INSTALL_DIR to a folder you own, for example SPECTER_INSTALL_DIR=\$HOME/.local/bin"
	fi
	if [ "$use_sudo" = yes ]; then
		sudo install -m 0755 "$tmp/specter" "$dir/specter"
	else
		install -m 0755 "$tmp/specter" "$dir/specter"
	fi

	printf 'Installed %s\n' "$("$dir/specter" --version)"
	printf 'Location: %s/specter\n' "$dir"
	case ":$PATH:" in
	*":$dir:"*) ;;
	*) printf 'Note: %s is not on your PATH. Add it to run "specter" from anywhere.\n' "$dir" ;;
	esac

	# strace reads the output of a running server and lsof or ss finds the port. Print the command, never run it.
	missing=""
	command -v strace >/dev/null 2>&1 || missing="$missing strace"
	command -v lsof >/dev/null 2>&1 || missing="$missing lsof"
	if [ -n "$missing" ]; then
		pre="sudo "
		[ "$(id -u)" -eq 0 ] && pre=""
		printf '\nSpecter also needs:%s\n' "$missing"
		if command -v apt-get >/dev/null 2>&1; then
			printf 'Install them with: %sapt install strace lsof iproute2\n' "$pre"
		elif command -v dnf >/dev/null 2>&1; then
			printf 'Install them with: %sdnf install strace lsof iproute\n' "$pre"
		elif command -v pacman >/dev/null 2>&1; then
			printf 'Install them with: %spacman -S strace lsof iproute2\n' "$pre"
		else
			printf 'Install strace and lsof with your package manager.\n'
		fi
	fi
}

main "$@"
