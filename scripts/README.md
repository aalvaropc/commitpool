# Scripts

| Script | What it does |
|---|---|
| `e2e.sh [playwright args]` | Builds the web app and a server with `-tags e2e`, starts it with the in-process fake PayPal and the fake interpreter, and runs the Playwright journeys in `web/e2e`. Arguments go to `playwright test`, e.g. `scripts/e2e.sh e2e/journey.spec.ts`. `E2E_DATABASE_URL` points at an existing database (CI's service container); without it the local compose database is started. `E2E_PORT` picks the server's port (default 18080, away from a dev server on 8080). On failure the server log is copied to `web/test-results/e2e-server.log`. |
| `deploy-ec2.sh [tag]` | Builds the app image for the demo VM (linux/arm64), pushes it to the ECR repository of `deploy/ec2`, installs this checkout's `deploy/ec2/deploy.sh` on the VM and runs it through SSM Run Command. The tag defaults to the short commit id, with `-dirty` when the tree has uncommitted changes. Needs the AWS CLI with credentials for the stack's account, Docker with Buildx and the Terraform state of `deploy/ec2`. |
| `register-webhook.sh <base-url>` | Registers the PayPal webhook of a deployment for `<base-url>/paypal/webhook` and prints its id, the value of `PAYPAL_WEBHOOK_ID`. A webhook already registered for that URL is reused. `PAYPAL_CLIENT_ID` and `PAYPAL_CLIENT_SECRET` come from the environment and are never printed; credentials and the access token reach curl on stdin, never in its arguments; `PAYPAL_BASE_URL` defaults to the Sandbox. Diagnostics go to stderr, and a failed call reports PayPal's error name and debug id, never the body. Needs curl and jq. |

`deploy/ec2/deploy.sh <image-tag>` runs on the VM: it pulls the image and
(re)starts PostgreSQL, the app and Caddy (HTTPS through Let's Encrypt) on one
Docker network. See `deploy/ec2/README.md`.
