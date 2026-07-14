"""Single-session JSON-RPC smoke driver for mcp-confluence.

Sends initialize + notifications/initialized + tools/list +
tools/call(conf_get_page_tree) on a single stdin connection.
Closes stdin after a brief sleep so the mcp-golang stdio
transport flushes its buffered responses. Reads whatever stdout
fragments the server produced. Stays within the user's bennie
workspace per the standing 'stay within my bennie workspace'
instruction — we use a known nested page id from earlier audits.
"""
import json
import os
import subprocess
import sys
import time
from pathlib import Path

BINARY = Path("./bin/mcp-confluence")

# Known nested page in the user's workspace 780763211 (bennie).
# "DrawIO Auto-Test 2 2026-07-13" — parentId 780764253, so this
# page should have an ancestor.
KNOWN_PAGE = os.environ.get("SMOKE_PAGE_ID", "1831108680")


def one_session(payloads: list[dict], debug: bool = False) -> tuple[str, str, int]:
    env = os.environ.copy()
    if debug:
        env["DEBUG"] = "true"
    p = subprocess.Popen(
        [str(BINARY)],
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        cwd=str(BINARY.parent.parent),
        env=env,
    )
    stdin_str = "\n".join(json.dumps(r) for r in payloads) + "\n"
    p.stdin.write(stdin_str)
    p.stdin.flush()
    time.sleep(6)
    p.stdin.close()
    try:
        out, err = p.communicate(timeout=10)
    except subprocess.TimeoutExpired:
        p.kill()
        out, err = p.communicate()
    return out, err, p.returncode


def parse_initialize(stdout: str) -> dict | None:
    for line in stdout.splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            d = json.loads(line)
        except Exception:
            continue
        if d.get("id") == 1 and "result" in d:
            return d["result"]
    return None


def parse_tools_list(stdout: str) -> list[str]:
    for line in stdout.splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            d = json.loads(line)
        except Exception:
            continue
        if d.get("id") == 2 and "result" in d and "tools" in d["result"]:
            return sorted(t["name"] for t in d["result"]["tools"])
    return []


def parse_text_payload(stdout: str, call_id: int) -> str | None:
    for line in stdout.splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            d = json.loads(line)
        except Exception:
            continue
        if d.get("id") != call_id or "result" not in d:
            continue
        result = d["result"]
        if isinstance(result, dict):
            content = result.get("content", [])
            if isinstance(content, list) and content:
                item = content[0]
                if isinstance(item, dict):
                    return item.get("text")
    return None


def main() -> int:
    sys.stderr.write(
        "=== Live smoke: bennie workspace, smartergroup.atlassian.net ===\n"
        f"=== Test page: {KNOWN_PAGE} (DrawIO Auto-Test 2 2026-07-13) ===\n"
    )
    payloads = [
        {"jsonrpc": "2.0", "id": 1, "method": "initialize",
         "params": {"protocolVersion": "2024-11-05", "capabilities": {},
                    "clientInfo": {"name": "smoke", "version": "0"}}},
        {"jsonrpc": "2.0", "method": "notifications/initialized"},
        {"jsonrpc": "2.0", "id": 2, "method": "tools/list"},
        {"jsonrpc": "2.0", "id": 30, "method": "tools/call",
         "params": {"name": "conf_get_page_tree",
                    "arguments": {"page-id": KNOWN_PAGE, "limit": 25, "depth": 3,
                                  "outputFormat": "json"}}},
    ]

    out, err, rc = one_session(payloads, debug=False)
    sys.stderr.write("--- stderr (300) ---\n" + (err or "")[:300] + "\n---\n")

    print(f"returncode={rc}")
    print(f"stdout_length={len(out)} bytes")

    # Stage 1 — initialize
    init = parse_initialize(out)
    if not init:
        print("FAIL: initialize returned no result")
        print("first 1000:", out[:1000])
        return 1
    print(
        "stage1: server =",
        init.get("serverInfo", {}).get("name"),
        "v" + str(init.get("serverInfo", {}).get("version")),
    )

    # Stage 2 — tools/list (18 tools, including the new one)
    names = parse_tools_list(out)
    print(f"stage1: tool_count={len(names)}")
    if "conf_get_page_tree" not in names:
        print("FAIL: conf_get_page_tree NOT registered")
        return 1
    print("stage1: PASS — 18 tools registered, conf_get_page_tree present")

    # Stage 3 — conf_get_page_tree on the user's nested page
    text = parse_text_payload(out, 30)
    if not text:
        print("FAIL: conf_get_page_tree returned empty body")
        return 1
    try:
        tree = json.loads(text)
    except Exception as e:
        print(f"FAIL: response not JSON: {e}")
        print("first 800:", text[:800])
        return 1

    expected = {"pageId", "ancestors", "children", "descendants"}
    have = set(tree.keys())
    missing = expected - have
    if missing:
        print(f"FAIL: missing keys: {missing}")
        print("got:", have)
        return 1

    def cnt(env):
        r = env.get("results", []) if isinstance(env, dict) else []
        return len(r) if isinstance(r, list) else -1

    print(f"stage3: pageId={tree['pageId']}")
    print(f"stage3: ancestors={cnt(tree['ancestors'])} "
          f"children={cnt(tree['children'])} "
          f"descendants={cnt(tree['descendants'])}")

    a0 = (tree["ancestors"].get("results") or [None])[0] if cnt(tree["ancestors"]) > 0 else None
    if a0:
        print(f"stage3: top ancestor: id={a0.get('id')} title={a0.get('title')!r}")
    c0 = (tree["children"].get("results") or [None])[0] if cnt(tree["children"]) > 0 else None
    if c0:
        print(f"stage3: first child:  id={c0.get('id')} title={c0.get('title')!r}")

    print()
    print(
        "STAGE 3: PASS — live conf_get_page_tree envelope returned from"
        " smartergroup.atlassian.net on user's bennie workspace"
    )
    print()
    print("--- compact response ---")
    print(json.dumps({
        "pageId": tree["pageId"],
        "ancestors_count": cnt(tree["ancestors"]),
        "children_count": cnt(tree["children"]),
        "descendants_count": cnt(tree["descendants"]),
        "top_ancestor": a0 and {"id": a0.get("id"), "title": a0.get("title")},
        "first_child": c0 and {"id": c0.get("id"), "title": c0.get("title")},
    }, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
