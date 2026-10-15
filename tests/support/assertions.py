"""Reusable assertions with failure messages that show the response body."""

import requests


def assert_status(response: requests.Response, expected: int) -> None:
    assert response.status_code == expected, (
        f"expected HTTP {expected}, got {response.status_code}: {response.text}"
    )


def assert_error(response: requests.Response, status: int, code: str) -> None:
    """Assert the API rejected the request with this status and error code."""
    assert_status(response, status)
    body = response.json()
    assert body["error"]["code"] == code, f"expected error code {code}, got {body}"
    assert body["error"]["message"], "error responses must include a human-readable message"
