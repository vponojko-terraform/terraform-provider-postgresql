---
layout: "postgresql"
page_title: "PostgreSQL: postgresql_import_foreign_schema"
sidebar_current: "docs-postgresql-resource-postgresql_import_foreign_schema"
description: |-
  Bulk-imports foreign tables from a remote schema using IMPORT FOREIGN SCHEMA.
---

# postgresql\_import\_foreign\_schema

The ``postgresql_import_foreign_schema`` resource runs
[`IMPORT FOREIGN SCHEMA`](https://www.postgresql.org/docs/current/sql-importforeignschema.html)
to bulk-create foreign tables in a local schema for every table found in a
remote schema on a foreign server.

Unlike `postgresql_foreign_table`, which requires each table's columns to be
declared explicitly, this resource derives the foreign tables from the remote
server's catalog at apply time. This makes it a good fit when the remote schema
is owned and evolved elsewhere (e.g. by an application's migrations) and you
only want the local database to mirror whatever exists remotely.

## Opt-in by design

The actual `IMPORT FOREIGN SCHEMA` statement only runs when `import` is set to
`true`. It defaults to `false`, in which case the resource is a managed
placeholder that performs no import. This lets you declare the resource (and its
dependencies on the server, user mapping, and local schema) up front, then
enable the import explicitly per environment when the remote tables are ready.

## Re-import / maintenance

`IMPORT FOREIGN SCHEMA` is a point-in-time snapshot. When new tables are added
to the remote schema, change the `triggers` map to force the import to run
again. Each run drops the foreign tables previously imported from this server
into the local schema and re-imports them in a single transaction, so re-imports
are idempotent (plain `IMPORT FOREIGN SCHEMA` otherwise errors when a target
foreign table already exists).

## Usage

```hcl
resource "postgresql_extension" "postgres_fdw" {
  name = "postgres_fdw"
}

resource "postgresql_server" "remote" {
  server_name = "remote"
  fdw_name    = "postgres_fdw"
  options = {
    host   = "remote.example.com"
    port   = "5432"
    dbname = "remote_db"
  }

  depends_on = [postgresql_extension.postgres_fdw]
}

resource "postgresql_user_mapping" "remote" {
  server_name = postgresql_server.remote.server_name
  user_name   = "CURRENT_USER"
  options = {
    user     = var.remote_user
    password = var.remote_password
  }
}

resource "postgresql_schema" "imported" {
  name = "imported_schema"
}

resource "postgresql_import_foreign_schema" "example" {
  server_name   = postgresql_server.remote.server_name
  remote_schema = "source_schema"
  local_schema  = postgresql_schema.imported.name

  # Opt in to actually running IMPORT FOREIGN SCHEMA.
  import = true

  # Bump to re-import and pick up newly added remote tables.
  triggers = {
    version = "1"
  }

  depends_on = [postgresql_user_mapping.remote]
}
```

## Argument Reference

* `server_name` - (Required) The name of an existing foreign server to import from. Changing this forces a new resource.
* `remote_schema` - (Required) The name of the schema on the foreign server whose tables are to be imported. Changing this forces a new resource.
* `local_schema` - (Required) The name of the (already existing) local schema into which the foreign tables are imported. Changing this forces a new resource.
* `import` - (Optional) Whether to actually execute `IMPORT FOREIGN SCHEMA`. When `false` (the default) the resource performs no import. (Default: `false`)
* `database` - (Optional) The database in which the import is performed. Defaults to the provider database. Changing this forces a new resource.
* `limit_to` - (Optional) A set of table names; only these tables are imported. Mutually exclusive with `except`.
* `except` - (Optional) A set of table names; all tables except these are imported. Mutually exclusive with `limit_to`.
* `options` - (Optional) Options passed to the foreign-data wrapper for the import. For `postgres_fdw` these include `import_default`, `import_generated`, `import_collate`, and `import_not_null`.
* `triggers` - (Optional) An arbitrary map of values. Changing it forces the import to re-run, which is how new remote tables are picked up.
* `drop_cascade` - (Optional) When `true`, dependent objects are dropped (`CASCADE`) when foreign tables previously imported by this resource are removed (on delete or before a re-import). (Default: `false`)

## Attributes Reference

* `imported_tables` - The names of the foreign tables currently present in the local schema for this server (the result of the import).

## Notes

* This resource assumes a given (`server_name`, `local_schema`) pair is managed by a single `postgresql_import_foreign_schema` resource. It considers every foreign table in `local_schema` backed by `server_name` to be under its management, and will drop those tables on delete or before a re-import.
* `IMPORT FOREIGN SCHEMA` runs under the current role's user mapping. If other roles will query the imported foreign tables directly, create a user mapping for each such role (or a `PUBLIC` mapping).

## Import

Import foreign schema resources can be imported using the database name, server name, remote schema and local schema, e.g.

`terraform import postgresql_import_foreign_schema.example mydb.remote.source_schema.imported_schema`
