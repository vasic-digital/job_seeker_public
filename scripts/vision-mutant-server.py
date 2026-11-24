#!/usr/bin/env python3
"""vision-mutant-server.py - the PAIRED MUTATION server for the vision QA leg (JSK-010, section 1.1).

Answers HTTP 200 to EVERY path and method, with a body that is NOT the job_seeker console: either an empty-ish "OK"
page (mode `ok`) or an error page with a stack trace served with status 200 (mode `error`). An HTTP-only check that
looks at the status code would pass this server; the vision check must not.
usage: vision-mutant-server.py <port> <ok|error>
"""
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer

OK = b"<!doctype html><html><head><title>OK</title></head><body><p>OK</p></body></html>"
ERR = (b"<!doctype html><html><head><title>Error</title></head><body style='font-family:monospace;background:#fff'>"
       b"<h1>panic: runtime error: invalid memory address or nil pointer dereference</h1>"
       b"<pre>goroutine 1 [running]:\nmain.render(0x0)\n\t/app/console.go:88 +0x1c\nexit status 2</pre></body></html>")

class H(BaseHTTPRequestHandler):
    def _go(self):
        body = ERR if MODE == "error" else OK
        self.send_response(200)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)
    do_GET = do_POST = do_PUT = do_DELETE = do_HEAD = _go
    def log_message(self, *a): pass

PORT, MODE = int(sys.argv[1]), sys.argv[2]
HTTPServer(("127.0.0.1", PORT), H).serve_forever()
