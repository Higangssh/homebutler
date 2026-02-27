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

function getVersion() {
  const pkg = require("./package.json");
  return pkg.version;
}

function fetch(url) {
  return new Promise((resolve, reject) => {
    https.get(url, { headers: { "User-Agent": "homebutler-mcp" } }, (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        return fetch(res.headers.location).then(resolve, reject);
      }
      if (res.statusCode !== 200) {
        return reject(new Error(`HTTP ${res.statusCode} for ${url}`));
      }
      resolve(res);
    }).on("error", reject);
  });
}

async function downloadAndExtract() {
  const { os, cpu } = getPlatform();
  const version = getVersion();
  const tag = `v${version}`;

  const ext = os === "windows" ? "zip" : "tar.gz";
  const assetName = `homebutler_${version}_${os}_${cpu}.${ext}`;
  const url = `https://github.com/${REPO}/releases/download/${tag}/${assetName}`;

  console.log(`Downloading homebutler ${tag} for ${os}/${cpu}...`);

  fs.mkdirSync(BIN_DIR, { recursive: true });

  const tmpFile = path.join(BIN_DIR, assetName);
  const stream = createWriteStream(tmpFile);
  const res = await fetch(url);
  await pipeline(res, stream);

  console.log("Extracting...");

  if (ext === "tar.gz") {
    execSync(`tar -xzf "${tmpFile}" -C "${BIN_DIR}"`, { stdio: "inherit" });
  } else {
    execSync(`unzip -o "${tmpFile}" -d "${BIN_DIR}"`, { stdio: "inherit" });
  }

  // Find the binary (goreleaser puts it inside a directory or at root)
  if (!fs.existsSync(BIN_PATH)) {
    // Search recursively
    const found = findFile(BIN_DIR, BIN_NAME);
    if (found && found !== BIN_PATH) {
      fs.renameSync(found, BIN_PATH);
    }
  }

  if (!fs.existsSync(BIN_PATH)) {
    throw new Error(`Binary not found after extraction: ${BIN_PATH}`);
  }

  fs.chmodSync(BIN_PATH, 0o755);

  // Cleanup archive
  fs.unlinkSync(tmpFile);

  // Cleanup extracted directories
  for (const entry of fs.readdirSync(BIN_DIR)) {
    const full = path.join(BIN_DIR, entry);
    if (fs.statSync(full).isDirectory()) {
      fs.rmSync(full, { recursive: true });
    }
  }

  console.log(`homebutler ${tag} installed successfully.`);
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

// Skip if binary already exists and is correct version
if (fs.existsSync(BIN_PATH)) {
  try {
    const out = execSync(`"${BIN_PATH}" version`, { encoding: "utf8" });
    if (out.includes(getVersion())) {
      console.log(`homebutler ${getVersion()} already installed.`);
      process.exit(0);
    }
  } catch {}
}

downloadAndExtract().catch((err) => {
  console.error("Failed to install homebutler:", err.message);
  process.exit(1);
});
