"""Minimal nREPL client for the tests: clone a session, eval code, print the value.

Usage: python3 client.py <port> <code>

Prints the value of the last response that carries one. Exits 1 on an
eval error (printing it to stderr) and 2 on a timeout. Standard library
only, with just enough bencode for nREPL's maps, strings and ints.
"""

import socket
import sys


def encode(v):
    if isinstance(v, int):
        return b"i%de" % v
    if isinstance(v, str):
        v = v.encode()
    if isinstance(v, bytes):
        return b"%d:%s" % (len(v), v)
    if isinstance(v, list):
        return b"l" + b"".join(encode(x) for x in v) + b"e"
    if isinstance(v, dict):
        return b"d" + b"".join(encode(k) + encode(v[k]) for k in sorted(v)) + b"e"
    raise TypeError(type(v))


class Reader:
    def __init__(self, sock):
        self.sock = sock
        self.buf = b""

    def need(self, n):
        while len(self.buf) < n:
            chunk = self.sock.recv(65536)
            if not chunk:
                raise EOFError
            self.buf += chunk

    def take(self, n):
        self.need(n)
        out, self.buf = self.buf[:n], self.buf[n:]
        return out

    def read(self):
        c = self.take(1)
        if c == b"i":
            return int(self.until(b"e"))
        if c == b"l":
            out = []
            while self.peek() != b"e":
                out.append(self.read())
            self.take(1)
            return out
        if c == b"d":
            out = {}
            while self.peek() != b"e":
                k = self.read()
                out[k] = self.read()
            self.take(1)
            return out
        n = int(c + self.until(b":"))
        return self.take(n).decode()

    def peek(self):
        self.need(1)
        return self.buf[:1]

    def until(self, sep):
        while sep not in self.buf:
            self.need(len(self.buf) + 1)
        i = self.buf.index(sep)
        out, self.buf = self.buf[:i], self.buf[i + 1:]
        return out


def done(msg):
    return "done" in (msg.get("status") or [])


def main():
    port, code = int(sys.argv[1]), sys.argv[2]
    sock = socket.create_connection(("127.0.0.1", port), timeout=5)
    r = Reader(sock)
    sock.sendall(encode({"op": "clone", "id": "1"}))
    session = None
    while session is None:
        session = r.read().get("new-session")
    sock.sendall(encode({"op": "eval", "id": "2", "session": session, "code": code}))
    value, err = None, None
    while True:
        msg = r.read()
        if msg.get("id") != "2":
            continue
        if "value" in msg:
            value = msg["value"]
        if "err" in msg:
            err = (err or "") + msg["err"]
        if done(msg):
            break
    sock.close()
    if err is not None and value is None:
        sys.stderr.write(err)
        return 1
    print(value)
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except socket.timeout:
        sys.stderr.write("nrepl client: timed out\n")
        sys.exit(2)
