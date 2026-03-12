"""TCP client for connecting to Micelio nodes.

Uses the same length-prefixed binary framing as the Go implementation:
4 bytes big-endian uint32 length + JSON payload bytes.
"""

import socket
import struct
from typing import Optional

from micelio.protocol import Message, decode_message


# Maximum message size (1 MB, same as Go handler)
MAX_MESSAGE_SIZE = 1 << 20

# Default read timeout in seconds
DEFAULT_TIMEOUT = 30.0


class MicelioClient:
    """TCP client for the AIP protocol.

    Connects to a Micelio node and exchanges length-prefixed JSON messages.
    Wire-compatible with the Go network handler.
    """

    def __init__(self, timeout: float = DEFAULT_TIMEOUT):
        self._sock: Optional[socket.socket] = None
        self._timeout = timeout

    def connect(self, host: str, port: int) -> None:
        """Connect to a Micelio node via TCP."""
        self._sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        self._sock.settimeout(self._timeout)
        self._sock.connect((host, port))

    def send_message(self, msg: Message) -> None:
        """Send a message using length-prefixed binary framing.

        Format: 4 bytes big-endian uint32 (length) + JSON bytes.
        Matches the Go SendDirect method.
        """
        if self._sock is None:
            raise RuntimeError("Not connected")

        data = msg.encode()
        length = len(data)
        if length > MAX_MESSAGE_SIZE:
            raise ValueError(f"Message too large: {length} bytes (max {MAX_MESSAGE_SIZE})")

        # Write 4-byte big-endian length header
        header = struct.pack(">I", length)
        self._sock.sendall(header)
        self._sock.sendall(data)

    def receive_message(self) -> Message:
        """Receive a length-prefixed message.

        Reads the 4-byte header, then the JSON payload.
        Matches the Go stream handler reading logic.
        """
        if self._sock is None:
            raise RuntimeError("Not connected")

        # Read 4-byte length header
        header = self._recv_exact(4)
        length = struct.unpack(">I", header)[0]

        if length > MAX_MESSAGE_SIZE:
            raise ValueError(f"Message too large: {length} bytes (max {MAX_MESSAGE_SIZE})")

        # Read payload
        data = self._recv_exact(length)
        return decode_message(data)

    def _recv_exact(self, n: int) -> bytes:
        """Read exactly n bytes from the socket."""
        buf = bytearray()
        while len(buf) < n:
            chunk = self._sock.recv(n - len(buf))
            if not chunk:
                raise ConnectionError("Connection closed by remote")
            buf.extend(chunk)
        return bytes(buf)

    def close(self) -> None:
        """Close the TCP connection."""
        if self._sock is not None:
            try:
                self._sock.close()
            except OSError:
                pass
            self._sock = None

    @property
    def connected(self) -> bool:
        """Return True if connected."""
        return self._sock is not None

    def __enter__(self) -> "MicelioClient":
        return self

    def __exit__(self, *args) -> None:
        self.close()

    def __del__(self) -> None:
        self.close()
