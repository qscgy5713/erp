#!/usr/bin/env bash
# 相依套件弱點掃描:把後端 go.mod 與前端 package-lock.json 的套件版本送到 OSV(osv.dev)比對已知弱點。
#   scripts/osv-scan.sh          有未評估的弱點時結束碼為 1(可放進排程 / CI);已評估的列在 scripts/osv-ignore.txt
# 需要 python3 與網路;只送出套件名稱與版本,不送任何業務資料。
set -euo pipefail
cd "$(dirname "$0")/.."
exec python3 -I - <<'PY'
import json, re, sys, urllib.request

def pkgs():
    out = []
    for line in open("backend/go.mod", encoding="utf-8"):
        m = re.match(r"\s+(\S+)\s+(v\S+)", line)
        if m and not line.lstrip().startswith("//"):
            out.append(("Go", m.group(1), m.group(2)))
    lock = json.load(open("frontend/package-lock.json", encoding="utf-8"))
    for path, info in lock.get("packages", {}).items():
        if path.startswith("node_modules/") and not info.get("dev") and info.get("version"):
            out.append(("npm", path.split("node_modules/")[-1], info["version"]))
    return out

def post(url, body):
    req = urllib.request.Request(url, json.dumps(body).encode(), {"Content-Type": "application/json"})
    return json.load(urllib.request.urlopen(req, timeout=60))

items = pkgs()
vulns = {}
for i in range(0, len(items), 500):
    chunk = items[i:i + 500]
    res = post("https://api.osv.dev/v1/querybatch", {"queries": [
        {"package": {"ecosystem": e, "name": n}, "version": v.lstrip("v") if e == "npm" else v} for e, n, v in chunk]})
    for pkg, r in zip(chunk, res["results"]):
        if r.get("vulns"):
            vulns[pkg] = [v["id"] for v in r["vulns"]]

ignored = set()
for line in open("scripts/osv-ignore.txt", encoding="utf-8"):
    line = line.split("#")[0].strip()
    if line:
        ignored.add(line)
vulns = {k: [i for i in ids if i not in ignored] for k, ids in vulns.items()}
vulns = {k: ids for k, ids in vulns.items() if ids}

print(f"掃描 {len(items)} 個套件(Go + npm 正式相依);略過已評估的 {len(ignored)} 項(scripts/osv-ignore.txt)")
if not vulns:
    print("沒有已知弱點")
    sys.exit(0)
for (e, n, v), ids in sorted(vulns.items()):
    print(f"[{e}] {n} {v}: {', '.join(ids)}")
print("\n請到 https://osv.dev/<編號> 查看影響範圍與修正版本,並評估是否實際用到該功能。")
sys.exit(1)
PY
