# Minimal AOCI Subject Repository

This tiny Go repository is an onboarding subject, not a benchmark result. Copy the directory outside the AOCI-CODE checkout, initialize Git, and follow [`docs/getting-started.md`](../../docs/getting-started.md).

It intentionally contains no generated `aoci.txt` or `.aoci` state. With the
recommended `--cognition project` mode, `aoci init` leaves formal cognition and
the Baseline absent while Fresh Bootstrap asks the active model to author and
Root-last publish the project-specific Root, Meta tags, and Entries. Compatible
no-mode init or explicit `--cognition generic` instead creates the fixed starter
skeleton before `scan`.
