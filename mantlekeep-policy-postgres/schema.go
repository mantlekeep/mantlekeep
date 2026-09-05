package pgpolicy

// Blank import: the //go:embed directive below needs the embed package linked in, but this
// file references no embed identifier of its own.
import _ "embed"

// Schema is the DDL this adapter expects, exactly as shipped in schema.sql.
//
// Exposed so a deployment can apply it through whatever migration tool it already uses, and
// so a test can create the tables it asserts against from the SAME text an operator runs —
// a test that built its own tables would be proving the SQL it invented, not the SQL that
// ships.
//
// This package never executes it on its own initiative. Creating a schema at startup means
// the application holds DDL rights on its own policy store forever, and means the shape of
// the policy tables changes without anybody reviewing a migration.
//
//go:embed schema.sql
var Schema string
