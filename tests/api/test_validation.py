"""Request validation for POST /transfers.

Every case also checks that the sender's balance is unchanged: a rejected
request returning the right error code but still moving money would be the
worst possible bug, and asserting only on the response would miss it.
"""

import allure
import pytest

from data.factories import idempotency_key, transfer_payload
from data.money import ringgit
from data.rules import MAX_IDEMPOTENCY_KEY_LENGTH, MAX_REFERENCE_LENGTH
from support.assertions import assert_error, assert_status

pytestmark = [allure.epic("Fund transfer API"), allure.feature("Validation"), pytest.mark.validation]

UNKNOWN_ACCOUNT_ID = 999_999_999

# Marks a field to be removed from the payload (distinct from sending JSON null).
MISSING = object()

# Each case: (field overrides applied to a valid payload, expected status, expected code).
INVALID_PAYLOADS = [
    pytest.param({"amount_sen": 0}, 400, "INVALID_AMOUNT", id="zero amount"),
    pytest.param({"amount_sen": -100}, 400, "INVALID_AMOUNT", id="negative amount"),
    pytest.param({"amount_sen": MISSING}, 400, "INVALID_AMOUNT", id="missing amount"),
    pytest.param({"amount_sen": None}, 400, "INVALID_AMOUNT", id="null amount"),
    pytest.param({"amount_sen": "1000"}, 400, "INVALID_AMOUNT", id="amount as string"),
    pytest.param({"amount_sen": 10.5}, 400, "INVALID_AMOUNT", id="fractional sen"),
    pytest.param({"amount_sen": 2**63}, 400, "INVALID_AMOUNT", id="amount overflows int64"),
    pytest.param({"from_account_id": MISSING}, 400, "INVALID_ACCOUNT_ID", id="missing sender"),
    pytest.param({"to_account_id": MISSING}, 400, "INVALID_ACCOUNT_ID", id="missing receiver"),
    pytest.param({"to_account_id": -1}, 400, "INVALID_ACCOUNT_ID", id="negative receiver id"),
    pytest.param({"to_account_id": UNKNOWN_ACCOUNT_ID}, 404, "ACCOUNT_NOT_FOUND", id="unknown receiver"),
    pytest.param({"reference": "x" * (MAX_REFERENCE_LENGTH + 1)}, 400, "INVALID_REFERENCE", id="reference too long"),
    pytest.param({"currency": "USD"}, 400, "INVALID_JSON", id="unknown field"),
]


@pytest.mark.parametrize(("overrides", "status", "code"), INVALID_PAYLOADS)
def test_invalid_transfer_is_rejected_and_moves_no_money(transfers, sender, receiver, balance_of, overrides, status, code):
    payload = transfer_payload(sender["id"], receiver["id"])
    for field, value in overrides.items():
        if value is MISSING:
            del payload[field]
        else:
            payload[field] = value

    response = transfers.create(payload, idempotency_key())

    assert_error(response, status, code)
    assert balance_of(sender["id"]) == ringgit("1000.00")
    assert balance_of(receiver["id"]) == 0


def test_transfer_to_same_account_is_rejected(transfers, sender, balance_of):
    payload = transfer_payload(sender["id"], sender["id"])

    assert_error(transfers.create(payload, idempotency_key()), 400, "SAME_ACCOUNT")
    assert balance_of(sender["id"]) == ringgit("1000.00")


def test_unknown_sender_is_rejected(transfers, receiver):
    payload = transfer_payload(UNKNOWN_ACCOUNT_ID, receiver["id"])

    assert_error(transfers.create(payload, idempotency_key()), 404, "ACCOUNT_NOT_FOUND")


def test_reference_at_max_length_is_allowed(transfers, sender, receiver):
    payload = transfer_payload(sender["id"], receiver["id"], reference="x" * MAX_REFERENCE_LENGTH)

    assert_status(transfers.create(payload, idempotency_key()), 201)


def test_reference_length_counts_characters_not_bytes(transfers, sender, receiver):
    # 140 multi-byte characters (280 bytes in UTF-8) must still be accepted.
    payload = transfer_payload(sender["id"], receiver["id"], reference="é" * MAX_REFERENCE_LENGTH)

    assert_status(transfers.create(payload, idempotency_key()), 201)


@pytest.mark.parametrize(
    "body",
    [
        pytest.param('{"from_account_id": 1,', id="truncated JSON"),
        pytest.param("not json", id="plain text"),
        pytest.param("[]", id="array instead of object"),
        pytest.param('{"amount_sen": 1}{"amount_sen": 2}', id="two JSON objects"),
    ],
)
def test_malformed_body_is_rejected(transfers, body):
    assert_error(transfers.create_raw(body, idempotency_key()), 400, "INVALID_JSON")


class TestIdempotencyKeyHeader:
    def test_missing_header_is_rejected(self, transfers, sender, receiver, balance_of):
        response = transfers.create(transfer_payload(sender["id"], receiver["id"]), idempotency_key=None)

        assert_error(response, 400, "MISSING_IDEMPOTENCY_KEY")
        assert balance_of(sender["id"]) == ringgit("1000.00")

    def test_empty_header_is_rejected(self, transfers, sender, receiver):
        response = transfers.create(transfer_payload(sender["id"], receiver["id"]), idempotency_key="")

        assert_error(response, 400, "MISSING_IDEMPOTENCY_KEY")

    def test_key_at_max_length_is_allowed(self, transfers, sender, receiver):
        key = idempotency_key()[:MAX_IDEMPOTENCY_KEY_LENGTH].ljust(MAX_IDEMPOTENCY_KEY_LENGTH, "k")

        assert_status(transfers.create(transfer_payload(sender["id"], receiver["id"]), key), 201)

    def test_key_too_long_is_rejected(self, transfers, sender, receiver):
        key = "k" * (MAX_IDEMPOTENCY_KEY_LENGTH + 1)

        response = transfers.create(transfer_payload(sender["id"], receiver["id"]), key)

        assert_error(response, 400, "INVALID_IDEMPOTENCY_KEY")
