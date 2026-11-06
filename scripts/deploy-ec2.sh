#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

out() { terraform -chdir=deploy/ec2 output -raw "$1"; }
region="$(out region)"
repository="$(out repository_url)"
instance="$(out instance_id)"
url="$(out public_url)"

tag="${1:-$(git rev-parse --short HEAD)}"
if [[ $# -eq 0 ]] && ! git diff --quiet HEAD; then
  tag="${tag}-dirty"
fi

aws ecr get-login-password --region "$region" | docker login --username AWS --password-stdin "${repository%%/*}" >/dev/null
docker buildx build --platform linux/arm64 -f deploy/Dockerfile -t "$repository:$tag" --push .

script="$(base64 -w0 deploy/ec2/deploy.sh)"
install="echo $script | base64 -d > /opt/commitpool/deploy.sh.new && chmod 700 /opt/commitpool/deploy.sh.new && mv /opt/commitpool/deploy.sh.new /opt/commitpool/deploy.sh"
command_id="$(aws ssm send-command --region "$region" --instance-ids "$instance" \
  --document-name AWS-RunShellScript --comment "commitpool deploy $tag" \
  --parameters "commands=[\"$install\",\"/opt/commitpool/deploy.sh $tag\"]" \
  --query Command.CommandId --output text)"

aws ssm wait command-executed --region "$region" --command-id "$command_id" --instance-id "$instance" || true
read -r status <<<"$(aws ssm get-command-invocation --region "$region" --command-id "$command_id" --instance-id "$instance" --query Status --output text)"
aws ssm get-command-invocation --region "$region" --command-id "$command_id" --instance-id "$instance" \
  --query '[StandardOutputContent,StandardErrorContent]' --output text
if [[ "$status" != "Success" ]]; then
  echo "deploy-ec2: the VM's deploy ended as $status" >&2
  exit 1
fi
echo "deploy-ec2: $tag is live at $url"
