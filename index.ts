import { Octokit } from "@octokit/core";
import axios from "axios";
import { writeFileSync } from "fs";
import { JSDOM } from "jsdom";
import yargs from "yargs";

const argv = yargs(process.argv)
  .usage(
    "$0 <org> <repo>",
    "Fetches for the download count for package & releases, and stores them in a JSON file."
  )
  .positional("org", {
    describe: "the Github organization",
    demandOption: true,
  })
  .positional("repo", {
    describe: "the Github repository",
    demandOption: true,
  })
  .help()
  .alias("help", "h").argv;

const token = process.env.PERSONAL_ACCESS_TOKEN;

if (!token) {
  console.error('Please provide the PERSONAL_ACCESS_TOKEN as an env var.')
  process.exit(1)
}

const octokit = new Octokit({ auth: process.env.PERSONAL_ACCESS_TOKEN });

async function fetchRelease() {
  const result = await octokit.request("GET /repos/{owner}/{repo}/releases", {
    owner: argv.org,
    repo: argv.repo,
  });

  let releases = {};

  for (let release of result.data) {
    releases[release.tag_name] = {};
    let releaseInfo = [];
    const result = await octokit.request("GET {url}", {
      url: release.url,
    });

    for (const asset of result.data.assets) {
      releaseInfo.push({
        content_type: asset.content_type,
        name: asset.name,
        downloads: asset.download_count,
      });
    }

    releases[release.tag_name] = releaseInfo;
  }

  return releases;
}

async function getPackageVersions() {
  const response = await octokit.request(
    "GET /orgs/{org}/packages/{package_type}/{package_name}/versions",
    {
      package_type: "container",
      package_name: argv.repo,
      org: argv.org,
    }
  );

  return response.data;
}

async function getPackageHTML(url) {
  const response = await axios.get(url);
  return response.data;
}

function getDownloadsFromPackagesHTML(html) {
  const dom = new JSDOM(html);
  const document = dom.window.document;
  const nodes = document.querySelectorAll("span");

  const nodesTranslation = {
    "Total downloads": "ever",
    "Last 30 days": "month",
    "Last week": "week",
    Today: "today",
  };

  let downloads = {
    ever: 0,
    month: 0,
    week: 0,
    today: 0,
  };

  for (let i = 0; i < nodes.length; ++i) {
    if (i === nodes.length - 1) {
      break;
    }
    if (Object.keys(nodesTranslation).indexOf(nodes[i].textContent) !== -1) {
      const keyName = nodesTranslation[nodes[i].textContent];
      downloads[keyName] = parseInt(
        nodes[i + 1].textContent.replace(",", "").replace(/^\s+|\s+$/g, "")
      );
    }
  }

  return downloads;
}

async function getPackageDownloads() {
  const versions = await getPackageVersions();
  const downloads = {};
  for (const version of versions) {
    const html = await getPackageHTML(version.html_url);
    downloads[version.name] = {
      tags: version.metadata.container.tags,
      downloads: getDownloadsFromPackagesHTML(html),
    };
  }
  return downloads;
}

async function getDownloadsInfo() {
  const pkg = await getPackageDownloads();
  const releases = await fetchRelease();

  return {
    date: new Date(),
    package: pkg,
    releases,
  };
}

async function printInfo() {
  const info = await getDownloadsInfo();
  console.log(JSON.stringify(info, null, 2));
}

async function saveInfo() {
  const d = await getDownloadsInfo();
  writeFileSync("./results/downloads.json", JSON.stringify(d, null, 2));
}

printInfo();
