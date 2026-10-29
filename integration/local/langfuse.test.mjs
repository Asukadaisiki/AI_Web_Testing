import test from "node:test";
import assert from "node:assert/strict";
import { spawn, spawnSync } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, statSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const launcher = join(dirname(fileURLToPath(import.meta.url)), "langfuse.mjs");
function call(root, action, overrides = {}) {
  return spawnSync(process.execPath, [launcher, action], {
    encoding: "utf8", timeout: 10000,
    env: { ...process.env, LANGFUSE_NATIVE_ROOT: root, ...overrides },
  });
}
async function fixture() {
  const root = mkdtempSync(join(tmpdir(), "langfuse-launcher-test-"));
  const child = spawn(process.execPath, ["-e", "setInterval(() => {}, 1000)"], {
    detached: true, stdio: "ignore",
  });
  const exited = new Promise((done) => child.once("exit", done));
  await new Promise((done, reject) => { child.once("spawn", done); child.once("error", reject); });
  const identity = spawnSync("ps", ["-p", String(child.pid), "-o", "lstart=", "-o", "command="], {
    encoding: "utf8", env: { ...process.env, LC_ALL: "C" },
  }).stdout.trim();
  return {
    root, child, identity, exited,
    cleanup() {
      try { process.kill(-child.pid, "SIGTERM"); } catch {}
      rmSync(root, { recursive: true, force: true });
    },
  };
}

test("status does not require credentials or native services", () => {
  const root = mkdtempSync(join(tmpdir(), "langfuse-launcher-test-"));
  try {
    const result = call(root, "status");
    assert.equal(result.status, 0, result.stderr);
    assert.equal((result.stdout.match(/not running \(owned\)/g) || []).length, 4);
  } finally { rmSync(root, { recursive: true, force: true }); }
});

test("ownership is stable across macOS process-display locales", async () => {
  const f = await fixture();
  try {
    writeFileSync(join(f.root, "clickhouse.pid.json"), JSON.stringify({ pid: f.child.pid, identity: f.identity }));
    const result = call(f.root, "status", { LC_ALL: "zh_CN.UTF-8", LANG: "zh_CN.UTF-8" });
    assert.equal(result.status, 0, result.stderr);
    assert.match(result.stdout, /clickhouse: running \(owned\)/);
  } finally { f.cleanup(); }
});

test("down refuses to signal a process whose saved identity does not match", async () => {
  const f = await fixture();
  try {
    writeFileSync(join(f.root, "clickhouse.pid.json"), JSON.stringify({ pid: f.child.pid, identity: "different process" }));
    const result = call(f.root, "down");
    assert.equal(result.status, 0, result.stderr);
    assert.doesNotThrow(() => process.kill(f.child.pid, 0));
  } finally { f.cleanup(); }
});

test("down stops only a matching detached process group", async () => {
  const f = await fixture();
  try {
    writeFileSync(join(f.root, "clickhouse.pid.json"), JSON.stringify({ pid: f.child.pid, identity: f.identity }));
    const result = call(f.root, "down");
    assert.equal(result.status, 0, result.stderr);
    assert.match(result.stdout, /Stopped owned clickhouse process/);
    await f.exited;
    assert.throws(() => process.kill(f.child.pid, 0), { code: "ESRCH" });
  } finally { f.cleanup(); }
});

