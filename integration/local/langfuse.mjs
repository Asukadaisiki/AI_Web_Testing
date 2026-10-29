import { spawn, spawnSync } from "node:child_process";
import { createHash, randomBytes, randomUUID } from "node:crypto";
import { createRequire } from "node:module";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { mkdirSync, readFileSync, writeFileSync, existsSync, lstatSync, openSync, closeSync, fchmodSync } from "node:fs";
import net from "node:net";

const here = dirname(fileURLToPath(import.meta.url));
const root = resolve(process.env.LANGFUSE_NATIVE_ROOT || "/tmp/agenttrace-native/langfuse");
const source = resolve(process.env.LANGFUSE_NATIVE_SOURCE || join(here, "../../../langfuse-agenttrace"));
const revision = "05fe6fe3c310299f4eb3a19989fc14c9a931878f";
const configPath = join(root, "credentials.json");
const names = ["clickhouse", "minio", "worker", "web"];
const ports = { clickhouse: 8123, minio: 9090, worker: 3330, web: 3300 };
const secret = () => randomBytes(32).toString("hex");
const delay = (ms) => new Promise((done) => setTimeout(done, ms));

function run(command, args, options = {}) {
  const result = spawnSync(command, args, { encoding: "utf8", ...options });
  if (result.error || result.status !== 0) {
    // Commands can include secrets; never echo argv or environment on failure.
    throw new Error(`${command} failed (exit ${result.status ?? "unavailable"}); inspect private runtime logs.`);
  }
  return result.stdout?.trim();
}

function config() {
  if (!existsSync(configPath)) throw new Error("Run setup first.");
  const c = JSON.parse(readFileSync(configPath, "utf8"));
  if (!c.clickhousePassword) {
    c.clickhousePassword = secret();
    writeFileSync(configPath, JSON.stringify(c, null, 2) + "\n", { mode: 0o600 });
  }
  return c;
}

