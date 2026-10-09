# Hyper Stack

One command-line tool (and a tray dashboard) that gives every Hyper Design developer the same local Laravel environment on Windows, with no manual installs: PHP 8.2/8.3/8.4, nginx, MySQL 8, phpMyAdmin, Composer, Node, trusted HTTPS on `*.test` domains.

Status: **Windows is supported. macOS is not implemented yet** (the design uses static PHP + Caddy; see `docs/spike-flyenv.md`).

## Install (Windows)

Needs a signed-in GitHub CLI while this repository is private (`gh auth login`).

```powershell
gh release download --repo HyperDesignCo/hyper-stack --pattern install.ps1 --output install.ps1 --clobber
powershell -ExecutionPolicy Bypass -File .\install.ps1 -Install
```

That installs `hyper.exe` to `%LOCALAPPDATA%\HyperStack\app`, adds it to your user PATH, adds a **Hyper Stack** Start menu shortcut, then runs `hyper install`, which downloads the stack (about 300 MB) and asks for administrator permission **once** (trust the local certificate, allow hosts-file edits).

Open a new terminal afterwards. Windows SmartScreen may warn about the unsigned exe: choose **More info, Run anyway**.

## Use

Click **Hyper Stack** in the Start menu (or run `hyper ui`) for the dashboard, or use the CLI in a project folder that contains `hyper.json`:

| Command | What it does |
| --- | --- |
| `hyper up` | Start the project: PHP version, database, certificate, hosts entry, `.env`, first-time `setup`, workers |
| `hyper down` | Stop the project's workers (`--all` also stops nginx, PHP and MySQL) |
| `hyper status` | Services and registered projects |
| `hyper doctor` | Check ports (it names Laragon/XAMPP/IIS conflicts), certificate trust, hosts entries. `--fix` applies the safe fixes |
| `hyper db:open` | phpMyAdmin on the project's database |
| `hyper db:import dump.sql.gz` | Import a dump with a progress bar |
| `hyper config set http_port 8080` | Move a port if another program owns 80, 443 or 3306 |
| `hyper self-install [--autostart]` | Install/refresh hyper.exe for this user (`--remove` undoes it) |
| `hyper uninstall [--purge]` | Remove certificate trust and hosts changes; `--purge` also deletes all data |

Add this to the project's repository:

```json
{
  "name": "bazooka",
  "php": "8.4",
  "extensions": ["gd", "intl", "zip"],
  "database": { "engine": "mysql", "name": "bazooka_gro" },
  "setup": ["composer install", "npm ci && npm run build", "php artisan key:generate", "php artisan migrate --seed --force"],
  "workers": ["php artisan queue:work"]
}
```

`php` must be one of `8.2`, `8.3`, `8.4` and must satisfy the project's `composer.lock`. Fields are documented in `schema/hyper.schema.json`. `redis` and `mailpit` services are not supported yet.

## Safety rules

- Admin rights are requested once, by a short-lived helper, never for the whole tool.
- Only the `# >>> hyper-stack >>>` block of the hosts file is touched. `.env` changes only the managed keys and a backup is kept in `.env.hyper-backup`.
- Programs already using a port (Laragon, XAMPP, IIS) are never stopped; `hyper doctor` tells you what to do.
- The MySQL password is generated at install time and stored in Windows Credential Manager, not in a file.
- The dashboard listens on `127.0.0.1` only, behind a random token.

## Develop

```powershell
go test ./...
.\scripts\build.ps1 -Version 0.0.0-dev     # builds dist\ like the release job does
```

Layout follows the brief in `HYPER_STACK_BRIEF.md`: `internal/cli` is flags only, `internal/app` is the logic, `internal/runtime/direct` manages the downloaded binaries. See `docs/RELEASING.md` to publish a version.
