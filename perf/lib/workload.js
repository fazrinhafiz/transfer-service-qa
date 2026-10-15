// The traffic mix shared by every test, and the test data it needs.
//
// Mix per iteration: 70% transfer, 20% balance check, 10% history.
// 5% of transfers are immediately retried with the same Idempotency-Key, as a
// mobile client would after a timeout, to load-test the replay path too.

import http from 'k6/http';
import { check, fail } from 'k6';
import { Counter } from 'k6/metrics';
import { createAccountRequest, getAccount, getHistory, transfer } from './api.js';

const idempotentReplays = new Counter('idempotent_replays');

// Test data is sized so no business rule can reject a request: any 4xx
// during a run is then a real problem, not exhausted test data.
//   - Amounts are 1-100 sen (avg ~50), so a sender reaches the RM 10,000
//     daily limit only after ~20,000 transfers. 500 senders => ~10M transfers.
//   - Senders only send and receivers only receive, so balances never run out.
const ACCOUNTS_PER_ROLE = Number(__ENV.ACCOUNTS || 500);
const SENDER_BALANCE_SEN = 2_000_000; // RM 20,000: above the daily limit
const BATCH_SIZE = 50;

export function createTestAccounts() {
  return {
    senders: createAccounts('k6 sender', SENDER_BALANCE_SEN),
    receivers: createAccounts('k6 receiver', 0),
  };
}

function createAccounts(label, balanceSen) {
  const ids = [];
  for (let start = 0; start < ACCOUNTS_PER_ROLE; start += BATCH_SIZE) {
    const batch = [];
    for (let i = start; i < Math.min(start + BATCH_SIZE, ACCOUNTS_PER_ROLE); i++) {
      batch.push(createAccountRequest(`${label} ${i}`, balanceSen));
    }
    for (const res of http.batch(batch)) {
      if (res.status !== 201) {
        fail(`setup: could not create account: ${res.status} ${res.body}`);
      }
      ids.push(res.json('id'));
    }
  }
  return ids;
}

const pick = (list) => list[Math.floor(Math.random() * list.length)];

export function bankingWorkload(data) {
  const roll = Math.random();
  if (roll < 0.7) {
    sendMoney(data);
  } else if (roll < 0.9) {
    const res = getAccount(pick(data.senders));
    check(res, { 'balance: 200': (r) => r.status === 200 });
  } else {
    const res = getHistory(pick(data.receivers));
    check(res, { 'history: 200': (r) => r.status === 200 });
  }
}

function sendMoney(data) {
  const from = pick(data.senders);
  const to = pick(data.receivers);
  const amount = 1 + Math.floor(Math.random() * 100);
  const key = `k6-${crypto.randomUUID()}`;

  const res = transfer(from, to, amount, key);
  check(res, { 'transfer: 201': (r) => r.status === 201 });

  if (res.status === 201 && Math.random() < 0.05) {
    const retry = transfer(from, to, amount, key);
    idempotentReplays.add(1);
    check(retry, {
      'replay: 201': (r) => r.status === 201,
      'replay: marked as replay': (r) => r.headers['Idempotent-Replayed'] === 'true',
      'replay: same transfer id': (r) => r.status === 201 && r.json('id') === res.json('id'),
    });
  }
}
