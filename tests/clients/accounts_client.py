import requests

from clients.api_client import ApiClient


class AccountsClient:
    def __init__(self, api: ApiClient):
        self._api = api

    def create(self, payload: dict) -> requests.Response:
        return self._api.post("/accounts", json=payload)

    def create_raw(self, body: str) -> requests.Response:
        """Send a body exactly as given, for malformed-JSON tests."""
        return self._api.post("/accounts", data=body)

    def get(self, account_id: int | str) -> requests.Response:
        return self._api.get(f"/accounts/{account_id}")

    def transactions(self, account_id: int | str, limit: int | str | None = None) -> requests.Response:
        params = {"limit": limit} if limit is not None else None
        return self._api.get(f"/accounts/{account_id}/transactions", params=params)
