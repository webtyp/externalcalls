package migrate

import (
	"webtyp.com/ddl"
	"webtyp.com/externalcalls"
)

// Migrate creates the queued_call table or adds the columns a newer model gained.
//
// Example: err := migrate.Migrate(conn, compiler)
func Migrate(conn ddl.Execer, compiler ddl.Compiler) error {
	return ddl.New(conn, compiler).Sync(&externalcalls.QueuedCall{})
}
