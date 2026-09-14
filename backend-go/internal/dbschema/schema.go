package dbschema

import _ "embed"

//go:embed schema.sql
var SchemaSQL string

//go:embed taskplan.sql
var TaskPlanMigrationSQL string
