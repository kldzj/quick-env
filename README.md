# quick-env

Two small command-line tools, for Linux and macOS, that hand secrets (API keys,
tokens, passwords) to commands.

If you type a token into a command, as in `mycli login --token 8f2c1e9d…`, it ends
up in your shell history. Keeping it in a `.env` file leaves a plain-text copy on
disk, and pasting it into a coding agent's chat sends it to the agent.

- **`qe`** is for a secret you need once. Put `@` where the secret goes, and `qe`
  asks for it in a hidden prompt, then runs the command with the secret filled
  in. Nothing is stored.
- **`qenv`** is for secrets you reuse. It saves them in your system keyring and
  gives them to a command as environment variables when you start it with
  `qenv run`. A coding agent can use them the same way, and `qenv run` masks
  them in the output the agent sees.

The two are independent: `qe` doesn't read what `qenv` saves.

## Install

Requirements: Linux or macOS, on x86-64 or arm64.

- **Linux:** `qenv` needs a keyring that implements the Secret Service API, such
  as gnome-keyring (tested), KeePassXC with Secret Service integration enabled,
  or a recent KWallet, running in your desktop session. Over plain SSH or on a
  headless server it reports `no keyring available`.
- **macOS:** `qenv` uses the built-in Keychain (tested on macOS 27).

```
curl -fsSL https://raw.githubusercontent.com/kldzj/quick-env/main/install.sh | sh
```

This downloads the release archive for your system, checks its SHA-256 against
the release's `checksums.txt`, and puts `qe` and `qenv` in `~/.local/bin` (no
sudo). If that directory isn't on your `PATH`, the script tells you how to add
it. To choose the directory or pin a version, set `INSTALL_DIR` or `VERSION` for
`sh`:

```
curl -fsSL https://raw.githubusercontent.com/kldzj/quick-env/main/install.sh | INSTALL_DIR=~/bin sh
curl -fsSL https://raw.githubusercontent.com/kldzj/quick-env/main/install.sh | VERSION=v0.1.0 sh
```

