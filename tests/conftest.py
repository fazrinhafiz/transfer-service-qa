"""Shared fixtures for the API and BDD suites.

Test isolation: every test creates its own accounts with fresh ids and
unique idempotency keys, so tests never share state, can run in any order,
and need no database cleanup between them.
"""

import time
from collections.abc import Callable, Iterator

import pytest
import requests

from clients.accounts_client import AccountsClient
from clients.api_client import ApiClient
from clients.transfers_client import TransfersClient
from config.settings import Settings, load_settings
from data.factories import account_payload, idempotency_key, transfer_payload
from data.money import ringgit
from support.assertions import assert_status


@pytest.fixture(scope="session")
def settings() -> Settings:
    return load_settings()


@pytest.fixture(scope="session", autouse=True)
def service_ready(settings: Settings) -> None:
    """Wait for /healthz once per run, so a cold container doesn't fail test 1."""
    deadline = time.monotonic() + settings.startup_timeout_seconds
    last_error = None
    while time.monotonic() < deadline:
        try:
            if requests.get(f"{settings.base_url}/healthz", timeout=2).status_code == 200:
                return
        except requests.RequestException as exc:
            last_error = exc
        time.sleep(1)
    pytest.exit(f"Service at {settings.base_url} not healthy after {settings.startup_timeout_seconds}s: {last_error}")


@pytest.fixture(scope="session")
def api(settings: Settings) -> Iterator[ApiClient]:
    client = ApiClient(settings)
    yield client
    client.close()


@pytest.fixture(scope="session")
def accounts(api: ApiClient) -> AccountsClient:
    return AccountsClient(api)


@pytest.fixture(scope="session")
def transfers(api: ApiClient) -> TransfersClient:
    return TransfersClient(api)


@pytest.fixture
def new_transfers_client(settings: Settings) -> Callable[[], TransfersClient]:
    """For concurrency tests: each thread needs its own session (requests.Session
    is not thread-safe), and per-request report attachments are turned off."""
    return lambda: TransfersClient(ApiClient(settings, report=False))


@pytest.fixture
def create_account(accounts: AccountsClient) -> Callable[..., dict]:
    """Factory fixture: create_account(ringgit("50.00")) -> account JSON."""

    def _create(initial_balance_sen: int = ringgit("1000.00"), **overrides) -> dict:
        response = accounts.create(account_payload(initial_balance_sen=initial_balance_sen, **overrides))
        assert_status(response, 201)
        return response.json()

    return _create


@pytest.fixture
def sender(create_account) -> dict:
    """An account holding RM 1,000.00."""
    return create_account(ringgit("1000.00"))


@pytest.fixture
def receiver(create_account) -> dict:
    """An empty account."""
    return create_account(0)


@pytest.fixture
def balance_of(accounts: AccountsClient) -> Callable[[int], int]:
    def _balance(account_id: int) -> int:
        response = accounts.get(account_id)
        assert_status(response, 200)
        return response.json()["balance_sen"]

    return _balance


@pytest.fixture
def send(transfers: TransfersClient) -> Callable[..., requests.Response]:
    """send(sender, receiver, ringgit("10.00")) with a fresh idempotency key."""

    def _send(from_account: dict, to_account: dict, amount_sen: int, **overrides) -> requests.Response:
        payload = transfer_payload(from_account["id"], to_account["id"], amount_sen=amount_sen, **overrides)
        return transfers.create(payload, idempotency_key())

    return _send
