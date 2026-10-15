"""Idempotency-Key behaviour.

The contract: a client that times out and retries with the same key must get
the original result, and must never be debited twice. A key reused for a
different request is a client bug and is rejected.
"""

import json

import allure
import pytest

from clients.transfers_client import REPLAYED_HEADER
from data.factories import idempotency_key, transfer_payload
from data.money import ringgit
from support.assertions import assert_error, assert_status
from support.concurrency import fire_concurrently

pytestmark = [allure.epic("Fund transfer API"), allure.feature("Idempotency"), pytest.mark.idempotency]


@pytest.mark.smoke
@allure.severity(allure.severity_level.BLOCKER)
@allure.title("Retrying with the same key replays the original response and debits once")
def test_retry_replays_original_response(transfers, sender, receiver, balance_of):
    payload = transfer_payload(sender["id"], receiver["id"], amount_sen=ringgit("100.00"))
    key = idempotency_key()

    first = transfers.create(payload, key)
    retry = transfers.create(payload, key)

    assert_status(first, 201)
    assert_status(retry, 201)
    assert retry.json() == first.json(), "replay must return the original transfer, same id and timestamp"
    assert REPLAYED_HEADER not in first.headers
    assert retry.headers[REPLAYED_HEADER] == "true"
    assert balance_of(sender["id"]) == ringgit("900.00")
    assert balance_of(receiver["id"]) == ringgit("100.00")


def test_many_retries_create_exactly_one_transfer(transfers, accounts, sender, receiver, balance_of):
    payload = transfer_payload(sender["id"], receiver["id"], amount_sen=ringgit("10.00"))
    key = idempotency_key()

    ids = {transfers.create(payload, key).json()["id"] for _ in range(5)}

    assert len(ids) == 1
    assert len(accounts.transactions(sender["id"]).json()["transactions"]) == 1
    assert balance_of(sender["id"]) == ringgit("990.00")


def test_different_keys_create_separate_transfers(transfers, sender, receiver, balance_of):
    # Identity comes from the key, not the body: two genuine RM 10 payments
    # to the same person must both go through.
    payload = transfer_payload(sender["id"], receiver["id"], amount_sen=ringgit("10.00"))

    first = transfers.create(payload, idempotency_key())
    second = transfers.create(payload, idempotency_key())

    assert_status(first, 201)
    assert_status(second, 201)
    assert first.json()["id"] != second.json()["id"]
    assert balance_of(sender["id"]) == ringgit("980.00")


@pytest.mark.parametrize(
    "change",
    [
        pytest.param({"amount_sen": ringgit("99.00")}, id="different amount"),
        pytest.param({"reference": "something else"}, id="different reference"),
    ],
)
def test_key_reused_for_different_request_is_rejected(transfers, sender, receiver, balance_of, change):
    payload = transfer_payload(sender["id"], receiver["id"], amount_sen=ringgit("10.00"))
    key = idempotency_key()
    assert_status(transfers.create(payload, key), 201)

    response = transfers.create({**payload, **change}, key)

    assert_error(response, 422, "IDEMPOTENCY_KEY_REUSED")
    assert balance_of(sender["id"]) == ringgit("990.00"), "the reused key must not trigger a second debit"


def test_key_reused_for_different_receiver_is_rejected(transfers, create_account, sender, receiver, balance_of):
    payload = transfer_payload(sender["id"], receiver["id"], amount_sen=ringgit("10.00"))
    key = idempotency_key()
    assert_status(transfers.create(payload, key), 201)
    other_receiver = create_account(0)

    response = transfers.create({**payload, "to_account_id": other_receiver["id"]}, key)

    assert_error(response, 422, "IDEMPOTENCY_KEY_REUSED")
    assert balance_of(other_receiver["id"]) == 0


def test_replay_ignores_json_formatting_and_field_order(transfers, sender, receiver):
    payload = transfer_payload(sender["id"], receiver["id"])
    key = idempotency_key()
    assert_status(transfers.create(payload, key), 201)

    reordered = json.dumps(dict(reversed(list(payload.items()))), indent=4)
    retry = transfers.create_raw(reordered, key)

    assert_status(retry, 201)
    assert retry.headers[REPLAYED_HEADER] == "true"


def test_validation_error_does_not_consume_key(transfers, sender, receiver, balance_of):
    key = idempotency_key()
    bad = transfers.create(transfer_payload(sender["id"], receiver["id"], amount_sen=0), key)

    fixed = transfers.create(transfer_payload(sender["id"], receiver["id"], amount_sen=ringgit("5.00")), key)

    assert_error(bad, 400, "INVALID_AMOUNT")
    assert_status(fixed, 201)
    assert REPLAYED_HEADER not in fixed.headers, "a corrected request must be executed, not replayed"
    assert balance_of(sender["id"]) == ringgit("995.00")


@allure.title("A business rejection is final: retrying the key replays the rejection")
def test_business_rejection_is_replayed_even_after_topping_up(transfers, send, create_account, receiver):
    # Documented behaviour (same as Stripe's API): the key's outcome is fixed
    # once processed. After topping up, the client must use a NEW key.
    poor = create_account(ringgit("10.00"))
    payload = transfer_payload(poor["id"], receiver["id"], amount_sen=ringgit("50.00"))
    key = idempotency_key()
    assert_error(transfers.create(payload, key), 422, "INSUFFICIENT_FUNDS")

    assert_status(send(create_account(ringgit("100.00")), poor, ringgit("100.00")), 201)  # top up
    retry = transfers.create(payload, key)

    assert_error(retry, 422, "INSUFFICIENT_FUNDS")
    assert retry.headers[REPLAYED_HEADER] == "true"
    assert_status(transfers.create(payload, idempotency_key()), 201)


@pytest.mark.concurrency
@allure.severity(allure.severity_level.CRITICAL)
@allure.title("Simultaneous requests with the same key debit exactly once")
def test_concurrent_requests_with_same_key_debit_once(new_transfers_client, sender, receiver, balance_of):
    """A double-tapped "Pay" button: identical requests arriving together.

    Exactly one is executed. The rest are told it is in progress (409) or,
    if they arrive after it finished, get the replayed result.
    """
    payload = transfer_payload(sender["id"], receiver["id"], amount_sen=ringgit("100.00"))
    key = idempotency_key()

    def request(_):
        return new_transfers_client().create(payload, key)

    responses = fire_concurrently(request, count=10)

    executed = [r for r in responses if r.status_code == 201 and REPLAYED_HEADER not in r.headers]
    replayed = [r for r in responses if r.status_code == 201 and REPLAYED_HEADER in r.headers]
    in_progress = [r for r in responses if r.status_code == 409]
    assert len(executed) == 1, [r.text for r in responses]
    assert len(executed) + len(replayed) + len(in_progress) == len(responses), [r.text for r in responses]
    for r in in_progress:
        assert r.json()["error"]["code"] == "REQUEST_IN_PROGRESS"
        assert r.headers["Retry-After"] == "1"
    for r in replayed:
        assert r.json() == executed[0].json()
    assert balance_of(sender["id"]) == ringgit("900.00")
