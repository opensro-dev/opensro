/*
===========================================================================

sqlitecol.go - the one additive column migration every SQLite owner shares

SQLite has no ADD COLUMN IF NOT EXISTS, so each owner that grew a table
after its first release (the character store's ledger and market, the
history journal, the account database, the connection journal) probes
the table's shape and adds what a file lacks. They all do it here.

The rule every caller relies on: a column is added with a default, never
by bumping a version stamp. An older binary on the same file never
notices it - its inserts name their columns - so a rollback keeps
opening the file it left behind.

===========================================================================
*/
package sqlitecol

import (
	"database/sql"
	"fmt"
)

/*
================
Column

One column a later release added: the table it belongs to, its name,
and the definition ALTER TABLE gives it (type and default).
================
*/
type Column struct {
	Table      string
	Name       string
	Definition string
}

/*
================
Ensure

Adds every column a file lacks, in order. A column already present is
left as it is, whatever its definition: the first release that added it
owns its shape.
================
*/
func Ensure(db *sql.DB, columns []Column) error {
	for _, column := range columns {
		present, err := Has(db, column.Table, column.Name)
		if err != nil {
			return fmt.Errorf("inspect %s: %w", column.Table, err)
		}
		if present {
			continue
		}
		if _, err := db.Exec("ALTER TABLE " + column.Table + " ADD COLUMN " + column.Name + " " + column.Definition); err != nil {
			return fmt.Errorf("add %s.%s: %w", column.Table, column.Name, err)
		}
	}
	return nil
}

/*
================
Has

Whether the table has the column. A table that does not exist has no
columns, so it reads false, not an error; Ensure's ALTER then names it.
================
*/
func Has(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query("SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}
