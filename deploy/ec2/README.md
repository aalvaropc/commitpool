# Demo deployment on one EC2 VM

The hosted demo runs on a single Graviton VM: Docker runs PostgreSQL, the app
and Caddy, which serves HTTPS with a Let's Encrypt certificate for an
`sslip.io` name derived from the VM's Elastic IP. The VM's role reaches
Bedrock, pulls the image from ECR and reads the secrets from SSM Parameter
Store; there is no SSH, only SSM. `deploy/aws` holds the production-shaped
alternative (Fargate, RDS, ALB and CloudFront) at about four times the cost.

```
browser ──HTTPS──► Caddy :443 ──► app :8080 ──► PostgreSQL     (one VM, one Docker network)
                                     │
                     instance role ──┼──► Bedrock (Claude Sonnet 4.5)
                                     ├──► ECR (image) · SSM Parameter Store (secrets)
PayPal Sandbox ◄─────────────────────┘    webhooks come back through Caddy
```

## Cost while it is on (us-east-1, on demand)

| Item | US$/month |
|---|---|
| `t4g.small` (2 vCPU, 2 GB), 730 h at 0.0168 | 12.3 |
| Elastic IP in use, 730 h at 0.005 | 3.7 |
| 16 GB gp3 root volume at 0.08 | 1.3 |
| ECR storage, SSM standard parameters | < 0.1 |
| **Total** | **≈ 17.3** |

Bedrock tokens are billed per use. `terraform destroy` stops every charge and
deletes the database.

## Prerequisites

- AWS CLI v2 with credentials for the account. A session from `aws login`
  works for the CLI; Terraform and the Go SDK may need it exported first:
  `eval "$(aws configure export-credentials --format env)"`.
- Terraform 1.15, Docker with Buildx (the image is built for `linux/arm64`
  without emulation: the build stages run on the host and Go cross-compiles).
- Claude Sonnet 4.5 usable in the region (the root README's Bedrock section).
- A PayPal Sandbox REST app: its client id and secret.

## First deploy, in order

From the repository root, in one shell:

```sh
export AWS_REGION=us-east-1
```

1. Store the PayPal Sandbox credentials as SecureString parameters (never in
   Terraform state or user data):

   ```sh
   aws ssm put-parameter --name /commitpool/paypal_client_id --type SecureString --overwrite --value "$PAYPAL_CLIENT_ID"
   aws ssm put-parameter --name /commitpool/paypal_client_secret --type SecureString --overwrite --value "$PAYPAL_CLIENT_SECRET"
   ```

2. Create the stack: ECR repository, Elastic IP, security group (80 and 443
   only), instance role, the database password parameter and the VM, which
   installs Docker and writes `/opt/commitpool/deploy.sh` at boot.

   ```sh
   terraform -chdir=deploy/ec2 init
   terraform -chdir=deploy/ec2 apply
   ```

3. Build, push and start the app; this also starts PostgreSQL and Caddy the
   first time, and waits until `/healthz` answers:

   ```sh
   scripts/deploy-ec2.sh
   ```

   The first HTTPS request may take a few seconds while Caddy obtains the
   certificate. `terraform -chdir=deploy/ec2 output -raw public_url` prints
   the URL.

4. Register the PayPal webhook for that URL, store its id and redeploy so the
   app verifies webhooks:

   ```sh
   url="$(terraform -chdir=deploy/ec2 output -raw public_url)"
   aws ssm put-parameter --name /commitpool/paypal_webhook_id --type SecureString --overwrite \
     --value "$(scripts/register-webhook.sh "$url")"
   scripts/deploy-ec2.sh
   ```

5. Seed the demo workshop: `go run ./cmd/seed -base "$url"`.

## Later deploys and operations

- New code: commit, then `scripts/deploy-ec2.sh` (the tag is the short
  commit id). The app restarts in a few seconds; data stays.
- Logs and a shell: `aws ssm start-session --target "$(terraform -chdir=deploy/ec2 output -raw instance_id)"`,
  then `sudo docker logs --tail 100 app`.
- `scripts/deploy-ec2.sh` installs the checkout's `deploy/ec2/deploy.sh` on
  the VM before running it, so a change to it ships with the next deploy.
  Terraform ignores the boot template after the first boot, so an `apply`
  never replaces the VM (and its database) for it.
- Optional parameters, read on every deploy: `/commitpool/mcp_secret` (a
  long random string) keeps MCP confirmation tokens valid across restarts;
  `/commitpool/mail_from`, a sender verified in Amazon SES, turns email on
  (the instance role may send with `ses:SendEmail`). While the SES account is
  in its sandbox, only verified recipients get mail. Without it,
  notifications are only logged.
- Tear down: `terraform -chdir=deploy/ec2 destroy`, then delete the three
  PayPal parameters with `aws ssm delete-parameter`.
