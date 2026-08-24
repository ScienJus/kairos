# Security Policy

[简体中文](SECURITY.zh-CN.md)

## Supported versions

Before `v1.0.0`, security fixes are applied to `main` and the latest published minor release. Older `0.x` releases may not receive backports.

## Reporting a vulnerability

Do not report suspected vulnerabilities through public Issues, Discussions, or pull requests. Use GitHub's private vulnerability reporting on the repository Security page and include:

- the affected version or commit;
- deployment and authentication mode;
- reproduction steps or a minimal proof of concept;
- likely impact;
- any suggested mitigation, if known.

Please omit live tokens, credentials, personal data, and production Artifact content. Maintainers will make a best effort to acknowledge a report within five business days, coordinate validation and remediation privately, and credit the reporter unless anonymity is requested.

If private vulnerability reporting is not available on the Security page, do not publish the details. Open a minimal Issue asking the maintainer to enable a private reporting channel.

## Deployment boundaries

- Trusted Mode accepts caller identity from HTTP headers and is only for loopback use or a network boundary that authenticates and overwrites those headers. Its default listener is `127.0.0.1:8080`.
- Shared or remotely reachable deployments within one trusted collaboration group must use Authenticated Mode with a high-entropy admin token and separately issued identity tokens.
- Authenticated Mode authenticates callers and enforces operation-specific execution rules, but all issued identities belong to one global trust domain. It does not isolate data by tenant, team, project, or object. Run separate Kairos instances for mutually untrusted groups.
- In Authenticated Mode, the operations console keeps its identity token only in browser `sessionStorage`. Signing out or receiving `401` clears the Token and cached API data. Protect the browser session and avoid using shared browser profiles for privileged identities.
- Kairos does not terminate TLS. Put shared deployments behind a correctly configured TLS reverse proxy.
- Treat the SQLite/PostgreSQL database, admin token, identity tokens, and managed Artifact directory as sensitive. Restrict filesystem and database access, back them up together, and never commit them.
- The bundled managed Artifact Store is intended for small files. Review retention settings and storage permissions before accepting untrusted uploads.

Authentication details and limits are documented in the [API Reference](docs/api-reference.md).
