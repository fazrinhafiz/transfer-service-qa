"""Business rules under test, written from the specification.

These are deliberately NOT read from the service: if a developer changed a
limit in the code by mistake, tests that derived their expectations from the
code would happily agree with the bug.
"""

from data.money import ringgit

PER_TRANSACTION_LIMIT_SEN = ringgit("5000.00")
DAILY_LIMIT_SEN = ringgit("10000.00")

MAX_OWNER_NAME_LENGTH = 100
MAX_REFERENCE_LENGTH = 140
MAX_IDEMPOTENCY_KEY_LENGTH = 64
MAX_HISTORY_LIMIT = 100
