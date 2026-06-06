# Security Policy

## Supported Versions

Only the current `main` branch is supported.

This project is small and does not maintain multiple release branches at this
time.

## Reporting a Vulnerability

Please do not open a public issue for security vulnerabilities.

Report security issues privately to the maintainer ( mail@thomas-kuhlmann.de ). Include:

- a short description of the issue
- affected version or commit
- steps to reproduce, if possible
- expected impact
- any relevant logs or configuration snippets

Do not include private keys, passwords, real service names, or production
hostnames in reports.

## Scope

Security-sensitive areas include:

- TLS and mutual TLS handling
- certificate verification
- password handling and logging
- Argon2id parameter handling
- secret file permissions
- config file permissions
- systemctl command execution
- wrong-password backoff and self-disable behavior

## Design Notes

The daemon is intended to run as root because it calls `systemctl` directly.
Keep changes small, explicit, and easy to audit.

The project deliberately avoids shell command execution for service control.
`systemctl` is called directly with argument lists.

Passwords must never be logged.
