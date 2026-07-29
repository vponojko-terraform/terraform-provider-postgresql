package postgresql

import (
	"bytes"
	"database/sql"
	"fmt"
	"log"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/lib/pq"
)

const (
	foreignTableNameAttr        = "name"
	foreignTableSchemaAttr      = "schema"
	foreignTableDatabaseAttr    = "database"
	foreignTableServerNameAttr  = "server_name"
	foreignTableOptionsAttr     = "options"
	foreignTableColumnAttr      = "column"
	foreignTableDropCascadeAttr = "drop_cascade"

	foreignTableColumnNameAttr    = "name"
	foreignTableColumnTypeAttr    = "type"
	foreignTableColumnCollateAttr = "collate"
	foreignTableColumnNotNullAttr = "not_null"
	foreignTableColumnOptionsAttr = "options"
)

func resourcePostgreSQLForeignTable() *schema.Resource {
	return &schema.Resource{
		Create: PGResourceFunc(resourcePostgreSQLForeignTableCreate),
		Read:   PGResourceFunc(resourcePostgreSQLForeignTableRead),
		Update: PGResourceFunc(resourcePostgreSQLForeignTableUpdate),
		Delete: PGResourceFunc(resourcePostgreSQLForeignTableDelete),
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			foreignTableNameAttr: {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The name of the foreign table to be created",
			},
			foreignTableSchemaAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Default:     "public",
				Description: "The schema in which the foreign table is created",
			},
			foreignTableDatabaseAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				ForceNew:    true,
				Description: "The database in which the foreign table is created (defaults to the provider database)",
			},
			foreignTableServerNameAttr: {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The name of an existing foreign server to use for the foreign table",
			},
			foreignTableOptionsAttr: {
				Type: schema.TypeMap,
				Elem: &schema.Schema{
					Type: schema.TypeString,
				},
				Optional:    true,
				Description: "Options for the foreign table. The allowed option names and values are specific to the server's foreign-data wrapper (e.g. schema_name, table_name for postgres_fdw)",
			},
			foreignTableColumnAttr: {
				Type:        schema.TypeList,
				Required:    true,
				ForceNew:    true,
				MinItems:    1,
				Description: "The ordered list of columns of the foreign table. Column matching to the remote table is by name",
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						foreignTableColumnNameAttr: {
							Type:        schema.TypeString,
							Required:    true,
							Description: "The name of the column",
						},
						foreignTableColumnTypeAttr: {
							Type:        schema.TypeString,
							Required:    true,
							Description: "The data type of the column. Use the canonical type name as reported by PostgreSQL (e.g. integer, text, timestamp with time zone) to avoid spurious diffs",
						},
						foreignTableColumnCollateAttr: {
							Type:        schema.TypeString,
							Optional:    true,
							Description: "The collation of the column (omit to use the default collation)",
						},
						foreignTableColumnNotNullAttr: {
							Type:        schema.TypeBool,
							Optional:    true,
							Default:     false,
							Description: "Whether the column has a NOT NULL constraint",
						},
						foreignTableColumnOptionsAttr: {
							Type: schema.TypeMap,
							Elem: &schema.Schema{
								Type: schema.TypeString,
							},
							Optional:    true,
							Description: "Per-column options (e.g. column_name for postgres_fdw)",
						},
					},
				},
			},
			foreignTableDropCascadeAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "When true, will drop objects that depend on the foreign table, and in turn all objects that depend on those objects",
			},
		},
	}
}

