# Deploying CommitPool on AWS

> The hosted demo does not use this configuration: it runs on one EC2 VM
> ([../ec2/README.md](../ec2/README.md)) for about a quarter of the cost. This
> one is the production-shaped alternative.

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
  Anthropic models need, once per account, the use-case form and the model
  agreement accepted in the Bedrock console. Until then every call answers
  403 and
  `aws bedrock get-foundation-model-availability --model-id anthropic.claude-sonnet-4-5-20250929-v1:0`
  shows `"agreementAvailability": {"status": "NOT_AVAILABLE"}`.
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
  cancel and webhook URLs need. CloudFront reaches the ALB over plain HTTP at
  the ALB's public endpoint; the ALB only accepts the CloudFront
  origin-facing prefix list (see "Not included" for what that hop leaves
  open).
- **`AllViewer` origin request policy.** It forwards every viewer header,
  including PayPal's webhook signature headers and the viewer `Host`
  (the CloudFront domain). The ALB has a single forward action and does not
  route on `Host`, and the app never reads it (its links come from
  `COMMITPOOL_BASE_URL`), so `AllViewerExceptHostHeader` is not needed.
- **Bedrock permissions.** The app calls Claude through the Bedrock runtime
  (`InvokeModel`) with the task role's credentials, by default Claude Sonnet
  4.5 through the `us.` inference profile. The task role allows
  `bedrock:InvokeModel` and `bedrock:InvokeModelWithResponseStream` on
  Anthropic foundation models in any region (`foundation-model/anthropic.*`,
  so the profile's cross-region routing works) and on the account's
  inference profiles. The only other wildcard is
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

Approximate on-demand prices in `us-east-1`, per month (730 hours), before
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

All commands run from the repository root, in one shell. Set the region and
the `project` value (the secret name prefix) once:

```sh
export AWS_REGION=us-east-1
project=commitpool
```

Optionally copy `deploy/aws/terraform.tfvars.example` to
`deploy/aws/terraform.tfvars` and adjust it.

1. **Create the image repository and the PayPal secrets first.** A targeted
   apply creates only these (Terraform warns that `-target` is for
   exceptional use; that is expected here), so no service exists yet that
   could start without an image or credentials:

   ```sh
   terraform -chdir=deploy/aws init
   terraform -chdir=deploy/aws apply -var region="$AWS_REGION" \
     -target=aws_ecr_repository.app -target=aws_secretsmanager_secret_version.paypal
   ```

2. **Put the PayPal credentials.** Read them into shell variables without
   echoing them, then pass them on stdin so they never appear in a command
   line:

   ```sh
   printf 'PayPal client id: ' && read -rs PAYPAL_CLIENT_ID && echo
   printf 'PayPal client secret: ' && read -rs PAYPAL_CLIENT_SECRET && echo
   export PAYPAL_CLIENT_ID PAYPAL_CLIENT_SECRET
   printf %s "$PAYPAL_CLIENT_ID" | aws secretsmanager put-secret-value --region "$AWS_REGION" \
     --secret-id "$project/paypal-client-id" --secret-string file:///dev/stdin
   printf %s "$PAYPAL_CLIENT_SECRET" | aws secretsmanager put-secret-value --region "$AWS_REGION" \
     --secret-id "$project/paypal-client-secret" --secret-string file:///dev/stdin
   ```

3. **Build and push the image.**

   ```sh
   repo="$(terraform -chdir=deploy/aws output -raw ecr_repository_url)"
   tag="$(git rev-parse --short HEAD)"
   aws ecr get-login-password --region "$AWS_REGION" | docker login --username AWS --password-stdin "${repo%%/*}"
   docker build --platform linux/amd64 -f deploy/Dockerfile -t "$repo:$tag" .
   docker push "$repo:$tag"
   ```

4. **Create everything else.** The service is created last, after the
   database and its `DATABASE_URL` secret, so its first task starts with the
   image, the database and the PayPal credentials in place. CloudFront and
   RDS take several minutes.

   ```sh
   terraform -chdir=deploy/aws plan -var region="$AWS_REGION" -var image="$repo:$tag"
   terraform -chdir=deploy/aws apply -var region="$AWS_REGION" -var image="$repo:$tag"
   cluster="$(terraform -chdir=deploy/aws output -raw ecs_cluster)"
   service="$(terraform -chdir=deploy/aws output -raw ecs_service)"
   aws ecs wait services-stable --region "$AWS_REGION" --cluster "$cluster" --services "$service"
   ```

   Every later `apply` needs the same `image`; put it in
   `deploy/aws/terraform.tfvars` (`image = "<repo>:<tag>"`) so a plain
   `apply` does not go back to `:latest`.

5. **Check the health endpoint.**

   ```sh
   base_url="https://$(terraform -chdir=deploy/aws output -raw cloudfront_domain)"
   curl -fsS "$base_url/healthz"    # {"ok":true}
   ```

   If it fails, read the logs:
   `aws logs tail "$(terraform -chdir=deploy/aws output -raw log_group)" --region "$AWS_REGION" --since 15m`.

6. **Register the PayPal webhook and put its id.** With `PAYPAL_CLIENT_ID`
   and `PAYPAL_CLIENT_SECRET` still exported; the secret is written only if
   the registration printed an id:

   ```sh
   PAYPAL_WEBHOOK_ID="$(scripts/register-webhook.sh "$base_url")" && [ -n "$PAYPAL_WEBHOOK_ID" ] &&
     printf %s "$PAYPAL_WEBHOOK_ID" | aws secretsmanager put-secret-value --region "$AWS_REGION" \
       --secret-id "$project/paypal-webhook-id" --secret-string file:///dev/stdin
   ```

   The script subscribes `<base_url>/paypal/webhook` to the seven payment
   events the app handles and prints only the webhook id; running it again
   reuses the existing webhook. Until this step `/paypal/webhook` answers 503.

7. **Deploy again** so the task reads the webhook id, then forget the
   credentials:

   ```sh
   aws ecs update-service --region "$AWS_REGION" --cluster "$cluster" --service "$service" --force-new-deployment
   aws ecs wait services-stable --region "$AWS_REGION" --cluster "$cluster" --services "$service"
   unset PAYPAL_CLIENT_ID PAYPAL_CLIENT_SECRET
   ```

## Redeploying a new version

These commands work in a new shell (the ECR login expires after 12 hours, so
it is repeated every time):

```sh
export AWS_REGION=us-east-1
repo="$(terraform -chdir=deploy/aws output -raw ecr_repository_url)"
tag="$(git rev-parse --short HEAD)"
aws ecr get-login-password --region "$AWS_REGION" | docker login --username AWS --password-stdin "${repo%%/*}"
docker build --platform linux/amd64 -f deploy/Dockerfile -t "$repo:$tag" .
docker push "$repo:$tag"
terraform -chdir=deploy/aws apply -var region="$AWS_REGION" -var image="$repo:$tag"
```

The apply registers a new task definition and the service rolls to it. After
changing only a secret, force a new deployment instead (step 7's
`update-service` and `wait` commands, with `cluster` and `service` read from
`terraform -chdir=deploy/aws output -raw ecs_cluster` and `ecs_service`).

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
`deploy/aws/*.tfstate*`, `deploy/aws/*.tfvars`, `deploy/aws/*.tfplan` and
`deploy/aws/crash.log`; `.terraform.lock.hcl` and
`terraform.tfvars.example` are committed. The PayPal secrets never pass
through Terraform: they are put with the AWS CLI and Terraform ignores their
values.

## Not included

- A custom domain and its certificate (CloudFront's default domain is used).
- Autoscaling: one task, always on.
- Multi-AZ RDS, read replicas or final snapshots.
- Encryption and exclusivity on the CloudFront → ALB hop. CloudFront calls
  the public ALB endpoint over plain HTTP: for an AWS origin the traffic stays
  on the AWS network, but owner tokens and webhook bodies are not
  TLS-protected on that hop, and the ALB accepts any CloudFront
  distribution's origin-facing servers, not only this one's. A later option
  is a CloudFront VPC origin with an internal ALB, which closes both gaps and
  also drops the ALB's two public IPv4 addresses.
- WAF, remote state with locking, and a CI deploy pipeline.
