package baseline

// cachedRunners holds, per rule code, the runner labels (.vlotpipe.yml's
// rules.<CODE>.cached_runners) a repo owner asserts already have
// persistent caching — an asserted fact, not a heuristic guess, so a
// match suppresses the rule entirely rather than just downgrading
// severity the way looksLikeEphemeralRunner's guess does. Only PERF001
// and LEAN010 consult this today (see ADR 0002); deliberately not
// merged into looksLikeEphemeralRunner itself, which SEC010 also uses
// for an unrelated attack-surface reason — a runner known to have a
// cache says nothing about whether it's safe to run untrusted fork-PR
// code on it.
//
// Package-level rather than threaded through the Rule interface: PERF001
// and LEAN010 are registered into rules.registry via init() like every
// other rule, so they only ever receive a *model.Pipeline through
// Check(p). SetCachedRunners is called once by the CLI, from the
// resolved .vlotpipe.yml, before rules.Run.
var cachedRunners = map[string][]string{}

// SetCachedRunners records the cached-runner labels for one rule code,
// replacing any previous value — a fresh scan (a new process, or a new
// call from a test) always starts from whatever the caller sets, never
// from a stale prior run's config.
func SetCachedRunners(code string, patterns []string) {
	cachedRunners[code] = patterns
}

// isCachedRunner reports whether runsOn matches one of code's configured
// cached-runner patterns — an exact label, or "*" (same wildcard
// convention as .vlotpipe.yml's ignore: path: "*").
func isCachedRunner(code, runsOn string) bool {
	for _, pat := range cachedRunners[code] {
		if pat == "*" || pat == runsOn {
			return true
		}
	}
	return false
}
