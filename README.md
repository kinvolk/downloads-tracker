# Github Downloads tracker

A script that fetches download stats for Github releases and GHCR packages.

## Usage

```bash
GITHUB_TOKEN="ghp_*****"
GITHUB_ORG=kinvolk
GITHUB_REPOS=headlamp,nebraska
PUSHGATEWAY_URL=http://localhost:9091
PUSHGATEWAY_USERNAME=test
PUSHGATEWAY_PASSWORD=test@123
go run main.go
```

This will extract the metrics and push it to pushgateway.

## Keep alive

We don't touch this repo much and Github actions get disabled if the repo doesn't get updates
in a timeframe of 60 days. So let's use the date below with a scheduled action that updates it
in order to not let the actions be turned off.

Last update: 2023-12-21