function buildFixture() {
  const root = realpathSync(mkdtempSync(join(tmpdir(), "langfuse-build-test-")));
  const source = join(root, "checkout");
  const bin = join(root, "bin");
  mkdirSync(join(source, "web"), { recursive: true });
  mkdirSync(join(source, "scripts"));
  mkdirSync(bin);
  writeFileSync(join(root, "credentials.json"), JSON.stringify({
    databaseUrl: "postgresql://test:test@127.0.0.1:5432/agenttrace_langfuse_local",
    authSecret: "fixture-private-auth", salt: "fixture-salt", encryptionKey: "a".repeat(64),
    clickhousePassword: "fixture-clickhouse", minioUser: "fixture", minioPassword: "fixture-minio",
    publicKey: "fixture-public", secretKey: "fixture-secret",
  }));
  // Only the external build boundary is replaced; the actual launcher runs unchanged.
  writeFileSync(join(bin, "pnpm"), `#!${process.execPath}
const fs = require("node:fs");
const path = require("node:path");
if (process.argv.slice(2).join(" ") === "--version") {
  if (process.env.TEST_CREATE_TARGET)
    fs.mkdirSync(path.join(process.env.TEST_NATIVE_ROOT, "checkout/web/.next-fresh"));
  console.log(process.env.TEST_PNPM_VERSION || "11.10.0");
  process.exit(0);
}
fs.writeFileSync(path.join(process.env.TEST_NATIVE_ROOT, "invocation.json"),
  JSON.stringify({ args: process.argv.slice(2), cwd: process.cwd(), env: process.env, node: process.versions.node }));
console.log("fixture-private-auth");
console.error("fixture-secret");
if (process.env.TEST_BUILD_FAIL) process.exit(7);
const dist = path.resolve(process.cwd(), process.env.NEXT_DIST_DIR);
fs.mkdirSync(path.join(dist, "static"), { recursive: true });
fs.writeFileSync(path.join(dist, "BUILD_ID"), "fixture");
`, { mode: 0o700 });
  writeFileSync(join(source, "scripts/scan-client-bundle.mjs"), `
import { writeFileSync } from "node:fs";
import { join } from "node:path";
writeFileSync(join(process.env.TEST_NATIVE_ROOT, "scan.json"),
  JSON.stringify({ args: process.argv.slice(2), cwd: process.cwd() }));
console.error("fixture-secret");
if (process.env.TEST_SCAN_FAIL) process.exit(1);
`);
  return {
    root, source,
    call(overrides = {}) {
      return call(root, "build", {
        LANGFUSE_NATIVE_SOURCE: source, LANGFUSE_NATIVE_NEXT_DIST_DIR: ".next-fresh",
        TEST_NATIVE_ROOT: root, PATH: `${bin}:${process.env.PATH}`, ...overrides,
      });
    },
    cleanup() { rmSync(root, { recursive: true, force: true }); },
  };
}

test("build requires an explicit dist directory before invoking pnpm", () => {
  const f = buildFixture();
  try {
    const result = f.call({ LANGFUSE_NATIVE_NEXT_DIST_DIR: "" });
    assert.equal(result.status, 1);
    assert.match(result.stderr, /explicit.*LANGFUSE_NATIVE_NEXT_DIST_DIR/i);
    assert.equal(existsSync(join(f.root, "invocation.json")), false);
  } finally { f.cleanup(); }
});

test("build refuses live, existing, unsafe and dangling-symlink targets without writes", () => {
  const f = buildFixture();
  try {
    mkdirSync(join(f.source, "web/.next-existing"));
    writeFileSync(join(f.source, "web/.next-existing/keep"), "preserve");
    symlinkSync(join(f.root, "missing"), join(f.source, "web/.next-link"));
    for (const target of [".next", ".next-existing", ".next-link", "../outside", "/tmp/outside", ".next\nsecret", ".next-valid\n"]) {
      const result = f.call({ LANGFUSE_NATIVE_NEXT_DIST_DIR: target });
      assert.equal(result.status, 1);
      assert.match(result.stderr, /fresh|existing|directory/i);
      assert.equal(existsSync(join(f.root, "invocation.json")), false);
    }
    assert.equal(readFileSync(join(f.source, "web/.next-existing/keep"), "utf8"), "preserve");
  } finally { f.cleanup(); }
});

test("build rejects incompatible Node before touching config or checkout", () => {
  const root = mkdtempSync(join(tmpdir(), "langfuse-node-test-"));
  try {
    const preload = join(root, "node22.mjs");
    writeFileSync(preload, 'Object.defineProperty(process.versions, "node", { value: "22.0.0" });');
    const result = spawnSync(process.execPath, ["--import", preload, launcher, "build"], {
      encoding: "utf8", timeout: 10000,
      env: { ...process.env, LANGFUSE_NATIVE_ROOT: root, LANGFUSE_NATIVE_SOURCE: join(root, "missing") },
    });
    assert.equal(result.status, 1);
    assert.match(result.stderr, /Node 24.*required/i);
    assert.equal(existsSync(join(root, "credentials.json")), false);
  } finally { rmSync(root, { recursive: true, force: true }); }
});

test("build rejects a pnpm version other than the upstream pin", () => {
  const f = buildFixture();
  try {
    const result = f.call({ TEST_PNPM_VERSION: "10.0.0" });
    assert.equal(result.status, 1);
    assert.match(result.stderr, /pnpm 11\.10\.0.*required/i);
    assert.equal(existsSync(join(f.root, "invocation.json")), false);
  } finally { f.cleanup(); }
});

