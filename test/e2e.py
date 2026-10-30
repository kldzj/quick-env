"""Drive qe through a real pty like a person would, and check what they'd see."""
import os, re, signal, subprocess, sys, tempfile, termios, time, select

BIN = sys.argv[1]
QE = os.path.join(BIN, "qe")
QENV = os.path.join(BIN, "qenv")
MACOS = sys.platform == "darwin"
# In a macOS desktop session a locked keychain brings up an unlock dialog for whoever runs this.
DESKTOP = MACOS and subprocess.run(["launchctl", "managername"], capture_output=True, text=True).stdout.strip() == "Aqua"
failures = 0

# Records the terminal settings right before and after the program, from inside its
# terminal: on macOS that terminal is gone for everyone else once the program exits.
WRAP = 'printf "TTY-BEFORE:%s\\n" "$(stty -g)"; "$@"; s=$?; printf "\\nTTY-AFTER:%s\\n" "$(stty -g)"; exit $s'


def ignore_pendin(state):
    """macOS's kernel sets PENDIN ("re-process pending input") by itself whenever a
    terminal switches back to canonical mode; it isn't a setting anyone restores."""
    if not MACOS:
        return state
    pendin = getattr(termios, "PENDIN", 0x20000000)
    return re.sub(r"lflag=([0-9a-f]+)", lambda m: "lflag=%x" % (int(m.group(1), 16) & ~pendin), state)


def kill(p):
    """Kill the program and everything it started; macOS sometimes refuses the group kill."""
    for k in (lambda: os.killpg(p.pid, signal.SIGKILL), p.kill):
        try:
            k()
            return
        except OSError:
            pass


def run(args, steps, timeout=5, env=None, prog=None):
    master, slave = os.openpty()

    def ctty():
        os.setsid()
        import fcntl
        fcntl.ioctl(0, termios.TIOCSCTTY, 0)

    p = subprocess.Popen(["sh", "-c", WRAP, "sh", prog or QE] + args, stdin=slave, stdout=slave, stderr=slave,
                         preexec_fn=ctty, close_fds=True, env=env)
    out = b""

    def pump(until=None, t=timeout):
        nonlocal out
        end = time.time() + t
        while time.time() < end:
            if until is not None and until in out:
                return True
            r, _, _ = select.select([master], [], [], 0.05)
            if r:
                try:
                    out += os.read(master, 4096)
                except OSError:
                    break
            elif until is None and p.poll() is not None:
                break
        return until is None or until in out

    for wait_for, send in steps:
        if wait_for is not None and not pump(wait_for.encode()):
            print(f"   !! timed out waiting for {wait_for!r}; output so far: {out!r}")
            kill(p)
            return out.decode(errors="replace"), None, False
        if isinstance(send, float):
            time.sleep(send)
            pump(t=0.1)
        elif callable(send):
            send(p)
        else:
            os.write(master, send.encode())
    # Keep reading while waiting: the program blocks once the terminal's buffer is
    # full, and macOS's is small.
    pump(t=timeout)
    if p.poll() is None:
        kill(p)
    try:
        p.wait(timeout=1)
    except subprocess.TimeoutExpired:
        pass
    pump(t=0.3)
    os.close(master); os.close(slave)
    text = out.decode(errors="replace")
    states = {k: ignore_pendin(v) for k, v in re.findall(r"TTY-(BEFORE|AFTER):(\S+)", text)}
    restored = "BEFORE" in states and states.get("BEFORE") == states.get("AFTER")
    return text, p.returncode, restored


def check(name, cond, out):
    global failures
    print(("ok   " if cond else "FAIL ") + name)
    if not cond:
        failures += 1
        print("     output:", repr(out))


