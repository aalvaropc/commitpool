variable "project" {
  description = "Name used for every resource, the ECR repository and the secret prefix."
  type        = string
  default     = "commitpool"

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{1,30}$", var.project))
    error_message = "project must be 2-31 lowercase letters, digits or hyphens, starting with a letter."
  }
}

variable "region" {
  description = "AWS region for every resource and for the Bedrock calls."
  type        = string
  default     = "us-east-2"
}

variable "image" {
  description = "Full container image URI with tag. Empty uses the created ECR repository with the tag latest."
  type        = string
  default     = ""
}

variable "bedrock_model" {
  description = "Bedrock model id (COMMITPOOL_BEDROCK_MODEL)."
  type        = string
  default     = "anthropic.claude-opus-5-5"
}

variable "bedrock_fallback_model" {
  description = "Bedrock model that answers when the main model declines a request, or none (COMMITPOOL_BEDROCK_FALLBACK_MODEL)."
  type        = string
  default     = "anthropic.claude-opus-4-8"
}

variable "bedrock_effort" {
  description = "Effort for every Bedrock call: low, medium, high, xhigh, max or none (COMMITPOOL_BEDROCK_EFFORT)."
  type        = string
  default     = "low"

  validation {
    condition     = contains(["low", "medium", "high", "xhigh", "max", "none"], var.bedrock_effort)
    error_message = "bedrock_effort must be low, medium, high, xhigh, max or none."
  }
}

variable "seat_ttl_min" {
  description = "Minutes a reserved seat is held while the buyer approves the payment (COMMITPOOL_SEAT_TTL_MIN)."
  type        = number
  default     = 30

  validation {
    condition     = var.seat_ttl_min >= 1 && floor(var.seat_ttl_min) == var.seat_ttl_min
    error_message = "seat_ttl_min must be a positive whole number of minutes."
  }
}

variable "db_instance_class" {
  description = "RDS instance class for PostgreSQL."
  type        = string
  default     = "db.t4g.micro"
}

variable "task_cpu" {
  description = "Fargate task CPU units."
  type        = number
  default     = 512
}

variable "task_memory" {
  description = "Fargate task memory in MiB."
  type        = number
  default     = 1024
}
