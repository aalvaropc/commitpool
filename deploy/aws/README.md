# Deploying CommitPool on AWS

This Terraform runs CommitPool as one container on ECS Fargate behind an
Application Load Balancer and CloudFront, with PostgreSQL 16 on RDS, the
PayPal credentials and the database URL in Secrets Manager, and Claude on
Amazon Bedrock as the AI provider. The public URL is the CloudFront domain,
served over HTTPS with CloudFront's default certificate.

```
buyer ──HTTPS──▶ CloudFront ──HTTP:80──▶ ALB ──:8080──▶ Fargate task ──:5432──▶ RDS
                 (default cert)          (CloudFront    (public subnet,        (private subnets,
                                          prefix list    public IP, no NAT)     no internet route)
                                          only)               │
                                                              └──HTTPS──▶ PayPal Sandbox, Bedrock,
                                                                          ECR, Secrets Manager, Logs
```

## Prerequisites

- AWS CLI v2 with credentials and a default region for the account you deploy to
  (`aws sts get-caller-identity` works).
- Terraform 1.15.
- Docker with Buildx, to build a `linux/amd64` image.
- Bedrock model access for Claude enabled in the chosen region, for the main
  model and the fallback model (`bedrock_model`, `bedrock_fallback_model`).
- A PayPal Sandbox REST app: its client id and secret.
- `curl` and `jq`, for `scripts/register-webhook.sh`.

## What it creates

| File | Resources |
|---|---|
| `network.tf` | VPC `10.40.0.0/16` in two AZs: two public subnets (ALB and tasks) with an internet gateway, two private subnets (RDS) without an internet route; three security groups chained CloudFront → ALB → tasks → RDS |
| `alb.tf` | Internet-facing ALB, HTTP listener on port 80, target group on 8080 with health check `GET /healthz` and a 30 s deregistration delay |
| `cloudfront.tf` | Distribution with the default certificate, `redirect-to-https`, all methods, managed `CachingDisabled` cache policy and `AllViewer` origin request policy, HTTP/2, `http-only` to the ALB |
| `ecs.tf` | Fargate cluster, task definition (0.5 vCPU, 1 GB, `LINUX/X86_64`, `stopTimeout` 30 s), one service with `desired_count = 1`, log group with 14-day retention |
| `database.tf` | RDS PostgreSQL 16 `db.t4g.micro`, 20 GB gp3, encrypted, not public, single AZ, 7-day backups, random master password |
| `secrets.tf` | `<project>/database-url` (written by Terraform), `<project>/paypal-client-id`, `<project>/paypal-client-secret`, `<project>/paypal-webhook-id` (put by hand) |
| `ecr.tf` | Repository `<project>`, scan on push, keeps the last 10 images |
| `iam.tf` | Execution role (pull from the repository, write the log group, read the four secrets) and task role (Bedrock) |

The container gets `PORT=8080`, `COMMITPOOL_BASE_URL=https://<cloudfront domain>`,
`PAYPAL_BASE_URL=https://api-m.sandbox.paypal.com`, `COMMITPOOL_AI_PROVIDER=bedrock`,
`AWS_REGION`, `COMMITPOOL_BEDROCK_MODEL`, `COMMITPOOL_BEDROCK_FALLBACK_MODEL`,
`COMMITPOOL_BEDROCK_EFFORT`, `COMMITPOOL_DEMO_MODE=1`, `COMMITPOOL_SANDBOX_MOCKS=1`
and `COMMITPOOL_SEAT_TTL_MIN` as plain environment variables, and `DATABASE_URL`,
`PAYPAL_CLIENT_ID`, `PAYPAL_CLIENT_SECRET` and `PAYPAL_WEBHOOK_ID` from Secrets Manager.

### Design decisions

- **No NAT gateway.** The tasks run in the public subnets with a public IP, so
  they reach PayPal, Bedrock, ECR and Secrets Manager directly; their security
  group only accepts port 8080 from the ALB. RDS stays in private subnets with
  no route out.
- **CloudFront for HTTPS.** There is no custom domain, so CloudFront's default
  `*.cloudfront.net` certificate gives the HTTPS origin that PayPal's return,
  cancel and webhook URLs need. CloudFront talks to the ALB over HTTP inside
  AWS; the ALB only accepts the CloudFront origin-facing prefix list.
