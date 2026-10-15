"""Step definitions for bdd/features/transfer.feature.

Steps reuse the same clients, fixtures and data helpers as the API suite, so
the BDD layer is a readable view over the same framework, not a second one.
"""

import uuid
from dataclasses import dataclass, field

import allure
import pytest
import requests
from pytest_bdd import given, parsers, scenarios, then, when

from clients.transfers_client import REPLAYED_HEADER
from data.factories import idempotency_key, transfer_payload
from data.money import ringgit
from data.rules import PER_TRANSACTION_LIMIT_SEN
from support.assertions import assert_error, assert_status

pytestmark = [allure.epic("Fund transfer API"), allure.feature("BDD scenarios")]

scenarios("transfer.feature")


def parse_money(text: str) -> int:
    """'1,000.00' -> 100000 sen."""
    return ringgit(text)


parse_money.pattern = r"[\d,]+\.\d{2}"
TYPES = {"Money": parse_money}


@dataclass
class World:
    """State shared between the steps of one scenario."""

    accounts: dict[str, dict] = field(default_factory=dict)
    responses: list[requests.Response] = field(default_factory=list)
    last_payload: dict | None = None
    keys: dict[str, str] = field(default_factory=dict)

    @property
    def last_response(self) -> requests.Response:
        return self.responses[-1]

    def key_for(self, alias: str) -> str:
        """Map a readable key from the feature file to one unique to this run."""
        return self.keys.setdefault(alias, f"{alias}-{uuid.uuid4()}")


@pytest.fixture
def world() -> World:
    return World()


# --- Given ---


@given(parsers.parse("{name:w} has an account with a balance of RM {amount:Money}", extra_types=TYPES))
def account_with_balance(world, create_account, name, amount):
    world.accounts[name] = create_account(amount, owner_name=name)


@given(parsers.parse("{sender:w} has already transferred RM {amount:Money} to {receiver:w} today", extra_types=TYPES))
def already_transferred_today(world, transfers, sender, amount, receiver):
    remaining = amount
    while remaining > 0:
        chunk = min(remaining, PER_TRANSACTION_LIMIT_SEN)
        payload = transfer_payload(world.accounts[sender]["id"], world.accounts[receiver]["id"], amount_sen=chunk)
        assert_status(transfers.create(payload, idempotency_key("bdd-setup")), 201)
        remaining -= chunk


# --- When ---


def _send(world, transfers, sender, amount, receiver, key):
    world.last_payload = transfer_payload(
        world.accounts[sender]["id"], world.accounts[receiver]["id"], amount_sen=amount, reference="BDD"
    )
    world.responses.append(transfers.create(world.last_payload, key))


@when(parsers.parse("{sender:w} transfers RM {amount:Money} to {receiver:w}", extra_types=TYPES))
def transfer(world, transfers, sender, amount, receiver):
    _send(world, transfers, sender, amount, receiver, idempotency_key("bdd"))


@when(
    parsers.parse('{sender:w} transfers RM {amount:Money} to {receiver:w} with Idempotency-Key "{key}"', extra_types=TYPES)
)
def transfer_with_key(world, transfers, sender, amount, receiver, key):
    _send(world, transfers, sender, amount, receiver, world.key_for(key))


@when(parsers.parse('the same request is sent again with Idempotency-Key "{key}"'))
def resend_same_request(world, transfers, key):
    world.responses.append(transfers.create(world.last_payload, world.key_for(key)))


# --- Then ---


@then("the transfer succeeds")
def transfer_succeeds(world):
    assert_status(world.last_response, 201)


@then(parsers.parse('the transfer is rejected with "{code}"'))
def transfer_rejected(world, code):
    status = world.last_response.status_code
    assert 400 <= status < 500, f"expected a client error, got {status}: {world.last_response.text}"
    assert_error(world.last_response, status, code)


@then(parsers.parse("{name:w}'s balance is RM {amount:Money}", extra_types=TYPES))
def balance_is(world, balance_of, name, amount):
    assert balance_of(world.accounts[name]["id"]) == amount


@then(parsers.parse("{name:w}'s latest transaction is a {direction:w} of RM {amount:Money}", extra_types=TYPES))
def latest_transaction(world, accounts, name, direction, amount):
    entries = accounts.transactions(world.accounts[name]["id"], limit=1).json()["transactions"]
    assert entries, f"{name} has no transactions"
    assert entries[0]["direction"] == direction
    assert entries[0]["amount_sen"] == amount


@then(parsers.parse("{name:w} has no transactions"))
def no_transactions(world, accounts, name):
    assert accounts.transactions(world.accounts[name]["id"]).json()["transactions"] == []


@then("both responses describe the same transfer")
def same_transfer(world):
    first, second = world.responses[-2:]
    assert_status(first, 201)
    assert_status(second, 201)
    assert first.json() == second.json()


@then("the second response is marked as a replay")
def marked_as_replay(world):
    first, second = world.responses[-2:]
    assert REPLAYED_HEADER not in first.headers
    assert second.headers.get(REPLAYED_HEADER) == "true"
