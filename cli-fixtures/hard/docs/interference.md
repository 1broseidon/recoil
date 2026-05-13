# Interference Corpus

This file is intentionally noisy. It contains plausible project notes, personal
assistant chatter, and overlapping entities that should not outrank the durable
facts from the focused transcripts.

## SQLite and Storage Alternatives

Several older experiments used phrases that look close to the current storage
decision. One prototype considered modernc.org/sqlite because a pure Go driver
felt attractive for cross-compilation and simple local setup. Another prototype
considered a hosted sync service with a small background daemon that could push
memories to a remote profile. Those alternatives were research notes, not the
current product direction.

The current implementation should still be judged by the direct product memory:
local SQLite, no hosted service requirement, no always-on daemon, and FTS5
enabled through the chosen SQLite driver. Any note about a server, queue, sync
worker, remote vector store, or browser extension cache is only background
discussion unless it is stored as an active decision.

Past notes also mention a WASM SQLite bundle, an in-browser cache, a disposable
test database, and a local file watcher. Those details overlap with search
terms such as SQLite, FTS5, search ranking, local-first, current state, daemon,
service, and hosted. They are not the answer when the question asks what Recoil
should do now.

## Fake Medical and Appointment Noise

The following examples are training-style distractors. They should not be used
as personal facts for the user.

Dr. Lane in a fictional case study referred a patient to a dermatologist after a
rash. A sample ENT handout says chronic sinusitis may involve saline rinses,
nasal sprays, or allergy review. Another article says a primary care doctor may
prescribe antibiotics for a urinary tract infection. A hospital brochure
mentions biopsy follow-ups, dermatology, ENT visits, and imaging reports.

Those examples share words with real personal memories, including doctor,
appointment, biopsy, dermatologist, antibiotics, urinary tract infection,
sinusitis, nasal spray, ENT, physician, specialist, and care provider. They are
not user history. They are general content mined from documents.

There is also a project contact named Lee who reviewed a search UI, a Smith
Street clinic mentioned in an address fixture, and a Patel Hall room from a
campus map. Those are entity collisions. They should not be treated as the
doctor facts unless the transcript itself connects Dr. Lee, Dr. Smith, or
Dr. Patel to the user.

## Family, Club, and Demographic Noise

Example datasets for counting tasks often include siblings, book clubs, class
rosters, and demographic breakdowns. One canned example says a sample club has
12 women and 2 men. Another says a reading circle has 6 women, 6 men, and no
non-binary members. A third says a family has two brothers and no sisters.

Those are not the user's facts. They are deliberately close to the real facts
about siblings and the weekly book club. The retrieval system should prefer the
user's transcript when asked about personal family or club composition, even
though the words siblings, sisters, brother, book club, women, men, member, and
non-binary all appear repeatedly here.

## Education, Career, and Certification Noise

A resume-template example says one candidate graduated from Stanford at age 22,
then became a growth analyst. Another says a learner is currently 29 and wants
the Google Analytics certificate first. A third says a Digital Marketing
Specialist should compare Coursera, HubSpot, Google Skillshop, Meta Blueprint,
and LinkedIn Learning before choosing.

Those examples are deliberately similar to actual profile facts. They should
not override the user-specific transcript about the user's current age, college,
graduation timing, job title, and preferred marketing certification sequence.

## Herbs, Cooking, Purchases, and Travel Noise

Some cooking notes mention parsley, cilantro, rosemary, dill, thyme, chives,
and oregano. One note says parsley is not useful for dinner recipes this week,
while another says mint can be used in tea and basil can be used in pesto. A
shopping article lists a toaster, blender, air fryer, smoker, coffee maker, and
stand mixer as kitchen appliances.

Travel accessory examples mention battery packs, wall chargers, USB-C cables,
noise-canceling headphones, wireless charging pads, travel routers, and plug
adapters. Those examples are general buying advice, not proof that the user
bought anything. When the query asks what the user bought or what accessories
the user wanted, transcript evidence should beat this general article text.

## CLI Evaluation Noise

Older notes used a benchmark harness with synthetic labels, fixture cases,
LongMemEval-style scoring, deterministic answer matching, and generated summary
JSON. The current validation direction is different: use the actual Recoil CLI,
ingest real-looking directories and transcripts, and score whether search and
wake surface the right memories through product commands. The product commands
of interest include init, add, mine, session-evidence ingest, search --json, and
wake --json. Benchmark-only helper functions are background context, not the
thing being validated here.

## Repeated Lexical Distractors

SQLite FTS5 current current current driver driver driver hosted hosted daemon.
Doctor appointment biopsy dermatologist antibiotics ENT sinusitis nasal spray.
Lee Smith Patel campus street contact clinic provider physician specialist.
Siblings sisters brother book club women men non-binary count demographics.
Graduated college university age current marketing certification HubSpot
Coursera specialist. Basil mint parsley herbs dinner recipes cooking garden.
Portable power battery charging wireless pad accessories travel. Smoker hickory
apple wood BBQ appliance purchase bought kitchen.

The repeated line above is intentionally unpleasant. It should raise lexical
pressure without becoming the answer for personal, current-state, or role-filter
queries.