- **`AllViewer` origin request policy.** It forwards every viewer header,
  including PayPal's webhook signature headers and the viewer `Host`
  (the CloudFront domain). The ALB has a single forward action and does not
  route on `Host`, and the app never reads it (its links come from
  `COMMITPOOL_BASE_URL`), so `AllViewerExceptHostHeader` is not needed.
- **Bedrock permissions.** The app calls Claude through the Bedrock Mantle
  endpoint with the task role's credentials. The task role allows
  `bedrock-mantle:CreateInference` on `*` (no narrower resource pattern for
  that service is certain) and `bedrock:InvokeModel` and
  `bedrock:InvokeModelWithResponseStream` on Anthropic foundation models in any
  region (`foundation-model/anthropic.*`, so cross-region routing works) and on
  the account's inference profiles. The only other wildcard is
  `ecr:GetAuthorizationToken`, which has no resource-level scope.
- **Placeholder secrets.** The three PayPal secrets are created with a single
  space as their value, and Terraform ignores later changes to them. The app
  trims every variable, so a placeholder reads as unset: it refuses to start
  without real PayPal credentials, and `/paypal/webhook` answers 503 until the
  webhook id is put.
- **Demo-friendly teardown.** Secrets are deleted without a recovery window,
  the ECR repository is deleted with its images, and RDS skips the final
  snapshot and has deletion protection off, so `terraform destroy` removes
  everything and a later `apply` can reuse the same names. Destroying loses
  the database.
