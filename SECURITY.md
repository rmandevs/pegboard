# Security

## Report a vulnerability

Please don't open a public issue. [Report it privately on GitHub](https://github.com/rmandevs/pegboard/security/advisories/new) with the steps to reproduce it and the version you ran (`pegboard --version`).

This is a small project with no guaranteed response time, but reports are taken seriously, and fixes are published in a new release with a security advisory.

## Supported versions

Only the latest release gets security fixes.

## Verify a download

The install script checks each archive against the release's `checksums.txt`. To check a manual download yourself, use the `checksums.txt` from the same release:

```sh
sha256sum --check --ignore-missing checksums.txt
```

## What Pegboard installs

Skills and MCP servers are third-party code that runs with your agent's access. `pegboard.lock` pins each git skill by commit and hash, and `pegboard update` shows a diff before moving to a new version, so read that diff.
