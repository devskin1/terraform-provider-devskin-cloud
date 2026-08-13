terraform {
  required_providers {
    kubmix = {
      source  = "devskin1/devskin-cloud"
      version = "~> 1.0"
    }
  }
}

# ---------------------------------------------------------------------------
# Variables
# ---------------------------------------------------------------------------

variable "kubmix_token" {
  type        = string
  sensitive   = true
  description = "Kubmix Cloud API token"
}

variable "region" {
  type        = string
  default     = "us-east-1"
  description = "Region to deploy resources in"
}

variable "environment" {
  type        = string
  default     = "staging"
  description = "Environment name (staging, production, etc.)"
}

variable "k8s_version" {
  type        = string
  default     = "1.29"
  description = "Kubernetes version for the cluster"
}

variable "db_storage_gb" {
  type        = number
  default     = 50
  description = "Database storage in GB"
}

variable "flux_api_key" {
  type        = string
  sensitive   = true
  default     = ""
  description = "Flux observability API key (from admin-flux.devskin.com). Optional — leave empty to skip auto-enrollment."
}

# ---------------------------------------------------------------------------
# Provider
# ---------------------------------------------------------------------------

provider "kubmix" {
  api_url = "https://cloud-api.kubmix.com/api"
  token   = var.kubmix_token
}

# ---------------------------------------------------------------------------
# VPC
# ---------------------------------------------------------------------------

resource "kubmix_vpc" "main" {
  name        = "${var.environment}-vpc"
  cidr_block  = "10.0.0.0/16"
  region      = var.region
  enable_dns  = true
  enable_ipv6 = false

  subnets {
    name       = "public-a"
    cidr_block = "10.0.1.0/24"
    zone       = "${var.region}a"
  }

  subnets {
    name       = "private-a"
    cidr_block = "10.0.10.0/24"
    zone       = "${var.region}a"
  }

  subnets {
    name       = "private-b"
    cidr_block = "10.0.11.0/24"
    zone       = "${var.region}b"
  }
}

# ---------------------------------------------------------------------------
# Compute Instance - Bastion / Jump Host
# ---------------------------------------------------------------------------

resource "kubmix_instance" "bastion" {
  name          = "${var.environment}-bastion"
  instance_type = "c5.large"
  image_id      = "tpl-9100"
  region        = var.region
  vpc_id        = kubmix_vpc.main.id
  subnet_id     = kubmix_vpc.main.default_subnet_id
  ipv6          = false

  # Optional Flux observability — only consumed at create time. Comment
  # out (or omit `monitoring_enrollment`) to skip auto-enrollment.
  # monitoring_enrollment {
  #   enabled = true
  #   api_key = var.flux_api_key
  # }
}

# ---------------------------------------------------------------------------
# Data-Platform VMs (replace deprecated managed Lakehouse paths)
# ---------------------------------------------------------------------------

# JupyterLab — replaces managed Lakehouse Notebooks (mp-030, tpl-9203).
resource "kubmix_instance" "jupyter" {
  name          = "${var.environment}-jupyter"
  instance_type = "c5.xlarge"
  image_id      = "tpl-9203"
  region        = var.region
  vpc_id        = kubmix_vpc.main.id
  subnet_id     = kubmix_vpc.main.default_subnet_id
}

# Apache Kafka — replaces deprecated devskin_lake_kafka_cluster (mp-040, tpl-9201).
resource "kubmix_instance" "kafka" {
  name          = "${var.environment}-kafka"
  instance_type = "c5.xlarge"
  image_id      = "tpl-9201"
  region        = var.region
  vpc_id        = kubmix_vpc.main.id
  subnet_id     = kubmix_vpc.main.default_subnet_id
}

# Apache Airflow — replaces deprecated devskin_lake_airflow_dag (mp-050, tpl-9202).
resource "kubmix_instance" "airflow" {
  name          = "${var.environment}-airflow"
  instance_type = "c5.xlarge"
  image_id      = "tpl-9202"
  region        = var.region
  vpc_id        = kubmix_vpc.main.id
  subnet_id     = kubmix_vpc.main.default_subnet_id
}

# ---------------------------------------------------------------------------
# Lakehouse — managed catalog (Iceberg + Polaris + Trino)
# ---------------------------------------------------------------------------

resource "kubmix_lake_database" "bronze" {
  name        = "${var.environment}_bronze"
  description = "Raw landing zone — events, click-stream, replays."
}

resource "kubmix_lake_table" "orders" {
  database_id = kubmix_lake_database.bronze.id
  name        = "orders"

  columns = [
    { name = "id",         type = "bigint" },
    { name = "amount",     type = "double" },
    { name = "currency",   type = "varchar" },
    { name = "created_at", type = "timestamp" },
    { name = "user_id",    type = "varchar" },
  ]
}

# ---------------------------------------------------------------------------
# Database
# ---------------------------------------------------------------------------

resource "kubmix_database" "postgres" {
  name           = "${var.environment}-postgres"
  engine         = "postgres"
  instance_class = "db.medium"
  storage        = var.db_storage_gb
  vpc_id         = kubmix_vpc.main.id
}

