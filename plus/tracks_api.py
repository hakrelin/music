#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
plus-api：给注入脚本用的轻量接口（只读）
  GET /tracks?id=<歌单ID>&limit=20   -> 该歌单的曲目 [{id,name,artist,album}]
  GET /healthz

数据来自本机的 jusicMusicApi 容器（127.0.0.1 网络内），只读歌单详情，不改任何状态。
由 nginx 的 /plus-api/ 反代到 127.0.0.1:8899。
"""
import json
import subprocess
import urllib.parse
import urllib.request
from http.server import BaseHTTPRequestHandler, HTTPServer
from socketserver import ThreadingMixIn


class ThreadingHTTPServer(ThreadingMixIn, HTTPServer):
    """Python 3.6 没有 http.server.ThreadingHTTPServer，自己拼一个"""
    daemon_threads = True

MUSIC_PORT = 3000


def music_api_ip():
    try:
        out = subprocess.check_output(
            ["docker", "inspect", "-f",
             "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", "jusicMusicApi"],
            timeout=5).decode().strip()
        return out or "127.0.0.1"
    except Exception:
        return "127.0.0.1"


IP = music_api_ip()


def post(path):
    url = "http://%s:%d%s" % (IP, MUSIC_PORT, path)
    req = urllib.request.Request(url, data=b"", method="POST")
    with urllib.request.urlopen(req, timeout=15) as r:
        return json.loads(r.read().decode())


def tracks(playlist_id, limit):
    d = post("/playlist/detail?id=%s" % urllib.parse.quote(playlist_id))
    pl = d.get("playlist") or {}
    out = []
    for t in (pl.get("tracks") or [])[:limit]:
        out.append({
            "id": str(t.get("id")),
            "name": t.get("name"),
            "artist": (t.get("ar") or [{}])[0].get("name") or (t.get("artists") or [{}])[0].get("name") or "",
            "album": ((t.get("al") or {}).get("name")) or ((t.get("album") or {}).get("name")) or "",
        })
    return {"playlist": pl.get("name"), "count": len(out), "tracks": out}


class H(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *a):
        pass

    def send_json(self, obj, code=200):
        body = json.dumps(obj, ensure_ascii=False).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json;charset=UTF-8")
        self.send_header("Cache-Control", "no-store")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        path, _, qs = self.path.partition("?")
        q = urllib.parse.parse_qs(qs)
        if path.rstrip("/") in ("/tracks", "/plus-api/tracks"):
            pid = (q.get("id") or [""])[0]
            limit = min(int((q.get("limit") or ["20"])[0] or 20), 50)
            if not pid:
                return self.send_json({"error": "missing id"}, 400)
            try:
                return self.send_json(tracks(pid, limit))
            except Exception as e:
                return self.send_json({"error": str(e)}, 502)
        if path.rstrip("/") in ("/healthz", "/plus-api/healthz", ""):
            return self.send_json({"ok": True, "music_api": IP})
        self.send_json({"error": "not found"}, 404)


if __name__ == "__main__":
    srv = ThreadingHTTPServer(("127.0.0.1", int(__import__("os").environ.get("PLUS_API_PORT", "18099"))), H)
    srv.daemon_threads = True
    print("[plus-api] 127.0.0.1:18099 -> music api %s:%d" % (IP, MUSIC_PORT), flush=True)
    srv.serve_forever()
