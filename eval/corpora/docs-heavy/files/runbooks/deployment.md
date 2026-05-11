# Deployment

This runbook covers how to ship a new version of any Pelican service.
The steps are the same for every service in the platform.

## Before you start

- Your change must be merged to main.
- CI must be green on the merge commit.
- You must have deployed to staging at least once today.

## Procedure

1. Tag the release with the date-prefixed version string. The release
   tooling will reject tags that do not match the expected format.
2. Run the release pipeline against staging. Wait for the smoke tests
   to pass.
3. Run the release pipeline against production. The pipeline does a
   canary rollout to one instance, waits ten minutes, then rolls out
   to the rest of the fleet if no alerts fire.
4. Watch the platform dashboard for fifteen minutes after the rollout
   completes. If anything looks off, roll back; do not try to forward
   fix during the watch window.

## Rolling back

The release pipeline has a rollback command. Use it. Do not attempt to
revert by hand.
