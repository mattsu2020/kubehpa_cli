# Security Policy

## Supported Versions

Security fixes target the latest released version of `kubectl-hpa-status`.
Users should prefer the newest release from GitHub Releases, Krew, or Homebrew.

## Reporting a Vulnerability

Please report security issues privately through GitHub Security Advisories when
available. If that is not possible, open a minimal public issue without exploit
details and request a private contact path.

Include:

- affected version or commit
- operating system and Kubernetes version
- whether the issue requires cluster credentials
- minimal reproduction details

## Security Model

The plugin uses the user's existing kubeconfig credentials and does not run a
server. It reads HPA objects and Events by default. Mutating behavior is limited
to explicit `--apply` workflows.

Safety rules for mutating workflows:

- `--suggest` emits dry-run patch commands.
- `--apply` defaults to server-side dry-run.
- persistent changes require `--dry-run=false`.
- confirmation is required unless `-y` is explicitly provided.

## Supply Chain

The release pipeline uses GoReleaser and generates checksums and SBOM metadata.
CI also runs tests, linting, govulncheck, gosec, and CodeQL.
