---
layout: "postgresql"
page_title: "PostgreSQL: postgresql_foreign_table"
sidebar_current: "docs-postgresql-resource-postgresql_foreign_table"
description: |-
  Creates and manages a foreign table on a PostgreSQL server.
---

# postgresql\_foreign\_table

The ``postgresql_foreign_table`` resource creates and manages a foreign table on a
PostgreSQL server. A foreign table maps to a table on a remote server accessed through
a foreign-data wrapper (such as `postgres_fdw`).

The columns of the foreign table are declared explicitly. If you need to bulk-import
every table from a remote schema, use `IMPORT FOREIGN SCHEMA` outside of this resource.

## Usage

```hcl
resource "postgresql_extension" "ext_postgres_fdw" {
  name = "postgres_fdw"
}

resource "postgresql_server" "myserver_postgres" {
  server_name = "myserver_postgres"
  fdw_name    = "postgres_fdw"
  options = {
    host   = "foo"
    dbname = "foodb"
    port   = "5432"
  }

  depends_on = [postgresql_extension.ext_postgres_fdw]
}

resource "postgresql_user_mapping" "remote" {
  server_name = postgresql_server.myserver_postgres.server_name
  user_name   = "local_user"
  options = {
    user     = "admin"
    password = "pass"
  }
}

resource "postgresql_foreign_table" "foreign_table" {
  name        = "foreign_table"
  schema      = "public"
  server_name = postgresql_server.myserver_postgres.server_name

  column {
    name     = "id"
    type     = "integer"
    not_null = true
  }

  column {
    name = "data"
    type = "text"
  }

  options = {
    schema_name = "some_schema"
    table_name  = "some_table"
  }

  depends_on = [postgresql_user_mapping.remote]
}
```

## Argument Reference

* `name` - (Required) The name of the foreign table to be created.
Changing this value
  will force the creation of a new resource as this value can only be set
  when the foreign table is created.
* `server_name` - (Required) The name of an existing foreign server to use for the foreign table.
Changing this value
  will force the creation of a new resource as this value can only be set
  when the foreign table is created.
* `column` - (Required) One or more `column` blocks defining the columns of the foreign table, in order. See below for the supported arguments. Changing columns will force the creation of a new resource.
* `schema` - (Optional) The schema in which the foreign table is created. (Default: `public`).
Changing this value
  will force the creation of a new resource as this value can only be set
  when the foreign table is created.
* `database` - (Optional) Which database to create the foreign table in. Defaults to the provider database.
Changing this value
  will force the creation of a new resource as this value can only be set
  when the foreign table is created.
* `options` - (Optional) This clause specifies the options for the foreign table. The allowed option names and values are specific to the server's foreign-data wrapper. For `postgres_fdw` these typically include `schema_name` and `table_name`.
* `drop_cascade` - (Optional) When true, will drop objects that depend on the foreign table, and in turn all objects that depend on those objects. (Default: false)

The `column` block supports:

* `name` - (Required) The name of the column. Matching to the remote column is by name.
* `type` - (Required) The data type of the column. Use the canonical type name as reported by PostgreSQL (e.g. `integer`, `text`, `timestamp with time zone`) to avoid spurious diffs.
* `collate` - (Optional) The collation of the column. Omit to use the default collation.
* `not_null` - (Optional) Whether the column has a `NOT NULL` constraint. (Default: false)
* `options` - (Optional) Per-column options. For `postgres_fdw` this is typically `column_name` when the local column name differs from the remote one.

## Import

Foreign tables can be imported using the database name, schema and the foreign table's name, e.g.

`terraform import postgresql_foreign_table.foreign_table example-database.public.foreign_table`
