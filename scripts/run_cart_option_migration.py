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

sql_path = Path("/tmp/cart_option_selections.sql")
sql = sql_path.read_text(encoding="utf-8")
cmd = [
    "mysql",
    f"-h{env.get('DB_HOST', '127.0.0.1')}",
    f"-P{env.get('DB_PORT', '3306')}",
    f"-u{env['DB_USER']}",
    f"-p{env['DB_PASSWORD']}",
    env["DB_NAME"],
]
proc = subprocess.run(cmd, input=sql, text=True, capture_output=True)
print(proc.stdout)
print(proc.stderr)
raise SystemExit(proc.returncode)
