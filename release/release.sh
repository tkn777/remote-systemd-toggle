#!/bin/sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
VERSION="${VERSION:-0.9.0}"
PACKAGE_VERSION="${PACKAGE_VERSION:-$(printf '%s' "$VERSION" | sed 's/-/~/g')}"
OUT_DIR="$ROOT_DIR/dist"
BUILD_DIR="$ROOT_DIR/.build"
SRC_DIR="$BUILD_DIR/src"

CLIENT_NAME="remote-systemd-toggle-client"
SERVER_NAME="remote-systemd-toggle-server"

rm -rf "$OUT_DIR" "$BUILD_DIR"
mkdir -p "$OUT_DIR" "$SRC_DIR"

copy_sources() {
	cp -R "$ROOT_DIR/common" "$SRC_DIR/"
	cp -R "$ROOT_DIR/remote-systemd-toggle" "$SRC_DIR/"
	cp -R "$ROOT_DIR/remote-systemd-toggled" "$SRC_DIR/"
	cp "$ROOT_DIR/go.work" "$SRC_DIR/"
	cp "$ROOT_DIR/go.work.sum" "$SRC_DIR/"
}

set_source_version() {
	version="$1"
	sed -i "s/var version = \"[^\"]*\"/var version = \"$version\"/" \
		"$SRC_DIR/remote-systemd-toggle/main.go" \
		"$SRC_DIR/remote-systemd-toggled/main.go"
}

package_unit_file() {
	unit_file="$BUILD_DIR/remote-systemd-toggled.service"
	cp "$ROOT_DIR/remote-systemd-toggled.service" "$unit_file"
	sed -i 's#/usr/local/bin/remote-systemd-toggled#/usr/bin/remote-systemd-toggled#' "$unit_file"
	printf '%s\n' "$unit_file"
}

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

	(
		cd "$SRC_DIR"
		GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 go build \
			-ldflags "-s -w" \
			-o "$out" "$module"
	)
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

install_doc() {
	package="$1"
	pkg_dir="$2"
	doc_dir="$pkg_dir/usr/share/doc/$package"

	mkdir -p "$doc_dir"
	install -m 0644 "$ROOT_DIR/README.md" "$doc_dir/"
	install -m 0644 "$ROOT_DIR/SECURITY.md" "$doc_dir/"
	install -m 0644 "$ROOT_DIR/CONTRIBUTING.md" "$doc_dir/"
	install -m 0644 "$ROOT_DIR/LICENSE" "$doc_dir/copyright"
	cp -R "$ROOT_DIR/cert-generation-examples" "$doc_dir/"
	cp -R "$ROOT_DIR/config-examples" "$doc_dir/"
}

install_fail2ban() {
	package="$1"
	pkg_dir="$2"
	doc_dir="$pkg_dir/usr/share/doc/$package"

	cp -R "$ROOT_DIR/fail2ban-examples" "$doc_dir/"
	mkdir -p "$pkg_dir/etc/fail2ban/filter.d"
	install -m 0644 "$ROOT_DIR/fail2ban-examples/remote-systemd-toggled.conf" \
		"$pkg_dir/etc/fail2ban/filter.d/"
}

install_service_scripts() {
	pkg_dir="$1"

	cat > "$pkg_dir/DEBIAN/postinst" <<'POSTINST'
#!/bin/sh
set -e

if command -v systemctl >/dev/null 2>&1; then
	systemctl daemon-reload
	if systemctl is-enabled remote-systemd-toggled.service >/dev/null 2>&1; then
		systemctl restart remote-systemd-toggled.service
	fi
fi

exit 0
POSTINST

	cat > "$pkg_dir/DEBIAN/postrm" <<'POSTRM'
#!/bin/sh
set -e

if command -v systemctl >/dev/null 2>&1; then
	systemctl daemon-reload
fi

exit 0
POSTRM

	cat > "$pkg_dir/DEBIAN/prerm" <<'PRERM'
#!/bin/sh
set -e

if [ "$1" = "remove" ] && command -v systemctl >/dev/null 2>&1; then
	systemctl stop remote-systemd-toggled.service || true
fi

exit 0
PRERM

	chmod 0755 "$pkg_dir/DEBIAN/postinst" "$pkg_dir/DEBIAN/postrm" "$pkg_dir/DEBIAN/prerm"
}

build_deb() {
	package="$1"
	binary="$2"
	install_name="$3"
	arch="$4"
	description="$5"
	with_service="$6"

	pkg_dir="$BUILD_DIR/${package}_${PACKAGE_VERSION}_${arch}"
	rm -rf "$pkg_dir"
	mkdir -p "$pkg_dir/DEBIAN" "$pkg_dir/usr/bin"

	install -m 0755 "$binary" "$pkg_dir/usr/bin/$install_name"
	control_file "$package" "$arch" "$description" "$pkg_dir/DEBIAN/control"
	install_doc "$package" "$pkg_dir"

	if [ "$with_service" = "yes" ]; then
		mkdir -p "$pkg_dir/usr/lib/systemd/system"
		install -m 0644 "$UNIT_FILE" "$pkg_dir/usr/lib/systemd/system/"
		install_fail2ban "$package" "$pkg_dir"
		install_service_scripts "$pkg_dir"
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
need sha256sum

copy_sources
set_source_version "$VERSION"
UNIT_FILE="$(package_unit_file)"

for arch in amd64 arm64; do
	client_bin="$BUILD_DIR/${CLIENT_NAME}_${arch}"
	server_bin="$BUILD_DIR/${SERVER_NAME}_${arch}"

	build_go ./remote-systemd-toggle linux "$arch" "$client_bin"
	build_go ./remote-systemd-toggled linux "$arch" "$server_bin"

	build_deb "$CLIENT_NAME" "$client_bin" "remote-systemd-toggle" "$arch" \
		"mTLS-protected remote systemd service toggle client" "no"
	build_deb "$SERVER_NAME" "$server_bin" "remote-systemd-toggled" "$arch" \
		"mTLS-protected remote systemd service toggle service" "yes"

	build_rpm_from_deb "$OUT_DIR/${CLIENT_NAME}_${PACKAGE_VERSION}_${arch}.deb"
	build_rpm_from_deb "$OUT_DIR/${SERVER_NAME}_${PACKAGE_VERSION}_${arch}.deb"
done

build_go ./remote-systemd-toggle windows amd64 "$OUT_DIR/${CLIENT_NAME}.exe"

(
	cd "$OUT_DIR"
	sha256sum * > SHA256SUMS
)

echo "release artifacts written to $OUT_DIR"

rm -rf "$BUILD_DIR"
