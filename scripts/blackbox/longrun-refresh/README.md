# Long-running cognition refresh black box

This runner starts the candidate AOCI binary as a real stdio MCP server and
creates two isolated Git repositories under a temporary directory. It never
connects to an AI endpoint or a production repository.

```bash
go run ./scripts/blackbox/longrun-refresh \
  --binary ./build/aoci \
  --output /tmp/aoci-code-longrun-refresh-report.json
```

The threshold experiment proves 29 versus 30 deduplicated semantic paths,
compact check_only behavior, complete-but-unreliable Dirty delivery, repeated
ordinary full delivery, local recall, and final Verify/Check/Guide alignment.
The second experiment labels compaction and the user question as simulated Host
events. It proves an initial strict 1/1 Attestation, one complete compaction
refresh with a test-only failed 0/1 Attestation derived solely from the delivered
body, disabled full-system claims, no same-generation Overview loop, a
source-bound Entry read after the question, dirty deferral, three-reason
merging, one deliberately injected `repair_required` candidate correction, and
final Verify/Check/Guide alignment.

The JSON report records the frozen subject commit, binary identity, tool count,
Rules/Overview/Maintain/local-recall counts, semantic checkpoints, reasons,
index digests, Attestation/continuation facts, repair attempts, output hashes,
token estimates, deterministic time, and wall time. Temporary repositories are
deleted unless `--keep` is supplied. The runner remains a test harness and does
not create a product Task Session or alternate recovery path.