- **Rolling deploys.** The service keeps the default 100 %/200 % deployment
  limits, so a new task starts and passes the health check before the old one
  stops (the app's job queue and migrations are safe with two tasks at once),
  and the deployment circuit breaker rolls back a task that never gets healthy.

## Estimated cost while it is on

Approximate on-demand prices in `us-east-2`, per month (730 hours), before
free tier and without Bedrock tokens, which are billed per use:

| Item | US$/month |
|---|---|
| ALB (hours + minimal LCUs) | 17–18 |
| Fargate, 0.5 vCPU / 1 GB, one task | 18 |
| Public IPv4 addresses (ALB in 2 AZs + the task, US$3.65 each) | 11 |
| RDS `db.t4g.micro`, single AZ | 12 |
| RDS 20 GB gp3 storage | 2–3 |
| Secrets Manager, 4 secrets | 1.6 |
| CloudFront, CloudWatch Logs, ECR storage | 1–2 |
| NAT gateway | 0 (none) |
| **Total** | **≈ 62–66** |

`terraform destroy` stops every charge. Stopping is not supported in place;
destroy and re-apply instead (the database is lost).

## First deploy, in order

All commands run from the repository root. Pick the region once:

```sh
export AWS_REGION=us-east-2
```

1. **Create the infrastructure.** Optionally copy
   `deploy/aws/terraform.tfvars.example` to `deploy/aws/terraform.tfvars` and
   adjust it.

   ```sh
   terraform -chdir=deploy/aws init
   terraform -chdir=deploy/aws plan -var region="$AWS_REGION"
   terraform -chdir=deploy/aws apply -var region="$AWS_REGION"
   ```

   CloudFront takes several minutes. Until steps 2–4 are done the service's
   task cannot start (no image yet, placeholder secrets); that is expected.

2. **Build and push the image.**

   ```sh
   repo="$(terraform -chdir=deploy/aws output -raw ecr_repository_url)"
   tag="$(git rev-parse --short HEAD)"
   aws ecr get-login-password --region "$AWS_REGION" | docker login --username AWS --password-stdin "${repo%%/*}"
   docker build --platform linux/amd64 -f deploy/Dockerfile -t "$repo:$tag" .
   docker push "$repo:$tag"
   terraform -chdir=deploy/aws apply -var region="$AWS_REGION" -var image="$repo:$tag"
   ```

   Every later `apply` needs the same `image`; put it in
   `deploy/aws/terraform.tfvars` (`image = "<repo>:<tag>"`) so a plain
   `apply` does not go back to `:latest`.

3. **Put the PayPal credentials.** Read them into shell variables without
   echoing them, then pass them on stdin so they never appear in a command
   line:

   ```sh
   printf 'PayPal client id: ' && read -rs PAYPAL_CLIENT_ID && echo
   printf 'PayPal client secret: ' && read -rs PAYPAL_CLIENT_SECRET && echo
   export PAYPAL_CLIENT_ID PAYPAL_CLIENT_SECRET
   printf %s "$PAYPAL_CLIENT_ID" | aws secretsmanager put-secret-value --region "$AWS_REGION" \
     --secret-id commitpool/paypal-client-id --secret-string file:///dev/stdin
   printf %s "$PAYPAL_CLIENT_SECRET" | aws secretsmanager put-secret-value --region "$AWS_REGION" \
     --secret-id commitpool/paypal-client-secret --secret-string file:///dev/stdin
   ```

   (Replace `commitpool/` if you changed `project`.)

4. **Deploy the task with the secrets.**

   ```sh
   cluster="$(terraform -chdir=deploy/aws output -raw ecs_cluster)"
   service="$(terraform -chdir=deploy/aws output -raw ecs_service)"
   aws ecs update-service --region "$AWS_REGION" --cluster "$cluster" --service "$service" --force-new-deployment
   aws ecs wait services-stable --region "$AWS_REGION" --cluster "$cluster" --services "$service"
   ```

5. **Check the health endpoint.**

   ```sh
   base_url="https://$(terraform -chdir=deploy/aws output -raw cloudfront_domain)"
   curl -fsS "$base_url/healthz"    # {"ok":true}
   ```

   If it fails, read the logs:
   `aws logs tail "$(terraform -chdir=deploy/aws output -raw log_group)" --region "$AWS_REGION" --since 15m`.

6. **Register the PayPal webhook.** With `PAYPAL_CLIENT_ID` and
   `PAYPAL_CLIENT_SECRET` still exported:

   ```sh
   PAYPAL_WEBHOOK_ID="$(scripts/register-webhook.sh "$base_url")"
   ```

   The script subscribes `<base_url>/paypal/webhook` to the seven payment
   events the app handles and prints only the webhook id; running it again
   reuses the existing webhook.

7. **Put the webhook id.**

   ```sh
   printf %s "$PAYPAL_WEBHOOK_ID" | aws secretsmanager put-secret-value --region "$AWS_REGION" \
     --secret-id commitpool/paypal-webhook-id --secret-string file:///dev/stdin
   ```

8. **Deploy again** so the task reads the webhook id (step 4's commands), then
   `unset PAYPAL_CLIENT_ID PAYPAL_CLIENT_SECRET`.

## Redeploying a new version

```sh
tag="$(git rev-parse --short HEAD)"
docker build --platform linux/amd64 -f deploy/Dockerfile -t "$repo:$tag" .
docker push "$repo:$tag"
terraform -chdir=deploy/aws apply -var region="$AWS_REGION" -var image="$repo:$tag"
```

The apply registers a new task definition and the service rolls to it. After
changing only a secret, use `aws ecs update-service --force-new-deployment`
(step 4) instead.

## Teardown

```sh
terraform -chdir=deploy/aws destroy -var region="$AWS_REGION"
```

This deletes the database without a snapshot, the secrets without a recovery
window and the image repository with its images. The PayPal webhook stays
registered in the PayPal app; delete it from the PayPal developer dashboard
if the deployment will not come back at the same CloudFront domain.

## State and secrets

State is local (`deploy/aws/terraform.tfstate`), and it holds the RDS master
password and the full `DATABASE_URL` in plain text: keep it on a private
machine and never commit it. `.gitignore` excludes `deploy/aws/.terraform/`,
`deploy/aws/*.tfstate*` and `deploy/aws/*.tfvars`; `.terraform.lock.hcl` and
`terraform.tfvars.example` are committed. The PayPal secrets never pass
through Terraform: they are put with the AWS CLI and Terraform ignores their
values.

## Not included

- A custom domain and its certificate (CloudFront's default domain is used).
- Autoscaling: one task, always on.
- Multi-AZ RDS, read replicas or final snapshots.
- A secret header between CloudFront and the ALB: the ALB accepts any
  CloudFront distribution's origin-facing servers, not only this one's.
- WAF, remote state with locking, and a CI deploy pipeline.