function env(c) {
  // Explicit overrides prevent inherited legacy Langfuse variables reaching this stack.
  const base = Object.fromEntries(Object.entries(process.env).filter(([key]) =>
    !/^(LANGFUSE_|CLICKHOUSE_|REDIS_|DATABASE_|DIRECT_URL$|NEXTAUTH_|NEXT_PUBLIC_|SENTRY_|VERCEL_|DOCKER_BUILD$|SKIP_ENV_VALIDATION$|NEXT_IGNORE_BUILD_ERRORS$|PORT$|HOSTNAME$|SALT$|ENCRYPTION_KEY$|NODE_ENV$)/.test(key)));
  return {
    ...base,
    LANG: "en_US.UTF-8",
    LC_ALL: "C",
    TZ: "UTC",
    NODE_ENV: "production",
    DATABASE_URL: c.databaseUrl,
    DIRECT_URL: c.databaseUrl,
    NEXTAUTH_URL: "http://localhost:3300",
    NEXTAUTH_SECRET: c.authSecret,
    SALT: c.salt,
    ENCRYPTION_KEY: c.encryptionKey,
    HOSTNAME: "127.0.0.1",
    PORT: "3330",
    CLICKHOUSE_URL: "http://127.0.0.1:8123",
    CLICKHOUSE_MIGRATION_URL: "clickhouse://127.0.0.1:9000",
    CLICKHOUSE_DB: "agenttrace_langfuse_local",
    CLICKHOUSE_USER: "agenttrace",
    CLICKHOUSE_PASSWORD: c.clickhousePassword,
    CLICKHOUSE_CLUSTER_ENABLED: "false",
    REDIS_CONNECTION_STRING: "redis://127.0.0.1:6379/13",
    REDIS_KEY_PREFIX: "agenttrace-native-langfuse",
    LANGFUSE_S3_EVENT_UPLOAD_BUCKET: "agenttrace-langfuse-events",
    LANGFUSE_S3_EVENT_UPLOAD_REGION: "us-east-1",
    LANGFUSE_S3_EVENT_UPLOAD_ENDPOINT: "http://127.0.0.1:9090",
    LANGFUSE_S3_EVENT_UPLOAD_ACCESS_KEY_ID: c.minioUser,
    LANGFUSE_S3_EVENT_UPLOAD_SECRET_ACCESS_KEY: c.minioPassword,
    LANGFUSE_S3_EVENT_UPLOAD_FORCE_PATH_STYLE: "true",
    LANGFUSE_S3_MEDIA_UPLOAD_BUCKET: "agenttrace-langfuse-media",
    LANGFUSE_S3_MEDIA_UPLOAD_REGION: "us-east-1",
    LANGFUSE_S3_MEDIA_UPLOAD_ENDPOINT: "http://127.0.0.1:9090",
    LANGFUSE_S3_MEDIA_UPLOAD_ACCESS_KEY_ID: c.minioUser,
    LANGFUSE_S3_MEDIA_UPLOAD_SECRET_ACCESS_KEY: c.minioPassword,
    LANGFUSE_S3_MEDIA_UPLOAD_FORCE_PATH_STYLE: "true",
    LANGFUSE_INIT_ORG_ID: "agenttrace-native-org",
    LANGFUSE_INIT_ORG_NAME: "AgentTrace Native Local",
    LANGFUSE_INIT_PROJECT_ID: "agenttrace-native-project",
    LANGFUSE_INIT_PROJECT_NAME: "AgentTrace Local Acceptance",
    LANGFUSE_INIT_PROJECT_PUBLIC_KEY: c.publicKey,
    LANGFUSE_INIT_PROJECT_SECRET_KEY: c.secretKey,
    LANGFUSE_INIT_USER_EMAIL: c.userEmail,
    LANGFUSE_INIT_USER_NAME: "AgentTrace Local",
    LANGFUSE_INIT_USER_PASSWORD: c.userPassword,
    LANGFUSE_CSP_ENFORCE_HTTPS: "false",
    LANGFUSE_TELEMETRY_ENABLED: "false",
    LANGFUSE_RATE_LIMITS_ENABLED: "false",
    // Empty values also block Next's local dotenv fallback; upstream treats them as undefined.
    NEXT_PUBLIC_LANGFUSE_CLOUD_REGION: "",
    SENTRY_AUTH_TOKEN: "",
    SENTRY_ORG: "",
    SENTRY_PROJECT: "",
    NEXT_TELEMETRY_DISABLED: "1",
    NEXT_DIST_DIR: process.env.LANGFUSE_NATIVE_NEXT_DIST_DIR || ".next",
    LANGFUSE_INGESTION_CLICKHOUSE_WRITE_INTERVAL_MS: "1000",
    MINIO_ROOT_USER: c.minioUser,
    MINIO_ROOT_PASSWORD: c.minioPassword,
    AGENTTRACE_CLICKHOUSE_DATA: join(root, "clickhouse-data") + "/",
    AGENTTRACE_CLICKHOUSE_TMP: join(root, "clickhouse-tmp") + "/",
    AGENTTRACE_CLICKHOUSE_USER_FILES: join(root, "clickhouse-user-files") + "/",
    AGENTTRACE_CLICKHOUSE_PASSWORD_SHA256: createHash("sha256").update(c.clickhousePassword).digest("hex"),
  };
}

function pidFile(name) { return join(root, `${name}.pid.json`); }
function processIdentity(pid) {
  const current = spawnSync("ps", ["-p", String(pid), "-o", "lstart=", "-o", "command="], {
    encoding: "utf8", env: { ...process.env, LC_ALL: "C" },
  });
  return current.status === 0 ? current.stdout.trim() : null;
}
function ownedProcess(name) {
  if (!existsSync(pidFile(name))) return null;
  const saved = JSON.parse(readFileSync(pidFile(name), "utf8"));
  return processIdentity(saved.pid) === saved.identity ? saved : null;
}

async function portFree(port) {
  return new Promise((done) => {
    const server = net.createServer();
    server.once("error", () => done(false));
    server.listen(port, "127.0.0.1", () => server.close(() => done(true)));
  });
}

