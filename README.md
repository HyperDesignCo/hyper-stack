# Hyper Stack

A Laragon/XAMPP-style local environment for Hyper Design developers on Windows and macOS. You pick **one folder** at install time. PHP 7.4 to 8.4, nginx, MySQL 8, phpMyAdmin, Composer and Node go inside it, plus a **`www`** folder: every project you put in `www` is served at `https://<folder>.test`, with trusted HTTPS and no configuration.

Status: **Windows and macOS are supported.** The macOS version is tested on every commit on a real GitHub macOS (Apple Silicon) machine: install, trusted HTTPS, PHP switching, moving the install and uninstall. See [macOS](#macos) for what differs.

Current release: **v0.7.0** (native Windows window; macOS since v0.6.0). Both installers fetch the latest release; to pin this one, pass `-Version v0.7.0` (Windows) or `--version v0.7.0` (macOS).

## Install (Windows)

While this repository is private you need the GitHub CLI signed in (`gh auth login`) and access to the `HyperDesignCo` organization.

```powershell
gh release download --repo HyperDesignCo/hyper-stack --pattern install.ps1 --output install.ps1 --clobber
powershell -ExecutionPolicy Bypass -File .\install.ps1 -Install
```

Options: `-Version v0.7.0`, `-Autostart` (start at sign-in), `-Install` (also run `hyper install`).

It puts `hyper.exe` on your PATH, adds a **Hyper Stack** Start menu entry, then asks **where to install**:

```
Install folder [C:\HyperStack]:
```

Choose a short folder with plain English letters (for example `C:\HyperStack` or `D:\Hyper`). It downloads about 300 MB and asks for administrator permission **once** (to trust the local certificate and allow hosts-file edits). Windows SmartScreen may warn about the unsigned exe: choose **More info, Run anyway**.

You can skip the question: `hyper install --dir D:\Hyper`.

**Running Laragon or XAMPP at the same time?** They own ports 80, 443 and 3306, and the installer will say so before downloading anything. Install next to them on other ports, or stop them first:

```powershell
hyper install --dir D:\Hyper --http-port 8080 --https-port 8443 --mysql-port 3307
```

## macOS

```bash
gh release download --repo HyperDesignCo/hyper-stack --pattern install.sh --output install.sh --clobber
bash install.sh --install
```

Options: `--version v0.7.0`, `--autostart` (start at login), `--install` (also run `hyper install`, about 400 MB). Without `--install`, open a new Terminal and run `hyper install` yourself.

It puts `hyper` on your PATH (new Terminal windows), then asks where to install, like on Windows. macOS asks for your password **once** (to trust the local certificate and allow hosts-file edits).

What differs from Windows:

- **Caddy** is the web server and **php-fpm** runs PHP (one pool per PHP version). Everything else (MySQL 8, phpMyAdmin, Composer, Node, `www`, `*.test`, `hyper php`, `hyper db:import`, `hyper move`) works the same way.
- **PHP 8.0 to 8.4 only.** There is no PHP 7.4 on macOS, so a Laravel 5.8 project cannot run unchanged on a Mac. Do that upgrade on Windows, or get the project to PHP 8.0 first.
- The PHP builds are static and have no `pdo_sqlite`; MySQL projects are unaffected.
- Needs **macOS 14 or newer** (the MySQL build from Oracle is built for it). Tested on macOS 26 only.
- No menu-bar icon. Use `hyper ui` (the dashboard opens in your browser); `hyper self-install --autostart` starts it at login.
- Firefox keeps its own certificate list: import the Hyper Stack CA there once, or use Safari/Chrome.
- Tested on Apple Silicon. Intel Macs build and are expected to work but are not tested.

## Use

1. Open **Hyper Stack** from the Start menu (or run `hyper ui`). On Windows this is a normal desktop window with a tray icon: closing the window keeps your sites running, **Quit** in the tray menu closes the app. **Open www folder** opens the folder in Explorer. The tray menu also has **Open in the browser** for the web version of the same screens.
2. Put a project folder inside `www`, for example `www\blog`.
3. Click **Start** (or **Start all**), or run `hyper up --all`. Open `https://blog.test`.

What Hyper Stack works out from the folder alone (no `hyper.json` needed):

| | |
| --- | --- |
| Address | `<folder>.test` (`My Shop` becomes `my-shop.test`) |
| PHP version | the lowest of 7.4, 8.0, 8.1, 8.2, 8.3, 8.4 that satisfies `composer.json` and every package in `composer.lock`; otherwise 8.3 |
| Web root | `public/`, `public_html/` or `web/` if it contains an index file, else the folder itself |
| Database | created automatically, named after the folder (`my_shop`) |
| `.env` | for Laravel-style folders only: `APP_URL`, `DB_*` are filled in; everything else is kept |

For plain PHP projects the connection is `127.0.0.1`, user `root`, **empty password** (`hyper db:info` shows host and port).

Add a `hyper.json` only when you need more control (a specific PHP version or extensions, first-time `setup` commands, background `workers`). Fields are in `schema/hyper.schema.json`:

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

First-time `setup` commands come from the project's own repository, so **Start all never runs them**. Start such a project on its own (the window shows the commands and asks you to confirm; on the command line `cd` into it and run `hyper up`).

### Upgrading an old project (for example Laravel 5.8 to 12)

Old PHP versions are there for this. A project runs on the PHP it needs, and you move it up one step at a time:

| Laravel | PHP to use | |
| --- | --- | --- |
| 5.8, 6, 7, 8 | 7.4 | `hyper php 7.4` |
| 9 | 8.0 or 8.1 | `hyper php 8.1` |
| 10 | 8.1 or 8.2 | `hyper php 8.1` |
| 11, 12 | 8.2 or newer | `hyper php 8.3` |

`hyper php <version>` (or the PHP dropdown on the project card) saves the version in the project's `hyper.json`, keeps its other settings, and restarts it. `hyper php` alone shows what a project runs on. PHP 7.4, 8.0 and 8.1 are end-of-life and are meant for upgrades and old code, not new projects.

### Start automatically

Two separate switches, in the window (**Settings** tab, and **Start with Hyper Stack** on each project) or on the command line:

- **Start Hyper Stack when I sign in to Windows**: the tray icon and dashboard start by themselves (`hyper autostart on` / `off`).
- **Start at login, per project**: at sign-in Hyper Stack also brings up every flagged project, and nginx, PHP and MySQL as they need (`hyper autostart add [folder]` / `remove [folder]`).

Projects whose `hyper.json` has first-time `setup` commands you have not reviewed are never started automatically: start them once yourself, then they can start at login. Both switches are off until you turn them on.

### Commands

| Command | What it does |
| --- | --- |
| `hyper ui` | The window and tray icon (on macOS: the dashboard in your browser) |
| `hyper up` / `hyper up --all` | Start the project in the current folder / every project in `www` |
| `hyper down` | Stop the project's workers (`--all` also stops nginx, PHP and MySQL) |
| `hyper www` | Open the `www` folder |
| `hyper status` | Services and projects |
| `hyper doctor [--fix]` | Check ports (it names Laragon/XAMPP/IIS), certificate trust and hosts entries |
| `hyper db:open` / `db:info` / `db:import dump.sql.gz` | phpMyAdmin / connection details / import with progress. Foreign keys are not checked while loading (like a normal restore). Add `--fresh` to empty the database first (its old contents are saved to `backups\`), for example after an import that stopped half way |
| `hyper config set http_port 8080` | Move a port later (then `hyper down --all` and start again) |
| `hyper autostart [on\|off\|add\|remove]` | What starts by itself at sign-in (see above) |
| `hyper self-install [--autostart] [--remove]` | Put hyper.exe on PATH, Start menu, optional login start |
| `hyper move <folder>` | Move the whole install to another folder or drive (`--keep-old` keeps the old folder) |
| `hyper uninstall [--purge]` | Remove certificate trust and hosts changes; `--purge` also deletes the runtime and databases |

## Where things live

Everything is in the folder you chose, except `hyper.exe` itself (`%LOCALAPPDATA%\HyperStack\app`):

```
D:\Hyper\
  www\        your projects (never touched by uninstall)
  bin\        PHP, MySQL, nginx, phpMyAdmin, Composer, Node
  data\       MySQL databases
  certs\ logs\ run\ sites\ cache\
```

`hyper uninstall --purge` removes everything above **except `www`**.

**Running low on space on that drive?** `hyper move E:\HyperStack` moves the whole install, projects and databases included, to another folder or drive. It copies first, checks that every file matches, starts the projects that were running from the new place, and only then deletes the old folder (if anything fails it switches back and the old one is untouched). No administrator prompt.

## Safety rules

- Admin rights are requested once, by a short-lived helper, never for the whole tool.
- Only the `# >>> hyper-stack >>>` block of the hosts file is touched. `.env` changes only the managed keys, and a backup is kept in `.env.hyper-backup`.
- Programs already using a port (Laragon, XAMPP, IIS) are never stopped; `hyper doctor` tells you what to do.
- The local certificate authority can only vouch for `*.test`, so it cannot be abused to impersonate real websites.
- MySQL listens on `127.0.0.1` only, and root has **no password**, exactly like Laragon and XAMPP, so `root` with an empty password just works for any project. That also means any program running as you on this machine can use the databases: do not keep production data in them. (Installs made by version 0.1.x keep the generated password they stored in Windows Credential Manager.)
- One broken project cannot take the others down: each site's web config is checked before it goes live.
- The dashboard listens on `127.0.0.1` only, behind a random token.

## Develop

```powershell
go test ./...
.\scripts\build.ps1 -Version 0.0.0-dev     # builds dist\ like the release job does
```

`internal/cli` is flags only, `internal/app` is the logic, `internal/runtime/direct` manages the downloaded binaries. `HYPER_HOME` points the tool at a throwaway home folder, which is how the install flow is tested without touching a real install. See `docs/RELEASING.md` to publish a version.
