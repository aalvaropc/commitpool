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
  commit id). The app restarts in a few seconds; data stays. `deploy.sh`
  stops the old container with a 35 s grace period, since the app waits up
  to 30 s for in-flight payment calls before it exits. Settings come from
  `/opt/commitpool/config`, written at boot, and secrets are read from SSM on
  every run and written only to root-only env files.
- Logs and a shell: `aws ssm start-session --target "$(terraform -chdir=deploy/ec2 output -raw instance_id)"`,
  then `sudo docker logs --tail 100 app`.
- `scripts/deploy-ec2.sh` installs the checkout's `deploy/ec2/deploy.sh` on
  the VM before running it, so a change to it ships with the next deploy.
  Terraform ignores the boot template after the first boot, so an `apply`
  never replaces the VM (and its database) for it.
- Optional parameters, read on every deploy: `/commitpool/mcp_secret` (a
  long random string) keeps open organizer approve pages working across
  restarts;
  `/commitpool/mail_from`, a sender verified in Amazon SES, turns email on
  (the instance role may send with `ses:SendEmail`). While the SES account is
  in its sandbox, only verified recipients get mail. Without it,
  notifications are only logged.
  `/commitpool/video_url` (String), a YouTube link, embeds the demo video in
  the landing's For judges section;
  `/commitpool/judge_buyer_email` (String) and
  `/commitpool/judge_buyer_password` (SecureString), a PayPal Sandbox
  Personal account, show there with copy buttons. The login is deployed only
  when both exist. A video link that is not a YouTube video is refused, so
  every deploy first runs the new image with `check-config` against the
  written environment; if the configuration is refused, the deploy stops
  there and the running app keeps serving.
- Tear down: `terraform -chdir=deploy/ec2 destroy`, then delete the three
  PayPal parameters with `aws ssm delete-parameter`.

## Email through Amazon SES

The demo sends real email once a sender is verified, stored as
`/commitpool/mail_from` and deployed:

1. Verify the sender (SES emails a link to click):

   ```sh
   aws sesv2 create-email-identity --region us-east-1 --email-identity you@example.com
   ```

2. Store it, with the name people see, and redeploy:

   ```sh
   aws ssm put-parameter --region us-east-1 --name /commitpool/mail_from --type String \
     --overwrite --value 'CommitPool <you@example.com>'
   scripts/deploy-ec2.sh
   ```

3. A new SES account is in its sandbox and only delivers to verified
   addresses. Verify the ones the demo uses the same way, or ask AWS to lift
   the sandbox (it answers within about a day):

   ```sh
   aws sesv2 put-account-details --region us-east-1 --production-access-enabled \
     --mail-type TRANSACTIONAL --website-url https://3-209-60-155.sslip.io \
     --use-case-description 'Transactional email for CommitPool, a booking app for small paid classes: a link to their own page when people join or create a class, and notices when their reservation is held, charged, released or refunded. Only people who type their own address receive mail; there is no marketing email.'
   ```

4. Check one message end to end: create a class with "Your email" filled in
   and look for "Your organizer link for …" in that inbox. The logs show the
   `notify:owner_created:…` job succeeding, or the masked recipient if SES
   refuses it; never the body.

A sender on a free mail domain (such as Gmail) is accepted by SES but its
DMARC policy can send the mail to spam; a domain you control, verified with
DKIM, delivers best.
