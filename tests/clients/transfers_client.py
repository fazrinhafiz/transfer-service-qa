import requests

from clients.api_client import ApiClient

IDEMPOTENCY_HEADER = "Idempotency-Key"
REPLAYED_HEADER = "Idempotent-Replayed"


class TransfersClient:
    def __init__(self, api: ApiClient):
        self._api = api

    def create(self, payload: dict, idempotency_key: str | None) -> requests.Response:
        """POST /transfers. Pass idempotency_key=None to omit the header."""
        return self._api.post("/transfers", json=payload, headers=_headers(idempotency_key))

    def create_raw(self, body: str, idempotency_key: str | None) -> requests.Response:
        """Send a body exactly as given, for malformed-JSON and formatting tests."""
        return self._api.post("/transfers", data=body, headers=_headers(idempotency_key))


def _headers(idempotency_key: str | None) -> dict:
    return {IDEMPOTENCY_HEADER: idempotency_key} if idempotency_key is not None else {}