func resourcePostgreSQLForeignTableCreate(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureServer) {
		return fmt.Errorf(
			"foreign table resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	tableName := d.Get(foreignTableNameAttr).(string)
	schemaName := d.Get(foreignTableSchemaAttr).(string)
	serverName := d.Get(foreignTableServerNameAttr).(string)
	database := getDatabaseForForeignTable(d, db.client.databaseName)

	b := bytes.NewBufferString("CREATE FOREIGN TABLE ")
	fmt.Fprintf(b, "%s.%s (", pq.QuoteIdentifier(schemaName), pq.QuoteIdentifier(tableName))

	columns := d.Get(foreignTableColumnAttr).([]any)
	for i, raw := range columns {
		col := raw.(map[string]any)
		if i > 0 {
			fmt.Fprint(b, ",")
		}
		fmt.Fprintf(b, " %s %s", pq.QuoteIdentifier(col[foreignTableColumnNameAttr].(string)), col[foreignTableColumnTypeAttr].(string))

		if opts, ok := col[foreignTableColumnOptionsAttr].(map[string]any); ok && len(opts) > 0 {
			writeOptions(b, opts)
		}

		if collate, ok := col[foreignTableColumnCollateAttr].(string); ok && collate != "" {
			fmt.Fprintf(b, " COLLATE %s", pq.QuoteIdentifier(collate))
		}

		if col[foreignTableColumnNotNullAttr].(bool) {
			fmt.Fprint(b, " NOT NULL")
		}
	}

	fmt.Fprint(b, " ) SERVER ", pq.QuoteIdentifier(serverName))

	if options, ok := d.GetOk(foreignTableOptionsAttr); ok {
		writeOptions(b, options.(map[string]any))
	}

	txn, err := startTransaction(db.client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	if _, err := txn.Exec(b.String()); err != nil {
		return fmt.Errorf("could not create foreign table: %w", err)
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("error creating foreign table: %w", err)
	}

	d.SetId(generateForeignTableID(d, database))

	return resourcePostgreSQLForeignTableReadImpl(db, d)
}

func resourcePostgreSQLForeignTableRead(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureServer) {
		return fmt.Errorf(
			"foreign table resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	return resourcePostgreSQLForeignTableReadImpl(db, d)
}

func resourcePostgreSQLForeignTableReadImpl(db *DBConnection, d *schema.ResourceData) error {
	database, schemaName, tableName, err := getDBSchemaForeignTableName(d, db.client)
	if err != nil {
		return err
	}

	txn, err := startTransaction(db.client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	var serverName string
	var tableOptions []string
	query := `SELECT s.srvname, COALESCE(ft.ftoptions, '{}') ` +
		`FROM pg_foreign_table ft ` +
		`JOIN pg_class c ON c.oid = ft.ftrelid ` +
		`JOIN pg_namespace n ON n.oid = c.relnamespace ` +
		`JOIN pg_foreign_server s ON s.oid = ft.ftserver ` +
		`WHERE c.relname = $1 AND n.nspname = $2`
	err = txn.QueryRow(query, tableName, schemaName).Scan(&serverName, pq.Array(&tableOptions))
	switch {
	case err == sql.ErrNoRows:
		log.Printf("[WARN] PostgreSQL foreign table (%s.%s) not found for database %s", schemaName, tableName, database)
		d.SetId("")
		return nil
	case err != nil:
		return fmt.Errorf("error reading foreign table: %w", err)
	}

	columns, err := readForeignTableColumns(txn, schemaName, tableName)
	if err != nil {
		return err
	}

	d.Set(foreignTableNameAttr, tableName)
	d.Set(foreignTableSchemaAttr, schemaName)
	d.Set(foreignTableDatabaseAttr, database)
	d.Set(foreignTableServerNameAttr, serverName)
	d.Set(foreignTableOptionsAttr, optionsToMap(tableOptions))
	d.Set(foreignTableColumnAttr, columns)
	d.SetId(generateForeignTableID(d, database))

	return nil
}

func readForeignTableColumns(txn *sql.Tx, schemaName, tableName string) ([]map[string]any, error) {
	query := `SELECT a.attname, ` +
		`pg_catalog.format_type(a.atttypid, a.atttypmod), ` +
		`a.attnotnull, ` +
		`COALESCE(a.attfdwoptions, '{}'), ` +
		`COALESCE(co.collname, '') ` +
		`FROM pg_attribute a ` +
		`JOIN pg_class c ON c.oid = a.attrelid ` +
		`JOIN pg_namespace n ON n.oid = c.relnamespace ` +
		`LEFT JOIN pg_collation co ON co.oid = a.attcollation ` +
		`WHERE c.relname = $1 AND n.nspname = $2 AND a.attnum > 0 AND NOT a.attisdropped ` +
		`ORDER BY a.attnum`

	rows, err := txn.Query(query, tableName, schemaName)
	if err != nil {
		return nil, fmt.Errorf("error reading foreign table columns: %w", err)
	}
	defer rows.Close()

	columns := []map[string]any{}
	for rows.Next() {
		var name, colType, collName string
		var notNull bool
		var colOptions []string
		if err := rows.Scan(&name, &colType, &notNull, pq.Array(&colOptions), &collName); err != nil {
			return nil, fmt.Errorf("error scanning foreign table column: %w", err)
		}

		column := map[string]any{
			foreignTableColumnNameAttr:    name,
			foreignTableColumnTypeAttr:    colType,
			foreignTableColumnNotNullAttr: notNull,
			foreignTableColumnOptionsAttr: optionsToMap(colOptions),
		}

		// Only surface an explicit collation; the default collation is left unset
		// to avoid spurious diffs since it is not written in the resource.
		if collName != "" && collName != "default" {
			column[foreignTableColumnCollateAttr] = collName
		}

		columns = append(columns, column)
	}

	return columns, rows.Err()
}

func resourcePostgreSQLForeignTableUpdate(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureServer) {
		return fmt.Errorf(
			"foreign table resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	database := getDatabaseForForeignTable(d, db.client.databaseName)
	txn, err := startTransaction(db.client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	if err := setForeignTableOptionsIfChanged(txn, d); err != nil {
		return err
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("error updating foreign table: %w", err)
	}

	return resourcePostgreSQLForeignTableReadImpl(db, d)
}

func setForeignTableOptionsIfChanged(txn *sql.Tx, d *schema.ResourceData) error {
	if !d.HasChange(foreignTableOptionsAttr) {
		return nil
	}

	schemaName := d.Get(foreignTableSchemaAttr).(string)
	tableName := d.Get(foreignTableNameAttr).(string)

	b := bytes.NewBufferString("ALTER FOREIGN TABLE ")
	fmt.Fprintf(b, "%s.%s OPTIONS ( ", pq.QuoteIdentifier(schemaName), pq.QuoteIdentifier(tableName))

	oldOptions, newOptions := d.GetChange(foreignTableOptionsAttr)
	newMap := newOptions.(map[string]any)
	toRemove := oldOptions.(map[string]any)

	cnt := 0
	total := len(newMap)
	for k, v := range newMap {
		operation := "ADD"
		if _, ok := toRemove[k]; ok {
			operation = "SET"
			delete(toRemove, k)
		}
		fmt.Fprintf(b, " %s %s %s ", operation, pq.QuoteIdentifier(k), pq.QuoteLiteral(v.(string)))
		if cnt < total-1 {
			fmt.Fprint(b, ", ")
		}
		cnt++
	}

	for k := range toRemove {
		if cnt != 0 {
			fmt.Fprint(b, " , ")
		}
		fmt.Fprintf(b, " DROP %s ", pq.QuoteIdentifier(k))
		cnt++
	}

	fmt.Fprint(b, " ) ")

	if _, err := txn.Exec(b.String()); err != nil {
		return fmt.Errorf("error updating foreign table options: %w", err)
	}

	return nil
}

func resourcePostgreSQLForeignTableDelete(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureServer) {
		return fmt.Errorf(
			"foreign table resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	schemaName := d.Get(foreignTableSchemaAttr).(string)
	tableName := d.Get(foreignTableNameAttr).(string)
	database := getDatabaseForForeignTable(d, db.client.databaseName)

	txn, err := startTransaction(db.client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	dropMode := "RESTRICT"
	if d.Get(foreignTableDropCascadeAttr).(bool) {
		dropMode = "CASCADE"
	}

	sql := fmt.Sprintf("DROP FOREIGN TABLE %s.%s %s", pq.QuoteIdentifier(schemaName), pq.QuoteIdentifier(tableName), dropMode)
	if _, err := txn.Exec(sql); err != nil {
		return err
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("error deleting foreign table: %w", err)
	}

	d.SetId("")

	return nil
}

// writeOptions appends an ` OPTIONS ( k 'v', ... ) ` clause to b.
func writeOptions(b *bytes.Buffer, options map[string]any) {
	fmt.Fprint(b, " OPTIONS ( ")
	cnt := 0
	total := len(options)
	for k, v := range options {
		fmt.Fprint(b, " ", pq.QuoteIdentifier(k), " ", pq.QuoteLiteral(v.(string)))
		if cnt < total-1 {
			fmt.Fprint(b, ", ")
		}
		cnt++
	}
	fmt.Fprint(b, " ) ")
}

// optionsToMap converts a Postgres options array ("key=value") into a map.
func optionsToMap(options []string) map[string]any {
	mapped := make(map[string]any)
	for _, v := range options {
		pair := strings.SplitN(v, "=", 2)
		if len(pair) == 2 {
			mapped[pair[0]] = pair[1]
		}
	}
	return mapped
}

func getDatabaseForForeignTable(d *schema.ResourceData, databaseName string) string {
	if v, ok := d.GetOk(foreignTableDatabaseAttr); ok {
		databaseName = v.(string)
	}
	return databaseName
}

func generateForeignTableID(d *schema.ResourceData, databaseName string) string {
	return strings.Join([]string{
		databaseName,
		d.Get(foreignTableSchemaAttr).(string),
		d.Get(foreignTableNameAttr).(string),
	}, ".")
}

// getDBSchemaForeignTableName returns database, schema and table name. When importing,
// they are parsed from the resource ID ("database.schema.table"), otherwise they come
// from the state.
func getDBSchemaForeignTableName(d *schema.ResourceData, client *Client) (string, string, string, error) {
	database := getDatabaseForForeignTable(d, client.databaseName)
	schemaName := d.Get(foreignTableSchemaAttr).(string)
	tableName := d.Get(foreignTableNameAttr).(string)

	if tableName == "" {
		parsed := strings.Split(d.Id(), ".")
		if len(parsed) != 3 {
			return "", "", "", fmt.Errorf("foreign table ID %s does not have the expected format 'database.schema.table'", d.Id())
		}
		database = parsed[0]
		schemaName = parsed[1]
		tableName = parsed[2]
	}

	return database, schemaName, tableName, nil
}
