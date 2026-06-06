# Contributing

Contributions are welcome if they keep the project small, explicit, and easy to
audit.

For larger changes or unclear ideas, it can be useful to open a GitHub
Discussion first. This helps avoid unnecessary work before implementation.

## Principles

- Keep changes minimal and focused.
- Prefer simple code over clever abstractions.
- Do not introduce frameworks.
- Do not add dependency injection, factories, builders, or generic layers.
- Avoid new dependencies unless clearly required.
- Preserve the current client/server design.
- Treat this as a security-sensitive tool.

## Development

Run formatting and tests before submitting changes:

```sh
gofmt -w common/*.go systemd-service-toggle/*.go systemd-service-toggled/*.go
go test ./common/... ./systemd-service-toggle/... ./systemd-service-toggled/...
```

If you change shell scripts, check their syntax:

```sh
sh -n cert-generation-examples/create-client-cert.sh
sh -n cert-generation-examples/create-server-cert.sh
```

## Pull Requests

Pull requests should include:

- a short explanation of the change
- the reason the change is needed
- security impact, if any
- test or verification notes

Avoid unrelated refactors. If a cleanup is useful, submit it separately.

## Security Changes

Changes touching authentication, TLS, certificate handling, password handling,
permissions, or systemctl execution should be especially small and easy to
review.

Never add logging of passwords, secrets, private keys, certificate material, or
full production configuration.
