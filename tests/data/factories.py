"""Test data factories.

Each factory returns a valid payload by default; tests override only the
fields they care about, which keeps the intent of each test obvious:

    transfer_payload(sender, receiver, amount_sen=0)   # "zero amount" test
"""

import uuid

from data.money import ringgit


def unique_suffix() -> str:
    return uuid.uuid4().hex[:8]


def idempotency_key(label: str = "qa") -> str:
    """A key that is unique per call, so tests never collide across runs."""
    return f"{label}-{uuid.uuid4()}"


def account_payload(**overrides) -> dict:
    payload = {
        "owner_name": f"QA Customer {unique_suffix()}",
        "initial_balance_sen": ringgit("1000.00"),
    }
    payload.update(overrides)
    return payload


def transfer_payload(from_account_id: int, to_account_id: int, **overrides) -> dict:
    payload = {
        "from_account_id": from_account_id,
        "to_account_id": to_account_id,
        "amount_sen": ringgit("10.00"),
        "reference": "QA transfer",
    }
    payload.update(overrides)
    return payload
