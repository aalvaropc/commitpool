output "public_url" {
  description = "The demo's HTTPS origin, the value of COMMITPOOL_BASE_URL."
  value       = "https://${local.domain}"
}

output "instance_id" {
  description = "The VM, for SSM Run Command and Session Manager."
  value       = aws_instance.app.id
}

output "repository_url" {
  description = "ECR repository the VM pulls the app image from."
  value       = aws_ecr_repository.app.repository_url
}

output "region" {
  description = "Region of the stack."
  value       = var.region
}

output "parameter_prefix" {
  description = "SSM path of the PayPal and database secrets."
  value       = var.parameter_prefix
}