# 1. the basic case: one @ as its own argument; also checks the command sees a normal (cooked) terminal
probe = 'printf "[%s]\\n" "$1"; stty -a | tr " " "\\n" | grep -E -x -- "-?icanon"'
out, code, restored = run(["sh", "-c", probe, "api-key", "@"], [
    ("(api-key): ", "  supersecretvalue1234 \r"),
    ("enter to run", "\r"),
])
check("standalone @: secret reaches the command, trimmed", "[supersecretvalue1234]" in out, out)
check("standalone @: preview masks all but last 4", "••••••••1234" in out and "supersecret" not in out.split("[")[0], out)
check("standalone @: one dot per typed char", "•" * 20 in out, out)
check("standalone @: command runs with a normal terminal (icanon on)", "\nicanon" in out.replace("\r", ""), out)
check("standalone @: exit code 0", code == 0, out)
check("standalone @: terminal restored", restored, out)

# 2. env var mode
out, code, restored = run(["-e", "FOO", "sh", "-c", 'echo "FOO=$FOO"'], [
    ("(FOO): ", "abc\r"),
    ("enter to run", "\r"),
])
check("-e: env var set", "FOO=abc" in out, out)
check("-e: short secret fully masked in preview", "FOO=•••••••• sh -c" in out, out)

# 2b. -e replaces a value that's already exported, instead of adding a duplicate
for prog, name in ((["sh", "-c", 'echo "FOO=$FOO"'], "sh"), (["printenv", "FOO"], "printenv")):
    out, code, restored = run(["-e", "FOO"] + prog, [
        ("(FOO): ", "new\r"),
        ("enter to run", "\r"),
    ], env={**os.environ, "FOO": "old"})
    check(f"-e over existing var: {name} sees the new value", "new" in out.split("cancel")[-1] and "old" not in out, out)

# 3. multiple placeholders, asked in order, = form
out, code, restored = run(["printf", "%s|%s\\n", "@", "--x=@"], [
    ("secret 1/2", "first\r"),
    ("secret 2/2 (x): ", "second\r"),
    ("enter to run", "\r"),
])
check("two @: both substituted in order", "first|--x=second" in out, out)

# 4. esc at the confirm step cancels, command never runs
out, code, restored = run(["sh", "-c", "echo $((40+2))", "@"], [
    ("secret", "abc\r"),
    ("enter to run", "\x1b"),
])
check("esc at confirm: not run", "42" not in out and "cancelled" in out, out)
check("esc at confirm: exit 130", code == 130, out)
check("esc at confirm: terminal restored", restored, out)

# 5. ctrl-c while typing the secret
out, code, restored = run(["sh", "-c", "echo $((40+2))", "@"], [
    ("secret", "ab\x03"),
])
check("ctrl-c at secret: not run, exit 130", "42" not in out and code == 130, out)
check("ctrl-c at secret: terminal restored", restored, out)

# 5b. an interrupt sent from outside (not Ctrl-C) while asking
def interrupt_program(p):
    """SIGINT to the program only, not the shell wrapped around it."""
    pids = subprocess.run(["pgrep", "-P", str(p.pid)], capture_output=True, text=True).stdout.split()
    for pid in pids:
        os.kill(int(pid), signal.SIGINT)


out, code, restored = run(["sh", "-c", "echo $((40+2))", "@"], [
    ("secret", "ab"),
    (None, 0.2),
    (None, interrupt_program),
])
check("SIGINT from outside at secret: not run, exit 130", "42" not in out and code == 130, out)
check("SIGINT from outside at secret: terminal restored", restored, out)

# 6. a paste with a stray extra newline must not skip the confirm step
out, code, restored = run(["sh", "-c", "echo $((40+2))", "@"], [
    ("secret", "abc\r\r"),
    ("enter to run", 0.5),
    (None, ""),
])
ran_early = "42" in out
out2 = out
check("stray newline in paste does not auto-confirm", not ran_early, out2)

