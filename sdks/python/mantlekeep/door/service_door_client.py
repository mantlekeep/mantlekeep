"""The service-mode door client: submit an intent to a remote door over HTTP.

It speaks the frozen ``POST /api/govern`` contract with the standard library alone. Identity
travels as a HEADER, never in the body — a body-supplied caller would be forgeable, and the
door authenticates the service, which then names the subject it acts for.
"""

from __future__ import annotations

import json
import urllib.error
import urllib.request
from typing import Any, Dict

from ..model.decision import Decision
from ..model.intent import Intent
from .config import DoorConfig
from .errors import DoorUnavailableError


class ServiceDoorClient:
    """Submits intents to a door running elsewhere (``mode: service``)."""

    def __init__(self, config: DoorConfig):
        self._config = config
        self._govern_url = config.base_url.rstrip("/") + "/api/govern"

    def submit(self, intent: Intent) -> Decision:
        """Submit an intent and return the door's Decision.

        A deny or require_approval is a normal ANSWER and comes back as a Decision (the door
        answers 4xx for a deny, 200 for require_approval). Only the absence of an answer — an
        unreachable door, a timeout, a server fault, a non-JSON body — raises
        DoorUnavailableError, because an unanswered request must never be read as a permit.
        """
        body = self._request_body(intent)
        request = urllib.request.Request(
            self._govern_url,
            data=json.dumps(body).encode("utf-8"),
            method="POST",
            headers=self._headers(intent),
        )
        try:
            with urllib.request.urlopen(request, timeout=self._config.timeout_seconds) as response:
                return self._decision_from(response.read())
        except urllib.error.HTTPError as http_error:
            # A 4xx carries a typed deny Decision in its body — read and return it. A 5xx is a
            # genuine server fault, not a verdict, so it is unavailability, not a deny.
            try:
                if http_error.code >= 500:
                    raise DoorUnavailableError(
                        f"door returned {http_error.code} (server fault, not a verdict)"
                    ) from http_error
                return self._decision_from(http_error.read())
            finally:
                http_error.close()
        except urllib.error.URLError as url_error:
            raise DoorUnavailableError(f"door unreachable: {url_error.reason}") from url_error

    def _request_body(self, intent: Intent) -> Dict[str, Any]:
        params = dict(intent.params or {})
        # env travels top-level in the contract as well as in params; the engine reads the key
        # a product's floor data refers to. Send both consistently so either reader sees it.
        env = str(params.get("env", "")) if params.get("env") is not None else ""
        return {
            "action": intent.action,
            "resource": intent.resource,
            "goal": intent.goal,
            "env": env,
            "params": params,
        }

    def _headers(self, intent: Intent) -> Dict[str, str]:
        headers = {"Content-Type": "application/json"}
        if intent.subject:
            headers[self._config.caller_header] = intent.subject
        if intent.via:
            # A service acting for a person: the person is the subject (caller header), the
            # service is recorded as via (on-behalf-of header). Both, or neither.
            headers[self._config.on_behalf_of_header] = intent.subject or ""
            headers[self._config.caller_header] = intent.via
        return headers

    @staticmethod
    def _decision_from(raw: bytes) -> Decision:
        try:
            payload = json.loads(raw.decode("utf-8"))
        # Both failures are ValueErrors: bytes that are not UTF-8 raise UnicodeDecodeError
        # (a UnicodeError, which is a ValueError) and text that is not JSON raises
        # json.JSONDecodeError (also a ValueError). One clause catches the pair.
        except ValueError as parse_error:
            raise DoorUnavailableError(
                "door response was not JSON — cannot read it as a verdict"
            ) from parse_error
        return Decision.from_wire(payload)
