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
	importForeignSchemaServerAttr         = "server_name"
	importForeignSchemaRemoteSchemaAttr   = "remote_schema"
	importForeignSchemaLocalSchemaAttr    = "local_schema"
	importForeignSchemaDatabaseAttr       = "database"
	importForeignSchemaLimitToAttr        = "limit_to"
	importForeignSchemaExceptAttr         = "except"
	importForeignSchemaOptionsAttr        = "options"
	importForeignSchemaImportAttr         = "import"
	importForeignSchemaTriggersAttr       = "triggers"
	importForeignSchemaDropCascadeAttr    = "drop_cascade"
	importForeignSchemaImportedTablesAttr = "imported_tables"
)

func resourcePostgreSQLImportForeignSchema() *schema.Resource {
	return &schema.Resource{
		Create: PGResourceFunc(resourcePostgreSQLImportForeignSchemaCreate),
		Read:   PGResourceFunc(resourcePostgreSQLImportForeignSchemaRead),
		Update: PGResourceFunc(resourcePostgreSQLImportForeignSchemaUpdate),
		Delete: PGResourceFunc(resourcePostgreSQLImportForeignSchemaDelete),
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			importForeignSchemaServerAttr: {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The name of an existing foreign server to import the schema from",
			},
			importForeignSchemaRemoteSchemaAttr: {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The name of the schema on the foreign server whose tables are to be imported",
			},
			importForeignSchemaLocalSchemaAttr: {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The name of the (already existing) local schema into which the foreign tables will be imported",
			},
			importForeignSchemaDatabaseAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				ForceNew:    true,
				Description: "The database in which the import is performed (defaults to the provider database)",
			},
			importForeignSchemaImportAttr: {
				Type:     schema.TypeBool,
				Optional: true,
				Default:  false,
				Description: "Whether to actually execute IMPORT FOREIGN SCHEMA. When false (the default), the resource is a managed placeholder that performs no import. " +
					"Set to true to opt in to running (and re-running) the import.",
			},
			importForeignSchemaLimitToAttr: {
				Type:          schema.TypeSet,
				Optional:      true,
				Elem:          &schema.Schema{Type: schema.TypeString},
				ConflictsWith: []string{importForeignSchemaExceptAttr},
				Description:   "Import only the foreign tables matching one of the given table names. Mutually exclusive with `except`",
			},
			importForeignSchemaExceptAttr: {
				Type:          schema.TypeSet,
				Optional:      true,
				Elem:          &schema.Schema{Type: schema.TypeString},
				ConflictsWith: []string{importForeignSchemaLimitToAttr},
				Description:   "Import all foreign tables except those matching one of the given table names. Mutually exclusive with `limit_to`",
			},
			importForeignSchemaOptionsAttr: {
				Type: schema.TypeMap,
				Elem: &schema.Schema{
					Type: schema.TypeString,
				},
				Optional:    true,
				Description: "Options to be passed to the foreign-data wrapper for the import (e.g. import_default, import_not_null for postgres_fdw)",
			},
			importForeignSchemaTriggersAttr: {
				Type:        schema.TypeMap,
				Optional:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Description: "Arbitrary map of values that, when changed, forces the import to be re-run. Useful to pick up new tables added on the remote server",
			},
			importForeignSchemaDropCascadeAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "When true, dependent objects are dropped (CASCADE) when foreign tables previously imported by this resource are dropped, either on delete or before a re-import. RESTRICT is the default",
			},
			importForeignSchemaImportedTablesAttr: {
				Type:        schema.TypeList,
				Computed:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Description: "The names of the foreign tables currently present in the local schema for this server (the result of the import)",
			},
		},
	}
}

