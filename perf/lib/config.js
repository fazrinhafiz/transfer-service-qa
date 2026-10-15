// Shared settings and service-level objectives (SLOs) for every k6 test.

export const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

// The SLOs from the brief. A test run exits non-zero if any is breached,
// which is what lets CI use k6 as a quality gate.
export const SLO = {
  p95Ms: 300,
  maxErrorRate: 0.01,
};

export const DEFAULT_THRESHOLDS = {
  http_req_duration: [`p(95)<${SLO.p95Ms}`],
  http_req_failed: [`rate<${SLO.maxErrorRate}`],
  // Checks catch wrong answers that are still 2xx, e.g. a replay without
  // the Idempotent-Replayed header.
  checks: ['rate>0.99'],
};

// Show the percentiles we report on, not just k6's defaults.
export const SUMMARY_TREND_STATS = ['avg', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'];
