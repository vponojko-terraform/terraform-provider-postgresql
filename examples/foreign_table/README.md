# Foreign table example (`postgres_fdw`)

A generic, end-to-end example that exposes a table from a remote PostgreSQL server as a
local foreign table, using pure Terraform. It wires up the whole foreign-data-wrapper
chain:

```
postgresql_extension  ->  postgresql_server  ->  postgresql_user_mapping  ->  postgresql_foreign_table  ->  postgresql_grant
```

This works for any two reachable PostgreSQL databases: same instance (cross-database),
same VPC, cross-region, or on-prem — as long as the local instance can reach the remote
host on its port and the remote role has the required privileges.

## Files

| File | Purpose |
|---|---|
| `main.tf` | Provider config and the FDW resource chain. |
| `variables.tf` | All inputs (local + remote connection, table shape, optional consuming role). |
| `terraform.tfvars.example` | Sample values — copy to `terraform.tfvars` and edit. |

## Usage

```sh
cp terraform.tfvars.example terraform.tfvars
# edit terraform.tfvars with your values
terraform init
terraform plan
terraform apply
```

Prefer environment variables or a secrets manager for passwords rather than committing
`terraform.tfvars`. Any of the variables can be supplied via `TF_VAR_<name>`.

## Notes

- Columns are declared explicitly and matched to the remote table **by name**. Use the
  canonical PostgreSQL type names (e.g. `integer`, `text`, `timestamp with time zone`)
  to avoid spurious diffs.
- `options.schema_name` / `options.table_name` let the local names differ from the remote
  ones. Omit them when they are identical.
- Changing columns, name, schema, server or database forces the foreign table to be
  recreated; only `options` are updated in place.
- This resource creates a **single** foreign table. To bulk-import every table from a
  remote schema, use
  [`IMPORT FOREIGN SCHEMA`](https://www.postgresql.org/docs/current/sql-importforeignschema.html),
  which is not modeled by this resource.

## References

- [`postgres_fdw` — PostgreSQL docs](https://www.postgresql.org/docs/current/postgres-fdw.html)
- [`CREATE FOREIGN TABLE`](https://www.postgresql.org/docs/current/sql-createforeigntable.html)
- [`CREATE SERVER`](https://www.postgresql.org/docs/current/sql-createserver.html)
- [`CREATE USER MAPPING`](https://www.postgresql.org/docs/current/sql-createusermapping.html)
- Provider resources:
  [`postgresql_extension`](https://registry.terraform.io/providers/cyrilgdn/postgresql/latest/docs/resources/extension),
  [`postgresql_server`](https://registry.terraform.io/providers/cyrilgdn/postgresql/latest/docs/resources/server),
  [`postgresql_user_mapping`](https://registry.terraform.io/providers/cyrilgdn/postgresql/latest/docs/resources/user_mapping),
  [`postgresql_foreign_table`](https://registry.terraform.io/providers/cyrilgdn/postgresql/latest/docs/resources/foreign_table),
  [`postgresql_grant`](https://registry.terraform.io/providers/cyrilgdn/postgresql/latest/docs/resources/grant)
