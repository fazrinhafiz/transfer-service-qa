"""Money precision and formatting.

Amounts are integer sen end to end. These tests pin down that nothing is
ever rounded: values that are not whole sen are rejected outright, and the
classic floating-point traps (0.1 + 0.2) come out exact.
"""

import allure
import pytest

from data.factories import account_payload, idempotency_key, transfer_payload
from data.money import display, ringgit
from support.assertions import assert_error, assert_status

pytestmark = [allure.epic("Fund transfer API"), allure.feature("Money precision"), pytest.mark.money]


def test_smallest_amount_one_sen(send, sender, receiver, balance_of):
    response = send(sender, receiver, 1)

    assert_status(response, 201)
    assert response.json()["amount"] == "RM 0.01"
    assert balance_of(sender["id"]) == ringgit("999.99")


@allure.title("RM 0.10 + RM 0.20 is exactly RM 0.30")
def test_ten_sen_plus_twenty_sen_is_exactly_thirty(send, sender, receiver, accounts):
    # In float64, 0.1 + 0.2 == 0.30000000000000004.
    send(sender, receiver, ringgit("0.10"))
    send(sender, receiver, ringgit("0.20"))

    body = accounts.get(receiver["id"]).json()
    assert body["balance_sen"] == 30
    assert body["balance"] == "RM 0.30"


def test_many_small_transfers_sum_exactly(send, sender, receiver, balance_of):
    for _ in range(10):
        assert_status(send(sender, receiver, ringgit("0.10")), 201)

    assert balance_of(receiver["id"]) == ringgit("1.00")
    assert balance_of(sender["id"]) == ringgit("999.00")


@pytest.mark.parametrize(
    "raw_amount",
    [
        pytest.param("10.5", id="half a sen"),
        pytest.param("0.01", id="ringgit sent instead of sen"),
        pytest.param("100.0", id="whole number written as decimal"),
        pytest.param("1e2", id="exponent notation"),
    ],
)
def test_non_integer_amounts_are_rejected_not_rounded(transfers, sender, receiver, balance_of, raw_amount):
    # The body is built by hand so each number reaches the API exactly as
    # written: json.dumps would serialise the Python float 1e2 as "100.0".
    body = (
        f'{{"from_account_id": {sender["id"]}, "to_account_id": {receiver["id"]}, '
        f'"amount_sen": {raw_amount}, "reference": "precision"}}'
    )

    response = transfers.create_raw(body, idempotency_key())

    assert_error(response, 400, "INVALID_AMOUNT")
    assert balance_of(sender["id"]) == ringgit("1000.00")


@pytest.mark.parametrize(
    ("sen", "expected"),
    [(0, "RM 0.00"), (5, "RM 0.05"), (50, "RM 0.50"), (100, "RM 1.00"), (123456, "RM 1,234.56"), (100_000_000, "RM 1,000,000.00")],
)
def test_display_format(accounts, sen, expected):
    body = accounts.create(account_payload(initial_balance_sen=sen)).json()

    assert body["balance"] == expected
    assert display(sen) == expected  # keeps the test helper honest too


@allure.title("Crediting an account at the maximum balance fails safely")
def test_credit_that_would_overflow_balance_is_rejected(transfers, create_account, sender, balance_of):
    # int64 max sen is ~RM 92 quadrillion: unrealistic, but an unchecked
    # addition would wrap around to a negative balance.
    whale = create_account(2**63 - 1)

    response = transfers.create(transfer_payload(sender["id"], whale["id"], amount_sen=1), idempotency_key())

    assert_error(response, 422, "BALANCE_LIMIT_EXCEEDED")
    assert balance_of(whale["id"]) == 2**63 - 1
    assert balance_of(sender["id"]) == ringgit("1000.00")
