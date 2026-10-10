# Releasing Hyper Stack

## Publish a version

```powershell
git tag v0.1.0
git push origin v0.1.0
```

The `release` workflow (`.github/workflows/release.yml`) then runs the tests, builds `hyper.exe` with the version injected, and publishes a GitHub Release containing:

| Asset | Purpose |
| --- | --- |
| `hyper-windows-amd64.zip` | the exe |
| `checksums.txt` | SHA-256 of the zip and `install.ps1`; `install.ps1` refuses a zip that does not match |
| `install.ps1` | the installer developers run |

To rehearse without publishing, run `.\scripts\build.ps1 -Version 0.1.0` and `.\scripts\test-release.ps1`.

## Updating developers

Re-running `install.ps1` (or `hyper self-install` with a newer exe) replaces `hyper.exe`, stops and restarts the dashboard if it was running, and changes nothing else. Projects, databases and the certificate are untouched.

## Known gaps

- **Not code-signed.** Windows SmartScreen warns on first run. To sign: buy a code-signing certificate, store it as a repository secret, and add a signing step (for example `signtool`) in `release.yml` between "Build" and "Publish". Do this before wide rollout.
- **macOS is not signed or notarized.** `install.sh` downloads with curl/gh, which avoids the Gatekeeper quarantine, but a binary downloaded in a browser will be blocked until `xattr -d com.apple.quarantine hyper`. Intel Macs are built but not tested in CI.
- **Private repository.** Developers need `gh auth login` to download releases. If the repository is made public, the one-line `irm ... | iex` form in `scripts/install.ps1` works too.
- No automatic update check yet; announce new versions to the team.
