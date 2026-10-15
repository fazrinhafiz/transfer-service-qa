import allure
import pytest

from data.factories import account_payload
from data.money import ringgit
from data.rules import MAX_OWNER_NAME_LENGTH
from support.assertions import assert_error, assert_status

pytestmark = [allure.epic("Fund transfer API"), allure.feature("Accounts")]


@pytest.mark.smoke
@allure.title("Create an account and read it back")
def test_create_account_returns_account_and_location(accounts):
    payload = account_payload(owner_name="Aisyah binti Ahmad", initial_balance_sen=ringgit("1234.56"))

    response = accounts.create(payload)

    assert_status(response, 201)
    body = response.json()
    assert body["id"] > 0
    assert body["owner_name"] == "Aisyah binti Ahmad"
    assert body["balance_sen"] == 123456
    assert body["balance"] == "RM 1,234.56"
    assert response.headers["Location"] == f"/accounts/{body['id']}"

    fetched = accounts.get(body["id"])
    assert_status(fetched, 200)
    assert fetched.json() == body


def test_account_with_zero_opening_balance_is_allowed(accounts):
    response = accounts.create(account_payload(initial_balance_sen=0))

    assert_status(response, 201)
    assert response.json()["balance_sen"] == 0


def test_owner_name_is_trimmed(accounts):
    response = accounts.create(account_payload(owner_name="  Badrul  "))

    assert_status(response, 201)
    assert response.json()["owner_name"] == "Badrul"


def test_owner_name_at_max_length_is_allowed(accounts):
    response = accounts.create(account_payload(owner_name="A" * MAX_OWNER_NAME_LENGTH))

    assert_status(response, 201)


@pytest.mark.validation
@pytest.mark.parametrize(
    ("overrides", "code"),
    [
        pytest.param({"initial_balance_sen": -1}, "INVALID_AMOUNT", id="negative balance"),
        pytest.param({"initial_balance_sen": 10.5}, "INVALID_AMOUNT", id="decimal balance"),
        pytest.param({"initial_balance_sen": "100"}, "INVALID_AMOUNT", id="balance as string"),
        pytest.param({"owner_name": ""}, "INVALID_OWNER_NAME", id="empty name"),
        pytest.param({"owner_name": "   "}, "INVALID_OWNER_NAME", id="whitespace name"),
        pytest.param({"owner_name": "A" * (MAX_OWNER_NAME_LENGTH + 1)}, "INVALID_OWNER_NAME", id="name too long"),
        pytest.param({"is_admin": True}, "INVALID_JSON", id="unknown field"),
    ],
)
def test_create_account_rejects_invalid_payload(accounts, overrides, code):
    response = accounts.create(account_payload(**overrides))

    assert_error(response, 400, code)


@pytest.mark.validation
def test_create_account_rejects_malformed_json(accounts):
    response = accounts.create_raw('{"owner_name": "Aisyah",')

    assert_error(response, 400, "INVALID_JSON")


@pytest.mark.parametrize(
    ("account_id", "status", "code"),
    [
        pytest.param(999_999_999, 404, "ACCOUNT_NOT_FOUND", id="unknown id"),
        pytest.param("abc", 400, "INVALID_ACCOUNT_ID", id="non-numeric id"),
        pytest.param(0, 400, "INVALID_ACCOUNT_ID", id="zero id"),
        pytest.param(-5, 400, "INVALID_ACCOUNT_ID", id="negative id"),
    ],
)
def test_get_account_errors(accounts, account_id, status, code):
    assert_error(accounts.get(account_id), status, code)


def test_new_account_has_empty_history(accounts, create_account):
    account = create_account()

    response = accounts.transactions(account["id"])

    assert_status(response, 200)
    assert response.json() == {"account_id": account["id"], "transactions": []}
