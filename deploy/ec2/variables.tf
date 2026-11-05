variable "project" {
  description = "Name used for every resource and the ECR repository."
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
  default     = "us-east-1"
}

variable "availability_zone_suffix" {
  description = "Letter of the availability zone whose default subnet holds the VM."
  type        = string
  default     = "a"
}

variable "instance_type" {
  description = "EC2 instance type. The image is built for linux/arm64, so it must be a Graviton type."
  type        = string
  default     = "t4g.small"
}

variable "root_volume_gb" {
  description = "Size of the VM's gp3 root volume, which also holds the database."
  type        = number
  default     = 16
}

variable "parameter_prefix" {
  description = "SSM Parameter Store path that holds the demo's secrets."
  type        = string
  default     = "/commitpool"

  validation {
    condition     = can(regex("^/[A-Za-z0-9_./-]+[^/]$", var.parameter_prefix))
    error_message = "parameter_prefix must start with / and not end with one."
  }
}
