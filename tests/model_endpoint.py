"""Small loopback Messages-API fixture for an offline Claude Code acceptance test."""

import json
import os
from http.server import BaseHTTPRequestHandler


class Messages(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        if self.path.split("?")[0] != "/v1/messages":
            self.send_error(404)
            return
        if self.headers.get("x-api-key") != "clistream-test-only":
            self.send_error(403)
            return
        content = json.dumps(body.get("messages", []))
        if self.server.challenge not in content:
            self.send_error(422)
            return
        self.server.seen.set()
        msg = {
            "type": "message",
            "id": "msg_fixture",
            "role": "assistant",
            "model": body["model"],
            "content": [],
            "stop_reason": None,
            "stop_sequence": None,
            "usage": {"input_tokens": 16, "output_tokens": 0},
        }
        output = self.server.answer
        if body.get("stream"):
            frames = [
                {"type": "message_start", "message": msg},
                {
                    "type": "content_block_start",
                    "index": 0,
                    "content_block": {"type": "text", "text": ""},
                },
                {
                    "type": "content_block_delta",
                    "index": 0,
                    "delta": {"type": "text_delta", "text": output},
                },
                {"type": "content_block_stop", "index": 0},
                {
                    "type": "message_delta",
                    "delta": {"stop_reason": "end_turn", "stop_sequence": None},
                    "usage": {"output_tokens": 8},
                },
                {"type": "message_stop"},
            ]
            response = "".join(
                f"event: {f['type']}\ndata: {json.dumps(f)}\n\n" for f in frames
            ).encode()
            content_type = "text/event-stream"
        else:
            msg.update(
                content=[{"type": "text", "text": output}], stop_reason="end_turn"
            )
            response = json.dumps(msg).encode()
            content_type = "application/json"
        self.send_response(200)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(response)))
        self.end_headers()
        self.wfile.write(response)


def claude_environment(config_dir, endpoint):
    """Direct only this Claude subprocess to the fixture using a synthetic key."""
    env = {
        k: os.environ[k] for k in ["PATH", "HOME", "USER", "TMPDIR"] if k in os.environ
    }
    return dict(
        env,
        ANTHROPIC_BASE_URL=f"http://127.0.0.1:{endpoint.server_port}",
        ANTHROPIC_API_KEY="clistream-test-only",
        CLAUDE_CONFIG_DIR=str(config_dir),
        CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC="1",
    )


CLAUDE_ARGS = [
    "-p",
    "--model",
    "sonnet",
    "--tools",
    "",
    "--no-session-persistence",
    "--strict-mcp-config",
    "--mcp-config",
    '{"mcpServers":{}}',
]
CODEX_ARGS = [
    "exec",
    "--ephemeral",
    "--skip-git-repo-check",
    "--sandbox",
    "read-only",
    "-",
]
