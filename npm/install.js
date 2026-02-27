#!/usr/bin/env node
"use strict";

const { execSync } = require("child_process");
const fs = require("fs");
const path = require("path");
const https = require("https");
const { createWriteStream } = require("fs");
const { pipeline } = require("stream/promises");

const REPO = "Higangssh/homebutler";
const BIN_NAME = process.platform === "win32" ? "homebutler.exe" : "homebutler";
const BIN_DIR = path.join(__dirname, "bin");
const BIN_PATH = path.join(BIN_DIR, BIN_NAME);

function getPlatform() {
  const platform = process.platform;
  const arch = process.arch;

  const osMap = { linux: "linux", darwin: "darwin", win32: "windows" };
  const archMap = { x64: "amd64", arm64: "arm64" };

  const os = osMap[platform];
  const cpu = archMap[arch];

  if (!os || !cpu) {
    throw new Error(`Unsupported platform: ${platform}/${arch}`);
  }

  return { os, cpu };
}

function fetchJSON(url) {
  return new Promise((resolve, reject) => {
    const req = https.get(url, { headers: { "User-Agent": "homebutler-mcp" }, timeout: 10000 }, (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        return fetchJSON(res.headers.location).then(resolve, reject);
      }
      if (res.statusCode !== 200) {
        return reject(new Error(`HTTP ${res.statusCode}`));
      }
      let data = "";
      res.on("data", (c) => (data += c));
      res.on("end", () => {
        try { resolve(JSON.parse(data)); } catch (e) { reject(e); }
      });
    });
    req.on("error", reject);
    req.on("timeout", () => { req.destroy(); reject(new Error("timeout")); });
  });
}

async function getVersion() {
  try {
    const data = await fetchJSON(
      `https://api.github.com/repos/${REPO}/releases/latest`
    );
    if (data.tag_name) return data.tag_name.replace(/^v/, "");
  } catch {}
  return require("./package.json").version;
}

function fetch(url) {
  return new Promise((resolve, reject) => {
    const req = https.get(url, { headers: { "User-Agent": "homebutler-mcp" }, timeout: 60000 }, (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        return fetch(res.headers.location).then(resolve, reject);
      }
      if (res.statusCode !== 200) {
        return reject(new Error(`HTTP ${res.statusCode} for ${url}`));
      }
      resolve(res);
    });
    req.on("error", reject);
    req.on("timeout", () => { req.destroy(); reject(new Error("download timeout")); });
  });
}

async function downloadAndExtract() {
  const { os, cpu } = getPlatform();
  const version = await getVersion();
  const tag = `v${version}`;

  const ext = os === "windows" ? "zip" : "tar.gz";
  const assetName = `homebutler_${version}_${os}_${cpu}.${ext}`;
  const url = `https://github.com/${REPO}/releases/download/${tag}/${assetName}`;

  process.stderr.write(`Downloading homebutler ${tag} for ${os}/${cpu}...\n`);

  fs.mkdirSync(BIN_DIR, { recursive: true });

  const tmpFile = path.join(BIN_DIR, assetName);
  const stream = createWriteStream(tmpFile);
  const res = await fetch(url);
  await pipeline(res, stream);

  if (ext === "tar.gz") {
    execSync(`tar -xzf "${tmpFile}" -C "${BIN_DIR}"`, { stdio: "pipe" });
  } else {
    execSync(`unzip -o "${tmpFile}" -d "${BIN_DIR}"`, { stdio: "pipe" });
  }

  // Find the binary (goreleaser puts it inside a directory or at root)
  if (!fs.existsSync(BIN_PATH)) {
    const found = findFile(BIN_DIR, BIN_NAME);
    if (found && found !== BIN_PATH) {
      fs.renameSync(found, BIN_PATH);
    }
  }

  if (!fs.existsSync(BIN_PATH)) {
    throw new Error(`Binary not found after extraction: ${BIN_PATH}`);
  }

  fs.chmodSync(BIN_PATH, 0o755);

  // Cleanup
  fs.unlinkSync(tmpFile);
  for (const entry of fs.readdirSync(BIN_DIR)) {
    const full = path.join(BIN_DIR, entry);
    if (fs.statSync(full).isDirectory()) {
      fs.rmSync(full, { recursive: true });
    }
  }

  process.stderr.write(`homebutler ${tag} installed successfully.\n`);
}

function findFile(dir, name) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isFile() && entry.name === name) return full;
    if (entry.isDirectory()) {
      const found = findFile(full, name);
      if (found) return found;
    }
  }
  return null;
}

async function main() {
  // Skip if binary already exists and is correct version
  if (fs.existsSync(BIN_PATH)) {
    try {
      const version = await getVersion();
      const out = execSync(`"${BIN_PATH}" version`, { encoding: "utf8" });
      if (out.includes(version)) {
        process.stderr.write(`homebutler ${version} already installed.\n`);
        process.exit(0);
      }
    } catch {}
  }

  await downloadAndExtract();
}

main().catch((err) => {
  process.stderr.write(`Failed to install homebutler: ${err.message}\n`);
  // Don't exit(1) during postinstall — let run.js handle lazy install
  // This prevents npm install -g from failing if download is slow
  if (process.env.npm_lifecycle_event === "postinstall") {
    process.stderr.write("Binary will be downloaded on first run.\n");
    process.exit(0);
  }
  process.exit(1);
});
