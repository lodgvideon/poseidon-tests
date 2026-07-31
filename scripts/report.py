#!/usr/bin/env python3
"""Turn a directory of benchmark JSON reports into a Markdown comparison.

The benchmark harness writes one JSON report per (regime, arm) combination into
a results directory, named ``<regime>-<arm>.json`` -- for example
``h2-poseidon.json``. Regimes are ``h1``, ``h2``, ``h3``, ``grpc``; arms are
``poseidon`` (the poseidon-http-client under test) and ``standard`` (the
stock Go client baseline).

This script reads every report in such a directory and emits a Markdown
document containing:

  * one table per resource metric (CPU, RSS avg, RSS peak, allocs/req,
    bytes/req), each with a row per regime and a poseidon-vs-standard delta,
  * a ``Validity`` section flagging anything that would make the comparison
    untrustworthy (errored runs, unequal work between arms, diverging load,
    missing files),
  * a ``Raw`` section listing the headline counters of every input file.

Every metric is "lower is better", so a negative delta means poseidon won.
The output goes to stdout and to ``<dir>/COMPARISON.md``.

Usage:
    python3 scripts/report.py [results-dir]    # default: results/

Standard library only; a partial or malformed results directory is reported
rather than crashed on.
"""

from __future__ import annotations

import json
import sys
from datetime import datetime
from pathlib import Path

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------

#: Regimes in presentation order, with their display labels.
REGIMES = [
    ("h1", "HTTP/1.1"),
    ("h2", "HTTP/2"),
    ("h3", "HTTP/3"),
    ("grpc", "gRPC"),
]

#: The two arms being compared. The second one is the baseline for deltas.
ARMS = ("poseidon", "standard")

MIB = 1024.0 * 1024.0

#: Deltas smaller than this (in absolute percent) are called a tie.
EQUAL_BAND_PCT = 5.0

#: Per-metric noise floors. A delta smaller than the metric's floor gets no
#: verdict at all, because the measurement cannot distinguish it from noise.
#:
#: CPU is the reason this exists. At 200 RPS the driver runs at 7-8% of one
#: core and only ~25% of its CPU samples are in the request path -- the rest is
#: goroutine park/unpark and the harness's own rate limiter. A CPU delta below
#: ~25% is not evidence of anything, and the previous flat 5% band stamped
#: "poseidon better" on -14%/-18%/-19% CPU rows that a re-run in another
#: environment flipped the sign of. Allocation counters are exact, accumulated
#: by the runtime rather than sampled, so they keep the tighter band.
#: See docs/FINDINGS.md, "The CPU column has poor signal at 200 RPS".
NOISE_FLOOR_PCT = {
    "CPU (millicores)": 25.0,
    "RSS avg (MiB)": 10.0,
    "RSS peak (MiB)": 10.0,
}

#: Verdict text for a delta inside the metric's noise floor.
BELOW_FLOOR = "below noise floor"

#: Wall-clock minus monotonic duration beyond this fraction means the measuring
#: host lost time mid-window (a paused or clock-stepped VM), so any rate-derived
#: figure from that cell -- CPU above all -- is suspect.
CLOCK_DRIFT_PCT = 1.0

#: Arms whose request counts or achieved RPS differ by more than this percent
#: are not comparable on a per-request basis.
PAIR_TOLERANCE_PCT = 2.0

#: Arms whose consumed response body volume differs by more than this percent
#: did not do the same work. Tighter than PAIR_TOLERANCE_PCT because the
#: payload sequence is deterministic and shared, so any divergence at all is a
#: harness bug rather than variance.
BODY_TOLERANCE_PCT = 0.5

DEFAULT_DIR = "results"
OUTPUT_NAME = "COMPARISON.md"

MISSING = "n/a"


# ---------------------------------------------------------------------------
# Small helpers
# ---------------------------------------------------------------------------


def number(record, key):
    """Return ``record[key]`` as a float, or None if absent/not numeric."""
    if not isinstance(record, dict):
        return None
    value = record.get(key)
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        return None
    return float(value)


def fmt_1dp(value):
    return MISSING if value is None else "{:.1f}".format(value)


def fmt_thousands(value):
    return MISSING if value is None else "{:,}".format(int(round(value)))


def fmt_count(value):
    return MISSING if value is None else "{:,}".format(int(value))


def to_mib(value):
    return None if value is None else value / MIB


def wall_seconds(start_iso, end_iso):
    """Seconds between two RFC 3339 snapshot timestamps, or None."""
    if not isinstance(start_iso, str) or not isinstance(end_iso, str):
        return None
    try:
        start = datetime.fromisoformat(start_iso)
        end = datetime.fromisoformat(end_iso)
    except ValueError:
        return None
    return (end - start).total_seconds()


