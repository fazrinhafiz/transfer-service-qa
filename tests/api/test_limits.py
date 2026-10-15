"""Balance, per-transaction and daily limit rules.

Boundary-value analysis: every limit is tested exactly at the limit (allowed)
and one sen beyond it (rejected). One sen is the smallest representable
step, so there is no gap between the two cases for a bug to hide in.
"""

import allure
import pytest

from data.factories import idempotency_key, transfer_payload
from data.money import ringgit
from data.rules import DAILY_LIMIT_SEN, PER_TRANSACTION_LIMIT_SEN
from support.assertions import assert_error, assert_status

pytestmark = [allure.epic("Fund transfer API"), allure.feature("Limits"), pytest.mark.limits]

RICH_BALANCE = ringgit("50000.00")  # well above the daily limit, so only limits bind


@allure.story("Balance")
class TestBalance:
    def test_amount_equal_to_balance_is_allowed(self, send, create_account, receiver):
        sender = create_account(ringgit("100.00"))

        assert_status(send(sender, receiver, ringgit("100.00")), 201)

    def test_amount_one_sen_over_balance_is_rejected(self, send, create_account, receiver, balance_of):
        sender = create_account(ringgit("100.00"))

        response = send(sender, receiver, ringgit("100.01"))

        assert_error(response, 422, "INSUFFICIENT_FUNDS")
        assert balance_of(sender["id"]) == ringgit("100.00")
        assert balance_of(receiver["id"]) == 0

    def test_empty_account_cannot_send_one_sen(self, send, create_account, receiver):
        sender = create_account(0)

        assert_error(send(sender, receiver, 1), 422, "INSUFFICIENT_FUNDS")

    def test_received_funds_can_be_spent(self, send, create_account, balance_of):
        a, b, c = create_account(ringgit("50.00")), create_account(0), create_account(0)
        assert_status(send(a, b, ringgit("50.00")), 201)

        assert_status(send(b, c, ringgit("50.00")), 201)

        assert balance_of(c["id"]) == ringgit("50.00")


@allure.story("Per-transaction limit")
class TestPerTransactionLimit:
    def test_exactly_the_limit_is_allowed(self, send, create_account, receiver):
        sender = create_account(RICH_BALANCE)

        assert_status(send(sender, receiver, PER_TRANSACTION_LIMIT_SEN), 201)

    def test_one_sen_over_the_limit_is_rejected(self, send, create_account, receiver, balance_of):
        sender = create_account(RICH_BALANCE)

        response = send(sender, receiver, PER_TRANSACTION_LIMIT_SEN + 1)

        assert_error(response, 422, "PER_TRANSACTION_LIMIT_EXCEEDED")
        assert "RM 5,000.00" in response.json()["error"]["message"]
        assert balance_of(sender["id"]) == RICH_BALANCE

    def test_limit_applies_even_with_sufficient_balance(self, send, create_account, receiver):
        sender = create_account(RICH_BALANCE)

        assert_error(send(sender, receiver, ringgit("20000.00")), 422, "PER_TRANSACTION_LIMIT_EXCEEDED")


@allure.story("Daily limit")
class TestDailyLimit:
    def _spend_full_daily_limit(self, send, sender, receiver):
        for _ in range(DAILY_LIMIT_SEN // PER_TRANSACTION_LIMIT_SEN):
            assert_status(send(sender, receiver, PER_TRANSACTION_LIMIT_SEN), 201)

    def test_exactly_the_daily_limit_is_allowed(self, send, create_account, receiver, balance_of):
        sender = create_account(RICH_BALANCE)

        self._spend_full_daily_limit(send, sender, receiver)

        assert balance_of(receiver["id"]) == DAILY_LIMIT_SEN

    def test_one_sen_over_the_daily_limit_is_rejected(self, send, create_account, receiver, balance_of):
        sender = create_account(RICH_BALANCE)
        self._spend_full_daily_limit(send, sender, receiver)

        response = send(sender, receiver, 1)

        assert_error(response, 422, "DAILY_LIMIT_EXCEEDED")
        assert balance_of(sender["id"]) == RICH_BALANCE - DAILY_LIMIT_SEN

    def test_error_reports_remaining_allowance(self, send, create_account, receiver):
        sender = create_account(RICH_BALANCE)
        assert_status(send(sender, receiver, ringgit("4000.00")), 201)
        assert_status(send(sender, receiver, ringgit("4000.00")), 201)

        response = send(sender, receiver, ringgit("2000.01"))

        assert_error(response, 422, "DAILY_LIMIT_EXCEEDED")
        assert "remaining today: RM 2,000.00" in response.json()["error"]["message"]

    def test_remaining_allowance_can_still_be_used(self, send, create_account, receiver):
        sender = create_account(RICH_BALANCE)
        assert_status(send(sender, receiver, ringgit("4000.00")), 201)
        assert_status(send(sender, receiver, ringgit("4000.00")), 201)

        assert_status(send(sender, receiver, ringgit("2000.00")), 201)

    def test_limit_is_per_sender(self, send, create_account, receiver):
        first, second = create_account(RICH_BALANCE), create_account(RICH_BALANCE)
        self._spend_full_daily_limit(send, first, receiver)

        assert_status(send(second, receiver, PER_TRANSACTION_LIMIT_SEN), 201)

    def test_incoming_transfers_do_not_count(self, send, create_account):
        a, b = create_account(RICH_BALANCE), create_account(RICH_BALANCE)
        self._spend_full_daily_limit(send, a, b)  # b receives RM 10,000

        assert_status(send(b, a, PER_TRANSACTION_LIMIT_SEN), 201)

    def test_rejected_transfers_do_not_count(self, send, create_account, receiver):
        sender = create_account(DAILY_LIMIT_SEN)
        # Rejected for exceeding the per-transaction limit: must not use up allowance.
        assert_error(send(sender, receiver, PER_TRANSACTION_LIMIT_SEN + 1), 422, "PER_TRANSACTION_LIMIT_EXCEEDED")

        self._spend_full_daily_limit(send, sender, receiver)

    @allure.story("Idempotency")
    def test_replayed_transfer_counts_once(self, transfers, send, create_account, receiver):
        sender = create_account(RICH_BALANCE)
        payload = transfer_payload(sender["id"], receiver["id"], amount_sen=PER_TRANSACTION_LIMIT_SEN)
        key = idempotency_key()
        for _ in range(3):
            assert_status(transfers.create(payload, key), 201)

        # Only RM 5,000 has really been sent, so another RM 5,000 fits today.
        assert_status(send(sender, receiver, PER_TRANSACTION_LIMIT_SEN), 201)
