// Spike test: a sudden burst, e.g. salary day at midnight or a promo push.
//
// Answers two questions, each with its own thresholds:
//   1. During the spike, does the service stay *correct* (no errors), even if
//      it slows down?
//   2. Once the burst passes, does latency recover to normal, or does a
//      backlog (exhausted connection pool, queued requests) linger?

import { sleep } from 'k6';
import { DEFAULT_THRESHOLDS, SLO, SUMMARY_TREND_STATS } from './lib/config.js';
import { tagCurrentPhase, thresholdsPerPhase } from './lib/stages.js';
import { bankingWorkload, createTestAccounts } from './lib/workload.js';

const BASELINE_VUS = 10;
const SPIKE_VUS = Number(__ENV.SPIKE_VUS || 300);

const PHASES = [
  { name: 'baseline', durationSeconds: 30 },
  { name: 'spike', durationSeconds: 70 }, // 10s surge + 60s at peak
  { name: 'recovery', durationSeconds: 70 }, // 10s drop + 60s back at baseline
];

export const options = {
  scenarios: {
    spike: {
      executor: 'ramping-vus',
      startVUs: BASELINE_VUS,
      stages: [
        { duration: '30s', target: BASELINE_VUS },
        { duration: '10s', target: SPIKE_VUS },
        { duration: '60s', target: SPIKE_VUS },
        { duration: '10s', target: BASELINE_VUS },
        { duration: '60s', target: BASELINE_VUS },
      ],
      gracefulRampDown: '10s',
    },
  },
  thresholds: {
    ...DEFAULT_THRESHOLDS,
    ...thresholdsPerPhase(PHASES, {
      http_req_failed: [`rate<${SLO.maxErrorRate}`],
    }),
    // After the spike, latency must be back within the SLO.
    'http_req_duration{stage:recovery}': [`p(95)<${SLO.p95Ms}`],
  },
  summaryTrendStats: SUMMARY_TREND_STATS,
};

export function setup() {
  return createTestAccounts();
}

export default function (data) {
  tagCurrentPhase(PHASES);
  bankingWorkload(data);
  sleep(0.2 + Math.random() * 0.3);
}
