"""Thin HTTP layer shared by all resource clients.

Clients return the raw requests.Response so tests can assert on status codes
and headers. Every request/response pair is attached to the Allure report,
so a failing test shows exactly what went over the wire.
"""

import json

import allure
import requests

from config.settings import Settings


class ApiClient:
    def __init__(self, settings: Settings, *, report: bool = True):
        self._base_url = settings.base_url
        self._timeout = settings.timeout_seconds
        # Concurrency tests create one client per thread with report=False:
        # requests.Session is not thread-safe, and attaching hundreds of
        # parallel calls would bury the useful parts of the report.
        self._report = report
        self._session = requests.Session()
        self._session.headers.update({"Content-Type": "application/json", "Accept": "application/json"})

    def request(self, method: str, path: str, **kwargs) -> requests.Response:
        kwargs.setdefault("timeout", self._timeout)
        url = f"{self._base_url}{path}"
        if not self._report:
            return self._session.request(method, url, **kwargs)

        with allure.step(f"{method} {path}"):
            response = self._session.request(method, url, **kwargs)
            _attach_exchange(response)
            return response

    def get(self, path: str, **kwargs) -> requests.Response:
        return self.request("GET", path, **kwargs)

    def post(self, path: str, **kwargs) -> requests.Response:
        return self.request("POST", path, **kwargs)

    def close(self) -> None:
        self._session.close()


def _attach_exchange(response: requests.Response) -> None:
    req = response.request
    request_text = f"{req.method} {req.url}\n" + "\n".join(f"{k}: {v}" for k, v in req.headers.items())
    if req.body:
        body = req.body.decode() if isinstance(req.body, bytes) else req.body
        request_text += f"\n\n{body}"
    allure.attach(request_text, name="request", attachment_type=allure.attachment_type.TEXT)

    try:
        pretty = json.dumps(response.json(), indent=2)
        allure.attach(pretty, name=f"response {response.status_code}", attachment_type=allure.attachment_type.JSON)
    except ValueError:
        allure.attach(response.text, name=f"response {response.status_code}", attachment_type=allure.attachment_type.TEXT)
