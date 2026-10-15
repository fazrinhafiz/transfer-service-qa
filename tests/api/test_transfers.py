from datetime import datetime

import allure
import pytest

from data.factories import idempotency_key, transfer_payload
from data.money import ringgit
from data.rules import MAX_HISTORY_LIMIT
from support.assertions import assert_error, assert_status

pytestmark = [allure.epic("Fund transfer API"), allure.feature("Transfers")]


@pytest.mark.smoke
@allure.severity(allure.severity_level.BLOCKER)
@allure.title("Transfer moves the exact amount from sender to receiver")
def test_transfer_moves_money(transfers, sender, receiver, balance_of):
    payload = transfer_payload(sender["id"], receiver["id"], amount_sen=ringgit("125.50"), reference="Rent October")

    response = transfers.create(payload, idempotency_key())

    assert_status(response, 201)
    body = response.json()
    assert body["id"] > 0
    assert body["from_account_id"] == sender["id"]
    assert body["to_account_id"] == receiver["id"]
    assert body["amount_sen"] == 12550
    assert body["amount"] == "RM 125.50"
    assert body["reference"] == "Rent October"
    datetime.fromisoformat(body["created_at"])  # raises if not ISO-8601

    assert balance_of(sender["id"]) == ringgit("874.50")
    assert balance_of(receiver["id"]) == ringgit("125.50")


def test_transfer_of_entire_balance_leaves_zero(send, create_account, receiver, balance_of):
    sender = create_account(ringgit("42.42"))

    assert_status(send(sender, receiver, ringgit("42.42")), 201)

    assert balance_of(sender["id"]) == 0


def test_transfer_without_reference_is_allowed(transfers, sender, receiver):
    payload = transfer_payload(sender["id"], receiver["id"])
    del payload["reference"]

    response = transfers.create(payload, idempotency_key())

    assert_status(response, 201)
    assert response.json()["reference"] == ""


@allure.title("Money is neither created nor destroyed by transfers")
def test_total_money_is_conserved(send, create_account, balance_of):
    a, b = create_account(ringgit("300.00")), create_account(ringgit("200.00"))

    for amount, (frm, to) in [(ringgit("50.00"), (a, b)), (ringgit("120.25"), (b, a)), (ringgit("0.01"), (a, b))]:
        assert_status(send(frm, to, amount), 201)

    assert balance_of(a["id"]) + balance_of(b["id"]) == ringgit("500.00")


class TestTransactionHistory:
    @allure.title("A transfer writes a debit for the sender and a credit for the receiver")
    def test_both_sides_are_recorded(self, accounts, send, sender, receiver):
        transfer = send(sender, receiver, ringgit("30.00"), reference="Makan").json()

        [debit] = accounts.transactions(sender["id"]).json()["transactions"]
        [credit] = accounts.transactions(receiver["id"]).json()["transactions"]

        assert debit == {
            "transfer_id": transfer["id"],
            "direction": "DEBIT",
            "amount_sen": 3000,
            "amount": "RM 30.00",
            "balance_after_sen": ringgit("970.00"),
            "counterparty_account_id": receiver["id"],
            "reference": "Makan",
            "created_at": debit["created_at"],
        }
        assert credit["transfer_id"] == transfer["id"]
        assert credit["direction"] == "CREDIT"
        assert credit["balance_after_sen"] == ringgit("30.00")
        assert credit["counterparty_account_id"] == sender["id"]

    def test_history_is_newest_first_with_running_balance(self, accounts, send, sender, receiver):
        amounts = [ringgit("1.00"), ringgit("2.00"), ringgit("3.00")]
        for amount in amounts:
            assert_status(send(sender, receiver, amount), 201)

        entries = accounts.transactions(sender["id"]).json()["transactions"]

        assert [e["amount_sen"] for e in entries] == list(reversed(amounts))
        assert [e["balance_after_sen"] for e in entries] == [ringgit("994.00"), ringgit("997.00"), ringgit("999.00")]

    def test_limit_returns_most_recent_entries(self, accounts, send, sender, receiver):
        for amount in (100, 200, 300):
            assert_status(send(sender, receiver, amount), 201)

        entries = accounts.transactions(sender["id"], limit=2).json()["transactions"]

        assert [e["amount_sen"] for e in entries] == [300, 200]

    def test_history_returned_matches_transfer_timestamps(self, accounts, send, sender, receiver):
        transfer = send(sender, receiver, 100).json()

        [entry] = accounts.transactions(sender["id"]).json()["transactions"]

        assert datetime.fromisoformat(entry["created_at"]) == datetime.fromisoformat(transfer["created_at"])

    @pytest.mark.validation
    @pytest.mark.parametrize("limit", [0, MAX_HISTORY_LIMIT + 1, -1, "ten"])
    def test_invalid_limit_is_rejected(self, accounts, sender, limit):
        assert_error(accounts.transactions(sender["id"], limit=limit), 400, "INVALID_LIMIT")

    def test_history_of_unknown_account(self, accounts):
        assert_error(accounts.transactions(999_999_999), 404, "ACCOUNT_NOT_FOUND")