async function start(name, command, args, c, cwd = root, extraPorts = []) {
  if (ownedProcess(name)) return ownedProcess(name).pid;
  for (const port of [ports[name], ...extraPorts]) {
    if (!(await portFree(port))) throw new Error(`Port ${port} occupied; no existing service was stopped.`);
  }
  const fd = openSync(join(root, `${name}.log`), "a", 0o600);
  const child = spawn(command, args, { cwd, env: env(c), detached: true, stdio: ["ignore", fd, fd] });
  closeSync(fd);
  await new Promise((done, reject) => { child.once("spawn", done); child.once("error", reject); });
  child.unref();
  await delay(250);
  const identity = processIdentity(child.pid);
  if (!identity) throw new Error(`${name} exited during startup; inspect ${root}/${name}.log`);
  writeFileSync(pidFile(name), JSON.stringify({ pid: child.pid, identity }), { mode: 0o600 });
  return child.pid;
}

function finalizeIdentity(name, pid) {
  const saved = JSON.parse(readFileSync(pidFile(name), "utf8"));
  const identity = processIdentity(pid);
  // Next.js changes its process title during startup; the start time must match.
  if (!identity || pid !== saved.pid || identity.slice(0, 24) !== saved.identity.slice(0, 24))
    throw new Error(`${name} process identity changed unexpectedly.`);
  writeFileSync(pidFile(name), JSON.stringify({ pid, identity }), { mode: 0o600 });
}

async function healthy(url, seconds = 90) {
  for (let i = 0; i < seconds; i++) {
    try {
      const response = await fetch(url, { signal: AbortSignal.timeout(3000) });
      if (response.ok) return;
    } catch {}
    await delay(1000);
  }
  throw new Error(`Health check timed out: ${url}; inspect ${root}/*.log`);
}

async function infrastructure(c) {
  await start("clickhouse", "clickhouse", ["server", `--config-file=${join(here, "langfuse.clickhouse.xml")}`], c, root, [9000]);
  await healthy("http://127.0.0.1:8123/ping");
  await start("minio", "minio", ["server", "--address", "127.0.0.1:9090", "--console-address", "127.0.0.1:9091", join(root, "minio-data")], c, root, [9091]);
  await healthy("http://127.0.0.1:9090/minio/health/live");
  const require = createRequire(join(source, "packages/shared/package.json"));
  const { S3Client, CreateBucketCommand, HeadBucketCommand } = require("@aws-sdk/client-s3");
  const s3 = new S3Client({
    region: "us-east-1", endpoint: "http://127.0.0.1:9090", forcePathStyle: true,
    credentials: { accessKeyId: c.minioUser, secretAccessKey: c.minioPassword },
  });
  try {
    for (const Bucket of ["agenttrace-langfuse-events", "agenttrace-langfuse-media"]) {
      try { await s3.send(new HeadBucketCommand({ Bucket })); }
      catch (error) {
        if (error.$metadata?.httpStatusCode !== 404) throw error;
        await s3.send(new CreateBucketCommand({ Bucket }));
      }
    }
  } finally { s3.destroy(); }
}

function exporterFile(c) {
  const values = {
    AGENTTRACE_LANGFUSE_ENABLED: "true",
    AGENTTRACE_LANGFUSE_BASE_URL: "http://127.0.0.1:3300",
    AGENTTRACE_LANGFUSE_PUBLIC_KEY: c.publicKey,
    AGENTTRACE_LANGFUSE_SECRET_KEY: c.secretKey,
    AGENTTRACE_LANGFUSE_PROJECT_ID: "agenttrace-native-project",
    LANGFUSE_HOST: "http://127.0.0.1:3300",
    LANGFUSE_PUBLIC_KEY: c.publicKey,
    LANGFUSE_SECRET_KEY: c.secretKey,
  };
  writeFileSync(join(root, "agenttrace.env"), Object.entries(values).map(([k, v]) => `${k}='${v}'`).join("\n") + "\n", { mode: 0o600 });
}

function provision(c) {
  const fd = openSync(join(root, "provision.log"), "a", 0o600);
  try {
    run(process.execPath, [join(here, "langfuse.provision.cjs")], {
      cwd: root, env: { ...env(c), AGENTTRACE_NATIVE_LANGFUSE_SOURCE: source },
      stdio: ["ignore", fd, fd],
    });
  } finally { closeSync(fd); }
}