def plateau_seconds(record):
    """The monotonic plateau duration the report's rates were divided by."""
    requests = number(record, "plateau_requests")
    rps = number(record, "achieved_rps")
    if requests is None or rps is None or rps <= 0:
        return None
    return requests / rps


def relative_gap_pct(a, b):
    """Symmetric percentage gap between two values, or None if undefined."""
    if a is None or b is None:
        return None
    scale = max(abs(a), abs(b))
    if scale == 0.0:
        return 0.0
    return abs(a - b) / scale * 100.0


#: (heading, extractor, formatter) for each compared metric.
METRICS = [
    (
        "CPU (millicores)",
        lambda r: number(r, "cpu_millicores"),
        fmt_1dp,
    ),
    (
        "RSS avg (MiB)",
        lambda r: to_mib(number(r, "rss_avg_bytes")),
        fmt_1dp,
    ),
    (
        "RSS peak (MiB)",
        lambda r: to_mib(number(r, "rss_peak_bytes")),
        fmt_1dp,
    ),
    (
        "Allocations per request",
        lambda r: number(r, "allocs_per_req"),
        fmt_1dp,
    ),
    (
        "Bytes allocated per request",
        lambda r: number(r, "alloc_bytes_per_req"),
        fmt_thousands,
    ),
]


# ---------------------------------------------------------------------------
# Loading
# ---------------------------------------------------------------------------


def load_reports(directory):
    """Read every ``*.json`` file in *directory*.

    Returns ``(runs, load_errors, extra_files, unreadable)`` where ``runs``
    maps ``(regime, arm)`` to the parsed report, ``load_errors`` maps a file
    name to the reason it could not be used, ``extra_files`` lists file names
    that do not follow the ``<regime>-<arm>.json`` convention, and
    ``unreadable`` is the set of ``(regime, arm)`` keys whose file exists but
    could not be parsed.
    """
    runs = {}
    load_errors = {}
    extra_files = []
    unreadable = set()
    known_regimes = {key for key, _ in REGIMES}

    for path in sorted(directory.glob("*.json")):
        if path.name == OUTPUT_NAME:
            continue
        stem = path.stem
        regime, _, arm = stem.rpartition("-")
        recognized = regime in known_regimes and arm in ARMS

        try:
            with path.open("r", encoding="utf-8") as handle:
                data = json.load(handle)
        except (OSError, ValueError) as exc:
            load_errors[path.name] = str(exc)
            if recognized:
                unreadable.add((regime, arm))
            continue

        if not isinstance(data, dict):
            load_errors[path.name] = "top-level JSON value is not an object"
            if recognized:
                unreadable.add((regime, arm))
            continue

        if recognized:
            runs[(regime, arm)] = data
        else:
            extra_files.append(path.name)

    return runs, load_errors, extra_files, unreadable


# ---------------------------------------------------------------------------
# Metric tables
# ---------------------------------------------------------------------------


def delta_and_verdict(poseidon_value, standard_value, heading=""):
    """Return ``(delta_text, verdict_text)`` for one lower-is-better pair."""
    if poseidon_value is None or standard_value is None:
        return MISSING, MISSING
    if standard_value == 0.0:
        # No meaningful ratio against a zero baseline.
        return MISSING, MISSING

    delta = (poseidon_value - standard_value) / standard_value * 100.0
    text = "{:+.1f}%".format(delta)
    floor = NOISE_FLOOR_PCT.get(heading, EQUAL_BAND_PCT)
    if abs(delta) < EQUAL_BAND_PCT:
        verdict = "~equal"
    elif abs(delta) < floor:
        # Larger than a tie but smaller than this metric can resolve.
        verdict = BELOW_FLOOR
    elif delta < 0:
        verdict = "poseidon better"
    else:
        verdict = "standard better"
    return text, verdict


def render_metric_table(heading, extract, fmt, runs):
    lines = [
        "## {}".format(heading),
        "",
        "| Regime | poseidon | standard | Δ | verdict |",
        "| --- | ---: | ---: | ---: | --- |",
    ]
    for regime, label in REGIMES:
        poseidon = extract(runs.get((regime, "poseidon")))
        standard = extract(runs.get((regime, "standard")))
        delta, verdict = delta_and_verdict(poseidon, standard, heading)
        lines.append(
            "| {} | {} | {} | {} | {} |".format(
                label, fmt(poseidon), fmt(standard), delta, verdict
            )
        )
    lines.append("")
    return lines


