// Per-stage metrics.
//
// k6 thresholds are evaluated over the whole run. In a stress test that ramps
// from 100 to 3000 req/s, a whole-run p95 blends healthy and overloaded
// periods and hides *where* the service broke. Tagging every request with the
// stage it ran in lets us put a threshold on each stage separately: the first
// stage that fails is the breaking point.

import exec from 'k6/execution';

// phases: [{ name, durationSeconds }] in order.
export function phaseAt(phases, elapsedMs) {
  let end = 0;
  for (const phase of phases) {
    end += phase.durationSeconds * 1000;
    if (elapsedMs < end) return phase.name;
  }
  return phases[phases.length - 1].name;
}

// Call at the start of each iteration. Setting a tag on exec.vu.metrics.tags
// applies it to every metric this VU emits from then on.
export function tagCurrentPhase(phases) {
  exec.vu.metrics.tags.stage = phaseAt(phases, Date.now() - exec.scenario.startTime);
}

export function thresholdsPerPhase(phases, rules) {
  const thresholds = {};
  for (const { name } of phases) {
    for (const [metric, conditions] of Object.entries(rules)) {
      thresholds[`${metric}{stage:${name}}`] = conditions;
    }
  }
  return thresholds;
}
