"""Concurrency: parallel requests against the same accounts.

These target race conditions that sequential tests can never see:
  * check-then-act: two requests both read "balance RM 100" and both debit
  * lost updates:   two credits to the same receiver, one overwrites the other
  * deadlocks:      A->B and B->A locking rows in opposite order
"""

import allure
import pytest

from data.factories import idempotency_key, transfer_payload
from data.money import ringgit
from data.rules import DAILY_LIMIT_SEN
from support.concurrency import fire_concurrently, outcome_counts

pytestmark = [allure.epic("Fund transfer API"), allure.feature("Concurrency"), pytest.mark.concurrency]


@allure.severity(allure.severity_level.BLOCKER)
@allure.title("50 parallel transfers from one account never overdraw it")
def test_parallel_transfers_never_overdraw(new_transfers_client, accounts, create_account, receiver, balance_of):
    sender = create_account(ringgit("100.00"))
    amount = ringgit("10.00")  # balance covers exactly 10 of the 50

    def request(_):
        return new_transfers_client().create(transfer_payload(sender["id"], receiver["id"], amount_sen=amount), idempotency_key())

    responses = fire_concurrently(request, count=50)

    assert outcome_counts(responses) == {201: 10, "INSUFFICIENT_FUNDS": 40}
    assert balance_of(sender["id"]) == 0
    assert balance_of(receiver["id"]) == ringgit("100.00")

    # The ledger tells the same story: 10 debits, each leaving a distinct,
    # non-negative balance (100 -> 90 -> ... -> 0, in some order).
    debits = accounts.transactions(sender["id"], limit=100).json()["transactions"]
    assert len(debits) == 10
    assert sorted(d["balance_after_sen"] for d in debits) == [ringgit(str(x)) for x in range(0, 100, 10)]


@allure.title("Parallel transfers cannot exceed the daily limit together")
def test_parallel_transfers_respect_daily_limit(new_transfers_client, create_account, receiver, balance_of):
    sender = create_account(ringgit("50000.00"))
    amount = ringgit("500.00")  # the daily limit allows exactly 20 of the 30

    def request(_):
        return new_transfers_client().create(transfer_payload(sender["id"], receiver["id"], amount_sen=amount), idempotency_key())

    responses = fire_concurrently(request, count=30)

    assert outcome_counts(responses) == {201: 20, "DAILY_LIMIT_EXCEEDED": 10}
    assert balance_of(receiver["id"]) == DAILY_LIMIT_SEN


@allure.title("Many senders paying one receiver at once: no credit is lost")
def test_parallel_credits_to_one_receiver_are_not_lost(new_transfers_client, create_account, receiver, balance_of):
    senders = [create_account(ringgit("10.00")) for _ in range(20)]

    def request(i):
        payload = transfer_payload(senders[i]["id"], receiver["id"], amount_sen=ringgit("10.00"))
        return new_transfers_client().create(payload, idempotency_key())

    responses = fire_concurrently(request, count=len(senders))

    assert outcome_counts(responses) == {201: 20}
    assert balance_of(receiver["id"]) == ringgit("200.00")


@allure.title("Opposite transfers between two accounts do not deadlock")
def test_opposite_transfers_do_not_deadlock(new_transfers_client, create_account, balance_of):
    a, b = create_account(ringgit("1000.00")), create_account(ringgit("1000.00"))
    pairs = [(a, b), (b, a)] * 15

    def request(i):
        frm, to = pairs[i]
        return new_transfers_client().create(transfer_payload(frm["id"], to["id"], amount_sen=ringgit("1.00")), idempotency_key())

    responses = fire_concurrently(request, count=len(pairs))

    # A deadlock would surface as a 500 (MySQL aborts one transaction).
    assert outcome_counts(responses) == {201: len(pairs)}
    assert balance_of(a["id"]) + balance_of(b["id"]) == ringgit("2000.00")
