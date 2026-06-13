#!/bin/sh
set -eu
umask 077

OUT_DIR="${1:-client-certs}"
CLIENT_NAME="${2:-remote-systemd-toggle-client}"
CLIENT_CA_CN="${CLIENT_CA_CN:-remote-systemd-toggle client CA}"
CLIENT_CN="${CLIENT_CN:-$CLIENT_NAME}"
DAYS="${DAYS:-1826}"

mkdir -p "$OUT_DIR"

CLIENT_CA_KEY="$OUT_DIR/client-ca.key"
CLIENT_CA_CERT="$OUT_DIR/client-ca.crt"
CLIENT_KEY="$OUT_DIR/client.key"
CLIENT_CSR="$OUT_DIR/client.csr"
CLIENT_CERT="$OUT_DIR/client.crt"
CLIENT_EXT="$OUT_DIR/client.ext"

for file in "$CLIENT_CA_KEY" "$CLIENT_CA_CERT" "$CLIENT_KEY" "$CLIENT_CERT"; do
	if [ -e "$file" ]; then
		echo "$file already exists" >&2
		exit 1
	fi
done

openssl genpkey -algorithm ED25519 -out "$CLIENT_CA_KEY"
openssl req -x509 -new -key "$CLIENT_CA_KEY" -days "$DAYS" -out "$CLIENT_CA_CERT" \
	-subj "/CN=$CLIENT_CA_CN"

openssl genpkey -algorithm ED25519 -out "$CLIENT_KEY"
openssl req -new -key "$CLIENT_KEY" -out "$CLIENT_CSR" \
	-subj "/CN=$CLIENT_CN"

cat > "$CLIENT_EXT" <<EOF
basicConstraints=CA:FALSE
keyUsage=digitalSignature
extendedKeyUsage=clientAuth
subjectAltName=DNS:$CLIENT_NAME
EOF

openssl x509 -req -in "$CLIENT_CSR" -CA "$CLIENT_CA_CERT" -CAkey "$CLIENT_CA_KEY" \
	-CAcreateserial -days "$DAYS" -out "$CLIENT_CERT" -extfile "$CLIENT_EXT"

rm -f "$CLIENT_CSR" "$CLIENT_EXT" "$OUT_DIR/client-ca.srl"
chmod 0600 "$CLIENT_CA_KEY" "$CLIENT_KEY"
chmod 0644 "$CLIENT_CA_CERT" "$CLIENT_CERT"

echo "client CA:   $CLIENT_CA_CERT"
echo "client cert: $CLIENT_CERT"
echo "client key:  $CLIENT_KEY"
echo "client CN:   $CLIENT_CN"