async function setup() {
  if (run("git", ["rev-parse", "HEAD"], { cwd: source }) !== revision) {
    throw new Error(`Expected Langfuse revision ${revision}; will not reset or modify the checkout.`);
  }
  mkdirSync(root, { recursive: true, mode: 0o700 });
  for (const directory of ["clickhouse-data", "clickhouse-tmp", "clickhouse-user-files", "minio-data"])
    mkdirSync(join(root, directory), { recursive: true, mode: 0o700 });
  if (!existsSync(configPath)) {
    const c = {
      revision,
      databaseUrl: process.env.LANGFUSE_NATIVE_DATABASE_URL || `postgresql://${process.env.USER}@127.0.0.1:5432/agenttrace_langfuse_local`,
      authSecret: secret(), salt: secret(), encryptionKey: secret(),
      publicKey: "pk-lf-" + randomUUID(), secretKey: "sk-lf-" + randomUUID(),
      minioUser: "agenttrace-native", minioPassword: secret(),
      userEmail: "agenttrace-local@example.test", userPassword: secret(),
    };
    writeFileSync(configPath, JSON.stringify(c, null, 2) + "\n", { mode: 0o600 });
  }
  const c = config();
  exporterFile(c);
  const db = new URL(c.databaseUrl);
  if (db.pathname !== "/agenttrace_langfuse_local" || db.hostname !== "127.0.0.1" || db.port !== "5432")
    throw new Error("Database URL must target agenttrace_langfuse_local on native 127.0.0.1:5432.");
  const postgres = new URL(c.databaseUrl);
  postgres.pathname = "/postgres";
  const present = run("psql", ["-d", postgres.href, "-Atc", "SELECT 1 FROM pg_database WHERE datname='agenttrace_langfuse_local'"]);
  if (present !== "1") run("psql", ["-d", postgres.href, "-c", 'CREATE DATABASE agenttrace_langfuse_local']);
  run("redis-cli", ["-p", "6379", "ping"]);
  await infrastructure(c);
  const shared = join(source, "packages/shared");
  const fd = openSync(join(root, "migrations.log"), "a", 0o600);
  try {
    run(process.execPath, [join(shared, "node_modules/prisma/build/index.js"), "migrate", "deploy", "--schema", join(shared, "prisma/schema.prisma")], {
      cwd: root, env: env(c), stdio: ["ignore", fd, fd],
    });
    const response = await fetch("http://127.0.0.1:8123", {
      method: "POST", body: "CREATE DATABASE IF NOT EXISTS agenttrace_langfuse_local",
    });
    if (!response.ok) throw new Error("ClickHouse database creation failed.");
    // Invoke the supported migration engine directly; upstream up.sh sources its old .env.
    run("migrate", [
      "-source", `file://${join(shared, "clickhouse/migrations/unclustered")}`,
      "-database", "clickhouse://127.0.0.1:9000?username=default&database=agenttrace_langfuse_local&x-multi-statement=true&x-migrations-table-engine=MergeTree",
      "up",
    ], { cwd: root, env: env(c), stdio: ["ignore", fd, fd] });
  } finally { closeSync(fd); }
  provision(c);
  console.log(`Native dependencies and migrations ready. Private exporter environment: ${join(root, "agenttrace.env")}`);
}

function requireNode24() {
  if (process.versions.node.split(".")[0] !== "24")
    throw new Error("Node 24 is required for native Langfuse build/up.");
}

