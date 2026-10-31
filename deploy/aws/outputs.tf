output "cloudfront_domain" {
  description = "Public HTTPS origin; COMMITPOOL_BASE_URL is https:// plus this domain."
  value       = aws_cloudfront_distribution.app.domain_name
}

output "alb_dns" {
  description = "ALB DNS name. It only answers CloudFront, so use the CloudFront domain."
  value       = aws_lb.app.dns_name
}

output "ecr_repository_url" {
  description = "ECR repository to push the image to."
  value       = aws_ecr_repository.app.repository_url
}

output "secret_arns" {
  description = "Secrets Manager ARNs by environment variable."
  value       = local.secrets
}

output "rds_endpoint" {
  description = "RDS PostgreSQL host, reachable from the app tasks only."
  value       = aws_db_instance.main.address
}

output "log_group" {
  description = "CloudWatch log group of the app."
  value       = aws_cloudwatch_log_group.app.name
}

output "ecs_cluster" {
  description = "ECS cluster name, for aws ecs update-service."
  value       = aws_ecs_cluster.main.name
}

output "ecs_service" {
  description = "ECS service name, for aws ecs update-service."
  value       = aws_ecs_service.app.name
}
