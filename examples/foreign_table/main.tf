## Generic example: expose a remote PostgreSQL table locally with postgres_fdw.
##
## This wires up the full foreign-data-wrapper chain in pure Terraform:
##   extension -> foreign server -> user mapping -> foreign table -> grants
##
## References:
##   - postgres_fdw:        https://www.postgresql.org/docs/current/postgres-fdw.html
##   - CREATE FOREIGN TABLE: https://www.postgresql.org/docs/current/sql-createforeigntable.html
##   - Provider docs:        https://registry.terraform.io/providers/cyrilgdn/postgresql/latest/docs

terraform {
  required_providers {
    postgresql = {
      source  = "cyrilgdn/postgresql"
      version = ">= 1.28.0"
    }
  }
  required_version = ">= 0.14.0"
}

# Connects to the LOCAL database where the foreign objects are created.
provider "postgresql" {
  host      = var.local_host
  port      = var.local_port
  database  = var.local_database
  username  = var.local_username
  password  = var.local_password
  sslmode   = var.sslmode
  superuser = false
}

# 1. Enable the foreign-data wrapper.
# https://registry.terraform.io/providers/cyrilgdn/postgresql/latest/docs/resources/extension
resource "postgresql_extension" "postgres_fdw" {
  name = "postgres_fdw"
}

# 2. Define the remote server (connection details only, no credentials).
# https://registry.terraform.io/providers/cyrilgdn/postgresql/latest/docs/resources/server
resource "postgresql_server" "remote" {
  server_name = var.server_name
  fdw_name    = "postgres_fdw"

  options = {
    host    = var.remote_host
    port    = var.remote_port
    dbname  = var.remote_database
    sslmode = var.sslmode
  }

  depends_on = [postgresql_extension.postgres_fdw]
}

# 3. Map the local role to remote credentials.
# https://registry.terraform.io/providers/cyrilgdn/postgresql/latest/docs/resources/user_mapping
resource "postgresql_user_mapping" "remote" {
  server_name = postgresql_server.remote.server_name
  user_name   = var.local_username

  options = {
    user     = var.remote_username
    password = var.remote_password
  }
}

# 4. Declare the foreign table (columns must match the remote table by name).
# https://registry.terraform.io/providers/cyrilgdn/postgresql/latest/docs/resources/foreign_table
resource "postgresql_foreign_table" "example" {
  name        = var.foreign_table_name
  schema      = var.local_schema
  server_name = postgresql_server.remote.server_name

  dynamic "column" {
    for_each = var.columns
    content {
      name     = column.value.name
      type     = column.value.type
      not_null = try(column.value.not_null, false)
    }
  }

  # For postgres_fdw, point at the remote schema/table when names differ.
  options = {
    schema_name = var.remote_schema
    table_name  = var.remote_table_name
  }

  depends_on = [postgresql_user_mapping.remote]
}

# 5. Grant read access to a consuming role.
# https://registry.terraform.io/providers/cyrilgdn/postgresql/latest/docs/resources/grant
resource "postgresql_grant" "usage_on_server" {
  count       = var.app_role == "" ? 0 : 1
  role        = var.app_role
  object_type = "foreign_server"
  objects     = [postgresql_server.remote.server_name]
  privileges  = ["USAGE"]
}

resource "postgresql_grant" "select_on_table" {
  count       = var.app_role == "" ? 0 : 1
  role        = var.app_role
  database    = var.local_database
  schema      = var.local_schema
  object_type = "table"
  objects     = [postgresql_foreign_table.example.name]
  privileges  = ["SELECT"]
}
