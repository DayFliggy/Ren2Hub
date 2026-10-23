#!/usr/bin/env python3
"""对运行中的隔离 Ren2Hub 执行 Responses WS 冒烟；上游仅为回环模拟器。"""

import base64
import hashlib
import json
import os
import re
import socket
import stat
import struct
import threading
import urllib.parse
import urllib.request

class CheckFailed(Exception):
    pass


def require(condition, code):
    if not condition:
        raise CheckFailed(code)


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *_):
        return None


def local_base(value):
    parsed = urllib.parse.urlsplit(value)
    require(parsed.scheme == "http" and parsed.hostname == "127.0.0.1"
            and not parsed.username and not parsed.password and not parsed.query
            and not parsed.fragment and parsed.path in ("", "/"),
            "base_must_be_literal_loopback_http_origin")
    return urllib.parse.urlunsplit((parsed.scheme, parsed.netloc, "", "", ""))


def credentials(path):
    descriptor = os.open(path, os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0))
    with os.fdopen(descriptor, encoding="utf-8") as stream:
        metadata = os.fstat(stream.fileno())
        require(stat.S_ISREG(metadata.st_mode) and metadata.st_size <= 16384,
                "credentials_require_small_regular_file")
        if os.name == "posix":
            require(stat.S_IMODE(metadata.st_mode) == 0o600,
                    "credentials_require_mode_0600")
        value = json.load(stream)
    require(isinstance(value, dict) and all(isinstance(value.get(key), str)
            and value[key] for key in ("ren2hub_username", "ren2hub_password")),
            "credentials_fields_invalid")
    return value


def logout_verified(client):
    failure = None
    try:
        client.api("POST", "/api/user/auth/logout", {})
    except Exception as error:
        failure = type(error).__name__
    # Logout can revoke the session before a cookie-cleanup or transport error.
    # Only an explicit rejection of the old access token proves cleanup then.
    status, _ = client.request("GET", "/api/user/self")
    require(status == 401, "logout_did_not_revoke_access")
    client.bearer = ""
    return failure


def upgrade_rejection(reader, headers, status, bearer):
    failure = "websocket_upgrade_rejected_" + status
    length = headers.get("content-length", "")
    if (status == "invalid" or not re.fullmatch(r"[0-9]{1,4}", length) or not 0 < int(length) <= 4096
            or headers.get("transfer-encoding") or headers.get("content-encoding")
            or headers.get("content-type", "").split(";", 1)[0].strip().lower() != "application/json"):
        return failure
    try:
        body = reader.read(int(length))
        if len(body) != int(length):
            return failure
        payload = json.loads(body)
        error = payload.get("error") if isinstance(payload, dict) else None
        if not isinstance(error, dict):
            return failure
        for field in ("code", "type"):
            value = error.get(field)
            if (isinstance(value, str) and re.fullmatch(r"[A-Za-z0-9_.-]{1,64}", value)
                    and (not bearer or bearer not in value)):
                failure += "_" + field + "_" + value
    except (OSError, ValueError, RecursionError):
        # Failed upgrades may have truncated/non-JSON bodies. Their contents
        # are never diagnostic output, including exception messages.
        pass
    return failure


