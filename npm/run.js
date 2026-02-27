#!/usr/bin/env node
"use strict";

const { spawn } = require("child_process");
const path = require("path");
const fs = require("fs");

const BIN_NAME = process.platform === "win32" ? "homebutler.exe" : "homebutler";
const BIN_PATH = path.join(__dirname, "bin", BIN_NAME);

if (!fs.existsSync(BIN_PATH)) {
  console.error(
    "homebutler binary not found. Run: npm rebuild homebutler-mcp"
  );
  process.exit(1);
}

const args = ["mcp", ...process.argv.slice(2)];

const child = spawn(BIN_PATH, args, {
  stdio: ["inherit", "inherit", "inherit"],
});

child.on("exit", (code) => process.exit(code ?? 0));
child.on("error", (err) => {
  console.error("Failed to start homebutler:", err.message);
  process.exit(1);
});
