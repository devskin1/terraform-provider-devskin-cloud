terraform {
  required_providers {
    devskin = {
      source  = "devskin1/devskin-cloud"
      version = "~> 0.1"
    }
  }
}

# ---------------------------------------------------------------------------
# Variables
# ---------------------------------------------------------------------------

variable "devskin_token" {
  type        = string
  sensitive   = true
  description = "DevskinCloud API token"
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

# ---------------------------------------------------------------------------
# Provider
# ---------------------------------------------------------------------------

provider "devskin" {
  api_url = "https://cloud-api.devskin.com/api"
  token   = var.devskin_token
}

# ---------------------------------------------------------------------------
# VPC
# ---------------------------------------------------------------------------

resource "devskin_vpc" "main" {
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

resource "devskin_instance" "bastion" {
  name          = "${var.environment}-bastion"
  instance_type = "ds.small"
  image_id      = "ubuntu-22.04"
  region        = var.region
  vpc_id        = devskin_vpc.main.id
  subnet_id     = devskin_vpc.main.default_subnet_id
  ipv6          = false
}

# ---------------------------------------------------------------------------
# Database
# ---------------------------------------------------------------------------

resource "devskin_database" "postgres" {
  name           = "${var.environment}-postgres"
  engine         = "postgres"
  instance_class = "db.medium"
  storage        = var.db_storage_gb
  vpc_id         = devskin_vpc.main.id
}

resource "devskin_database" "redis" {
  name           = "${var.environment}-redis"
  engine         = "redis"
  instance_class = "db.small"
  storage        = 10
  vpc_id         = devskin_vpc.main.id
}

# ---------------------------------------------------------------------------
# Kubernetes Cluster
# ---------------------------------------------------------------------------

resource "devskin_k8s_cluster" "main" {
  name    = "${var.environment}-cluster"
  version = var.k8s_version
  region  = var.region

  node_groups {
    name          = "system"
    instance_type = "ds.medium"
    desired_size  = 2
  }

  node_groups {
    name          = "workers"
    instance_type = "ds.large"
    desired_size  = 3
  }
}

# ---------------------------------------------------------------------------
# Container Services
# ---------------------------------------------------------------------------

resource "devskin_container_service" "api" {
  name          = "${var.environment}-api"
  image         = "myorg/backend-api:latest"
  port          = 3000
  desired_count = 2

  environment = {
    NODE_ENV     = var.environment
    DATABASE_URL = "postgres://app:secret@${devskin_database.postgres.endpoint}:${devskin_database.postgres.port}/app"
    REDIS_URL    = "redis://${devskin_database.redis.endpoint}:${devskin_database.redis.port}"
  }
}

resource "devskin_container_service" "frontend" {
  name              = "${var.environment}-frontend"
  source_repository = "https://github.com/myorg/frontend.git"
  port              = 80
  desired_count     = 2

  environment = {
    API_URL = devskin_container_service.api.url
  }
}

# ---------------------------------------------------------------------------
# Data Source - List all instances
# ---------------------------------------------------------------------------

data "devskin_instances" "current_region" {
  region = var.region
}

# ---------------------------------------------------------------------------
# Outputs
# ---------------------------------------------------------------------------

output "vpc_id" {
  description = "The VPC ID"
  value       = devskin_vpc.main.id
}

output "bastion_public_ip" {
  description = "Public IP of the bastion host"
  value       = devskin_instance.bastion.public_ip
}

output "bastion_private_ip" {
  description = "Private IP of the bastion host"
  value       = devskin_instance.bastion.private_ip
}

output "postgres_endpoint" {
  description = "PostgreSQL connection endpoint"
  value       = devskin_database.postgres.endpoint
  sensitive   = true
}

output "redis_endpoint" {
  description = "Redis connection endpoint"
  value       = devskin_database.redis.endpoint
  sensitive   = true
}

output "k8s_endpoint" {
  description = "Kubernetes API server endpoint"
  value       = devskin_k8s_cluster.main.endpoint
}

output "api_url" {
  description = "URL of the API container service"
  value       = devskin_container_service.api.url
}

output "frontend_url" {
  description = "URL of the frontend container service"
  value       = devskin_container_service.frontend.url
}

output "instance_count" {
  description = "Number of instances in the current region"
  value       = length(data.devskin_instances.current_region.instances)
}
