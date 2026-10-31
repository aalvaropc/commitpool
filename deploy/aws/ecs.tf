locals {
  image     = var.image != "" ? var.image : "${aws_ecr_repository.app.repository_url}:latest"
  container = "app"

  environment = {
    PORT                              = "8080"
    COMMITPOOL_BASE_URL               = "https://${aws_cloudfront_distribution.app.domain_name}"
    PAYPAL_BASE_URL                   = "https://api-m.sandbox.paypal.com"
    COMMITPOOL_AI_PROVIDER            = "bedrock"
    AWS_REGION                        = var.region
    COMMITPOOL_BEDROCK_MODEL          = var.bedrock_model
    COMMITPOOL_BEDROCK_FALLBACK_MODEL = var.bedrock_fallback_model
    COMMITPOOL_BEDROCK_EFFORT         = var.bedrock_effort
    COMMITPOOL_DEMO_MODE              = "1"
    COMMITPOOL_SANDBOX_MOCKS          = "1"
    COMMITPOOL_SEAT_TTL_MIN           = tostring(var.seat_ttl_min)
  }

  secrets = {
    DATABASE_URL         = aws_secretsmanager_secret.database_url.arn
    PAYPAL_CLIENT_ID     = aws_secretsmanager_secret.paypal["client_id"].arn
    PAYPAL_CLIENT_SECRET = aws_secretsmanager_secret.paypal["client_secret"].arn
    PAYPAL_WEBHOOK_ID    = aws_secretsmanager_secret.paypal["webhook_id"].arn
  }
}

resource "aws_cloudwatch_log_group" "app" {
  name              = "/ecs/${var.project}"
  retention_in_days = 14
}

resource "aws_ecs_cluster" "main" {
  name = var.project

  setting {
    name  = "containerInsights"
    value = "disabled"
  }
}

resource "aws_ecs_task_definition" "app" {
  family                   = var.project
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = var.task_cpu
  memory                   = var.task_memory
  execution_role_arn       = aws_iam_role.execution.arn
  task_role_arn            = aws_iam_role.task.arn

  runtime_platform {
    operating_system_family = "LINUX"
    cpu_architecture        = "X86_64"
  }

  container_definitions = jsonencode([{
    name        = local.container
    image       = local.image
    essential   = true
    stopTimeout = 30

    portMappings = [{
      containerPort = 8080
      protocol      = "tcp"
    }]

    environment = [for k, v in local.environment : { name = k, value = v }]
    secrets     = [for k, v in local.secrets : { name = k, valueFrom = v }]

    logConfiguration = {
      logDriver = "awslogs"
      options = {
        awslogs-group         = aws_cloudwatch_log_group.app.name
        awslogs-region        = var.region
        awslogs-stream-prefix = "app"
      }
    }
  }])
}

resource "aws_ecs_service" "app" {
  name             = var.project
  cluster          = aws_ecs_cluster.main.id
  task_definition  = aws_ecs_task_definition.app.arn
  desired_count    = 1
  launch_type      = "FARGATE"
  platform_version = "LATEST"

  health_check_grace_period_seconds = 60

  deployment_circuit_breaker {
    enable   = true
    rollback = true
  }

  network_configuration {
    subnets          = aws_subnet.public[*].id
    security_groups  = [aws_security_group.tasks.id]
    assign_public_ip = true
  }

  load_balancer {
    target_group_arn = aws_lb_target_group.app.arn
    container_name   = local.container
    container_port   = 8080
  }

  depends_on = [aws_lb_listener.http, aws_iam_role_policy.execution, aws_secretsmanager_secret_version.database_url]
}
