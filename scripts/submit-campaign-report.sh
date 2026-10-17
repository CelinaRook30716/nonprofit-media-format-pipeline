#!/bin/sh
set -eu

image_path=${1:?"usage: scripts/submit-campaign-report.sh IMAGE"}

curl --fail-with-body --request POST http://localhost:8080/uploads \
  --form 'reference=spring-drive' \
  --form 'kind=campaign_report' \
  --form "image=@${image_path}"
