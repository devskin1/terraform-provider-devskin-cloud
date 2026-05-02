# Terraform Provider for DevskinCloud

[![Terraform Registry](https://img.shields.io/badge/terraform-registry-blueviolet)](https://registry.terraform.io/providers/devskin1/devskin-cloud/latest)

A Terraform provider for managing infrastructure on [DevskinCloud](https://cloud.devskin.com).

**Registry:** [registry.terraform.io/providers/devskin1/devskin-cloud](https://registry.terraform.io/providers/devskin1/devskin-cloud/latest)

## Requirements

- [Terraform](https://www.terraform.io/downloads.html) >= 1.0
- [Go](https://golang.org/doc/install) >= 1.21

## Building the Provider

```bash
go build -o terraform-provider-devskin
```

## Installing Locally

```bash
# Build
go build -o terraform-provider-devskin

# Install to local Terraform plugin directory
mkdir -p ~/.terraform.d/plugins/registry.terraform.io/devskin1/devskin-cloud/1.0.0/linux_amd64/
cp terraform-provider-devskin ~/.terraform.d/plugins/registry.terraform.io/devskin1/devskin-cloud/1.0.0/linux_amd64/
```

## Provider Configuration

```hcl
terraform {
  required_providers {
    devskin = {
      source  = "devskin1/devskin-cloud"
      version = "~> 1.0"
    }
  }
}

provider "devskin" {
  api_url = "https://cloud-api.devskin.com/api"  # Optional, this is the default
  token   = var.devskin_token                      # Required - your API Bearer token
}
```

### Authentication

The provider requires a Bearer token for authentication. You can pass it via a variable:

```hcl
variable "devskin_token" {
  type      = string
  sensitive = true
}
```

Then set it via environment or `terraform.tfvars`:

```bash
export TF_VAR_devskin_token="your-api-token-here"
```

## Resources

### devskin_vpc

Manages a Virtual Private Cloud.

```hcl
resource "devskin_vpc" "main" {
  name        = "production-vpc"
  cidr_block  = "10.0.0.0/16"
  region      = "us-east-1"
  enable_dns  = true
  enable_ipv6 = false

  subnets {
    name       = "public-subnet"
    cidr_block = "10.0.1.0/24"
    zone       = "us-east-1a"
  }

  subnets {
    name       = "private-subnet"
    cidr_block = "10.0.2.0/24"
    zone       = "us-east-1b"
  }
}
```

### devskin_instance

Manages a compute instance.

```hcl
resource "devskin_instance" "web" {
  name          = "web-server"
  instance_type = "ds.medium"
  image_id      = "ubuntu-22.04"
  region        = "us-east-1"
  vpc_id        = devskin_vpc.main.id
  subnet_id     = devskin_vpc.main.default_subnet_id
  ipv6          = false

  # Optional: enroll the VM into Flux observability at first boot.
  # Only consumed at create time; modifying this block on an existing
  # instance is a no-op.
  monitoring_enrollment {
    enabled = true
    api_key = var.flux_api_key   # Flux project key (sensitive)
  }
}
```

#### Marketplace VM data-platform shortcut

To spin up a JupyterLab / Kafka / Airflow VM, use `devskin_instance` with the
template image id directly (or `POST /api/marketplace/products/:id/deploy`):

```hcl
# JupyterLab — replaces the legacy managed Lakehouse Notebooks
resource "devskin_instance" "jupyter" {
  name          = "data-team-jupyter"
  instance_type = "ds.medium"
  image_id      = "tpl-206"        # JupyterLab — mp-030
  region        = "us-east-1"
  vpc_id        = devskin_vpc.main.id
  subnet_id     = devskin_vpc.main.default_subnet_id
}

# Apache Kafka — replaces managed /lakehouse/streaming
resource "devskin_instance" "kafka" {
  name          = "events-kafka"
  instance_type = "ds.medium"
  image_id      = "tpl-204"        # Apache Kafka — mp-040
  region        = "us-east-1"
  vpc_id        = devskin_vpc.main.id
  subnet_id     = devskin_vpc.main.default_subnet_id
}

# Apache Airflow — replaces managed /lakehouse/workflows
resource "devskin_instance" "airflow" {
  name          = "etl-airflow"
  instance_type = "ds.medium"
  image_id      = "tpl-205"        # Apache Airflow — mp-050
  region        = "us-east-1"
  vpc_id        = devskin_vpc.main.id
  subnet_id     = devskin_vpc.main.default_subnet_id
}
```

### devskin_database

Manages a database instance.

```hcl
resource "devskin_database" "postgres" {
  name           = "app-db"
  engine         = "postgres"
  instance_class = "db.medium"
  storage        = 50
  vpc_id         = devskin_vpc.main.id
}
```

### devskin_k8s_cluster

Manages a Kubernetes cluster.

```hcl
resource "devskin_k8s_cluster" "production" {
  name    = "prod-cluster"
  version = "1.29"
  region  = "us-east-1"

  node_groups {
    name          = "general"
    instance_type = "ds.large"
    desired_size  = 3
  }

  node_groups {
    name          = "gpu"
    instance_type = "ds.gpu.xlarge"
    desired_size  = 1
  }
}
```

### devskin_container_service

Manages a container service (CloudFeet/ECS).

```hcl
resource "devskin_container_service" "api" {
  name          = "my-api"
  image         = "myorg/api:latest"
  port          = 3000
  desired_count = 2

  environment = {
    NODE_ENV     = "production"
    DATABASE_URL = devskin_database.postgres.endpoint
  }

  # Optional: enroll the service into Flux observability.
  # Only consumed at create time.
  monitoring {
    enabled = true
    api_key = var.flux_api_key
  }
}

# Deploy from Git repository
resource "devskin_container_service" "frontend" {
  name              = "my-frontend"
  source_repository = "https://github.com/myorg/frontend.git"
  port              = 80
  desired_count     = 2
}
```

### devskin_lake_database

Manages a Lakehouse (KubmixLake) catalog database backed by Apache Polaris and S3.

```hcl
resource "devskin_lake_database" "bronze" {
  name        = "bronze"
  description = "Raw landing zone"
}
```

### devskin_lake_table

Manages an Iceberg table inside a catalog database. Schema is immutable —
changing `columns`, `name`, or `database_id` forces a destroy/replace.

```hcl
resource "devskin_lake_table" "orders" {
  database_id = devskin_lake_database.bronze.id
  name        = "orders"

  columns = [
    { name = "id",         type = "bigint" },
    { name = "amount",     type = "double" },
    { name = "created_at", type = "timestamp" },
    { name = "user_id",    type = "varchar" },
  ]
}
```

### devskin_lake_spark_job

Manages a Spark job (PySpark / Scala / Spark-SQL) executed against the lake.

```hcl
resource "devskin_lake_spark_job" "rollup" {
  name           = "daily-rollup"
  language       = "pyspark"
  code           = file("${path.module}/jobs/daily_rollup.py")
  executor_cores = 2
  executor_memory_gb = 4
  num_executors  = 3
  schedule_cron  = "0 2 * * *"
}
```

### devskin_lake_quality_rule, devskin_lake_materialized_view, devskin_lake_saved_query

Standard CRUD resources for data-quality rules, materialized views, and
saved SQL queries on the catalog. See the resource schema docs in
`docs/resources/` for full attribute reference.

### Deprecated resources

The following resources are deprecated — managed paths were retired in favour
of marketplace VMs (mp-030 / mp-040 / mp-050). The schemas are preserved so
existing state files still parse, but every CRUD method short-circuits with
an error pointing to the marketplace flow:

| Resource | Replacement |
|----------|-------------|
| `devskin_lake_kafka_cluster` | `devskin_instance` with `image_id = "tpl-204"` |
| `devskin_lake_kafka_topic` | Run `kafka-topics.sh` against the Kafka VM |
| `devskin_lake_airflow_dag` | `devskin_instance` with `image_id = "tpl-205"`, drop DAG files at `/opt/airflow/dags/` via SSH |

To migrate: `terraform state rm <resource>.<name>` followed by adding a
`devskin_instance` with the matching `image_id`.

## Data Sources

### devskin_instances

Lists existing compute instances, optionally filtered by region.

```hcl
data "devskin_instances" "all" {}

data "devskin_instances" "us_east" {
  region = "us-east-1"
}

output "instance_ids" {
  value = [for i in data.devskin_instances.all.instances : i.id]
}
```

## Full Stack Example

```hcl
variable "devskin_token" {
  type      = string
  sensitive = true
}

variable "region" {
  default = "us-east-1"
}

variable "environment" {
  default = "production"
}

provider "devskin" {
  token = var.devskin_token
}

# VPC
resource "devskin_vpc" "main" {
  name       = "${var.environment}-vpc"
  cidr_block = "10.0.0.0/16"
  region     = var.region
  enable_dns = true

  subnets {
    name       = "public"
    cidr_block = "10.0.1.0/24"
  }

  subnets {
    name       = "private"
    cidr_block = "10.0.2.0/24"
  }
}

# Web server instance
resource "devskin_instance" "web" {
  name          = "${var.environment}-web"
  instance_type = "ds.medium"
  image_id      = "ubuntu-22.04"
  region        = var.region
  vpc_id        = devskin_vpc.main.id
  subnet_id     = devskin_vpc.main.default_subnet_id
}

# Database
resource "devskin_database" "main" {
  name           = "${var.environment}-db"
  engine         = "postgres"
  instance_class = "db.medium"
  storage        = 100
  vpc_id         = devskin_vpc.main.id
}

# Kubernetes cluster
resource "devskin_k8s_cluster" "main" {
  name    = "${var.environment}-k8s"
  version = "1.29"
  region  = var.region

  node_groups {
    name          = "workers"
    instance_type = "ds.large"
    desired_size  = 3
  }
}

# Container service
resource "devskin_container_service" "api" {
  name          = "${var.environment}-api"
  image         = "myorg/api:latest"
  port          = 3000
  desired_count = 2

  environment = {
    DATABASE_URL = devskin_database.main.endpoint
    K8S_ENDPOINT = devskin_k8s_cluster.main.endpoint
  }
}

# Outputs
output "vpc_id" {
  value = devskin_vpc.main.id
}

output "web_public_ip" {
  value = devskin_instance.web.public_ip
}

output "database_endpoint" {
  value     = devskin_database.main.endpoint
  sensitive = true
}

output "k8s_endpoint" {
  value = devskin_k8s_cluster.main.endpoint
}

output "api_url" {
  value = devskin_container_service.api.url
}
```

## Development

```bash
# Run tests
go test ./...

# Build
go build -o terraform-provider-devskin

# Run acceptance tests (requires a valid API token)
DEVSKIN_TOKEN="your-token" go test ./... -v -acc
```
