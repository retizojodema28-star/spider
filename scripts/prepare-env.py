#!/usr/bin/env python3
"""Create owner-readable Compose credentials without overwriting an existing install."""
import argparse
import os
from pathlib import Path
import secrets
from urllib.parse import urlsplit

p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--existing-preview', action='store_true', help='Keep the original local preview database passwords; development mode only.')
p.add_argument('--preview', action='store_true', help='Fresh HTTP preview with random database credentials.')
p.add_argument('--admin-origin', default='', help='Production HTTPS origin, e.g. https://apk.example.com')
default_runtime = Path(
    os.environ.get('APK_HTML_RUNTIME_DIR', str(Path.home() / 'apk-html-go-runtime'))
)
p.add_argument('--output', type=Path, default=default_runtime / 'go.env')
a = p.parse_args()
origin = urlsplit(a.admin_origin)
if a.admin_origin and (origin.scheme != 'https' or not origin.hostname or origin.username or origin.path or origin.query or origin.fragment or any(c.isspace() for c in a.admin_origin)):
    p.error('--admin-origin must be an HTTPS origin without credentials, path, query or fragment.')
if not (a.preview or a.existing_preview) and not a.admin_origin:
    p.error('Production requires --admin-origin https://YOUR_DOMAIN; use --preview for local HTTP.')
a.output.parent.mkdir(parents=True, exist_ok=True)
settings = {
    'APP_ENV': 'development' if a.preview or a.existing_preview else 'production',
    'APP_PORT': '18082',
    'ADMIN_ORIGIN': a.admin_origin,
    'ADMIN_USERNAME': 'admin',
    'ADMIN_PASSWORD': secrets.token_urlsafe(32),
    'MYSQL_PASSWORD': 'local-dev-password' if a.existing_preview else secrets.token_hex(24),
    'MYSQL_ROOT_PASSWORD': 'local-root-password' if a.existing_preview else secrets.token_hex(24),
}
try:
    fd = os.open(a.output, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
except FileExistsError:
    raise SystemExit(f'Kept existing credentials: {a.output}')
with os.fdopen(fd, 'w') as f:
    f.write(''.join(f'{key}={value}\n' for key, value in settings.items()))
print(f'Created credentials at {a.output} (mode 0600).')
