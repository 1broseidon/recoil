# Incident response

This runbook is for engineers on call when Pelican is misbehaving in
production. The goal is to stabilize quickly and write down what
happened so the next person on call has a better baseline than you did.

## Step 1: confirm scope

Before changing anything, confirm what is actually broken. Check the
platform dashboard. Check whether the failure is ingestion, storage,
transformation, or serving. A 500 from the serving layer can be caused
by any of the four; do not start fixing the serving layer until you know
the failure is there.

## Step 2: stabilize

If the ingestion service is failing, slow or pause the consumer offset
advancement and check the dead letter topic. If the serving layer is
failing, fail over to the read replica. If Postgres is the problem,
page the database operator on duty before doing anything destructive.

## Step 3: communicate

Post in the incident channel every fifteen minutes whether or not
anything has changed. Silence is the worst signal you can send.

## Step 4: write it down

When the incident is over, write a brief postmortem within 48 hours.
This is not optional. The postmortem template lives in the team wiki.
