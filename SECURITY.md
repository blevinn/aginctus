# Security Policy

Aginctus is an experimental, single-operator NixOS/Incus agent lab. The v0.1
release is a pilot, not an audited or production-hardened multi-tenant platform.
The [technical security guide](docs/reference/security.md) documents current
controls, known limitations, and evidence boundaries.

## Versions and maintenance

| Version | Security handling |
| --- | --- |
| Current published v0.1.x pilot release | Vulnerability reports are welcome; assessment and fixes are best effort. |
| Unreleased `main` / v0.2 development | Reports are welcome; fixes may be made on the active development branch. |
| Older or otherwise unmaintained releases | No security maintenance or backport commitment. |

There is no guaranteed support window, response deadline, remediation timeline,
or backport policy. A successful hermetic test or pilot check is not a security
certification.

## Privately report a vulnerability

Use [GitHub Private Vulnerability Reporting](https://github.com/blevinn/aginctus/security/advisories/new)
for this repository. From the repository's **Security** tab, open
**Advisories** and select **Report a vulnerability**. This is the
maintainer-approved confidential reporting channel.

Please provide, to the extent safe and available:

- A concise description of the vulnerability and its potential impact.
- The affected release, commit or configuration, and prerequisites.
- Reproduction steps using a disposable environment and synthetic data, where
  possible.
- Suggested mitigation, if known, and a safe way to coordinate follow-up.

Avoid including actual access tokens, passwords, private keys, personal data,
unredacted configuration dumps, or real provider credentials. **Do not open a
public GitHub issue, pull request, or discussion containing vulnerability
details or exploit steps.** If private reporting is unavailable, report only
that the private intake mechanism is inaccessible in an ordinary issue, with
no sensitive details.

## Response and disclosure

The maintainer reviews reports on a **best-effort** basis and may request
additional information through the private advisory. There is no service-level
agreement, guaranteed fix, bounty commitment, or obligation to maintain
historical pilot versions. Please coordinate public disclosure through the
private report rather than exposing a vulnerability before it can be assessed.

For non-sensitive bugs or feature requests, use the repository's existing
[public issue templates](.github/ISSUE_TEMPLATE/). External pull requests
are not currently being accepted.
