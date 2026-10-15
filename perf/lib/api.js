// Thin client for the transfer service.
//
// Every request carries a `name` tag with the URL *template*. Without it,
// /accounts/1, /accounts/2, ... would each become a separate metric series
// and the summary would be unreadable.

import http from 'k6/http';
import { BASE_URL } from './config.js';

const JSON_HEADERS = { 'Content-Type': 'application/json' };

export function createAccountRequest(ownerName, balanceSen) {
  return {
    method: 'POST',
    url: `${BASE_URL}/accounts`,
    body: JSON.stringify({ owner_name: ownerName, initial_balance_sen: balanceSen }),
    params: { headers: JSON_HEADERS, tags: { name: 'POST /accounts' } },
  };
}

export function transfer(fromId, toId, amountSen, idempotencyKey) {
  const body = JSON.stringify({
    from_account_id: fromId,
    to_account_id: toId,
    amount_sen: amountSen,
    reference: 'k6',
  });
  return http.post(`${BASE_URL}/transfers`, body, {
    headers: { ...JSON_HEADERS, 'Idempotency-Key': idempotencyKey },
    tags: { name: 'POST /transfers' },
  });
}

export function getAccount(id) {
  return http.get(`${BASE_URL}/accounts/${id}`, { tags: { name: 'GET /accounts/{id}' } });
}

export function getHistory(id) {
  return http.get(`${BASE_URL}/accounts/${id}/transactions?limit=20`, {
    tags: { name: 'GET /accounts/{id}/transactions' },
  });
}
