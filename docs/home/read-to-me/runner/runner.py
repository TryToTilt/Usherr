"""Read-to-Me's runner: lets Usherr start and stop its voice engines.

Usherr isn't given Docker itself (that would let it control every container).
This small server has the Docker socket instead and does three things, for
the containers named in RUNNER_CONTAINERS only, behind a token Usherr knows:

  GET  /status         {"engines": {name: "running" | "stopped" | "missing"}}
  POST /start/{name}   starts it (and stops any container that shares its
                       port, from RUNNER_EXCLUSIVE: chatterbox / chatterbox-turbo)
  POST /stop/{name}    stops it

Every request needs "Authorization: Bearer <RUNNER_TOKEN>". Nothing else of
Docker's is reachable through it: no other container, no images, no exec.
"""
import hmac
import http.client
import json
import os
import re
import socket
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

TOKEN = os.environ.get("RUNNER_TOKEN", "")
ALLOWED = [c.strip() for c in os.environ.get("RUNNER_CONTAINERS", "kokoro,chatterbox,chatterbox-turbo").split(",") if c.strip()]
# Groups of containers that can't run together (they answer on one port): "a+b,c+d".
EXCLUSIVE = [set(g.split("+")) for g in os.environ.get("RUNNER_EXCLUSIVE", "chatterbox+chatterbox-turbo").split(",") if "+" in g]
NAME = re.compile(r"^[a-z0-9][a-z0-9_.-]{0,62}$")


class DockerSocket(http.client.HTTPConnection):
    def __init__(self):
        super().__init__("localhost", timeout=120)

    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(120)
        self.sock.connect("/var/run/docker.sock")


def docker(method, path):
    c = DockerSocket()
    try:
        c.request(method, path)
        r = c.getresponse()
        return r.status, r.read()
    finally:
        c.close()


def state(name):
    status, body = docker("GET", f"/containers/{name}/json")
    if status == 404:
        return "missing"
    if status != 200:
        return "unknown"
    return "running" if json.loads(body).get("State", {}).get("Running") else "stopped"


def stop(name):
    status, _ = docker("POST", f"/containers/{name}/stop?t=20")
    return status in (204, 304, 404)


def start(name):
    for group in EXCLUSIVE:
        if name in group:
            for other in group - {name}:
                if other in ALLOWED and state(other) == "running":
                    stop(other)
    status, _ = docker("POST", f"/containers/{name}/start")
    return status in (204, 304)


class Handler(BaseHTTPRequestHandler):
    server_version = "rtm-runner"

    def send(self, code, data):
        body = json.dumps(data).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def authorized(self):
        got = self.headers.get("Authorization", "")
        return bool(TOKEN) and hmac.compare_digest(got.encode(), f"Bearer {TOKEN}".encode())

    def do_GET(self):
        if not self.authorized():
            return self.send(401, {"error": "not allowed"})
        if self.path != "/status":
            return self.send(404, {"error": "not found"})
        self.send(200, {"engines": {n: state(n) for n in ALLOWED}})

    def do_POST(self):
        if not self.authorized():
            return self.send(401, {"error": "not allowed"})
        m = re.fullmatch(r"/(start|stop)/([^/]+)", self.path)
        if not m or not NAME.match(m.group(2)) or m.group(2) not in ALLOWED:
            return self.send(404, {"error": "not found"})
        action, name = m.groups()
        if state(name) == "missing":
            return self.send(404, {"error": f"{name} hasn't been created (docker compose create it once)"})
        ok = start(name) if action == "start" else stop(name)
        self.send(200 if ok else 502, {"engines": {n: state(n) for n in ALLOWED}})

    def log_message(self, fmt, *args):
        print("runner:", self.address_string(), fmt % args, flush=True)


if __name__ == "__main__":
    if not TOKEN or len(TOKEN) < 24:
        raise SystemExit("RUNNER_TOKEN must be set (24 characters or more)")
    ThreadingHTTPServer(("0.0.0.0", int(os.environ.get("PORT", "4130"))), Handler).serve_forever()
