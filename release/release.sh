#!/bin/sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
VERSION="${VERSION:-0.9.0}"
PACKAGE_VERSION="${PACKAGE_VERSION:-$(printf '%s' "$VERSION" | sed 's/-/~/g')}"
OUT_DIR="$ROOT_DIR/dist"
BUILD_DIR="$ROOT_DIR/.build"

CLIENT_SRC="$ROOT_DIR/systemd-service-toggle/main.go"
SERVER_SRC="$ROOT_DIR/systemd-service-toggled/main.go"

CLIENT_NAME="systemd-service-toggle-client"
SERVER_NAME="systemd-service-toggle-server"

set_source_version() {
	version="$1"
	sed -i "s/var version = \"[^\"]*\"/var version = \"$version\"/" "$CLIENT_SRC" "$SERVER_SRC"
}

cleanup() {
	set_source_version dev
}

trap cleanup EXIT INT TERM
set_source_version "$VERSION"

rm -rf "$OUT_DIR" "$BUILD_DIR"
mkdir -p "$OUT_DIR" "$BUILD_DIR"

need() {
	if ! command -v "$1" >/dev/null 2>&1; then
		echo "missing required command: $1" >&2
		exit 1
	fi
}

build_go() {
	module="$1"
	goos="$2"
	goarch="$3"
	out="$4"

	GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 go build \
		-ldflags "-s -w" \
		-o "$out" "$module"
}

control_file() {
	package="$1"
	arch="$2"
	description="$3"
	file="$4"

	cat > "$file" <<CONTROL
Package: $package
Version: $PACKAGE_VERSION
Section: utils
Priority: optional
Architecture: $arch
Maintainer: Thomas Kuhlmann (mail@thomas-kuhlmann.de)
Description: $description
CONTROL
}

build_deb() {
	package="$1"
	binary="$2"
	arch="$3"
	description="$4"
	with_service="$5"

	pkg_dir="$BUILD_DIR/${package}_${PACKAGE_VERSION}_${arch}"
	rm -rf "$pkg_dir"
	mkdir -p "$pkg_dir/DEBIAN" "$pkg_dir/usr/bin"

	install -m 0755 "$binary" "$pkg_dir/usr/bin/"
	control_file "$package" "$arch" "$description" "$pkg_dir/DEBIAN/control"

	if [ "$with_service" = "yes" ]; then
		mkdir -p "$pkg_dir/lib/systemd/system"
		install -m 0644 "$ROOT_DIR/systemd-service-toggled.service" "$pkg_dir/lib/systemd/system/"
	fi

	dpkg-deb --root-owner-group --build "$pkg_dir" "$OUT_DIR/${package}_${PACKAGE_VERSION}_${arch}.deb"
}

build_rpm_from_deb() {
	deb="$1"
	(
		cd "$OUT_DIR"
		alien --to-rpm --scripts "$(basename "$deb")"
	)
}

need go
need dpkg-deb
need alien

for arch in amd64 arm64; do
	client_bin="$BUILD_DIR/${CLIENT_NAME}_${arch}"
	server_bin="$BUILD_DIR/${SERVER_NAME}_${arch}"

	build_go ./systemd-service-toggle linux "$arch" "$client_bin"
	build_go ./systemd-service-toggled linux "$arch" "$server_bin"

	build_deb "$CLIENT_NAME" "$client_bin" "$arch" \
		"mTLS-protected remote systemd service toggle client" "no"
	build_deb "$SERVER_NAME" "$server_bin" "$arch" \
		"mTLS-protected remote systemd service toggle service" "yes"

	build_rpm_from_deb "$OUT_DIR/${CLIENT_NAME}_${PACKAGE_VERSION}_${arch}.deb"
	build_rpm_from_deb "$OUT_DIR/${SERVER_NAME}_${PACKAGE_VERSION}_${arch}.deb"
done

build_go ./systemd-service-toggle windows amd64 "$OUT_DIR/${CLIENT_NAME}.exe"

echo "release artifacts written to $OUT_DIR"

rm -rf "$BUILD_DIR"
