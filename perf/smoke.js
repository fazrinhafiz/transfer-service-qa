// Smoke test: a short, light run used as the CI quality gate on every PR.
// Not about capacity: it proves the service handles a realistic request mix
// within the SLOs and that every response is correct.

import { sleep } from 'k6';
import { DEFAULT_THRESHOLDS, SUMMARY_TREND_STATS } from './lib/config.js';
import { bankingWorkload, createTestAccounts } from './lib/workload.js';

export const options = {
  vus: 5,
  duration: '30s',
  thresholds: DEFAULT_THRESHOLDS,
  summaryTrendStats: SUMMARY_TREND_STATS,
};

export function setup() {
  return createTestAccounts();
}

export default function (data) {
  bankingWorkload(data);
  sleep(0.2);
}
