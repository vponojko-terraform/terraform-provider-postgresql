# Local (target) instance where the foreign objects are created.
variable "local_host" {
  type        = string
  description = "Host of the local PostgreSQL instance."
  default     = "127.0.0.1"
}

variable "local_port" {
  type        = number
  description = "Port of the local PostgreSQL instance."
  default     = 5432
}

variable "local_database" {
  type        = string
  description = "Local database where the foreign objects are created."
}

variable "local_username" {
  type        = string
  description = "Local role used to connect and create the FDW objects (also the mapped user)."
}

variable "local_password" {
  type        = string
  description = "Password for the local role."
  sensitive   = true
}

variable "local_schema" {
  type        = string
  description = "Local schema in which the foreign table is created."
  default     = "public"
}

variable "sslmode" {
  type        = string
  description = "SSL mode used both for the provider connection and the foreign server."
  default     = "require"
}

# Remote (source) server that holds the real data.
variable "server_name" {
  type        = string
  description = "Name of the foreign server object to create locally."
  default     = "remote_server"
}

variable "remote_host" {
  type        = string
  description = "Hostname/endpoint of the remote PostgreSQL server."
}

variable "remote_port" {
  type        = string
  description = "Port of the remote PostgreSQL server."
  default     = "5432"
}

variable "remote_database" {
  type        = string
  description = "Database name on the remote server."
}

variable "remote_username" {
  type        = string
  description = "Role used to authenticate on the remote server."
}

variable "remote_password" {
  type        = string
  description = "Password for the remote role."
  sensitive   = true
}

variable "remote_schema" {
  type        = string
  description = "Schema of the table on the remote server."
  default     = "public"
}

variable "remote_table_name" {
  type        = string
  description = "Name of the table on the remote server."
}

# Foreign table shape.
variable "foreign_table_name" {
  type        = string
  description = "Local name for the foreign table."
}

variable "columns" {
  description = "Ordered column definitions matching the remote table (by name)."
  type = list(object({
    name     = string
    type     = string
    not_null = optional(bool, false)
  }))
}

# Optional consuming role to grant read access to.
variable "app_role" {
  type        = string
  description = "Existing role to grant USAGE on the server and SELECT on the foreign table. Leave empty to skip grants."
  default     = ""
}