function build() {
  requireNode24();
  const dist = process.env.LANGFUSE_NATIVE_NEXT_DIST_DIR;
  if (!dist) throw new Error("Build requires explicit LANGFUSE_NATIVE_NEXT_DIST_DIR.");
  // A single web-local basename excludes traversal and symlinked parent directories.
  if (!/^[A-Za-z0-9_-][A-Za-z0-9._-]*$|^\.[A-Za-z0-9_-][A-Za-z0-9._-]*$/.test(dist) || dist === ".next")
    throw new Error("Build requires a fresh web-local directory name other than .next.");
  const web = join(source, "web");
  const target = join(web, dist);
  if (lstatSync(target, { throwIfNoEntry: false }))
    throw new Error("Build target already exists; select a fresh directory.");
  const nativeEnv = {
    ...env(config()),
    PATH: `${dirname(process.execPath)}:${process.env.PATH || ""}`,
    INLINE_RUNTIME_CHUNK: "false",
  };
  const fd = openSync(join(root, "build.log"), "a", 0o600);
  try {
    fchmodSync(fd, 0o600);
    // Capture version stdout, but keep diagnostic stderr private like build/scan output.
    const version = run("pnpm", ["--version"], {
      cwd: source, env: nativeEnv, stdio: ["ignore", "pipe", fd],
    });
    if (version !== "11.10.0") throw new Error("Installed upstream pnpm 11.10.0 is required.");
    // Atomically claim the fresh target, including races during the version check.
    try { mkdirSync(target, { mode: 0o700 }); }
    catch { throw new Error("Cannot reserve fresh build directory; target may already exist."); }
    run("pnpm", ["exec", "next", "build"], {
      cwd: web, env: nativeEnv, stdio: ["ignore", fd, fd],
    });
    run(process.execPath, [join(source, "scripts/scan-client-bundle.mjs"), join(target, "static")], {
      cwd: source, env: nativeEnv, stdio: ["ignore", fd, fd],
    });
  } finally { closeSync(fd); }
  console.log(`Native production build and client scan succeeded: web/${dist}.`);
}

async function up() {
  requireNode24();
  const c = config();
  await infrastructure(c);
  provision(c);
  const web = join(source, "web");
  const dist = env(c).NEXT_DIST_DIR;
  if (!existsSync(resolve(web, dist, "BUILD_ID"))) throw new Error("Selected Langfuse production build required; check LANGFUSE_NATIVE_NEXT_DIST_DIR.");
  const workerPid = await start("worker", process.execPath, ["--import", join(source, "worker/node_modules/tsx/dist/loader.mjs"), join(source, "worker/src/index.ts")], c, join(source, "worker"));
  await healthy("http://127.0.0.1:3330/api/health");
  finalizeIdentity("worker", workerPid);
  const webPid = await start("web", process.execPath, [join(web, "node_modules/next/dist/bin/next"), "start", "-p", "3300", "-H", "127.0.0.1"], c, web);
  await healthy("http://127.0.0.1:3300/api/public/health");
  finalizeIdentity("web", webPid);
  console.log("Real native Langfuse ready: http://localhost:3300 (worker 3330).");
}