# ---------------------------------------------------------------------------
# Validity
# ---------------------------------------------------------------------------


def validity_findings(runs, load_errors, extra_files, unreadable):
    """Collect every reason the comparison might not be trustworthy."""
    findings = []

    # Files we could not read at all.
    for name in sorted(load_errors):
        findings.append("Could not read `{}`: {}".format(name, load_errors[name]))

    # Files present but not part of the expected matrix.
    for name in extra_files:
        findings.append(
            "Unrecognized file `{}` (expected `<regime>-<arm>.json`); ignored.".format(
                name
            )
        )

    # Missing (or unusable) regime/arm combinations.
    for regime, label in REGIMES:
        for arm in ARMS:
            if (regime, arm) in runs:
                continue
            state = "Unusable" if (regime, arm) in unreadable else "Missing"
            findings.append(
                "{} result file `{}-{}.json` ({} / {}) - "
                "that regime cannot be compared.".format(
                    state, regime, arm, label, arm
                )
            )

    # Runs that reported hard errors during the plateau.
    for regime, label in REGIMES:
        for arm in ARMS:
            record = runs.get((regime, arm))
            if record is None:
                continue
            errors = number(record, "plateau_errors")
            if errors is not None and errors > 0:
                message = "{} / {}: {} plateau error(s)".format(
                    label, arm, fmt_count(errors)
                )
                sample = record.get("sample_error")
                if isinstance(sample, str) and sample.strip():
                    message += " - sample: `{}`".format(sample.strip())
                findings.append(message)

    # Response body volume consumed per arm.
    #
    # This is the mechanical guard for the failure mode this project has hit
    # repeatedly: the harness doing unequal work in the two arms while every
    # request count matches. It would have caught both the
    # buffered-vs-discarded body bug and the fixed-size fetch regression, each
    # of which was invisible to request counts and found only because a number
    # looked implausible.
    for regime, label in REGIMES:
        poseidon = runs.get((regime, "poseidon"))
        standard = runs.get((regime, "standard"))
        if poseidon is None or standard is None:
            continue
        p_body = number(poseidon, "plateau_body_bytes")
        s_body = number(standard, "plateau_body_bytes")
        if p_body is None or s_body is None:
            continue
        body_gap = relative_gap_pct(p_body, s_body)
        if body_gap is not None and body_gap > BODY_TOLERANCE_PCT:
            findings.append(
                "{}: plateau response body volume differs by {:.2f}% "
                "(poseidon {}, standard {} bytes) - the two arms did not "
                "consume the same work, so per-request figures are not "
                "comparable.".format(
                    label, body_gap, fmt_count(p_body), fmt_count(s_body)
                )
            )

    # Hosts that lost wall-clock time during a measured window.
    #
    # Every rate-derived figure divides by the monotonic plateau duration. If
    # the wall clock advanced materially further than the monotonic clock, the
    # measuring host was paused or clock-stepped mid-window, and CPU
    # millicores from that cell are not trustworthy.
    for regime, label in REGIMES:
        for arm in ARMS:
            record = runs.get((regime, arm))
            if record is None:
                continue
            start = record.get("plateau_start_snapshot")
            end = record.get("plateau_end_snapshot")
            if not isinstance(start, dict) or not isinstance(end, dict):
                continue
            wall = wall_seconds(start.get("at"), end.get("at"))
            declared = number(record, "cpu_busy_seconds")
            monotonic = plateau_seconds(record)
            if wall is None or monotonic is None or monotonic <= 0:
                continue
            drift = (wall - monotonic) / monotonic * 100.0
            if drift > CLOCK_DRIFT_PCT:
                findings.append(
                    "{} / {}: wall clock advanced {:.1f}% further than the "
                    "monotonic plateau ({:.2f}s vs {:.2f}s) - the measuring "
                    "host lost time mid-window, so CPU figures for this cell "
                    "are unreliable.".format(
                        label, arm, drift, wall, monotonic
                    )
                )
            _ = declared

    # Achieved rate against the requested rate.
    for regime, label in REGIMES:
        for arm in ARMS:
            record = runs.get((regime, arm))
            if record is None:
                continue
            achieved = number(record, "achieved_rps")
            profile = record.get("profile")
            target = number(profile, "target_rps") if isinstance(profile, dict) else None
            if achieved is None or target is None or target <= 0:
                continue
            off = abs(achieved - target) / target * 100.0
            if off > PAIR_TOLERANCE_PCT:
                findings.append(
                    "{} / {}: achieved {:.1f} RPS against a target of {:.0f} "
                    "({:+.1f}%) - the run did not deliver the intended load, "
                    "so the figures describe a different rate than stated."
                    .format(label, arm, achieved, target, achieved - target)
                )

    # Arms that did unequal amounts of work, or ran at different rates.
    for regime, label in REGIMES:
        poseidon = runs.get((regime, "poseidon"))
        standard = runs.get((regime, "standard"))
        if poseidon is None or standard is None:
            continue

        p_requests = number(poseidon, "plateau_requests")
        s_requests = number(standard, "plateau_requests")
        gap = relative_gap_pct(p_requests, s_requests)
        if gap is None:
            findings.append(
                "{}: `plateau_requests` missing from one or both arms - "
                "per-request figures cannot be validated.".format(label)
            )
        elif gap > PAIR_TOLERANCE_PCT:
            findings.append(
                "{}: plateau_requests differ by {:.1f}% "
                "(poseidon {}, standard {}) - unequal work invalidates the "
                "per-request comparison.".format(
                    label, gap, fmt_count(p_requests), fmt_count(s_requests)
                )
            )

        p_rps = number(poseidon, "achieved_rps")
        s_rps = number(standard, "achieved_rps")
        rps_gap = relative_gap_pct(p_rps, s_rps)
        if rps_gap is None:
            findings.append(
                "{}: `achieved_rps` missing from one or both arms - "
                "cannot confirm the arms ran at the same load.".format(label)
            )
        elif rps_gap > PAIR_TOLERANCE_PCT:
            findings.append(
                "{}: achieved_rps diverges by {:.1f}% "
                "(poseidon {}, standard {}) - the arms were not held at the "
                "same offered load.".format(
                    label, rps_gap, fmt_1dp(p_rps), fmt_1dp(s_rps)
                )
            )

    return findings


