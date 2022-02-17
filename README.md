# Github Downloads tracker

A script that fetches download stats for Github releases and GHCR packages.

## Usage

```bash
PERSONAL_ACCESS_TOKEN="abcdf..."

npm run build
node ./index.js MyOrg RepoName
```

This will print out the stats to stdout.