async function verify() {
  const c = config();
  for (const url of [
    "http://127.0.0.1:8123/ping",
    "http://127.0.0.1:9090/minio/health/live",
    "http://127.0.0.1:3330/api/health",
    "http://127.0.0.1:3300/api/public/health",
  ]) await healthy(url, 5);
  const cookies = new Map();
  const collectCookies = (response) => {
    for (const value of response.headers.getSetCookie()) {
      const pair = value.split(";")[0];
      const separator = pair.indexOf("=");
      cookies.set(pair.slice(0, separator), pair.slice(separator + 1));
    }
  };
  const cookieHeader = () => [...cookies].map(([k, v]) => `${k}=${v}`).join("; ");
  const csrf = await fetch("http://127.0.0.1:3300/api/auth/csrf");
  collectCookies(csrf);
  const { csrfToken } = await csrf.json();
  const login = await fetch("http://127.0.0.1:3300/api/auth/callback/credentials", {
    method: "POST", redirect: "manual",
    headers: { Cookie: cookieHeader(), "Content-Type": "application/x-www-form-urlencoded" },
    body: new URLSearchParams({ csrfToken, email: c.userEmail, password: c.userPassword, json: "true", callbackUrl: "http://localhost:3300" }),
  });
  collectCookies(login);
  const sessionResponse = await fetch("http://127.0.0.1:3300/api/auth/session", { headers: { Cookie: cookieHeader() } });
  const session = await sessionResponse.json();
  if (session.user?.email !== c.userEmail) throw new Error("Real Langfuse owner login failed.");
  const headers = { Authorization: "Basic " + Buffer.from(`${c.publicKey}:${c.secretKey}`).toString("base64"), "Content-Type": "application/json" };
  const traceId = randomUUID();
  const observationId = randomUUID();
  const timestamp = new Date().toISOString();
  const response = await fetch("http://127.0.0.1:3300/api/public/ingestion", {
    method: "POST", headers,
    body: JSON.stringify({ batch: [
      { id: randomUUID(), timestamp, type: "trace-create", body: { id: traceId, timestamp, name: "native-local-acceptance", input: { native: true }, output: { accepted: true }, metadata: { runtime: "macos-native", containers: false } } },
      { id: randomUUID(), timestamp, type: "span-create", body: { id: observationId, traceId, name: "native-ingestion-span", startTime: timestamp, endTime: timestamp, input: { check: "real-s3-redis-clickhouse" }, output: { persisted: true } } },
    ] }),
  });
  const accepted = await response.json();
  if (response.status !== 207 || accepted.errors?.length || accepted.successes?.length !== 2)
    throw new Error(`Real ingestion rejected (${response.status}).`);
  let trace;
  for (let i = 0; i < 90; i++) {
    const read = await fetch(`http://127.0.0.1:3300/api/public/traces/${traceId}`, { headers });
    if (read.ok) {
      trace = await read.json();
      if (trace.id === traceId && trace.observations?.some((item) => item.id === observationId)) break;
    }
    await delay(1000);
  }
  if (trace?.id !== traceId || !trace.observations?.some((item) => item.id === observationId))
    throw new Error("Ingestion accepted but trace/span did not persist within 90 seconds.");
  const require = createRequire(join(source, "packages/shared/package.json"));
  const { S3Client, ListObjectsV2Command } = require("@aws-sdk/client-s3");
  const s3 = new S3Client({ region: "us-east-1", endpoint: "http://127.0.0.1:9090", forcePathStyle: true, credentials: { accessKeyId: c.minioUser, secretAccessKey: c.minioPassword } });
  let objects;
  try { objects = await s3.send(new ListObjectsV2Command({ Bucket: "agenttrace-langfuse-events", MaxKeys: 10 })); }
  finally { s3.destroy(); }
  if (!objects.KeyCount) throw new Error("No real S3 ingestion objects found.");
  const evidence = { verifiedAt: new Date().toISOString(), revision, baseUrl: "http://127.0.0.1:3300", projectId: "agenttrace-native-project", ownerLogin: true, traceId, observationId, ingestionStatus: response.status, ingestionSuccesses: accepted.successes.length, observationCount: trace.observations.length, s3Objects: objects.KeyCount, runtime: "native", traceUrl: `http://localhost:3300/project/agenttrace-native-project/traces/${traceId}` };
  writeFileSync(join(root, "verification.json"), JSON.stringify(evidence, null, 2) + "\n", { mode: 0o600 });
  console.log(JSON.stringify(evidence, null, 2));
}

async function down() {
  // Only stop processes whose PID, start time, and command match our saved identity.
  for (const name of [...names].reverse()) {
    const saved = ownedProcess(name);
    if (!saved) continue;
    process.kill(-saved.pid, "SIGTERM");
    for (let i = 0; i < 30 && ownedProcess(name); i++) await delay(1000);
    if (ownedProcess(name)) throw new Error(`${name} is still stopping; no forced kill used.`);
    console.log(`Stopped owned ${name} process.`);
  }
}

async function status() {
  for (const name of names) console.log(`${name}: ${ownedProcess(name) ? "running (owned)" : "not running (owned)"}; port ${ports[name]}`);
  console.log(`Runtime directory: ${root}`);
}

try {
  const action = process.argv[2];
  const actions = { setup, build, up, down, status, verify };
  if (!actions[action]) throw new Error("Usage: bash integration/local/langfuse.sh setup|build|up|verify|status|down");
  await actions[action]();
} catch (error) {
  console.error(error.message);
  process.exitCode = 1;
}