You can also download an archive from
[Releases](https://github.com/kldzj/quick-env/releases) and put `qe` and `qenv`
on your `PATH` yourself, or install with Go 1.26 or newer:

```
go install github.com/kldzj/quick-env/cmd/...@latest
```

The macOS binaries aren't notarized by Apple. If you download an archive in a
browser, macOS blocks them until you remove the quarantine flag:

```
xattr -d com.apple.quarantine qe qenv
```

Installing with the script, `curl` or `go install` avoids this.

## qe

Write the command as usual, with `qe` in front and `@` where the secret goes:

```
qe mycli login --token @
```

`qe` asks for the secret, then shows the command with the secret masked so you
can check it before it runs:

```
$ qe mycli login --token @
  secret 1/1 (token): ••••••••••••••••••••••••••••••••   ← paste it; it shows as dots

  mycli login --token ••••••••3f9a                       ← only the last 4 characters
  enter to run · esc to cancel
```

Enter runs `mycli login --token <your secret>`; Esc cancels and nothing runs.
Your shell history only ever contains `qe mycli login --token @`.

- `@` must be its own argument (`--token @`) or follow `=` (`--token=@`).
  An `@` inside other text, like `user@host`, is left alone.
- Each `@` gets its own prompt, labelled with the argument in front of it
  without its dashes (`token` for both `--token @` and `--token=@`).
- `qe -e NAME COMMAND ...` passes the secret as environment variable `NAME`
  instead. `-e` can be repeated and combined with `@`.
- The preview shows the last 4 characters of secrets that are 12 characters or
  longer; shorter ones are shown as dots only.
- Leading and trailing whitespace from a paste is trimmed. Only the first line of
  a paste is used. Anything typed before a prompt or preview appears is ignored,
  so a stray newline in a paste can't confirm the command for you.
- Esc, Ctrl-C or Ctrl-D cancels and nothing runs. Arrow keys send Esc too, so they
  also cancel. Backspace and Ctrl-U edit as usual.
- The command then replaces `qe`: it runs in your terminal and its exit code is
  yours.
- `qe` and `qenv set` need an interactive terminal, so scripts and coding agents
  can't use them.

## qenv

Save a secret once, then start the commands that need it with `qenv run`. Inside
the command it's an ordinary environment variable (`$API_TOKEN`).

```
qenv set API_TOKEN       # hidden prompt, masked preview, enter saves
qenv ls                  # API_TOKEN  ••••••••x9Qk
qenv rm API_TOKEN
qenv get API_TOKEN       # raw value, no trailing newline
qenv run -- ./deploy.sh  # run one command with every saved variable set
qenv env                 # export lines, for eval "$(qenv env)"
```

`qenv set` asks with the same prompt as `qe`. Every command started with
`qenv run` gets every saved variable, and they override variables of the same
name already in your environment.

### Masking

`qenv run` replaces saved values with `[qenv:NAME]` in the command's stdout and
stderr when they **aren't going to a terminal**: a pipe, a file, or a coding
agent capturing the output. Output going to your terminal is left alone, so
colours and progress bars keep working. `--no-mask` turns masking off.

Masking is a safety net, not a guarantee:

- It only catches values exactly as stored, not encoded forms such as base64 or
  URL-escaping.
- Values shorter than 8 characters aren't masked, since they'd also match
  ordinary text.
- Only the command's stdout and stderr are masked, not files it writes or data
  it sends elsewhere.
- While masking, `qenv run` waits for the command's output to close. A command
  that leaves a background process holding its output open keeps `qenv run`
  waiting too. `--no-mask` avoids this.

### With Claude Code and other coding agents

Saving a secret with `qenv set` means you never paste it into the chat. Add
something like this to `~/.claude/CLAUDE.md` (or your agent's equivalent):

```markdown
## Secrets (qenv)
- Saved secrets (e.g. API_TOKEN) live in the keyring via `qenv`; `qenv ls` lists them, masked.
- Run commands that need one via `qenv run -- <command>`. Its output is masked.
- If a tool only takes the secret as an argument, reference it inside qenv run:
  `qenv run -- sh -c 'tool --token "$API_TOKEN"'` (prefer tools that read the variable directly).
- Never run `qenv get`, `qenv env` or `qenv run --no-mask`, and never print a secret's value.
- Only the user can save secrets (`qenv set` needs their terminal). If one is missing, ask them to run `qenv set NAME`.
```

Agents capture output, so `qenv run` masks it, within the limits above. Anything
an agent can use, it can also read, though: masking keeps accidental leaks out
of the transcript; it can't stop a deliberate one.

### Loading into a shell

`eval "$(qenv env)"` loads every saved variable into the current shell. A small
function in your `.zshrc` or `.bashrc` makes that a single word:

```sh
qload() { eval "$(qenv env)"; }
```

You can put `eval "$(qenv env)"` itself in your shell's startup file to load
everything into every new shell, but think twice: every program you start then
gets every secret, including package install scripts, and coding agents started
from that shell see them directly, bypassing `qenv run` and its masking.

### Where secrets are stored

Variables stay in your system keyring, which encrypts them on disk and is
usually unlocked when you log in, until you `qenv rm` them.

- **Linux:** through the freedesktop Secret Service API. If the keyring is
  locked, commands that need it show the keyring's unlock dialog. `qenv env`
  never prompts: it loads only what's already unlocked.
- **macOS:** in your default (normally login) Keychain, through Apple's
  `security` tool. Entries show up in Keychain Access as "qenv: NAME", with
  values stored as `qenv-base64url:…` so they pass through `security` intact
  (an encoding, not extra encryption). Because access goes through Apple's tool,
  the Keychain doesn't ask again after `qenv` updates; for the same reason, any
  program running as you can read these entries without a prompt. If the
  Keychain is locked, macOS shows its unlock dialog, even for `qenv env`, which
  gives up after 3 seconds so a new shell never hangs.
  `QENV_KEYCHAIN=/path/to/file.keychain-db` uses a different keychain.

## What this protects against

| Risk | Covered |
|---|---|
| Secrets in shell history | ✓ only `@` or the variable name is recorded |
| Secrets pasted into an agent's chat | ✓ you save them with `qenv set` instead |
| Secrets leaking into an agent's transcript | mostly: masked when run via `qenv run` (see the limits above) |
| Plaintext `.env` files and config | ✓ the keyring encrypts them at rest |
| A command run with `qenv run` misusing a secret | ✗ every command gets every saved variable |
| Other programs running as you while the keyring is unlocked | ✗ they can read the keyring too |
| `ps` while a `qe` command runs | ✗ arguments are visible to other processes; prefer `-e` where the tool supports it |

## Exit codes

`qe` and `qenv run` exit with the command's own exit code (128+n if it was killed
by signal n). Otherwise: 130 cancelled, 127 command not found, 126 command
couldn't be started, 2 usage error, 1 any other error.

## Development

```
go test ./...      # unit tests
test/run.sh        # everything, including keyring and end-to-end tests
```

`test/run.sh` never touches your real keyring. On Linux it starts a throwaway
gnome-keyring on a private D-Bus with its own data directory, and needs
`gnome-keyring-daemon`, `dbus-run-session`, `busctl` and `python3`. On macOS it
uses temporary keychain files and needs `python3`; in a desktop session it skips
the locked-keychain tests, which would show macOS's unlock dialog.

## License

MIT
