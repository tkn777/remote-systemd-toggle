#!/bin/sh
set -eu
umask 077

OUT_DIR="${1:-server-certs}"
SERVER_NAME="${2:-localhost}"
SERVER_CA_CN="${SERVER_CA_CN:-systemd-service-toggle server CA}"
SERVER_CN="${SERVER_CN:-$SERVER_NAME}"
DAYS="${DAYS:-36500}"

mkdir -p "$OUT_DIR"

SERVER_CA_KEY="$OUT_DIR/server-ca.key"
SERVER_CA_CERT="$OUT_DIR/server-ca.crt"
SERVER_KEY="$OUT_DIR/server.key"
SERVER_CSR="$OUT_DIR/server.csr"
SERVER_CERT="$OUT_DIR/server.crt"
SERVER_EXT="$OUT_DIR/server.ext"

for file in "$SERVER_CA_KEY" "$SERVER_CA_CERT" "$SERVER_KEY" "$SERVER_CERT"; do
	if [ -e "$file" ]; then
		echo "$file already exists" >&2
		exit 1
	fi
done

openssl genpkey -algorithm ED25519 -out "$SERVER_CA_KEY"
openssl req -x509 -new -key "$SERVER_CA_KEY" -days "$DAYS" -out "$SERVER_CA_CERT" \
	-subj "/CN=$SERVER_CA_CN"

openssl genpkey -algorithm ED25519 -out "$SERVER_KEY"
openssl req -new -key "$SERVER_KEY" -out "$SERVER_CSR" \
	-subj "/CN=$SERVER_CN"

case "$SERVER_NAME" in
	*[!0-9.]*)
		SAN="DNS:$SERVER_NAME"
		;;
	*)
		SAN="IP:$SERVER_NAME"
		;;
esac

cat > "$SERVER_EXT" <<EOF
basicConstraints=CA:FALSE
keyUsage=digitalSignature
extendedKeyUsage=serverAuth
subjectAltName=$SAN
EOF

openssl x509 -req -in "$SERVER_CSR" -CA "$SERVER_CA_CERT" -CAkey "$SERVER_CA_KEY" \
	-CAcreateserial -days "$DAYS" -out "$SERVER_CERT" -extfile "$SERVER_EXT"

rm -f "$SERVER_CSR" "$SERVER_EXT" "$OUT_DIR/server-ca.srl"
chmod 0600 "$SERVER_CA_KEY" "$SERVER_KEY"
chmod 0644 "$SERVER_CA_CERT" "$SERVER_CERT"

echo "server CA:   $SERVER_CA_CERT"
echo "server cert: $SERVER_CERT"
echo "server key:  $SERVER_KEY"
echo "server CN:   $SERVER_CN"
