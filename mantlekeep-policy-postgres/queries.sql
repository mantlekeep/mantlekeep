-- Reading the policy store by hand.
--
-- These are real, runnable statements rather than examples pasted into a comment in
-- schema.sql: a query nobody can execute goes stale the first time a column is renamed, and
-- nothing fails. Keep them working — they are the operator's view of the store, and the door
-- offers no other read path into it.

-- WHAT IS IN FORCE RIGHT NOW.
-- One row, always. The revision is derived from the documents' own content, so the same policy
-- has the same revision whatever storage holds it — a file-backed and a database-backed
-- deployment showing the same revision are provably serving the same law.
SELECT revision, updated_at, jsonb_pretty(grants_doc)
  FROM mantlekeep_policy_head;

-- WHAT CHANGED, IN ORDER.
-- Append-only. Note there is no "who": that lives on the hash chain under the policy.grant
-- action, and a second unsigned copy here could disagree with the signed one. Join on revision
-- when you need to put a name to a change.
SELECT applied_at, change_role, change_action, change_grant, change_reason
  FROM mantlekeep_policy_history
 ORDER BY seq ASC;

-- WHAT DID THE POLICY SAY AT A POINT IN TIME?
-- The question an auditor asks. Replace the timestamp.
SELECT revision, jsonb_pretty(grants_doc)
  FROM mantlekeep_policy_history
 WHERE applied_at <= TIMESTAMPTZ '2026-08-14 23:59:59+00'
 ORDER BY seq DESC
 LIMIT 1;