resource "kubmix_database" "redis" {
  name           = "${var.environment}-redis"
  engine         = "redis"
  instance_class = "db.small"
  storage        = 10
  vpc_id         = kubmix_vpc.main.id
}

# ---------------------------------------------------------------------------
# Kubernetes Cluster
# ---------------------------------------------------------------------------

resource "kubmix_k8s_cluster" "main" {
  name    = "${var.environment}-cluster"
  version = var.k8s_version
  region  = var.region
  vpc_id  = kubmix_vpc.main.id

  node_groups {
    name          = "system"
    instance_type = "c5.xlarge"
    desired_size  = 2
  }

  node_groups {
    name          = "workers"
    instance_type = "c5.2xlarge"
    desired_size  = 3
  }
}

# ---------------------------------------------------------------------------
# Container Services
# ---------------------------------------------------------------------------

resource "kubmix_container_service" "api" {
  name               = "${var.environment}-api"
  cluster_id         = kubmix_container_cluster.main.id
  task_definition_id = kubmix_task_definition.api.id
  image              = "myorg/backend-api:latest"
  port               = 3000
  desired_count      = 2

  environment = {
    NODE_ENV     = var.environment
    DATABASE_URL = "postgres://app:secret@${kubmix_database.postgres.endpoint}:${kubmix_database.postgres.port}/app"
    REDIS_URL    = "redis://${kubmix_database.redis.endpoint}:${kubmix_database.redis.port}"
  }

  # Optional Flux observability — only consumed at create time.
  # monitoring {
  #   enabled = true
  #   api_key = var.flux_api_key
  # }
}

resource "kubmix_container_service" "frontend" {
  name              = "${var.environment}-frontend"
  cluster_id        = kubmix_container_cluster.main.id
  source_repository = "https://github.com/myorg/frontend.git"
  port              = 80
  desired_count     = 2

  environment = {
    API_URL = kubmix_container_service.api.url
  }
}

# ---------------------------------------------------------------------------
# Container Cluster
# ---------------------------------------------------------------------------

resource "kubmix_container_cluster" "main" {
  name              = "${var.environment}-ecs-cluster"
  vpc_id            = kubmix_vpc.main.id
  region            = var.region
  capacity_provider = "FARGATE"
}

# ---------------------------------------------------------------------------
# Task Definitions
# ---------------------------------------------------------------------------

resource "kubmix_task_definition" "api" {
  family = "${var.environment}-api-task"
  image  = "myorg/backend-api:latest"
  cpu    = 512
  memory = 1024

  port_mappings {
    container_port = 3000
    host_port      = 3000
    protocol       = "tcp"
  }

  environment = {
    NODE_ENV     = var.environment
    DATABASE_URL = "postgres://app:secret@${kubmix_database.postgres.endpoint}:${kubmix_database.postgres.port}/app"
  }
}

resource "kubmix_task_definition" "worker" {
  family            = "${var.environment}-worker-task"
  image             = "myorg/worker:latest"
  cpu               = 256
  memory            = 512
  source_repository = "https://github.com/myorg/worker.git"
  source_branch     = "main"

  environment = {
    NODE_ENV  = var.environment
    REDIS_URL = "redis://${kubmix_database.redis.endpoint}:${kubmix_database.redis.port}"
  }
}

# ---------------------------------------------------------------------------
# Data Source - List all instances
# ---------------------------------------------------------------------------

data "kubmix_instances" "current_region" {
  region = var.region
}

# ---------------------------------------------------------------------------
# Outputs
# ---------------------------------------------------------------------------

output "vpc_id" {
  description = "The VPC ID"
  value       = kubmix_vpc.main.id
}

output "bastion_public_ip" {
  description = "Public IP of the bastion host"
  value       = kubmix_instance.bastion.public_ip
}

output "bastion_private_ip" {
  description = "Private IP of the bastion host"
  value       = kubmix_instance.bastion.private_ip
}

output "postgres_endpoint" {
  description = "PostgreSQL connection endpoint"
  value       = kubmix_database.postgres.endpoint
  sensitive   = true
}

output "redis_endpoint" {
  description = "Redis connection endpoint"
  value       = kubmix_database.redis.endpoint
  sensitive   = true
}

output "k8s_endpoint" {
  description = "Kubernetes API server endpoint"
  value       = kubmix_k8s_cluster.main.endpoint
}

output "api_url" {
  description = "URL of the API container service"
  value       = kubmix_container_service.api.url
}

output "frontend_url" {
  description = "URL of the frontend container service"
  value       = kubmix_container_service.frontend.url
}

output "container_cluster_id" {
  description = "Container cluster ID"
  value       = kubmix_container_cluster.main.id
}

output "api_task_definition_id" {
  description = "API task definition ID"
  value       = kubmix_task_definition.api.id
}

output "instance_count" {
  description = "Number of instances in the current region"
  value       = length(data.kubmix_instances.current_region.instances)
}