func resourcePostgreSQLImportForeignSchemaCreate(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureServer) {
		return fmt.Errorf(
			"import foreign schema resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	database := getDatabaseForImportForeignSchema(d, db.client.databaseName)

	if d.Get(importForeignSchemaImportAttr).(bool) {
		if err := runImportForeignSchema(db, d, database); err != nil {
			return err
		}
	}

	d.SetId(generateImportForeignSchemaID(d, database))

	return resourcePostgreSQLImportForeignSchemaReadImpl(db, d)
}

func resourcePostgreSQLImportForeignSchemaRead(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureServer) {
		return fmt.Errorf(
			"import foreign schema resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	return resourcePostgreSQLImportForeignSchemaReadImpl(db, d)
}

func resourcePostgreSQLImportForeignSchemaReadImpl(db *DBConnection, d *schema.ResourceData) error {
	database, serverName, remoteSchema, localSchema, err := getImportForeignSchemaParts(d, db.client)
	if err != nil {
		return err
	}

	txn, err := startTransaction(db.client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	// Make sure the referenced foreign server still exists. If it is gone the
	// mapping this resource represents is no longer valid.
	var serverExists bool
	if err := txn.QueryRow(
		"SELECT true FROM pg_foreign_server WHERE srvname = $1", serverName,
	).Scan(&serverExists); err != nil {
		if err == sql.ErrNoRows {
			log.Printf("[WARN] PostgreSQL foreign server (%s) not found, removing import_foreign_schema from state", serverName)
			d.SetId("")
			return nil
		}
		return fmt.Errorf("error reading foreign server: %w", err)
	}

	tables, err := listForeignTablesForServer(txn, localSchema, serverName)
	if err != nil {
		return err
	}

	if err := txn.Commit(); err != nil {
		return fmt.Errorf("error reading import foreign schema: %w", err)
	}

	d.Set(importForeignSchemaServerAttr, serverName)
	d.Set(importForeignSchemaRemoteSchemaAttr, remoteSchema)
	d.Set(importForeignSchemaLocalSchemaAttr, localSchema)
	d.Set(importForeignSchemaDatabaseAttr, database)
	d.Set(importForeignSchemaImportedTablesAttr, tables)
	d.SetId(generateImportForeignSchemaID(d, database))

	return nil
}

func resourcePostgreSQLImportForeignSchemaUpdate(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureServer) {
		return fmt.Errorf(
			"import foreign schema resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	database := getDatabaseForImportForeignSchema(d, db.client.databaseName)

	// Only (re-)run the import when the user has opted in. Toggling `import`
	// back to false, or changing `triggers`/scope while it stays false, leaves
	// any previously imported foreign tables untouched.
	if d.Get(importForeignSchemaImportAttr).(bool) {
		if err := runImportForeignSchema(db, d, database); err != nil {
			// keep the prior triggers in state so the next apply retries the import
			d.Partial(true)
			return err
		}
	}

	return resourcePostgreSQLImportForeignSchemaReadImpl(db, d)
}

func resourcePostgreSQLImportForeignSchemaDelete(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureServer) {
		return fmt.Errorf(
			"import foreign schema resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	serverName := d.Get(importForeignSchemaServerAttr).(string)
	localSchema := d.Get(importForeignSchemaLocalSchemaAttr).(string)
	database := getDatabaseForImportForeignSchema(d, db.client.databaseName)

	txn, err := startTransaction(db.client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	// Clean up the foreign tables this resource is responsible for (those in the
	// local schema pointing at this server). If nothing was ever imported this
	// is a no-op.
	if err := dropForeignTablesForServer(txn, localSchema, serverName, d.Get(importForeignSchemaDropCascadeAttr).(bool)); err != nil {
		return err
	}

	if err := txn.Commit(); err != nil {
		return fmt.Errorf("error deleting import foreign schema: %w", err)
	}

	d.SetId("")

	return nil
}

// runImportForeignSchema drops any foreign tables previously imported from the
// server into the local schema and then (re-)runs IMPORT FOREIGN SCHEMA. Doing
// the drop and import in a single transaction makes re-imports idempotent:
// PostgreSQL otherwise errors when a target foreign table already exists, and a
// failed import rolls back to the previous state.
func runImportForeignSchema(db *DBConnection, d *schema.ResourceData, database string) error {
	serverName := d.Get(importForeignSchemaServerAttr).(string)
	localSchema := d.Get(importForeignSchemaLocalSchemaAttr).(string)

	txn, err := startTransaction(db.client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	if err := dropForeignTablesForServer(txn, localSchema, serverName, d.Get(importForeignSchemaDropCascadeAttr).(bool)); err != nil {
		return err
	}

	if _, err := txn.Exec(buildImportForeignSchemaQuery(d)); err != nil {
		return fmt.Errorf("could not import foreign schema: %w", err)
	}

	if err := txn.Commit(); err != nil {
		return fmt.Errorf("error importing foreign schema: %w", err)
	}

	return nil
}

func buildImportForeignSchemaQuery(d *schema.ResourceData) string {
	remoteSchema := d.Get(importForeignSchemaRemoteSchemaAttr).(string)
	serverName := d.Get(importForeignSchemaServerAttr).(string)
	localSchema := d.Get(importForeignSchemaLocalSchemaAttr).(string)

	b := bytes.NewBufferString("IMPORT FOREIGN SCHEMA ")
	fmt.Fprint(b, pq.QuoteIdentifier(remoteSchema))

	if v, ok := d.GetOk(importForeignSchemaLimitToAttr); ok && v.(*schema.Set).Len() > 0 {
		fmt.Fprintf(b, " LIMIT TO (%s)", setToPgIdentListWithoutSchema(v.(*schema.Set)))
	} else if v, ok := d.GetOk(importForeignSchemaExceptAttr); ok && v.(*schema.Set).Len() > 0 {
		fmt.Fprintf(b, " EXCEPT (%s)", setToPgIdentListWithoutSchema(v.(*schema.Set)))
	}

	fmt.Fprint(b, " FROM SERVER ", pq.QuoteIdentifier(serverName))
	fmt.Fprint(b, " INTO ", pq.QuoteIdentifier(localSchema))

	if options, ok := d.GetOk(importForeignSchemaOptionsAttr); ok && len(options.(map[string]any)) > 0 {
		writeOptions(b, options.(map[string]any))
	}

	return b.String()
}

// listForeignTablesForServer returns the names of the foreign tables located in
// schemaName that are backed by the given foreign server.
func listForeignTablesForServer(txn *sql.Tx, schemaName, serverName string) ([]string, error) {
	query := `SELECT c.relname ` +
		`FROM pg_foreign_table ft ` +
		`JOIN pg_class c ON c.oid = ft.ftrelid ` +
		`JOIN pg_namespace n ON n.oid = c.relnamespace ` +
		`JOIN pg_foreign_server s ON s.oid = ft.ftserver ` +
		`WHERE n.nspname = $1 AND s.srvname = $2 ` +
		`ORDER BY c.relname`

	rows, err := txn.Query(query, schemaName, serverName)
	if err != nil {
		return nil, fmt.Errorf("error listing foreign tables for server %s: %w", serverName, err)
	}
	defer rows.Close()

	tables := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("error scanning foreign table name: %w", err)
		}
		tables = append(tables, name)
	}

	return tables, rows.Err()
}

// dropForeignTablesForServer drops every foreign table in schemaName that is
// backed by serverName.
func dropForeignTablesForServer(txn *sql.Tx, schemaName, serverName string, cascade bool) error {
	tables, err := listForeignTablesForServer(txn, schemaName, serverName)
	if err != nil {
		return err
	}

	dropMode := "RESTRICT"
	if cascade {
		dropMode = "CASCADE"
	}

	for _, table := range tables {
		sql := fmt.Sprintf(
			"DROP FOREIGN TABLE %s.%s %s",
			pq.QuoteIdentifier(schemaName), pq.QuoteIdentifier(table), dropMode,
		)
		if _, err := txn.Exec(sql); err != nil {
			return fmt.Errorf("could not drop foreign table %s.%s: %w", schemaName, table, err)
		}
	}

	return nil
}

func getDatabaseForImportForeignSchema(d *schema.ResourceData, databaseName string) string {
	if v, ok := d.GetOk(importForeignSchemaDatabaseAttr); ok {
		databaseName = v.(string)
	}
	return databaseName
}

func generateImportForeignSchemaID(d *schema.ResourceData, databaseName string) string {
	return strings.Join([]string{
		databaseName,
		d.Get(importForeignSchemaServerAttr).(string),
		d.Get(importForeignSchemaRemoteSchemaAttr).(string),
		d.Get(importForeignSchemaLocalSchemaAttr).(string),
	}, ".")
}

// getImportForeignSchemaParts returns database, server, remote schema and local
// schema. When importing the resource, they are parsed from the ID
// ("database.server.remote_schema.local_schema"), otherwise they come from state.
func getImportForeignSchemaParts(d *schema.ResourceData, client *Client) (string, string, string, string, error) {
	database := getDatabaseForImportForeignSchema(d, client.databaseName)
	serverName := d.Get(importForeignSchemaServerAttr).(string)
	remoteSchema := d.Get(importForeignSchemaRemoteSchemaAttr).(string)
	localSchema := d.Get(importForeignSchemaLocalSchemaAttr).(string)

	if serverName == "" {
		parsed := strings.Split(d.Id(), ".")
		if len(parsed) != 4 {
			return "", "", "", "", fmt.Errorf(
				"import foreign schema ID %s does not have the expected format 'database.server.remote_schema.local_schema'", d.Id(),
			)
		}
		database = parsed[0]
		serverName = parsed[1]
		remoteSchema = parsed[2]
		localSchema = parsed[3]
	}

	return database, serverName, remoteSchema, localSchema, nil
}
