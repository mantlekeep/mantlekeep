-- MantleKeep governed policy, held in Postgres.
--
-- Shipped as a file and applied by an operator, NOT generated at runtime. A schema a process
-- creates for itself is a schema nobody reviewed, that no migration tool knows about, and
-- that needs the application to hold DDL rights on its own policy store for the life of the
-- deployment. This file is the artefact: read it, diff it, run it through whatever migration
-- tool the deployment already has.
--
-- Names are unqualified so the deployment chooses the schema (SET search_path, or run this
-- inside a CREATE SCHEMA). Everything is prefixed mantlekeep_ so it is obvious in a database
-- that holds other things too.
--
-- Nothing here is seeded. A deployment with the tables created and no policy row is an ERROR
-- (pgpolicy.ErrNoPolicy), not an empty policy, because empty grants deny everything and
-- "schema applied, policy never loaded" would otherwise present as a working deny-all. See
-- README.md for the seeding step; it is deliberately a separate, explicit act.

-- ---------------------------------------------------------------------------------------
-- The policy in force. Exactly one row, ever.
-- ---------------------------------------------------------------------------------------
--
-- One row rather than one-row-per-version-with-a-flag: "which policy is in force" must have
-- exactly one answer, and a boolean is_current column has as many answers as there are rows
-- somebody forgot to clear. The history below is where versions live.
--
-- To read it by hand, see queries.sql beside this file — the statements live there as real,
-- runnable SQL rather than as text in a comment, so they cannot quietly rot when a column
-- is renamed.
CREATE TABLE IF NOT EXISTS mantlekeep_policy_head (
    -- The single-row guard. A boolean primary key that must be true admits exactly one row,
    -- enforced by the database rather than by every writer remembering to.
    id          boolean     PRIMARY KEY DEFAULT true CHECK (id),

    -- The two documents that govern together: role→action grants, and the per-action
    -- attribute floor.
    --
    -- jsonb, not text. It refuses a document that is not JSON at INSERT time rather than at
    -- load time, and it lets an operator query the policy directly
    -- (grants_doc -> 'role_actions' -> 'L2-Operator'). Its normalisation — key order,
    -- whitespace — cannot change the revision, because the revision is derived from the
    -- DECODED document, not from the bytes in the column.
    grants_doc  jsonb       NOT NULL,
    floors_doc  jsonb       NOT NULL,

    -- The revision the documents carried when they were written.
    --
    -- DERIVED, never authoritative. What identifies this policy is the hash of its content,
    -- recomputed on every load (grants.RevisionOf); this column is a denormalised copy that
    -- earns its place twice: an operator reads it to see whether two replicas are serving the
    -- same policy, and it is the predicate the compare-and-set write turns on. A governed
    -- write rewrites it in the same transaction as the documents, so the two can only
    -- disagree after an edit made outside the door — and the next governed write puts it
    -- right rather than refusing to proceed.
    revision    text        NOT NULL,

    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------------------------------
-- Every change ever applied. Append-only.
-- ---------------------------------------------------------------------------------------
--
-- Worth its cost, decided deliberately: a store that holds only the current state can answer
-- what the policy IS. An audit asks what it SAID on the fourteenth — and the thing an auditor
-- is actually hunting is the permission that existed for three days and was quietly removed,
-- which a current-state-only store cannot show at all. One row per change buys that.
--
-- WHO made the change is deliberately not here. The actor, the decision and the
-- before-revision are already on the hash chain, recorded by the door under the
-- "policy.grant" action before this row was written. A second, unsigned copy of "who did
-- this" in an application table could disagree with the signed one, and an audit trail with
-- two answers is worse than one with a join. Join on revision.
--
-- To read the history, or to ask what the policy said at a point in time, see queries.sql
-- beside this file.
CREATE TABLE IF NOT EXISTS mantlekeep_policy_history (
    -- The order changes landed in. A revision cannot order the history: it is content-derived,
    -- so a grant and its later revoke return to a revision that was already used, on purpose.
    seq             bigserial   PRIMARY KEY,

    applied_at      timestamptz NOT NULL DEFAULT now(),

    -- The revision this change was applied TO, and the one it produced. Together they make
    -- the history a chain an auditor can walk, and they are what the chain's own record of
    -- the decision refers to.
    parent_revision text        NOT NULL,
    revision        text        NOT NULL,

    -- The whole policy as of this change, not a diff. A diff-only history cannot answer "what
    -- did it say on the fourteenth" without replaying every row correctly, and a replay is a
    -- second implementation of the writer that can drift from the first.
    grants_doc      jsonb       NOT NULL,
    floors_doc      jsonb       NOT NULL,

    -- The change itself, in the same four fields grants.Change carries. The reason is
    -- required by the core before the door ever sees the change; it is the only part of the
    -- record that still means something to whoever reads it a year later.
    change_role     text        NOT NULL,
    change_action   text        NOT NULL,
    change_grant    boolean     NOT NULL,
    change_reason   text        NOT NULL

    -- The GENESIS row — the one written when the policy was first seeded — carries an empty
    -- change_role and change_action, because seeding is not a change to anybody's authority;
    -- its change_reason says where the policy came from. Without it a point-in-time question
    -- about a date before the first change would have nothing to answer with.
);

-- Point-in-time lookup: the ORDER BY seq DESC LIMIT 1 above, made cheap.
CREATE INDEX IF NOT EXISTS mantlekeep_policy_history_applied_at_idx
    ON mantlekeep_policy_history (applied_at, seq);

-- "Where did this revision come from" — the lookup an auditor makes from a chain record,
-- which carries the revision and not the sequence number.
CREATE INDEX IF NOT EXISTS mantlekeep_policy_history_revision_idx
    ON mantlekeep_policy_history (revision);