# 7. empty enter asks again
out, code, restored = run(["sh", "-c", 'echo "got=$1"', "k", "@"], [
    ("(k): ", "\r"),
    (None, 0.3),
    (None, "real\r"),
    ("enter to run", "\r"),
])
check("empty input asks again", out.count("(k): ") == 2 and "got=real" in out, out)

# 8. the command's exit code comes through
out, code, restored = run(["sh", "-c", "exit 7", "@"], [
    ("secret", "abc\r"),
    ("enter to run", "\r"),
])
check("exit code passes through", code == 7, out)

# ---------------------------------------------------------------- qenv
# Only against the throwaway keyring test/run.sh starts; never your real one.
if os.environ.get("QENV_TEST_KEYRING") != "1":
    print("skip qenv cases: run them through test/run.sh")
else:
    def qenv(*args, env=None):
        return subprocess.run([QENV, *args], capture_output=True, env=env, timeout=20)

    def lock_keyring():
        if MACOS:
            cmd = ["security", "lock-keychain", os.environ["QENV_KEYCHAIN"]]
        else:
            cmd = ["busctl", "--user", "call", "org.freedesktop.secrets", "/org/freedesktop/secrets",
                   "org.freedesktop.Secret.Service", "Lock", "ao", "1", "/org/freedesktop/secrets/collection/login"]
        subprocess.run(cmd, check=True, capture_output=True)

    # start from an empty keyring (the Go keyring tests leave items behind)
    for line in qenv("ls").stdout.decode().splitlines():
        qenv("rm", line.split()[0])
    r = qenv("ls")
    check("qenv ls when empty: says so", r.returncode == 0 and r.stdout == b"" and b"no saved variables" in r.stderr, r)

    out, code, restored = run(["set", "HF_TOKEN"], [
        ("(HF_TOKEN): ", "  hf_secretvalue1234 \r"),
        ("enter to save", "\r"),
    ], prog=QENV)
    check("qenv set: preview masks the value", "HF_TOKEN=••••••••1234" in out and "replaces" not in out, out)
    check("qenv set: saved", code == 0 and "saved HF_TOKEN" in out, out)
    check("qenv set: terminal restored", restored, out)

    r = qenv("get", "HF_TOKEN")
    check("qenv get: exact value, trimmed, no trailing newline", r.returncode == 0 and r.stdout == b"hf_secretvalue1234", r)

    out, code, restored = run(["set", "HF_TOKEN"], [
        ("(HF_TOKEN): ", "hf_othervalue5678\r"),
        ("enter to save", "\x1b"),
    ], prog=QENV)
    check("qenv set over existing: preview shows what it replaces",
          "HF_TOKEN=••••••••5678  (replaces ••••••••1234)" in out, out)
    check("qenv set, esc: old value kept", code == 130 and qenv("get", "HF_TOKEN").stdout == b"hf_secretvalue1234", out)

    out, code, restored = run(["set", "PIN"], [("(PIN): ", "1234\r"), ("enter to save", "\r")], prog=QENV)
    r = qenv("ls")
    check("qenv ls: sorted, aligned, masked",
          r.stdout.decode() == "HF_TOKEN  ••••••••1234\nPIN       ••••••••\n", r)

    r = qenv("run", "--", "sh", "-c", 'echo "out=$HF_TOKEN pin=$PIN"; echo "err=$HF_TOKEN" >&2')
    check("qenv run captured: stdout masked, short values left alone",
          r.stdout == b"out=[qenv:HF_TOKEN] pin=1234\n", r)
    check("qenv run captured: stderr masked", r.stderr == b"err=[qenv:HF_TOKEN]\n", r)

    r = qenv("run", "--", "sh", "-c", 't=$HF_TOKEN; printf "a%s" "${t%????}"; sleep 0.3; printf "%sb\\n" "${t#${t%????}}"')
    check("qenv run captured: value split across two writes still masked", r.stdout == b"a[qenv:HF_TOKEN]b\n", r)

    r = qenv("run", "--no-mask", "--", "sh", "-c", 'echo "$HF_TOKEN"')
    check("qenv run --no-mask: raw value", r.stdout == b"hf_secretvalue1234\n", r)

    r = qenv("run", "--no-mask", "--", "printenv", "HF_TOKEN", env={**os.environ, "HF_TOKEN": "old"})
    check("qenv run: replaces a value already in the environment", r.stdout == b"hf_secretvalue1234\n", r)

    out, code, restored = run(["run", "--", "sh", "-c", 'test -t 1 && echo "tty $HF_TOKEN"'], [], prog=QENV)
    check("qenv run in a terminal: output untouched, command sees the terminal",
          "tty hf_secretvalue1234" in out and code == 0, out)

    # Ctrl-C while qenv run masks the output: the command gets it from the terminal
    # and its last words still come through.
    inner = 'trap "echo got-INT; exit 3" INT; echo started; while :; do sleep 0.1; done'
    outer = f"{QENV} run -- sh -c '{inner}' </dev/null | (trap '' INT; cat)"
    out, code, restored = run(["-c", outer], [("started", 0.2), (None, "\x03")], prog="sh")
    check("qenv run captured, Ctrl-C in the terminal: command gets it, output flushed", "got-INT" in out, out)

    r = qenv("run", "--", "sh", "-c", "exit 7")
    check("qenv run captured: exit code passes through", r.returncode == 7, r)
    r = qenv("run", "--", "sh", "-c", "kill -TERM $$")
    check("qenv run captured: death by signal reported as 128+n", r.returncode == 143, r)
    r = qenv("run", "--", "no-such-command-xyz")
    check("qenv run: unknown command → 127", r.returncode == 127, r)

    r = qenv("env")
    check("qenv env: quoted export lines",
          r.stdout == b"export HF_TOKEN='hf_secretvalue1234'\nexport PIN='1234'\n" and r.returncode == 0, r)
    r2 = subprocess.run(["sh", "-c", 'eval "$1"; printf "%s" "$HF_TOKEN"', "sh", r.stdout.decode()], capture_output=True)
    check("qenv env: eval loads the value", r2.stdout == b"hf_secretvalue1234", r2)

    if not MACOS:
        no_bus = {k: v for k, v in os.environ.items() if k != "DBUS_SESSION_BUS_ADDRESS"}
        no_bus["XDG_RUNTIME_DIR"] = tempfile.mkdtemp()
        t0 = time.time()
        r = qenv("env", env=no_bus)
        check("qenv env without a session bus: silent, nothing loaded, fast",
              r.returncode == 0 and r.stdout == b"" and r.stderr == b"" and time.time() - t0 < 1, r)

    r = qenv("rm", "HF_TOKEN", "NOPE")
    check("qenv rm: removes, reports unknown names",
          r.returncode == 1 and b"removed HF_TOKEN" in r.stderr and b"no saved variable NOPE" in r.stderr, r)
    check("qenv rm: value is gone", qenv("get", "HF_TOKEN").returncode == 1, r)

    if DESKTOP:
        print("skip locked-keychain cases: in a desktop session they'd show macOS's unlock dialog")
    else:
        # Last, because the test keyring can't be unlocked again without a dialog.
        lock_keyring()
        t0 = time.time()
        r = qenv("env")
        check("qenv env on a locked keyring: silent, nothing loaded, fast",
              r.returncode == 0 and r.stdout == b"" and r.stderr == b"" and time.time() - t0 < 1, r)
        t0 = time.time()
        r = qenv("get", "PIN")
        took = time.time() - t0
        check(f"qenv get on a locked keyring with no display to ask on: fails cleanly ({took:.1f}s)",
              r.returncode == 1 and b"locked" in r.stderr and r.stdout == b"" and took < 5, r)

print()
print("ALL PASSED" if failures == 0 else f"{failures} FAILED")
sys.exit(1 if failures else 0)
