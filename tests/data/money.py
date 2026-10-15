"""Money helpers for test data. Amounts are always handled as integer sen.

Conversions go through Decimal from a string, never float: ringgit("0.1")
is exactly 10 sen, whereas int(0.1 * 100) happens to work but
int(0.29 * 100) == 28.
"""

from decimal import Decimal


def ringgit(amount: str | int) -> int:
    """ringgit("25.50") -> 2550 sen."""
    sen = Decimal(str(amount).replace(",", "")) * 100
    if sen != sen.to_integral_value():
        raise ValueError(f"{amount!r} has more than 2 decimal places")
    return int(sen)


def display(sen: int) -> str:
    """Format sen the way the API does: 123456 -> 'RM 1,234.56'."""
    sign = "-" if sen < 0 else ""
    whole, cents = divmod(abs(sen), 100)
    return f"{sign}RM {whole:,}.{cents:02d}"
