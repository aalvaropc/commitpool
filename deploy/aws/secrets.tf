locals {
  paypal_secrets = {
    client_id     = "paypal-client-id"
    client_secret = "paypal-client-secret"
    webhook_id    = "paypal-webhook-id"
  }
}

resource "aws_secretsmanager_secret" "database_url" {
  name                    = "${var.project}/database-url"
  description             = "DATABASE_URL for ${var.project}, written by Terraform."
  recovery_window_in_days = 0
}

resource "aws_secretsmanager_secret_version" "database_url" {
  secret_id     = aws_secretsmanager_secret.database_url.id
  secret_string = local.database_url
}

resource "aws_secretsmanager_secret" "paypal" {
  for_each = local.paypal_secrets

  name                    = "${var.project}/${each.value}"
  description             = "${upper(replace(each.value, "-", "_"))} for ${var.project}, put by hand."
  recovery_window_in_days = 0
}

resource "aws_secretsmanager_secret_version" "paypal" {
  for_each = local.paypal_secrets

  secret_id = aws_secretsmanager_secret.paypal[each.key].id

  # A single space is the placeholder: the app trims every variable, so it
  # reads the value as unset until the real one is put by hand.
  secret_string = " "

  lifecycle {
    ignore_changes = [secret_string]
  }
}
