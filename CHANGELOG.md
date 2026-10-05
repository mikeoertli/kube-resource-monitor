# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.2.1] - Work in progress

### Changed

- Replaced storage letter markers with two percentage bars: used/request and
  used/limit or PVC capacity. Missing bounds show n/a tracks rather than a full
  bar; missing usage shows unknown.

### Fixed

- Storage defaults to use% descending and cycles only use%, kind, request, and
  used sorts. Unknown values stay last in either direction. CPU/memory sorts
  are rejected for storage.

## [1.2.0] - 2026-10-05

### Added

- `krm storage` combines ephemeral pod budgets, disk-backed `emptyDir` volumes,
  and PVCs in one view, distinguished by KIND. `--type ephemeral` and `--type pvc`
  filter the same view; all types appear by default. Uses one shared kubelet
  scrape per refresh, defaults to one-minute refreshes, and supports top, watch,
  filtering, notifications via grouping, and exports.
- Storage bars place request, usage and limit/capacity markers on one scale.
- Storage request/limit fields in JSON, CSV and Prometheus exports. CSV appends
  reject older column layouts instead of mixing schemas.

### Changed

- Storage has one command and TUI grouping. Existing `-g volume` and `-g pvc`
  shorthands select the corresponding storage type for compatibility.
- Storage views use kubelet summaries independently of metrics-server. Missing
  and partially unavailable samples remain explicit.

## [1.1.0] - 2026-10-05

### Added

- `krm node` (alias `nodes`) opens node CPU/memory monitoring, with `node top`
  and `node watch` for explicit modes. Uses allocatable capacity, covers all
  namespaces, and supports filtering and machine-readable/CSV export.

## [1.0.0] - 2026-10-05

### Added

- TUI menu actions briefly highlight in bright pink with black text when pressed,
  making keyboard interactions easier to follow.
- VHS demo tape and README animation showcasing synthetic metrics, live controls,
  resource grouping, alerts, and CSV export, with highlighted TUI menu actions.
  Regenerate with `make demo-gif`.

### Changed

- Promoted krm to version 1.0.0.

## [0.3.0] - 2026-10-01

### Changed

- Updated the Go module path, imports, build configuration, and repository links
  to `github.com/mikeoertli/kube-resource-monitor` following the repository rename
  (previousy used underscores: `kube_resource_monitor`).


## [0.2.0] - 2026-09-16

### Added

- `--csv <output-dir>` appends timestamped samples to one CSV per resource in
  snapshot, watch, and notify modes, preserving the normal terminal output.
  Filenames use lowercase resource types and underscores for spaces and slashes.
  CSV columns start with the UTC sample timestamp, followed by resource identity,
  status, usage, requests, limits, percentages, and missing-metrics status.
- `VERSION` tracks the development version and supplies the version for Makefile
  builds. Changes belong under its matching `Work in progress` heading; a version
  bump dates the previous heading and starts a new work-in-progress section.

## [0.1.0] - 2026-08-19

First working version.

### Added

- **Live view.** A full-screen terminal UI that refreshes on an interval, with
  expandable rows, live filtering, sorting, and regrouping without restarting.
  `p` pauses, `+`/`-` change the refresh rate, `?` lists every key.
- **Workload rollup.** Usage summed to the Deployment, StatefulSet, DaemonSet,
  Job, or CronJob that owns a pod, resolved through the intermediate ReplicaSet
  or Job, with per-pod and per-container breakdowns underneath.
- **Grouping** by workload, pod, container, node, namespace, or
  PersistentVolumeClaim, plus kind-restricted views (`-g deployment`,
  `-g statefulset`, `-g daemonset`, `-g job`). Kubectl abbreviations accepted.
- **Requests and limits** shown alongside usage (`--requests`, `--limits`), with
  percentages measured against the limit, falling back to the request and then
  to node allocatable.
- **Color by headroom**, using a severity scale from idle through over-limit.
  Honors `NO_COLOR` and detects whether stdout is a terminal.
- **Volume usage** via `-g pvc`, read from each kubelet's summary endpoint, with
  provisioned capacity from the PVC when node proxy access is unavailable.
- **Notify mode** with threshold rules in relative (`cpu>85%`,
  `mem>90% of request`) and absolute (`cpu>1500m`, `mem>2Gi`) forms, hysteresis
  to stop alerts flapping, `--for` to require a breach to persist, `--repeat` to
  re-notify, and desktop delivery through terminal-notifier, osascript, or
  notify-send, falling back to stdout.
- **Machine-readable output**: `-o json`, `-o csv`, `-o prometheus`.
- **Filtering** by label selector, field selector, and name (regular expression,
  or case-insensitive substring when the pattern is not valid regex).
- **metrics-server detection and install** via `krm install-metrics-server`,
  which distinguishes "not installed" from "installed but not serving" and does
  not advise reinstalling over a deployment that is already there.
- **Context and namespace selection** following kubectl's resolution order, with
  `krm contexts` to list what is available.
- **`--demo` mode** running against a synthetic cluster, so the tool can be
  evaluated, demonstrated, and debugged without one.
- **Brand assets** under `assets/`: an icon, a hero logo, single-color variants,
  a multi-resolution `favicon.ico`, and a macOS `.icns`.

### Notes on correctness

These are choices that differ from a naive implementation, recorded because
they change the numbers you see:

- Pod requests and limits follow the scheduler's rules, not a plain sum over
  containers: init containers contribute their maximum rather than their sum,
  restartable sidecars add to the steady state, and pod overhead is included.
- Node rows measure against allocatable rather than the summed limits of their
  pods, which routinely exceed what the machine has, and prefer node-level
  metrics that include the kubelet and system daemons.
- A pod with no metrics sample renders as `-`, never as `0m`. A percentage with
  no denominator renders as `-`, never as `0%`.

[0.2.0]: https://github.com/mikeoertli/kube-resource-monitor/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/mikeoertli/kube-resource-monitor/releases/tag/v0.1.0