class WebSocket:
    """Bounded RFC 6455 framing for local acceptance; never follows redirects."""

    def __init__(self, connection, reader, masked):
        self.connection, self.reader, self.masked = connection, reader, masked
        self.lock = threading.Lock()

    @classmethod
    def connect(cls, base, bearer):
        parsed = urllib.parse.urlsplit(local_base(base))
        connection = socket.create_connection((parsed.hostname, parsed.port or 80), 15)
        reader = connection.makefile("rb")
        key = base64.b64encode(os.urandom(16)).decode()
        request = (f"GET /v1/responses HTTP/1.1\r\nHost: {parsed.netloc}\r\n"
                   "Upgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\n"
                   "Sec-WebSocket-Protocol: responses\r\n"
                   f"Sec-WebSocket-Key: {key}\r\nAuthorization: Bearer {bearer}\r\n\r\n")
        try:
            connection.sendall(request.encode())
            status = reader.readline(4096)
            parts = status.split()
            code = parts[1].decode("ascii") if len(parts) > 1 and re.fullmatch(rb"[0-9]{3}", parts[1]) else "invalid"
            headers, total = {}, len(status)
            try:
                require(status.endswith(b"\r\n") and parts[0] in (b"HTTP/1.0", b"HTTP/1.1"), "invalid_upgrade_headers")
                while True:
                    line = reader.readline(min(4096, 16385 - total))
                    total += len(line)
                    require(line.endswith(b"\r\n") and total <= 16384, "invalid_upgrade_headers")
                    if line == b"\r\n":
                        break
                    require(b":" in line, "invalid_upgrade_headers")
                    name, value = line.decode("latin-1").split(":", 1)
                    require(name.lower() not in headers, "invalid_upgrade_headers")
                    headers[name.lower()] = value.strip()
            except (OSError, ValueError, CheckFailed):
                if code != "101":
                    raise CheckFailed("websocket_upgrade_rejected_" + code) from None
                raise
            if code != "101":
                raise CheckFailed(upgrade_rejection(reader, headers, code, bearer))
            expected = base64.b64encode(hashlib.sha1(
                (key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode()).digest()).decode()
            require(headers.get("sec-websocket-accept") == expected
                    and headers.get("upgrade", "").lower() == "websocket"
                    and "upgrade" in headers.get("connection", "").lower(),
                    "invalid_websocket_upgrade")
            return cls(connection, reader, True)
        except Exception:
            reader.close()
            connection.close()
            raise

    def exact(self, size):
        output = bytearray()
        while len(output) < size:
            part = self.reader.read(size - len(output))
            if not part:
                raise EOFError("websocket_closed")
            output.extend(part)
        return bytes(output)

    def send(self, body, opcode=1):
        if not isinstance(body, bytes):
            body = json.dumps(body, separators=(",", ":")).encode()
        require(len(body) <= 1 << 20, "outgoing_frame_too_large")
        length = len(body)
        header = bytes([0x80 | opcode])
        flag = 0x80 if self.masked else 0
        if length < 126:
            header += bytes([flag | length])
        elif length < 65536:
            header += bytes([flag | 126]) + struct.pack("!H", length)
        else:
            header += bytes([flag | 127]) + struct.pack("!Q", length)
        if self.masked:
            mask = os.urandom(4)
            header += mask
            body = bytes(value ^ mask[index % 4] for index, value in enumerate(body))
        with self.lock:
            self.connection.sendall(header + body)

    def receive(self):
        content, started = bytearray(), False
        while True:
            first, second = self.exact(2)
            opcode, final, masked = first & 15, bool(first & 0x80), bool(second & 0x80)
            require(first & 0x70 == 0 and masked != self.masked, "invalid_frame_flags")
            length = second & 0x7F
            if length == 126:
                length = struct.unpack("!H", self.exact(2))[0]
            elif length == 127:
                length = struct.unpack("!Q", self.exact(8))[0]
            require(length <= 1 << 20, "incoming_frame_too_large")
            if opcode >= 8:
                require(final and length <= 125, "invalid_control_frame")
            mask = self.exact(4) if masked else b""
            body = self.exact(length)
            if masked:
                body = bytes(value ^ mask[index % 4] for index, value in enumerate(body))
            if opcode == 8:
                raise EOFError("websocket_close_frame")
            if opcode == 9:
                self.send(body, 10)
                continue
            if opcode == 10:
                continue
            require((opcode == 1 and not started) or (opcode == 0 and started),
                    "unexpected_websocket_opcode")
            started = True
            content.extend(body)
            require(len(content) <= 1 << 20, "incoming_message_too_large")
            if final:
                return json.loads(content)

    def close(self):
        try:
            self.connection.shutdown(socket.SHUT_RDWR)
        except OSError:
            pass
        self.connection.close()
        self.reader.close()
