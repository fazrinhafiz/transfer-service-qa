// Load test: expected peak traffic, sustained.
//
// Closed model: 50 virtual users, each pausing 0.5-1.5s between actions like
// a person using the app. Answers "do we meet the SLOs at normal peak load?"

import { sleep } from 'k6';
import { DEFAULT_THRESHOLDS, SUMMARY_TREND_STATS } from './lib/config.js';
import { bankingWorkload, createTestAccounts } from './lib/workload.js';

export const options = {
  scenarios: {
    load: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '1m', target: 50 }, // ramp up
        { duration: '3m', target: 50 }, // steady state at peak
        { duration: '30s', target: 0 }, // ramp down
      ],
      gracefulRampDown: '10s',
    },
  },
  thresholds: {
    ...DEFAULT_THRESHOLDS,
    // The money-moving endpoint gets its own SLO, so fast reads can't
    // mask slow transfers in the overall p95.
    'http_req_duration{name:POST /transfers}': ['p(95)<300'],
  },
  summaryTrendStats: SUMMARY_TREND_STATS,
};

export function setup() {
  return createTestAccounts();
}

export default function (data) {
  bankingWorkload(data);
  sleep(0.5 + Math.random());
}
