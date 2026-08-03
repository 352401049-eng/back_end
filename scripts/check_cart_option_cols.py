#!/usr/bin/env python3
import subprocess
from pathlib import Path

env = {}
for line in Path("/opt/yujixinjiang/.env").read_text(encoding="utf-8").splitlines():
    line = line.strip()
    if not line or line.startswith("#") or "=" not in line:
        continue
    k, v = line.split("=", 1)
    env[k.strip()] = v.strip().strip('"').strip("'")

cmd = [
    "mysql", "-N",
    f"-h{env.get('DB_HOST', '127.0.0.1')}",
    f"-P{env.get('DB_PORT', '3306')}",
    f"-u{env['DB_USER']}",
    f"-p{env['DB_PASSWORD']}",
    env["DB_NAME"],
    "-e", "SHOW COLUMNS FROM cart_item LIKE 'option%';",
]
print(subprocess.check_output(cmd, text=True, stderr=subprocess.STDOUT))
