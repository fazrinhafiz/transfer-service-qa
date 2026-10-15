// Stress test: find the breaking point.
//
// Open model: k6 starts a fixed number of iterations per second whatever the
// response times. (With a closed model, slow responses would make virtual
// users send *less*, so the load would back off just as the service struggles.)
//
// The rate climbs in steps. Each step has its own SLO thresholds (see
// lib/stages.js); the first step whose thresholds fail is the breaking point.
// This test is expected to fail: that's how it reports the limit.

import { DEFAULT_THRESHOLDS, SLO, SUMMARY_TREND_STATS } from './lib/config.js';
import { tagCurrentPhase, thresholdsPerPhase } from './lib/stages.js';
import { bankingWorkload, createTestAccounts } from './lib/workload.js';

const STEPS_RPS = (__ENV.STEPS || '100,250,500,1000,1500,2000,3000').split(',').map(Number);
const RAMP_SECONDS = 10;
const HOLD_SECONDS = 40;

// Each step: ramp to the new rate, then hold it.
const PHASES = STEPS_RPS.map((rps) => ({
  name: `${String(rps).padStart(4, '0')}rps`, // zero-padded so the summary sorts in order
  durationSeconds: RAMP_SECONDS + HOLD_SECONDS,
}));

export const options = {
  scenarios: {
    stress: {
      executor: 'ramping-arrival-rate',
      startRate: STEPS_RPS[0],
      timeUnit: '1s',
      preAllocatedVUs: 100,
      maxVUs: 1000,
      stages: STEPS_RPS.flatMap((rps) => [
        { duration: `${RAMP_SECONDS}s`, target: rps },
        { duration: `${HOLD_SECONDS}s`, target: rps },
      ]),
    },
  },
  thresholds: {
    ...DEFAULT_THRESHOLDS,
    ...thresholdsPerPhase(PHASES, {
      http_req_duration: [`p(95)<${SLO.p95Ms}`],
      http_req_failed: [`rate<${SLO.maxErrorRate}`],
    }),
    // Safety valve: stop early once the service is clearly down, instead of
    // hammering it for the remaining steps.
    http_req_failed: [
      `rate<${SLO.maxErrorRate}`,
      { threshold: 'rate<0.25', abortOnFail: true, delayAbortEval: '20s' },
    ],
  },
  summaryTrendStats: SUMMARY_TREND_STATS,
};

export function setup() {
  return createTestAccounts();
}

export default function (data) {
  tagCurrentPhase(PHASES);
  bankingWorkload(data);
}
