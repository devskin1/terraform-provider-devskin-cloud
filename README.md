# Terraform Provider for DevskinCloud

A Terraform provider for managing infrastructure on [DevskinCloud](https://cloud.devskin.com).

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
mkdir -p ~/.terraform.d/plugins/registry.terraform.io/devskin1/devskin-cloud/0.1.0/linux_amd64/
cp terraform-provider-devskin ~/.terraform.d/plugins/registry.terraform.io/devskin1/devskin-cloud/0.1.0/linux_amd64/
```

## Provider Configuration

```hcl
terraform {
  required_providers {
    devskin = {
      source  = "devskin1/devskin-cloud"
      version = "~> 0.1"
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
}

# Deploy from Git repository
resource "devskin_container_service" "frontend" {
  name              = "my-frontend"
  source_repository = "https://github.com/myorg/frontend.git"
  port              = 80
  desired_count     = 2
}
```

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
