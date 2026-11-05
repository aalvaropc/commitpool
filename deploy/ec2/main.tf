data "aws_subnet" "default" {
  availability_zone = "${var.region}${var.availability_zone_suffix}"
  default_for_az    = true
}

data "aws_ssm_parameter" "al2023_arm64" {
  name = "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-arm64"
}

locals {
  domain = "${replace(aws_eip.app.public_ip, ".", "-")}.sslip.io"
}

resource "aws_ecr_repository" "app" {
  name                 = var.project
  image_tag_mutability = "MUTABLE"
  force_delete         = true
}

resource "aws_ecr_lifecycle_policy" "app" {
  repository = aws_ecr_repository.app.name
  policy = jsonencode({
    rules = [{
      rulePriority = 1
      description  = "Keep the last ten images"
      selection    = { tagStatus = "any", countType = "imageCountMoreThan", countNumber = 10 }
      action       = { type = "expire" }
    }]
  })
}

resource "random_password" "postgres" {
  length  = 32
  special = false
}

resource "aws_ssm_parameter" "postgres_password" {
  name  = "${var.parameter_prefix}/postgres_password"
  type  = "SecureString"
  value = random_password.postgres.result
}

resource "aws_security_group" "app" {
  name        = "${var.project}-vm"
  description = "HTTP and HTTPS to the demo VM; no SSH (use SSM)."
  vpc_id      = data.aws_subnet.default.vpc_id

  ingress {
    description = "HTTP, for the ACME challenge and the redirect to HTTPS"
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    description = "HTTPS"
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

data "aws_iam_policy_document" "ec2_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ec2.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "app" {
  name               = "${var.project}-vm"
  assume_role_policy = data.aws_iam_policy_document.ec2_assume.json
}

data "aws_iam_policy_document" "app" {
  statement {
    sid     = "BedrockInvokeAnthropic"
    actions = ["bedrock:InvokeModel", "bedrock:InvokeModelWithResponseStream"]
    resources = [
      "arn:${data.aws_partition.current.partition}:bedrock:*::foundation-model/anthropic.*",
      "arn:${data.aws_partition.current.partition}:bedrock:*:${data.aws_caller_identity.current.account_id}:inference-profile/*",
    ]
  }

  statement {
    sid       = "EcrAuth"
    actions   = ["ecr:GetAuthorizationToken"]
    resources = ["*"]
  }

  statement {
    sid       = "EcrPull"
    actions   = ["ecr:BatchCheckLayerAvailability", "ecr:BatchGetImage", "ecr:GetDownloadUrlForLayer"]
    resources = [aws_ecr_repository.app.arn]
  }

  statement {
    sid       = "ReadSecrets"
    actions   = ["ssm:GetParameter"]
    resources = ["arn:${data.aws_partition.current.partition}:ssm:${var.region}:${data.aws_caller_identity.current.account_id}:parameter${var.parameter_prefix}/*"]
  }
}

resource "aws_iam_role_policy" "app" {
  name   = "${var.project}-vm"
  role   = aws_iam_role.app.id
  policy = data.aws_iam_policy_document.app.json
}

resource "aws_iam_role_policy_attachment" "ssm_core" {
  role       = aws_iam_role.app.name
  policy_arn = "arn:${data.aws_partition.current.partition}:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_instance_profile" "app" {
  name = "${var.project}-vm"
  role = aws_iam_role.app.name
}

resource "aws_eip" "app" {
  domain = "vpc"
  tags   = { Name = "${var.project}-vm" }
}

resource "aws_instance" "app" {
  ami                    = data.aws_ssm_parameter.al2023_arm64.insecure_value
  instance_type          = var.instance_type
  subnet_id              = data.aws_subnet.default.id
  vpc_security_group_ids = [aws_security_group.app.id]
  iam_instance_profile   = aws_iam_instance_profile.app.name

  user_data = templatefile("${path.module}/user-data.sh.tftpl", {
    region           = var.region
    repository       = aws_ecr_repository.app.repository_url
    domain           = local.domain
    parameter_prefix = var.parameter_prefix
    deploy_script    = base64encode(file("${path.module}/deploy.sh"))
  })
  user_data_replace_on_change = true

  metadata_options {
    http_endpoint               = "enabled"
    http_tokens                 = "required"
    http_put_response_hop_limit = 2
  }

  root_block_device {
    volume_type = "gp3"
    volume_size = var.root_volume_gb
    encrypted   = true
  }

  tags = { Name = "${var.project}-vm" }

  lifecycle {
    ignore_changes = [ami]
  }
}

resource "aws_eip_association" "app" {
  instance_id   = aws_instance.app.id
  allocation_id = aws_eip.app.id
}