def render_validity(findings):
    lines = ["## Validity", ""]
    if findings:
        lines.extend("- {}".format(item) for item in findings)
    else:
        lines.append("- No validity problems detected.")
    lines.append("")
    return lines


# ---------------------------------------------------------------------------
# Raw table
# ---------------------------------------------------------------------------


def render_raw(runs):
    lines = [
        "## Raw",
        "",
        "| File | Requests | Errors | Non-2xx | Achieved RPS |",
        "| --- | ---: | ---: | ---: | ---: |",
    ]
    present = False
    for regime, _label in REGIMES:
        for arm in ARMS:
            record = runs.get((regime, arm))
            if record is None:
                continue
            present = True
            lines.append(
                "| `{}-{}.json` | {} | {} | {} | {} |".format(
                    regime,
                    arm,
                    fmt_count(number(record, "plateau_requests")),
                    fmt_count(number(record, "plateau_errors")),
                    fmt_count(number(record, "plateau_non_2xx")),
                    fmt_1dp(number(record, "achieved_rps")),
                )
            )
    if not present:
        lines.append("| _no readable reports found_ | | | | |")
    lines.append("")
    return lines


# ---------------------------------------------------------------------------
# Document assembly
# ---------------------------------------------------------------------------


def build_document(directory, runs, load_errors, extra_files, unreadable):
    lines = [
        "# poseidon-http-client vs standard clients",
        "",
        "All figures are measured over the plateau window only; lower is better "
        "for every metric, so a negative Δ means poseidon won.",
        "",
        "Source directory: `{}`".format(directory.as_posix()),
        "",
    ]

    for heading, extract, fmt in METRICS:
        lines.extend(render_metric_table(heading, extract, fmt, runs))

    lines.extend(
        render_validity(
            validity_findings(runs, load_errors, extra_files, unreadable)
        )
    )
    lines.extend(render_raw(runs))

    return "\n".join(lines).rstrip() + "\n"


def main(argv):
    # Markdown output contains a Δ; make sure a non-UTF-8 console cannot kill us.
    try:
        sys.stdout.reconfigure(encoding="utf-8")
    except (AttributeError, OSError, ValueError):  # pragma: no cover
        pass

    directory = Path(argv[1] if len(argv) > 1 else DEFAULT_DIR)
    if not directory.is_dir():
        sys.stderr.write(
            "error: results directory not found: {}\n".format(directory)
        )
        return 2

    runs, load_errors, extra_files, unreadable = load_reports(directory)
    document = build_document(directory, runs, load_errors, extra_files, unreadable)

    sys.stdout.write(document)

    output_path = directory / OUTPUT_NAME
    try:
        output_path.write_text(document, encoding="utf-8")
    except OSError as exc:
        sys.stderr.write("warning: could not write {}: {}\n".format(output_path, exc))
        return 1

    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
