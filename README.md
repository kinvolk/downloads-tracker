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