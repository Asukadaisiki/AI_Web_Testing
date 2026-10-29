#!/usr/bin/env python3
"""Test-only Worker forwarding proxy; records scoped request metadata, never bodies."""

import argparse
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import threading
import time
import urllib.error
import urllib.request


def serve(port, upstream, path):
    scopes = {}
    lock = threading.Lock()
    path.parent.mkdir(parents=True, exist_ok=True)
    output = path.open("a", buffering=1)
    os.chmod(path, 0o600)

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *_args):
            pass

        def forward(self):
            body = self.rfile.read(int(self.headers.get("Content-Length", "0")))
            parts = self.path.split("/")
            scope = scopes.get(parts[2], "") if len(parts) > 2 else ""
            if body:
                try:
                    scope = json.loads(body).get("session_id", scope)
                except ValueError:
                    pass
            with lock:
                output.write(json.dumps({"at": time.time(), "scope": scope,
                                         "method": self.command, "path": self.path}) + "\n")
            request = urllib.request.Request(upstream + self.path, data=body or None,
                                             method=self.command,
                                             headers={"Content-Type": "application/json"})
            try:
                response = urllib.request.urlopen(request, timeout=120)
            except urllib.error.HTTPError as error:
                response = error
            except OSError:
                self.send_error(502)
                return
            with response:
                data = response.read()
                if self.command == "POST" and self.path == "/sessions" and response.status == 200:
                    with lock:
                        scopes[json.loads(data)["session_id"]] = scope
                # Separate response records preserve the existing request-only
                # scope filter used by fault drills.
                closed = None
                if self.command == "DELETE":
                    try:
                        closed = json.loads(data).get("closed")
                    except ValueError:
                        pass
                with lock:
                    output.write(json.dumps({
                        "at": time.time(), "scope": "", "response_scope": scope,
                        "response_method": self.command, "response_path": self.path,
                        "status": response.status, "closed_ack": closed,
                    }) + "\n")
                self.send_response(response.status)
                self.send_header("Content-Type", response.headers.get_content_type())
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)

        do_GET = forward
        do_POST = forward
        do_DELETE = forward

    server = ThreadingHTTPServer(("127.0.0.1", port), Handler)
    try:
        server.serve_forever()
    finally:
        server.server_close()
        output.close()


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--port", type=int, default=8111)
    parser.add_argument("--upstream", default="http://127.0.0.1:8110")
    parser.add_argument("--log", type=Path, required=True)
    args = parser.parse_args()
    serve(args.port, args.upstream, args.log)
