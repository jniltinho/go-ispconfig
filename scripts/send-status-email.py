#!/usr/bin/env python3
"""Send go-ispconfig task status e-mails via SMTP (STARTTLS, port 587).

Credentials come from the gitignored `.mail.env` file at the repo root
(SMTP_HOST/SMTP_PORT/SMTP_USER/SMTP_PASS/MAIL_TO) — never hardcoded here.

Usage:
  send-status-email.py --subject "..." --body-file status.md [--attach img1.png ...]
  echo "body" | send-status-email.py --subject "..."
"""

import argparse
import mimetypes
import smtplib
import sys
from email.message import EmailMessage
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent


def load_env() -> dict:
    env = {}
    env_file = ROOT / ".mail.env"
    if not env_file.exists():
        sys.exit("missing .mail.env (gitignored) at repo root")
    for line in env_file.read_text().splitlines():
        line = line.strip()
        if line and not line.startswith("#") and "=" in line:
            k, _, v = line.partition("=")
            env[k.strip()] = v.strip()
    for key in ("SMTP_HOST", "SMTP_PORT", "SMTP_USER", "SMTP_PASS", "MAIL_TO"):
        if key not in env:
            sys.exit(f".mail.env missing {key}")
    return env


def main() -> None:
    p = argparse.ArgumentParser()
    p.add_argument("--subject", required=True)
    p.add_argument("--body-file", help="file with the body (markdown/plain); default: stdin")
    p.add_argument("--attach", nargs="*", default=[], help="attachment paths (e.g. docs/prints/*.png)")
    args = p.parse_args()

    env = load_env()
    body = Path(args.body_file).read_text() if args.body_file else sys.stdin.read()

    msg = EmailMessage()
    msg["From"] = env["SMTP_USER"]
    msg["To"] = env["MAIL_TO"]
    msg["Subject"] = args.subject
    msg.set_content(body)

    for path in args.attach:
        f = Path(path)
        if not f.exists():
            print(f"warn: attachment not found, skipping: {path}", file=sys.stderr)
            continue
        ctype, _ = mimetypes.guess_type(f.name)
        maintype, subtype = (ctype or "application/octet-stream").split("/", 1)
        msg.add_attachment(f.read_bytes(), maintype=maintype, subtype=subtype, filename=f.name)

    with smtplib.SMTP(env["SMTP_HOST"], int(env["SMTP_PORT"]), timeout=30) as s:
        s.starttls()
        s.login(env["SMTP_USER"], env["SMTP_PASS"])
        s.send_message(msg)
    print(f"sent: '{args.subject}' -> {env['MAIL_TO']}")


if __name__ == "__main__":
    main()