test("build refuses a target created during the toolchain check", () => {
  const f = buildFixture();
  try {
    const result = f.call({ TEST_CREATE_TARGET: "1" });
    assert.equal(result.status, 1);
    assert.match(result.stderr, /fresh|existing/i);
    assert.equal(existsSync(join(f.root, "invocation.json")), false);
  } finally { f.cleanup(); }
});

test("build uses private native env, direct Next and selected static scan with private logs", () => {
  const f = buildFixture();
  try {
    writeFileSync(join(f.root, "build.log"), "previous log\n", { mode: 0o644 });
    const result = f.call({
      NEXT_PUBLIC_LANGFUSE_CLOUD_REGION: "US", NEXT_PUBLIC_LEGACY: "legacy",
      SENTRY_AUTH_TOKEN: "fixture-secret", SENTRY_ORG: "cloud", SENTRY_PROJECT: "cloud",
      LANGFUSE_INIT_ORG_CLOUD_PLAN: "Team", DOCKER_BUILD: "1",
      SKIP_ENV_VALIDATION: "1", NEXT_IGNORE_BUILD_ERRORS: "true", DATABASE_URL: "legacy",
      AGENTTRACE_BRIDGE_TOKEN: "fixture-bridge", AGENTTRACE_PROJECT_ID: "bound-project",
    });
    assert.equal(result.status, 0, result.stderr);
    assert.match(result.stdout, /\.next-fresh/);
    assert.doesNotMatch(result.stdout + result.stderr, /fixture-private-auth|fixture-secret|fixture-bridge/);
    const invocation = JSON.parse(readFileSync(join(f.root, "invocation.json")));
    assert.deepEqual(invocation.args, ["exec", "next", "build"]);
    assert.equal(invocation.cwd, join(f.source, "web"));
    assert.match(invocation.node, /^24\./);
    assert.equal(invocation.env.NEXT_DIST_DIR, ".next-fresh");
    assert.equal(invocation.env.DATABASE_URL, "postgresql://test:test@127.0.0.1:5432/agenttrace_langfuse_local");
    assert.equal(invocation.env.AGENTTRACE_BRIDGE_TOKEN, "fixture-bridge");
    assert.equal(invocation.env.AGENTTRACE_PROJECT_ID, "bound-project");
    for (const key of ["NEXT_PUBLIC_LANGFUSE_CLOUD_REGION", "NEXT_PUBLIC_LEGACY", "SENTRY_AUTH_TOKEN", "SENTRY_ORG", "SENTRY_PROJECT",
      "LANGFUSE_INIT_ORG_CLOUD_PLAN", "DOCKER_BUILD", "SKIP_ENV_VALIDATION", "NEXT_IGNORE_BUILD_ERRORS"]) {
      assert.ok(!invocation.env[key], `${key} must not enable inherited build behavior`);
    }
    const scan = JSON.parse(readFileSync(join(f.root, "scan.json")));
    assert.deepEqual(scan.args, [join(f.source, "web/.next-fresh/static")]);
    assert.equal(scan.cwd, f.source);
    assert.equal(statSync(join(f.root, "build.log")).mode & 0o777, 0o600);
    assert.match(readFileSync(join(f.root, "build.log"), "utf8"), /previous log/);
    assert.match(readFileSync(join(f.root, "build.log"), "utf8"), /fixture-private-auth/);
    assert.equal(existsSync(join(f.root, "provision.log")), false);
    assert.equal(existsSync(join(f.root, "migrations.log")), false);
    assert.equal(existsSync(join(f.source, "web/.next")), false);
  } finally { f.cleanup(); }
});

test("build and scan failures never report success or disclose child output", () => {
  for (const failure of ["TEST_BUILD_FAIL", "TEST_SCAN_FAIL"]) {
    const f = buildFixture();
    try {
      const result = f.call({ [failure]: "1" });
      assert.equal(result.status, 1);
      assert.doesNotMatch(result.stdout + result.stderr, /fixture-private-auth|fixture-secret|ready|succeeded/i);
      assert.match(result.stderr, /private.*log/i);
      assert.equal(existsSync(join(f.root, "scan.json")), failure === "TEST_SCAN_FAIL");
    } finally { f.cleanup(); }
  }
});
