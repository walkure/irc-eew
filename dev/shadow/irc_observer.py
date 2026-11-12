"""Minimal IRC observer for the dev/shadow stack.

Stays joined to the notification channels (the shadow Ergo keeps no
history, so anything sent while nobody listens is lost) and prints every
NOTICE/PRIVMSG it sees, one line each, to stdout -- i.e. to
`docker compose logs observer`. Reconnects on any disconnect.

Deliberately independent of goirc and internal/irc, so it shows what was
actually put on the wire.
"""
import datetime
import os
import socket
import sys
import time

HOST = os.environ.get("IRC_HOST", "ergo")
PORT = int(os.environ.get("IRC_PORT", "6667"))
NICK = os.environ.get("IRC_NICK", "observer")
CHANNELS = os.environ.get("IRC_CHANNELS", "#eew,#eew-limited")


JST = datetime.timezone(datetime.timedelta(hours=9))


def log(*parts):
    # Fixed +09:00: the alpine image has no zoneinfo, and the notifier's
    # own timestamps are JST.
    now = datetime.datetime.now(JST).isoformat(timespec="milliseconds")
    print(now, *parts, flush=True)


def run_once():
    with socket.create_connection((HOST, PORT), timeout=30) as s:
        s.settimeout(300)
        s.sendall(f"NICK {NICK}\r\nUSER {NICK} 0 * :shadow observer\r\n".encode())
        buf = b""
        while True:
            data = s.recv(4096)
            if not data:
                raise ConnectionError("server closed the connection")
            buf += data
            while b"\r\n" in buf:
                raw, buf = buf.split(b"\r\n", 1)
                line = raw.decode("utf-8", "replace")
                prefix = ""
                rest = line
                if rest.startswith(":"):
                    prefix, _, rest = rest[1:].partition(" ")
                cmd, _, params = rest.partition(" ")
                sender = prefix.split("!", 1)[0]
                if cmd == "PING":
                    s.sendall(b"PONG " + params.encode() + b"\r\n")
                elif cmd == "001":
                    s.sendall(f"JOIN {CHANNELS}\r\n".encode())
                elif cmd == "433":  # nickname in use
                    s.sendall(f"NICK {NICK}_{int(time.time()) % 1000}\r\n".encode())
                elif cmd in ("NOTICE", "PRIVMSG"):
                    target, _, text = params.partition(" ")
                    if target.startswith("#"):
                        log("MSG", cmd, target, sender, text[1:] if text.startswith(":") else text)
                elif cmd in ("JOIN", "PART", "QUIT", "KICK"):
                    log("EVENT", cmd, sender, params)
                elif cmd == "366":
                    log("JOINED", params.split(" ")[1] if " " in params else params)


def main():
    while True:
        try:
            log("CONNECT", f"{HOST}:{PORT}")
            run_once()
        except Exception as e:  # noqa: BLE001 - keep observing whatever happens
            log("DISCONNECTED", repr(e))
        time.sleep(5)


if __name__ == "__main__":
    sys.exit(main())
